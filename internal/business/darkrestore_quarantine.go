package business

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"

	ix "github.com/darktunnelmika/dark-xray-pro/internal/inboundexec"
	mx "github.com/darktunnelmika/dark-xray-pro/internal/managedxray"
	"github.com/darktunnelmika/dark-xray-pro/internal/subscriptioncodec"
	uc "github.com/darktunnelmika/dark-xray-pro/internal/usagecheckpoint"
	"github.com/darktunnelmika/dark-xray-pro/internal/usageledger"
	"github.com/darktunnelmika/dark-xray-pro/internal/xrayconfig"
)

// Local quarantine staging is separate from the native command queue. Its
// private candidate can only be consumed by the offline checker below.
type DarkRestoreQuarantinePlan struct {
	ClientID                 string
	NodeID                   string
	InstallationID           string
	TransitionSHA            string
	StateSHA                 string
	Prepared                 DarkRestorePreparedBindingV1
	RemainingBytes           int64
	ConfigSHA                string
	ManifestSHA              string
	SystemProbeID            string `json:"SystemProbeID,omitempty"`
	SystemProbeCredentialSHA string `json:"SystemProbeCredentialSHA,omitempty"`
	PreparedAdmissionSHA     string `json:"PreparedAdmissionSHA,omitempty"`
	candidate                xrayconfig.Candidate
}

func (p DarkRestoreQuarantinePlan) Digest() (string, error) {
	return cutoverSnapshotDigest("DARK-PRO-QUARANTINE-PLAN-v1", p)
}

func (s *Store) DarkRestoreQuarantinePlan(ctx context.Context, key []byte, token, client string) (DarkRestoreQuarantinePlan, error) {
	var zero DarkRestoreQuarantinePlan
	tx, err := executionTx(s, ctx, key)
	if err != nil {
		return zero, err
	}
	defer tx.Rollback()
	actor, err := ownershipOwner(ctx, tx, token)
	if err != nil {
		return zero, err
	}
	return quarantinePlanTx(ctx, tx, key, actor, client)
}

func quarantinePlanTx(ctx context.Context, tx *sql.Tx, key []byte, actor, client string) (DarkRestoreQuarantinePlan, error) {
	var zero DarkRestoreQuarantinePlan
	receipt, err := readCutoverReceipt(ctx, tx, client)
	if err != nil || receipt == nil || receipt.ActorUserID != actor {
		return zero, ErrDarkRestoreCutover
	}
	if err = verifyPreparedPlanReceipt(ctx, tx, receipt); err != nil {
		return zero, err
	}
	evidence, err := readCutoverEvidence(ctx, tx, client)
	if err != nil || evidence == nil || receipt.TransitionSHA != evidence.TransitionSHA || receipt.EvidenceApprovalSHA256 != evidence.CoverageApprovalSHA256 || receipt.EvidenceInventorySHA256 != evidence.InventorySHA256 {
		return zero, ErrDarkRestoreCutover
	}
	snap, remaining, err := cutoverSnapshotMatchesEvidence(ctx, tx, evidence)
	if err != nil || remaining != receipt.RemainingBudgetBytes || receipt.CurrentFiniteEntitlement != snap.CurrentEntitlement || receipt.SettledHistoricalBytes != evidence.SettledHistoricalBytes {
		return zero, ErrDarkRestoreCutover
	}
	b := receipt.Prepared
	plan, err := readNativeInboundPlan(ctx, tx, key, b.InboundID)
	if err != nil {
		return zero, err
	}
	if plan.Profile.Spec != nil {
		return zero, ErrDarkRestoreCutover
	}
	c, err := readClient(ctx, tx, client)
	if err != nil {
		return zero, err
	}
	original, err := renderPreparedLoopbackReality(plan, c, b.Format)
	if err != nil || original.SHA256 != b.ContentSHA {
		return zero, ErrDarkRestoreCutover
	}
	_, profile, err := editablePlanProfile(ctx, tx, key, plan)
	if err != nil {
		return zero, err
	}
	email, err := uc.CounterEmail(client)
	if err != nil {
		return zero, err
	}
	profile.Users = []xrayconfig.User{{UUID: c.UUID, Email: email}}
	node := plan.Nodes[0]
	probeID, probeCredentialSHA := "", ""
	probe, probeErr := usageledger.ProbeIdentityTx(ctx, tx, node.NodeID, node.InstallationID, b.InboundID)
	if probeErr == nil {
		probeEmail, e := uc.CounterEmail(probe.ID)
		if e != nil {
			return zero, e
		}
		profile.Users = append(profile.Users, xrayconfig.User{UUID: probe.UUID, Email: probeEmail})
		h := sha256.Sum256([]byte(probe.UUID))
		probeID, probeCredentialSHA = probe.ID, hex.EncodeToString(h[:])
	} else if !errors.Is(probeErr, usageledger.ErrMissing) {
		return zero, probeErr
	}
	candidate, err := xrayconfig.RenderAssigned(node.NodeID, node.InstallationID, node.ListenIP, []xrayconfig.Assignment{{Profile: profile, DirectPort: node.DirectPort}})
	if err != nil {
		return zero, err
	}
	routes, err := xrayconfig.LabSubscriptionRoutes(candidate, c.UUID, map[uint64]string{b.InboundID: fmt.Sprintf("LAB direct %d", b.InboundID)})
	if err != nil {
		return zero, err
	}
	raw, err := subscriptioncodec.Render(routes, subscriptioncodec.FormatRaw)
	if err != nil || ix.BytesSHA(raw) != b.ContentSHA {
		return zero, ErrDarkRestoreCutover
	}
	candidate, err = xrayconfig.WithUsageAPI(candidate, mx.MeteredStatsEndpoint)
	if err != nil {
		return zero, err
	}
	admissionSHA := ""
	if probeID != "" {
		admissionSHA, err = mx.PreparedUsageAdmissionSHA(candidate, true)
		if err != nil {
			return zero, err
		}
	}
	return DarkRestoreQuarantinePlan{ClientID: client, NodeID: node.NodeID, InstallationID: node.InstallationID, TransitionSHA: receipt.TransitionSHA, StateSHA: snap.StateSHA, Prepared: b, RemainingBytes: remaining, ConfigSHA: candidate.SHA256, ManifestSHA: candidate.ManifestSHA256, SystemProbeID: probeID, SystemProbeCredentialSHA: probeCredentialSHA, PreparedAdmissionSHA: admissionSHA, candidate: candidate}, nil
}

// Check the frozen candidate against independently selected node, installation,
// plan and core pins. Only Xray's offline test mode is used.
func (p DarkRestoreQuarantinePlan) ValidateOffline(ctx context.Context, node, installation, planSHA, binary, binarySHA string) (xrayconfig.Validation, error) {
	digest, err := p.Digest()
	if err != nil || digest != planSHA || node != p.NodeID || installation != p.InstallationID || p.ConfigSHA != p.candidate.SHA256 || p.ManifestSHA != p.candidate.ManifestSHA256 {
		return xrayconfig.Validation{}, ErrDarkRestoreCutover
	}
	return xrayconfig.ValidateOffline(ctx, p.candidate, binary, binarySHA)
}

// ReviewDarkRestoreQuarantineRegistration rederives the current owner-approved
// plan before reviewing a node's durable offline registration. The independent
// signer and binary pins are trusted local operator inputs.
func (s *Store) ReviewDarkRestoreQuarantineRegistration(ctx context.Context, key []byte, token, client string, pub ed25519.PublicKey, coreSHA string, receipt DarkRestoreQuarantineRegistration) (DarkRestoreQuarantinePlan, error) {
	var zero DarkRestoreQuarantinePlan
	plan, err := s.DarkRestoreQuarantinePlan(ctx, key, token, client)
	if err != nil {
		return zero, err
	}
	sha, err := plan.Digest()
	if err != nil || !receipt.Verify(pub, sha, coreSHA) {
		return zero, ErrDarkRestoreCutover
	}
	return plan, nil
}
