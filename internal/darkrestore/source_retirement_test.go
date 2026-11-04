package darkrestore

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"

	uc "github.com/darktunnelmika/dark-xray-pro/internal/usagecheckpoint"
)

const retirementTestDestination = "abababababababababababababababababababababababababababababababab"

type retirementKeyPair struct {
	public  ed25519.PublicKey
	private ed25519.PrivateKey
}

type retirementFixture struct {
	inventory   LocalHandoffInventoryV1
	inventorySH string
	handoffs    []VerifiedLocalHandoff
	retirements []VerifiedSourceRetirementV1
	raw         [][]byte
	witnessRaw  [][]byte
	sourceKeys  []ed25519.PublicKey
	witnessKeys []ed25519.PublicKey
	pins        []SourceRetirementPinsV1
}

func retirementSHA(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func retirementKey(t *testing.T) retirementKeyPair {
	t.Helper()
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return retirementKeyPair{public: public, private: private}
}

func signRetirementFixture(t *testing.T, statement SourceRetirementStatementV1, source, witness retirementKeyPair) ([]byte, []byte, SourceRetirementPinsV1) {
	t.Helper()
	message, err := retirementMessageV1(statement)
	if err != nil {
		t.Fatal(err)
	}
	signed := SignedSourceRetirementV1{Version: 1, Statement: statement, Signature: ed25519.Sign(source.private, message)}
	raw, err := json.Marshal(signed)
	if err != nil {
		t.Fatal(err)
	}
	raw = append(raw, '\n')
	witnessStatement := SourceRetirementWitnessV1{
		Version: 1, Scope: SourceRetirementWitnessScopeV1,
		RetirementDocumentSHA: retirementSHA(raw), SourcePublicKeySHA: statement.SourceSignerPublicSHA,
		InventorySHA: statement.InventorySHA, DestinationSHA: statement.DestinationSHA,
		WitnessedUnixNano: statement.FenceObservedUnixNano + 1_000,
	}
	message, err = retirementWitnessMessageV1(witnessStatement)
	if err != nil {
		t.Fatal(err)
	}
	signedWitness := SignedSourceRetirementWitnessV1{Version: 1, Witness: witnessStatement, Signature: ed25519.Sign(witness.private, message)}
	witnessRaw, err := json.Marshal(signedWitness)
	if err != nil {
		t.Fatal(err)
	}
	witnessRaw = append(witnessRaw, '\n')
	return raw, witnessRaw, SourceRetirementPinsV1{
		RetirementDocumentSHA: retirementSHA(raw), SourcePublicKeySHA: retirementSHA(source.public),
		WitnessDocumentSHA: retirementSHA(witnessRaw), WitnessPublicKeySHA: retirementSHA(witness.public),
		FenceDocumentSHA: statement.FenceDocumentSHA,
		InventorySHA:     statement.InventorySHA, DestinationSHA: statement.DestinationSHA,
	}
}

func newRetirementFixture(t *testing.T) retirementFixture {
	t.Helper()
	sourceSHA := retirementSHA([]byte("source"))
	f := retirementFixture{inventory: LocalHandoffInventoryV1{Version: 1, SourceSHA: sourceSHA, SourceID: "source-set"}}
	sourceKeys := []retirementKeyPair{retirementKey(t), retirementKey(t)}
	witnessKeys := []retirementKeyPair{retirementKey(t), retirementKey(t)}
	for i, epoch := range []string{strings.Repeat("a", 32), strings.Repeat("b", 32)} {
		receiptSHA := retirementSHA([]byte("receipt-" + epoch))
		authoritySHA := retirementSHA([]byte("authority-" + epoch))
		journalSHA := retirementSHA([]byte("journal-" + epoch))
		handoffSHA := retirementSHA([]byte("handoff-" + epoch))
		publicSHA := retirementSHA(sourceKeys[i].public)
		sequence := uint64(i + 1)
		f.inventory.Sources = append(f.inventory.Sources, LocalHandoffSourcePinV1{
			EpochID: epoch, Sequence: sequence, ReceiptSHA: receiptSHA, AuthoritySHA: authoritySHA,
			JournalSHA: journalSHA, HandoffSHA: handoffSHA, SignerPublicSHA: publicSHA,
		})
		f.handoffs = append(f.handoffs, VerifiedLocalHandoff{
			settlement: LocalSourceSettlement{
				Scope: "single_local_source_epoch", SourceID: f.inventory.SourceID, SourceSHA: sourceSHA,
				AuthoritySHA: authoritySHA, JournalSHA: journalSHA,
				Terminal:   uc.Receipt{EpochID: epoch, Sequence: sequence, SHA: receiptSHA},
				PriorBytes: 10, Up: int64(i + 1), Down: int64(i + 2), Historical: int64(13 + 2*i), Remaining: 100,
			},
			digest: handoffSHA, publicKeySHA: publicSHA, valid: true,
		})
	}
	var err error
	f.inventorySH, err = DigestLocalHandoffInventoryV1(f.inventory)
	if err != nil {
		t.Fatal(err)
	}
	for i, source := range f.inventory.Sources {
		statement := SourceRetirementStatementV1{
			Version: 1, Scope: SourceRetirementScopeV1, SourceSHA: sourceSHA, SourceID: f.inventory.SourceID,
			InventorySHA: f.inventorySH, DestinationSHA: retirementTestDestination,
			EpochID: source.EpochID, Sequence: source.Sequence, TerminalReceiptSHA: source.ReceiptSHA,
			AuthoritySHA: source.AuthoritySHA, JournalSHA: source.JournalSHA, HandoffSHA: source.HandoffSHA,
			SourceSignerPublicSHA: source.SignerPublicSHA, RetiredUnixNano: int64(10_000 + i*10),
			FenceDocumentSHA: retirementSHA([]byte("fence-" + source.EpochID)), FenceObservedUnixNano: int64(11_000 + i*10),
		}
		raw, witnessRaw, pins := signRetirementFixture(t, statement, sourceKeys[i], witnessKeys[i])
		verified, err := VerifySignedSourceRetirementV1(raw, sourceKeys[i].public, witnessRaw, witnessKeys[i].public, pins)
		if err != nil {
			t.Fatal(err)
		}
		f.raw = append(f.raw, raw)
		f.witnessRaw = append(f.witnessRaw, witnessRaw)
		f.sourceKeys = append(f.sourceKeys, sourceKeys[i].public)
		f.witnessKeys = append(f.witnessKeys, witnessKeys[i].public)
		f.pins = append(f.pins, pins)
		f.retirements = append(f.retirements, verified)
	}
	return f
}

func TestLocalHandoffRetirementCoverageV1(t *testing.T) {
	f := newRetirementFixture(t)
	out, err := CheckLocalHandoffRetirementsV1(f.inventory, f.inventorySH, f.handoffs, f.retirements, retirementTestDestination)
	if err != nil || out.Scope != "pinned_declared_sources_only" || out.InventorySHA != f.inventorySH ||
		out.DestinationSHA != retirementTestDestination || out.DeclaredSources != 2 || len(out.EpochIDs) != 2 ||
		!inventoryDigestValid(out.BundleSHA) || out.AuthorizesActivation {
		t.Fatal("retirement coverage", out, err)
	}
	if _, _, _, mutation, err := f.retirements[0].Evidence(); err != nil || mutation {
		t.Fatal("verified receipt granted mutation", mutation, err)
	}
	durable, err := CheckDeclaredSourceRetirementsV1(f.inventory, f.inventorySH, f.retirements, retirementTestDestination)
	if err != nil || durable.BundleSHA != out.BundleSHA {
		t.Fatal("durable revalidation changed coverage", durable, err)
	}
	retirementRaw, sourcePublic, witnessRaw, witnessPublic, pins, err := f.retirements[0].Documents()
	if err != nil || !bytes.Equal(retirementRaw, f.raw[0]) || !bytes.Equal(sourcePublic, f.sourceKeys[0]) ||
		!bytes.Equal(witnessRaw, f.witnessRaw[0]) || !bytes.Equal(witnessPublic, f.witnessKeys[0]) || pins != f.pins[0] {
		t.Fatal("verified documents changed", err)
	}
	retirementRaw[0], sourcePublic[0], witnessRaw[0], witnessPublic[0] = 0, 0, 0, 0
	again, againSource, againWitness, againWitnessKey, _, err := f.retirements[0].Documents()
	if err != nil || !bytes.Equal(again, f.raw[0]) || !bytes.Equal(againSource, f.sourceKeys[0]) ||
		!bytes.Equal(againWitness, f.witnessRaw[0]) || !bytes.Equal(againWitnessKey, f.witnessKeys[0]) {
		t.Fatal("document accessor leaked mutable storage", err)
	}
}

func TestLocalHandoffRetirementCoverageRefusesIncompleteOrForeignEvidence(t *testing.T) {
	f := newRetirementFixture(t)
	if _, err := CheckLocalHandoffRetirementsV1(f.inventory, f.inventorySH, f.handoffs, f.retirements[:1], retirementTestDestination); err == nil {
		t.Fatal("missing retirement accepted")
	}
	if _, err := CheckLocalHandoffRetirementsV1(f.inventory, f.inventorySH, f.handoffs, []VerifiedSourceRetirementV1{f.retirements[0], f.retirements[0]}, retirementTestDestination); err == nil {
		t.Fatal("duplicate retirement accepted")
	}
	if _, err := CheckLocalHandoffRetirementsV1(f.inventory, f.inventorySH, f.handoffs, f.retirements, retirementSHA([]byte("foreign destination"))); err == nil {
		t.Fatal("foreign destination accepted")
	}
	bad := f.retirements[0]
	bad.statement.TerminalReceiptSHA = retirementSHA([]byte("foreign receipt"))
	if _, err := CheckLocalHandoffRetirementsV1(f.inventory, f.inventorySH, f.handoffs, []VerifiedSourceRetirementV1{bad, f.retirements[1]}, retirementTestDestination); err == nil {
		t.Fatal("foreign terminal receipt accepted")
	}
}

func TestVerifySignedSourceRetirementV1RefusesTampering(t *testing.T) {
	f := newRetirementFixture(t)
	foreignPin := f.pins[0]
	foreignPin.RetirementDocumentSHA = retirementSHA([]byte("foreign"))
	foreignFencePin := f.pins[0]
	foreignFencePin.FenceDocumentSHA = retirementSHA([]byte("foreign fence"))
	cases := []struct {
		name                             string
		raw, source, witness, witnessKey []byte
		pins                             SourceRetirementPinsV1
	}{
		{"retirement document pin", f.raw[0], f.sourceKeys[0], f.witnessRaw[0], f.witnessKeys[0], foreignPin},
		{"active fence pin", f.raw[0], f.sourceKeys[0], f.witnessRaw[0], f.witnessKeys[0], foreignFencePin},
		{"source key pin", f.raw[0], f.sourceKeys[1], f.witnessRaw[0], f.witnessKeys[0], f.pins[0]},
		{"same source and witness key", f.raw[0], f.sourceKeys[0], f.witnessRaw[0], f.sourceKeys[0], f.pins[0]},
		{"witness document pin", f.raw[0], f.sourceKeys[0], f.witnessRaw[1], f.witnessKeys[0], f.pins[0]},
		{"noncanonical retirement", append([]byte(" "), f.raw[0]...), f.sourceKeys[0], f.witnessRaw[0], f.witnessKeys[0], f.pins[0]},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := VerifySignedSourceRetirementV1(tc.raw, tc.source, tc.witness, tc.witnessKey, tc.pins); err == nil {
				t.Fatal("invalid retirement admitted")
			}
		})
	}
}

func TestVerifySignedSourceRetirementStatementV1IsSourceOnly(t *testing.T) {
	f := newRetirementFixture(t)
	pins := SourceRetirementStatementPinsV1{
		RetirementDocumentSHA: f.pins[0].RetirementDocumentSHA,
		SourcePublicKeySHA:    f.pins[0].SourcePublicKeySHA,
		FenceDocumentSHA:      f.pins[0].FenceDocumentSHA,
		InventorySHA:          f.pins[0].InventorySHA,
		DestinationSHA:        f.pins[0].DestinationSHA,
	}
	verified, err := VerifySignedSourceRetirementStatementV1(f.raw[0], f.sourceKeys[0], pins)
	if err != nil {
		t.Fatal("source statement rejected", err)
	}
	statement, gotPins, mutation, err := verified.Evidence()
	if err != nil || mutation || statement != f.retirements[0].statement || gotPins != pins {
		t.Fatal("source-only evidence changed or granted mutation", mutation, err)
	}
	if _, _, mutation, err := (VerifiedSourceRetirementStatementV1{}).Evidence(); err == nil || mutation {
		t.Fatal("zero source evidence admitted")
	}

	cases := []struct {
		name   string
		raw    []byte
		public []byte
		pins   SourceRetirementStatementPinsV1
	}{
		{"foreign document", f.raw[1], f.sourceKeys[0], pins},
		{"foreign source key", f.raw[0], f.sourceKeys[1], pins},
		{"noncanonical document", append([]byte(" "), f.raw[0]...), f.sourceKeys[0], pins},
		{"missing newline", bytes.TrimSuffix(f.raw[0], []byte("\n")), f.sourceKeys[0], pins},
	}
	badFence := pins
	badFence.FenceDocumentSHA = retirementSHA([]byte("foreign fence"))
	cases = append(cases, struct {
		name   string
		raw    []byte
		public []byte
		pins   SourceRetirementStatementPinsV1
	}{"foreign fence pin", f.raw[0], f.sourceKeys[0], badFence})
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := VerifySignedSourceRetirementStatementV1(tc.raw, tc.public, tc.pins); err == nil {
				t.Fatal("invalid source-only statement admitted")
			}
		})
	}
}

func TestVerifySignedSourceRetirementV1RefusesOverlongFence(t *testing.T) {
	f := newRetirementFixture(t)
	source, witness := retirementKey(t), retirementKey(t)
	statement := f.retirements[0].statement
	statement.SourceSignerPublicSHA = retirementSHA(source.public)
	statement.FenceObservedUnixNano = statement.RetiredUnixNano + maxRetirementWitnessDelayNano + 1
	raw, witnessRaw, pins := signRetirementFixture(t, statement, source, witness)
	if _, err := VerifySignedSourceRetirementV1(raw, source.public, witnessRaw, witness.public, pins); err == nil {
		t.Fatal("overlong source fence admitted")
	}
}
