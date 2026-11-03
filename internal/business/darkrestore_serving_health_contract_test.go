//go:build linux

package business

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	mx "github.com/darktunnelmika/dark-xray-pro/internal/managedxray"
)

func servingHealthFixture(t *testing.T) (DarkRestoreServingHealthEvidenceV1, DarkRestoreServingHealthPinsV1, []byte) {
	t.Helper()
	key := bytes.Repeat([]byte{0x5a}, 64)
	listenerSHA := strings.Repeat("7", 64)
	q, err := mx.SignProtocolHealthRequest(key, mx.ProtocolHealthRequest{
		Version: mx.InitialApplyProtocolHealthVersion, ID: strings.Repeat("1", 32),
		NodeID: "darkrestore-health-node", InstallationID: strings.Repeat("2", 32),
		RuntimeInstance: strings.Repeat("3", 32), StageID: strings.Repeat("4", 32),
		ApplyCommandID: strings.Repeat("5", 32), InboundID: 503, Port: 24503,
		CredentialSHA: strings.Repeat("6", 64), SystemProbeID: "darkrestore-system-probe",
		EndpointSHA: strings.Repeat("8", 64), InitialApplySHA: listenerSHA,
		Expected: mx.RuntimeCondition{ProcessReady: true, ActiveSequence: 1, ActiveSHA: strings.Repeat("9", 64)},
		Usage:    mx.MeteredCondition{EpochID: strings.Repeat("a", 32), EpochSHA: strings.Repeat("b", 64), AdmissionSHA: strings.Repeat("c", 64)},
		Issued:   200, Expires: 1000,
	})
	if err != nil {
		t.Fatal(err)
	}
	requestRaw, _ := json.Marshal(q)
	body := sha256.Sum256([]byte("DARK-PRO-PROTOCOL-HEALTH-v1\n" + q.ID + "\n"))
	receipt := mx.ProtocolHealthReceipt{
		Version: q.Version, RequestSHA: admissionBytesSHA(requestRaw),
		ResponseSHA: hex.EncodeToString(body[:]), Completed: 300,
	}
	unsigned, _ := json.Marshal(receipt)
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte("DARK-PRO-PROTOCOL-HEALTH-RECEIPT-v4\x00"))
	mac.Write(unsigned)
	receipt.Signature = hex.EncodeToString(mac.Sum(nil))
	receiptRaw, _ := json.Marshal(receipt)
	e := DarkRestoreServingHealthEvidenceV1{
		Scope:    DarkRestoreServingHealthEvidenceScopeV1,
		ClientID: "m5-health-client", TransitionSHA: strings.Repeat("d", 64),
		PlanSHA: strings.Repeat("e", 64), ConfigSHA: strings.Repeat("9", 64),
		EpochID: strings.Repeat("a", 32), AdmissionEvidenceSHA: strings.Repeat("f", 64),
		ListenerApplyReceiptSHA: listenerSHA, ListenerAppliedUnixNano: 100,
		EndpointSHA: strings.Repeat("8", 64), Request: q, Receipt: receipt,
		RecordedUnixNano: 400,
	}
	p := DarkRestoreServingHealthPinsV1{
		ClientID: e.ClientID, TransitionSHA: e.TransitionSHA, PlanSHA: e.PlanSHA,
		ConfigSHA: e.ConfigSHA, EpochID: e.EpochID,
		AdmissionEvidenceSHA:    e.AdmissionEvidenceSHA,
		ListenerApplyReceiptSHA: e.ListenerApplyReceiptSHA, EndpointSHA: e.EndpointSHA,
		RequestSHA: admissionBytesSHA(requestRaw), ReceiptSHA: admissionBytesSHA(receiptRaw),
	}
	p.DocumentSHA, _ = cutoverSnapshotDigest("DARK-PRO-SERVING-HEALTH-EVIDENCE-v1", e)
	return e, p, key
}

func TestDarkRestoreServingHealthEvidenceV1(t *testing.T) {
	e, p, key := servingHealthFixture(t)
	got, err := ValidateDarkRestoreServingHealthEvidenceV1(e, p, key, 500)
	if err != nil || got.Scope != DarkRestoreServingHealthEvidenceScopeV1 ||
		got.EvidenceSHA != p.DocumentSHA || got.RequestSHA != p.RequestSHA ||
		got.ReceiptSHA != p.ReceiptSHA || !got.ServingHealthRecorded ||
		got.NativeApplyRecorded || got.AuthorizesMutation {
		t.Fatalf("valid evidence: %+v %v", got, err)
	}
	cases := []struct {
		name string
		edit func(*DarkRestoreServingHealthEvidenceV1, *DarkRestoreServingHealthPinsV1, *[]byte)
		now  int64
	}{
		{"foreign transition", func(e *DarkRestoreServingHealthEvidenceV1, _ *DarkRestoreServingHealthPinsV1, _ *[]byte) {
			e.TransitionSHA = strings.Repeat("0", 64)
		}, 500},
		{"foreign listener", func(e *DarkRestoreServingHealthEvidenceV1, _ *DarkRestoreServingHealthPinsV1, _ *[]byte) {
			e.ListenerApplyReceiptSHA = strings.Repeat("0", 64)
		}, 500},
		{"foreign config", func(e *DarkRestoreServingHealthEvidenceV1, _ *DarkRestoreServingHealthPinsV1, _ *[]byte) {
			e.ConfigSHA = strings.Repeat("0", 64)
		}, 500},
		{"foreign epoch", func(e *DarkRestoreServingHealthEvidenceV1, _ *DarkRestoreServingHealthPinsV1, _ *[]byte) {
			e.EpochID = strings.Repeat("0", 32)
		}, 500},
		{"foreign endpoint", func(e *DarkRestoreServingHealthEvidenceV1, _ *DarkRestoreServingHealthPinsV1, _ *[]byte) {
			e.EndpointSHA = strings.Repeat("0", 64)
		}, 500},
		{"request before listener", func(e *DarkRestoreServingHealthEvidenceV1, _ *DarkRestoreServingHealthPinsV1, _ *[]byte) {
			e.ListenerAppliedUnixNano = e.Request.Issued
		}, 500},
		{"recorded before probe", func(e *DarkRestoreServingHealthEvidenceV1, _ *DarkRestoreServingHealthPinsV1, _ *[]byte) {
			e.RecordedUnixNano = e.Receipt.Completed - 1
		}, 500},
		{"recorded in future", func(e *DarkRestoreServingHealthEvidenceV1, _ *DarkRestoreServingHealthPinsV1, _ *[]byte) {
			e.RecordedUnixNano = 501
		}, 500},
		{"native apply invented", func(e *DarkRestoreServingHealthEvidenceV1, _ *DarkRestoreServingHealthPinsV1, _ *[]byte) {
			e.NativeApplyRecorded = true
		}, 500},
		{"mutation invented", func(e *DarkRestoreServingHealthEvidenceV1, _ *DarkRestoreServingHealthPinsV1, _ *[]byte) {
			e.AuthorizesMutation = true
		}, 500},
		{"wrong key", func(_ *DarkRestoreServingHealthEvidenceV1, _ *DarkRestoreServingHealthPinsV1, key *[]byte) {
			*key = bytes.Repeat([]byte{0x6b}, 64)
		}, 500},
		{"foreign request pin", func(_ *DarkRestoreServingHealthEvidenceV1, p *DarkRestoreServingHealthPinsV1, _ *[]byte) {
			p.RequestSHA = strings.Repeat("0", 64)
		}, 500},
		{"foreign receipt pin", func(_ *DarkRestoreServingHealthEvidenceV1, p *DarkRestoreServingHealthPinsV1, _ *[]byte) {
			p.ReceiptSHA = strings.Repeat("0", 64)
		}, 500},
		{"future document clock", func(_ *DarkRestoreServingHealthEvidenceV1, _ *DarkRestoreServingHealthPinsV1, _ *[]byte) {}, 399},
		{"repinned document only", func(e *DarkRestoreServingHealthEvidenceV1, p *DarkRestoreServingHealthPinsV1, _ *[]byte) {
			e.TransitionSHA = strings.Repeat("0", 64)
			p.DocumentSHA, _ = cutoverSnapshotDigest("DARK-PRO-SERVING-HEALTH-EVIDENCE-v1", *e)
		}, 500},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			bad, pins, useKey := e, p, append([]byte(nil), key...)
			tc.edit(&bad, &pins, &useKey)
			if got, err := ValidateDarkRestoreServingHealthEvidenceV1(bad, pins, useKey, tc.now); !errors.Is(err, ErrDarkRestoreCutover) || got != (DarkRestoreServingHealthValidationV1{}) {
				t.Fatalf("accepted: %+v %v", got, err)
			}
		})
	}
}
