package business

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"reflect"
	"testing"
	"time"

	"github.com/darktunnelmika/dark-xray-pro/internal/xrayconfig"
)

func businessTLSCredentials(t *testing.T, before, after time.Time) *TLSCredentials {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 120))
	if err != nil {
		t.Fatal(err)
	}
	cert := &x509.Certificate{SerialNumber: serial, Subject: pkix.Name{CommonName: "TLS authoring fixture"},
		DNSNames: []string{"www.example.org", "other.example.org"}, NotBefore: before, NotAfter: after,
		BasicConstraintsValid: true, KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, err := x509.CreateCertificate(rand.Reader, cert, cert, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	private, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	return &TLSCredentials{CertificatePEM: string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})),
		PrivateKeyPEM: string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: private}))}
}

func tlsCreationPlanFixture(t *testing.T) (*Store, []byte, NativeInboundPlanInput) {
	t.Helper()
	s, key, in := creationPlanFixture(t)
	in.Profile.Security, in.Profile.Transport = "tls", "ws"
	in.Profile.RealityTarget, in.Profile.GRPCService = "", ""
	in.Profile.ShortIDs = []string{}
	in.Profile.ServerNames = []string{"www.example.org"}
	in.Profile.Options = &xrayconfig.ProfileOptions{WebSocket: &xrayconfig.WebSocketOptions{Path: "/socket", Host: "edge.example.org"}, Client: &xrayconfig.ClientOptions{Fingerprint: "firefox"}}
	in.TLSCredentials = businessTLSCredentials(t, time.Now().Add(-time.Hour), time.Now().Add(24*time.Hour))
	return s, key, in
}

func requirePublicTLSOnly(t *testing.T, value any) {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"-----BEGIN", "certificate_pem", "private_key_pem", "tls_credentials", "reality_private_key", `"config":`} {
		if bytes.Contains(raw, []byte(forbidden)) {
			t.Fatal("public TLS result contains private material or configuration", forbidden)
		}
	}
}

func requireTLSPlanError(t *testing.T, err error, field, code string, kind error) {
	t.Helper()
	var issue *NativeInboundPlanError
	if !errors.Is(err, kind) || !errors.As(err, &issue) || issue.Field != field || issue.Code != code {
		t.Fatalf("wrong TLS field/code: expected %s/%s, got %v", field, code, err)
	}
}

func TestCreationTLSWriteOnlyMaterialRetentionRenewalAndHistoricalReplay(t *testing.T) {
	s, key, in := tlsCreationPlanFixture(t)
	ctx := context.Background()
	generation := planGeneration(t, s)
	preview, err := s.PreviewNativeInboundPlan(ctx, key, in)
	if err != nil || !preview.Valid || preview.Plan.Profile.TLS == nil || preview.Plan.Profile.PublicKey != "" || preview.Plan.Profile.Enabled {
		t.Fatal("TLS preview rejected or exposed an active identity", err)
	}
	requirePublicTLSOnly(t, preview)
	for _, endpoint := range preview.Endpoints {
		if endpoint.Security != "tls" || endpoint.SpiderX != "" || endpoint.TLSSHA256 != preview.Plan.Profile.TLS.SHA256 || endpoint.TLSPublicKeySHA256 != preview.Plan.Profile.TLS.PublicKeySHA256 || endpoint.WebSocket == nil || endpoint.WebSocket.Path != "/socket" {
			t.Fatal("endpoint preview lost TLS/transport binding")
		}
	}
	created, err := s.SaveNativeInboundPlan(ctx, key, in)
	if err != nil || created.Plan.InboundRevision != 1 || created.Plan.PlanRevision != 1 || !reflect.DeepEqual(preview.Plan.Profile.TLS, created.Plan.Profile.TLS) {
		t.Fatal("TLS create changed certificate metadata", err)
	}
	requirePublicTLSOnly(t, created)
	public, original, err := readNativeInbound(ctx, s.db, key, in.Profile.ID)
	if err != nil || original.TLS == nil || original.TLS.PrivateKeyPEM != in.TLSCredentials.PrivateKeyPEM || original.RealityPrivateKey != "" || public.PublicKey != "" {
		t.Fatal("TLS identity was not stored privately", err)
	}
	requirePublicTLSOnly(t, public)
	for _, query := range []struct{ sql, prefix string }{
		{`SELECT profile_enc FROM native_inbounds WHERE id=501`, "DXPPRF5\x00"},
		{`SELECT payload_enc FROM native_inbound_plans WHERE inbound_id=501`, "DXPPLN3\x00"},
		{`SELECT receipt_enc FROM native_inbound_plan_events LIMIT 1`, "DXPPLN3\x00"},
	} {
		var encrypted []byte
		if err = s.db.QueryRow(query.sql).Scan(&encrypted); err != nil || !bytes.HasPrefix(encrypted, []byte(query.prefix)) || bytes.Contains(encrypted, []byte("-----BEGIN")) {
			t.Fatal("TLS storage format/plaintext boundary", err)
		}
	}
	retained := copiedPlan(in)
	retained.EventID = "tls-retained-material-edit-001"
	retained.ExpectedPlanRevision, retained.ExpectedInboundRevision = 1, 1
	retained.TLSCredentials = nil
	retained.Profile.Transport = "httpupgrade"
	retained.Profile.Options.WebSocket = &xrayconfig.WebSocketOptions{}
	retained.Profile.Options.HTTPUpgrade = &xrayconfig.HTTPUpgradeOptions{Path: "/upgrade", Host: "upgrade.example.org"}
	edited, err := s.SaveNativeInboundPlan(ctx, key, retained)
	if err != nil || edited.Plan.InboundRevision != 2 || !reflect.DeepEqual(edited.Plan.Profile.TLS, created.Plan.Profile.TLS) {
		t.Fatal("omitted credentials did not retain the saved identity", err)
	}
	_, unchanged, err := readNativeInbound(ctx, s.db, key, in.Profile.ID)
	if err != nil || !reflect.DeepEqual(original.TLS, unchanged.TLS) {
		t.Fatal("transport edit changed private TLS material", err)
	}
	renewal := copiedPlan(retained)
	renewal.EventID = "tls-explicit-renewal-001"
	renewal.ExpectedPlanRevision, renewal.ExpectedInboundRevision = 2, 2
	renewal.TLSCredentials = businessTLSCredentials(t, time.Now().Add(-time.Hour), time.Now().Add(48*time.Hour))
	renewed, err := s.SaveNativeInboundPlan(ctx, key, renewal)
	if err != nil || renewed.Plan.InboundRevision != 3 || renewed.Plan.Profile.TLS.SHA256 == created.Plan.Profile.TLS.SHA256 {
		t.Fatal("explicit replacement failed to advance identity revision", err)
	}
	for _, prior := range []struct {
		input NativeInboundPlanInput
		want  NativeInboundPlan
	}{{in, created.Plan}, {retained, edited.Plan}, {renewal, renewed.Plan}} {
		replayed, err := s.SaveNativeInboundPlan(ctx, key, prior.input)
		if err != nil || !replayed.Replayed || !reflect.DeepEqual(replayed.Plan, prior.want) {
			t.Fatal("historical TLS receipt was rebound to the latest certificate", err)
		}
		requirePublicTLSOnly(t, replayed)
	}
	reused := copiedPlan(retained)
	reused.TLSCredentials = renewal.TLSCredentials
	if _, err = s.SaveNativeInboundPlan(ctx, key, reused); !errors.Is(err, ErrConflict) {
		t.Fatal("retention event accepted a replacement pair", err)
	}
	identical := copiedPlan(renewal)
	identical.EventID = "tls-explicit-identical-pair-001"
	identical.ExpectedPlanRevision, identical.ExpectedInboundRevision = 3, 3
	again, err := s.SaveNativeInboundPlan(ctx, key, identical)
	if err != nil || again.Plan.InboundRevision != 4 || !reflect.DeepEqual(again.Plan.Profile.TLS, renewed.Plan.Profile.TLS) {
		t.Fatal("explicit replacement did not give deterministic revision semantics", err)
	}
	nodeEdit := copiedPlan(identical)
	nodeEdit.EventID = "tls-retain-node-only-edit-001"
	nodeEdit.ExpectedPlanRevision, nodeEdit.ExpectedInboundRevision = 4, 4
	nodeEdit.TLSCredentials = nil
	nodeEdit.Nodes[0].DirectHosts = []string{"updated.example.org"}
	last, err := s.SaveNativeInboundPlan(ctx, key, nodeEdit)
	if err != nil || last.Plan.InboundRevision != 4 || last.Plan.PlanRevision != 5 {
		t.Fatal("node-only edit rewrote TLS identity", err)
	}
	loaded, err := s.NativeInboundPlan(ctx, key, in.Profile.ID)
	if err != nil || !loaded.Editable || !reflect.DeepEqual(loaded, last.Plan) {
		t.Fatal("TLS plan readback mismatch", err)
	}
	listed, err := s.ListInbounds(ctx, key)
	if err != nil || len(listed) != 1 || !reflect.DeepEqual(listed[0].TLS, renewed.Plan.Profile.TLS) {
		t.Fatal("TLS listing lost public metadata", err)
	}
	requirePublicTLSOnly(t, listed)
	if planGeneration(t, s) != generation {
		t.Fatal("TLS authoring changed runtime generation")
	}
	for _, table := range []string{"native_inbound_targets", "native_inbound_clients", "native_runtime_delivery", "clients", "events"} {
		if planCount(t, s, table) != 0 {
			t.Fatal("TLS authoring mutated runtime or customer records", table)
		}
	}
}

func TestCreationTLSRejectsAmbiguousCredentialsSecurityChangesAndRuntimeBypasses(t *testing.T) {
	s, key, in := tlsCreationPlanFixture(t)
	ctx := context.Background()
	created, err := s.SaveNativeInboundPlan(ctx, key, in)
	if err != nil {
		t.Fatal(err)
	}
	edit := copiedPlan(in)
	edit.EventID, edit.ExpectedPlanRevision, edit.ExpectedInboundRevision = "tls-rejected-edit-001", 1, 1
	edit.TLSCredentials = nil
	for _, security := range []string{"", "reality"} {
		request := copiedPlan(edit)
		request.Profile.Security = security
		_, err := s.SaveNativeInboundPlan(ctx, key, request)
		requireTLSPlanError(t, err, "profile.security", "security_immutable", ErrConflict)
	}
	raw, _ := json.Marshal(edit)
	var body map[string]any
	_ = json.Unmarshal(raw, &body)
	for _, value := range []any{nil, map[string]any{}, map[string]any{"certificate_pem": in.TLSCredentials.CertificatePEM}, map[string]any{"private_key_pem": in.TLSCredentials.PrivateKeyPEM}} {
		body["tls_credentials"] = value
		wire, _ := json.Marshal(body)
		var request NativeInboundPlanInput
		if err := json.Unmarshal(wire, &request); err != nil {
			t.Fatal(err)
		}
		if _, err := s.SaveNativeInboundPlan(ctx, key, request); !errors.Is(err, ErrInvalid) {
			t.Fatal("null/partial credentials were retained", err)
		}
	}
	copy := copiedPlan(edit)
	copy.ExpectedPlanRevision, copy.ExpectedInboundRevision = 0, 0
	copy.Profile.ID++
	if _, err = s.SaveNativeInboundPlan(ctx, key, copy); !errors.Is(err, ErrInvalid) {
		t.Fatal("TLS copy reused an unreadable credential", err)
	}
	stale := copiedPlan(edit)
	stale.ExpectedPlanRevision, stale.ExpectedInboundRevision = 2, 2
	if _, err = s.SaveNativeInboundPlan(ctx, key, stale); !errors.Is(err, ErrConflict) {
		t.Fatal("TLS retain ignored CAS", err)
	}
	endpoint := copiedPlan(edit)
	n := &endpoint.Nodes[0]
	n.DirectEndpoints = []NativeInboundEndpoint{{Address: n.DirectHosts[0], Enabled: true, SpiderX: "/reality-only"}}
	_, err = s.PreviewNativeInboundPlan(ctx, key, endpoint)
	requireTLSPlanError(t, err, "direct_endpoints.0.spider_x", "tls_spider_x_invalid", ErrInvalid)
	if _, err = s.CreateInbound(ctx, key, in.Profile); !errors.Is(err, ErrInvalid) {
		t.Fatal("generic create admitted TLS", err)
	}
	if _, err = s.MutateInbound(ctx, key, in.Profile.ID, InboundMutation{EventID: "tls-generic-edit-001", ExpectedRevision: 1, Action: "edit", Reason: "Reject generic TLS", Profile: &in.Profile}); !errors.Is(err, ErrInvalid) {
		t.Fatal("generic edit admitted TLS", err)
	}
	if _, err = s.MutateInbound(ctx, key, in.Profile.ID, InboundMutation{EventID: "tls-generic-enable-001", ExpectedRevision: 1, Action: "enable", Reason: "Reject generic TLS"}); err == nil {
		t.Fatal("generic enable admitted TLS")
	}
	loaded, err := s.NativeInboundPlan(ctx, key, in.Profile.ID)
	if err != nil || !reflect.DeepEqual(loaded, created.Plan) || planCount(t, s, "native_inbound_plan_events") != 1 {
		t.Fatal("rejected mutation changed TLS plan", err)
	}
}

func TestCreationTLSExpiredStoredMaterialRemainsReadableAndReplayable(t *testing.T) {
	s, key, in := tlsCreationPlanFixture(t)
	in.TLSCredentials = businessTLSCredentials(t, time.Now().Add(-48*time.Hour), time.Now().Add(-time.Hour))
	in, err := normalizeNativeInboundPlan(in)
	if err != nil {
		t.Fatal(err)
	}
	private := plannedProfile(in.Profile, "")
	private.TLS = planTLSMaterial(in.TLSCredentials)
	_, meta, err := xrayconfig.NormalizeTLSMaterial(private.TLS)
	if err != nil {
		t.Fatal(err)
	}
	// Independently seed a valid historical commit whose certificate has since
	// expired. No production clock is changed, and fresh save still validates it.
	stamp := time.Now().Add(-24 * time.Hour).Unix()
	plan := NativeInboundPlan{Profile: NativeInbound{InboundInput: in.Profile, TLS: meta, Revision: 1}, Nodes: in.Nodes, PlanRevision: 1, InboundRevision: 1, Status: nativePlanStatus(in.Nodes), LastEventID: in.EventID, Reason: in.Reason, CreatedUnix: stamp, UpdatedUnix: stamp, Editable: true}
	receipt := NativeInboundPlanReceipt{Plan: plan, EventID: in.EventID}
	profileEnc, err := sealInboundProfile(key, private)
	if err != nil {
		t.Fatal(err)
	}
	planEnc, err := sealPlanValue(key, planAAD(in.Profile.ID, 1), plan)
	if err != nil {
		t.Fatal(err)
	}
	receiptEnc, err := sealPlanValue(key, planReceiptAAD(in.EventID, in.Profile.ID, 1), receipt)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.db.Exec(`INSERT INTO native_inbounds(id,name,profile_enc,public_key,created_unix,enabled,archived,revision) VALUES(?,?,?,'',?,0,0,1)`, in.Profile.ID, in.Profile.Name, profileEnc, stamp); err != nil {
		t.Fatal(err)
	}
	if _, err = s.db.Exec(`INSERT INTO native_inbound_plans(inbound_id,revision,inbound_revision,status,last_event_id,payload_enc,created_unix,updated_unix) VALUES(?,1,1,?,?,?,?,?)`, in.Profile.ID, plan.Status, in.EventID, planEnc, stamp, stamp); err != nil {
		t.Fatal(err)
	}
	if _, err = s.db.Exec(`INSERT INTO native_inbound_plan_events(event_id,fingerprint,kind,inbound_id,revision,receipt_enc,created_unix) VALUES(?,?,'create',?,1,?,?)`, in.EventID, fingerprint("native_inbound_plan_create", in), in.Profile.ID, receiptEnc, stamp); err != nil {
		t.Fatal(err)
	}
	got, err := s.NativeInboundPlan(context.Background(), key, in.Profile.ID)
	if err != nil || !got.Editable || !reflect.DeepEqual(got, plan) {
		t.Fatal("expired saved plan cannot be reopened", err)
	}
	if _, err = s.ListInbounds(context.Background(), key); err != nil {
		t.Fatal("expired TLS profile cannot be listed", err)
	}
	replayed, err := s.SaveNativeInboundPlan(context.Background(), key, in)
	if err != nil || !replayed.Replayed || !reflect.DeepEqual(replayed.Plan, plan) {
		t.Fatal("expired historical receipt cannot replay", err)
	}
	in.EventID = "tls-expired-fresh-operation-001"
	in.ExpectedPlanRevision, in.ExpectedInboundRevision = 1, 1
	in.TLSCredentials = nil
	_, err = s.PreviewNativeInboundPlan(context.Background(), key, in)
	requireTLSPlanError(t, err, "tls_credentials.certificate_pem", "tls_certificate_expired", ErrInvalid)
	_, err = s.SaveNativeInboundPlan(context.Background(), key, in)
	requireTLSPlanError(t, err, "tls_credentials.certificate_pem", "tls_certificate_expired", ErrInvalid)
	if planCount(t, s, "native_inbound_plan_events") != 1 {
		t.Fatal("expired fresh mutation persisted")
	}
}

func TestInboundTLSVersionedEnvelopesRejectOldReadersAndAuthenticatedDowngrades(t *testing.T) {
	_, key, in := tlsCreationPlanFixture(t)
	private := plannedProfile(in.Profile, "")
	private.TLS = planTLSMaterial(in.TLSCredentials)
	encrypted, err := sealInboundProfile(key, private)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(encrypted, []byte("DXPPRF5\x00")) {
		t.Fatal("TLS profile envelope is not v5")
	}
	a := codecFixtureAEAD(t, key)
	// The previous decoder recognizes only v2/v3/v4, so v5 is attempted as
	// original v1 and authentication must fail before JSON could drop fields.
	if _, err = a.Open(nil, encrypted[:a.NonceSize()], encrypted[a.NonceSize():], []byte(fmt.Sprintf("DARK-PRO-NATIVE-INBOUND-v1:%d", in.Profile.ID))); err == nil {
		t.Fatal("pre-v5 decoder accepted TLS")
	}
	makeEnvelope := func(raw []byte, version int) []byte {
		nonce := bytes.Repeat([]byte{0x38}, a.NonceSize())
		sealed := a.Seal(append([]byte(nil), nonce...), nonce, raw, []byte(fmt.Sprintf("DARK-PRO-NATIVE-INBOUND-v%d:%d", version, in.Profile.ID)))
		if version > 1 {
			sealed = append([]byte(fmt.Sprintf("DXPPRF%d\x00", version)), sealed...)
		}
		return sealed
	}
	raw, _ := json.Marshal(private)
	for version := 1; version <= 4; version++ {
		if _, err = openInboundProfile(key, makeEnvelope(raw, version), in.Profile.ID); !errors.Is(err, ErrUnavailable) {
			t.Fatal("authenticated TLS downgrade accepted", version, err)
		}
	}
	for _, mutate := range []func(*xrayconfig.Profile){
		func(p *xrayconfig.Profile) { p.TLS = nil },
		func(p *xrayconfig.Profile) { p.Security = "" },
		func(p *xrayconfig.Profile) { p.RealityTarget = "www.example.org:443" },
		func(p *xrayconfig.Profile) { p.ServerNames = []string{"uncovered.example.org"} },
		func(p *xrayconfig.Profile) { p.Options = &xrayconfig.ProfileOptions{RealityShow: true} },
	} {
		p := private
		mutate(&p)
		invalid, _ := json.Marshal(p)
		if _, err = openInboundProfile(key, makeEnvelope(invalid, 5), in.Profile.ID); !errors.Is(err, ErrUnavailable) {
			t.Fatal("authenticated v5 security mismatch accepted", err)
		}
	}
	for _, value := range [][]byte{encrypted[:len(encrypted)-1], encrypted[len("DXPPRF5\x00"):], append(append([]byte(nil), encrypted[:len(encrypted)-1]...), encrypted[len(encrypted)-1]^1)} {
		if _, err = openInboundProfile(key, value, in.Profile.ID); !errors.Is(err, ErrUnavailable) {
			t.Fatal("tampered v5 opened", err)
		}
	}
	legacyRaw, want := codecFixtureProfileJSON("")
	nonce := append([]byte("DXPPRF5\x00"), 1, 2, 3, 4)
	legacy := codecFixtureEnvelope(t, key, nonce, legacyRaw, 1)
	got, err := openInboundProfile(key, legacy, codecFixtureID)
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatal("legacy nonce prefix collision failed", err)
	}
	_, meta, err := xrayconfig.NormalizeTLSMaterial(private.TLS)
	if err != nil {
		t.Fatal(err)
	}
	plan := NativeInboundPlan{Profile: NativeInbound{InboundInput: in.Profile, TLS: meta, Revision: 1}, Nodes: in.Nodes}
	planKey, err := planAEAD(key)
	if err != nil {
		t.Fatal(err)
	}
	aad := planAAD(in.Profile.ID, 1)
	sealed, err := sealPlanValue(key, aad, plan)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(sealed, []byte("DXPPLN3\x00")) {
		t.Fatal("TLS public plan envelope is not v3")
	}
	if _, err = planKey.Open(nil, sealed[:planKey.NonceSize()], sealed[planKey.NonceSize():], []byte(aad)); err == nil {
		t.Fatal("old plan reader opened TLS plan")
	}
	planRaw, _ := json.Marshal(plan)
	for _, version := range []int{1, 2} {
		n := bytes.Repeat([]byte{0x41}, planKey.NonceSize())
		binding := aad
		if version == 2 {
			binding = "DARK-PRO-CREATION-v2:" + aad
		}
		old := planKey.Seal(append([]byte(nil), n...), n, planRaw, []byte(binding))
		if version == 2 {
			old = append([]byte("DXPPLN2\x00"), old...)
		}
		var decoded NativeInboundPlan
		if err = openPlanValue(key, aad, old, &decoded); !errors.Is(err, ErrUnavailable) {
			t.Fatal("authenticated public-plan downgrade accepted", err)
		}
	}
	requirePublicTLSOnly(t, plan)
}

func TestCreationTLSCredentialJSONRejectsDuplicatesAndPrivateReadbackFields(t *testing.T) {
	for _, raw := range []string{
		`{"tls_credentials":null,"TLS_CREDENTIALS":{}}`,
		`{"tls_credentials":{"private_key_pem":"one","private_key_pem":"two"}}`,
		`{"tls_credentials":{"certificate_file":"/etc/private.pem"}}`,
		`{"profile":{"tls":{"private_key_pem":"secret"}}}`,
	} {
		var in NativeInboundPlanInput
		if json.Unmarshal([]byte(raw), &in) == nil {
			t.Fatal("ambiguous or readback secret input accepted")
		}
	}
	var in NativeInboundPlanInput
	if err := json.Unmarshal([]byte(`{"tls_credentials": null }`), &in); err != nil {
		t.Fatal(err)
	}
	if err := normalizePlanTLS(&in); !errors.Is(err, ErrInvalid) {
		t.Fatal("explicit null lost its presence", err)
	}
}
