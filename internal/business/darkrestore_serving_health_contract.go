//go:build linux

package business

import (
	"bytes"
	"encoding/json"
	"time"

	mx "github.com/darktunnelmika/dark-xray-pro/internal/managedxray"
)

const DarkRestoreServingHealthEvidenceScopeV1 = "darkrestore_serving_health_evidence_v1"

// DarkRestoreServingHealthEvidenceV1 is historical evidence for one exact,
// externally challenged protocol probe after the signed listener receipt. It
// does not prove that undeclared serving installations do not exist and never
// authorizes activation, publication, credit, assignment or native mutation.
type DarkRestoreServingHealthEvidenceV1 struct {
	Scope                   string                   `json:"scope"`
	ClientID                string                   `json:"client_id"`
	TransitionSHA           string                   `json:"transition_sha256"`
	PlanSHA                 string                   `json:"plan_sha256"`
	ConfigSHA               string                   `json:"config_sha256"`
	EpochID                 string                   `json:"epoch_id"`
	AdmissionEvidenceSHA    string                   `json:"admission_evidence_sha256"`
	ListenerApplyReceiptSHA string                   `json:"listener_apply_receipt_sha256"`
	ListenerAppliedUnixNano int64                    `json:"listener_applied_unix_nano"`
	EndpointSHA             string                   `json:"endpoint_sha256"`
	Request                 mx.ProtocolHealthRequest `json:"request"`
	Receipt                 mx.ProtocolHealthReceipt `json:"receipt"`
	RecordedUnixNano        int64                    `json:"recorded_unix_nano"`
	NativeApplyRecorded     bool                     `json:"native_apply_recorded"`
	AuthorizesMutation      bool                     `json:"authorizes_mutation"`
}

type DarkRestoreServingHealthPinsV1 struct {
	ClientID                string
	TransitionSHA           string
	PlanSHA                 string
	ConfigSHA               string
	EpochID                 string
	AdmissionEvidenceSHA    string
	ListenerApplyReceiptSHA string
	EndpointSHA             string
	RequestSHA              string
	ReceiptSHA              string
	DocumentSHA             string
}

type DarkRestoreServingHealthValidationV1 struct {
	Scope                 string `json:"scope"`
	EvidenceSHA           string `json:"evidence_sha256"`
	RequestSHA            string `json:"request_sha256"`
	ReceiptSHA            string `json:"receipt_sha256"`
	ServingHealthRecorded bool   `json:"serving_health_recorded"`
	NativeApplyRecorded   bool   `json:"native_apply_recorded"`
	AuthorizesMutation    bool   `json:"authorizes_mutation"`
}

// ValidateDarkRestoreServingHealthEvidenceV1 validates a complete immutable
// document against independent exact pins. The HMAC proves possession of the
// paired Agent key; operational truth still depends on the reviewed real-probe
// emitter. Historical validation is performed at the receipt completion time,
// never by extending or replaying the expired challenge.
func ValidateDarkRestoreServingHealthEvidenceV1(e DarkRestoreServingHealthEvidenceV1, p DarkRestoreServingHealthPinsV1, agentKey []byte, nowUnixNano int64) (DarkRestoreServingHealthValidationV1, error) {
	var zero DarkRestoreServingHealthValidationV1
	q, r := e.Request, e.Receipt
	qRaw, qErr := json.Marshal(q)
	rRaw, rErr := json.Marshal(r)
	qSHA, rSHA := admissionBytesSHA(qRaw), admissionBytesSHA(rRaw)
	if len(agentKey) < 32 || qErr != nil || rErr != nil ||
		e.Scope != DarkRestoreServingHealthEvidenceScopeV1 || !idRE.MatchString(e.ClientID) ||
		!routeSHA.MatchString(e.TransitionSHA) || !routeSHA.MatchString(e.PlanSHA) ||
		!routeSHA.MatchString(e.ConfigSHA) || !routeID.MatchString(e.EpochID) ||
		!routeSHA.MatchString(e.AdmissionEvidenceSHA) || !routeSHA.MatchString(e.ListenerApplyReceiptSHA) ||
		e.ListenerAppliedUnixNano <= 0 || !routeSHA.MatchString(e.EndpointSHA) ||
		e.RecordedUnixNano < r.Completed || e.RecordedUnixNano > nowUnixNano ||
		e.NativeApplyRecorded || e.AuthorizesMutation ||
		q.Version != mx.InitialApplyProtocolHealthVersion || q.HandoverID != "" ||
		q.InitialApplySHA != e.ListenerApplyReceiptSHA || q.NodeID == "" ||
		!routeID.MatchString(q.InstallationID) || !routeID.MatchString(q.RuntimeInstance) ||
		!routeID.MatchString(q.StageID) || !routeID.MatchString(q.ApplyCommandID) ||
		q.EndpointSHA != e.EndpointSHA || q.Expected.ActiveSHA != e.ConfigSHA ||
		!q.Expected.ProcessReady || q.Expected.ActiveSequence == 0 ||
		q.Usage.EpochID != e.EpochID || q.Issued <= e.ListenerAppliedUnixNano ||
		r.Completed < q.Issued || r.Completed >= q.Expires ||
		!mx.VerifyProtocolHealthReceipt(agentKey, q, r, time.Unix(0, r.Completed)) ||
		p.ClientID != e.ClientID || p.TransitionSHA != e.TransitionSHA ||
		p.PlanSHA != e.PlanSHA || p.ConfigSHA != e.ConfigSHA || p.EpochID != e.EpochID ||
		p.AdmissionEvidenceSHA != e.AdmissionEvidenceSHA ||
		p.ListenerApplyReceiptSHA != e.ListenerApplyReceiptSHA ||
		p.EndpointSHA != e.EndpointSHA || p.RequestSHA != qSHA || p.ReceiptSHA != rSHA ||
		!routeSHA.MatchString(p.DocumentSHA) {
		return zero, ErrDarkRestoreCutover
	}
	canonicalQ, _ := json.Marshal(q)
	canonicalR, _ := json.Marshal(r)
	if !bytes.Equal(canonicalQ, qRaw) || !bytes.Equal(canonicalR, rRaw) {
		return zero, ErrDarkRestoreCutover
	}
	docSHA, err := cutoverSnapshotDigest("DARK-PRO-SERVING-HEALTH-EVIDENCE-v1", e)
	if err != nil || docSHA != p.DocumentSHA {
		return zero, ErrDarkRestoreCutover
	}
	return DarkRestoreServingHealthValidationV1{
		Scope: DarkRestoreServingHealthEvidenceScopeV1, EvidenceSHA: docSHA,
		RequestSHA: qSHA, ReceiptSHA: rSHA, ServingHealthRecorded: true,
		NativeApplyRecorded: false, AuthorizesMutation: false,
	}, nil
}
