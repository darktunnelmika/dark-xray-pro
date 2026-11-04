//go:build linux

package darkrestore

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

type retirementApprovalFixture struct {
	manifest     retirementManifestFixture
	approval     HandoffCoverageApprovalV1
	approvalPath string
	approvalSHA  string
	publicPath   string
	publicSHA    string
	public       ed25519.PublicKey
	private      ed25519.PrivateKey
}

func newRetirementApprovalFixture(t *testing.T) retirementApprovalFixture {
	t.Helper()
	manifest := newRetirementManifestFixture(t)
	report, err := VerifySourceRetirementManifestFileV1(manifest.path, manifest.sha)
	if err != nil {
		t.Fatal(err)
	}
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	approval := HandoffCoverageApprovalV1{
		Version: 1, InventorySHA: report.Coverage.InventorySHA,
		SourceSHA: manifest.manifest.Inventory.SourceSHA, SourceID: manifest.manifest.Inventory.SourceID,
		Scope: HandoffCoverageScopeV1, Statement: HandoffCoverageStatementV1,
		DeclaredSources: report.Coverage.DeclaredSources, EpochIDs: append([]string(nil), report.Coverage.EpochIDs...),
		InventoryCompleteAsserted: true, DeclaredSourcesRetired: true,
		RetirementBundleSHA:   report.Coverage.BundleSHA,
		ApprovedUnixNano:      report.Coverage.LatestWitnessedUnixNano + 1,
		DestinationSHA:        manifest.manifest.DestinationSHA,
		DestinationStateSHA:   retirementSHA([]byte("synthetic destination state")),
		NativeBaselineSHA:     retirementSHA([]byte("synthetic native baseline")),
		ImportReceiptSHA:      retirementSHA([]byte("synthetic import receipt")),
		BillingAttributionSHA: retirementSHA([]byte("synthetic billing attribution")),
		BillingAttribution:    "disjoint_source_epoch_deltas",
	}
	dir := t.TempDir()
	if err = os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	f := retirementApprovalFixture{
		manifest: manifest, approval: approval,
		approvalPath: filepath.Join(dir, "approval.json"),
		publicPath:   filepath.Join(dir, "approval.pub"),
		publicSHA:    retirementSHA(public), public: public, private: private,
	}
	if err = os.WriteFile(f.publicPath, public, 0600); err != nil {
		t.Fatal(err)
	}
	f.rewriteApproval(t)
	return f
}

func (f *retirementApprovalFixture) rewriteApproval(t *testing.T) {
	t.Helper()
	message, err := coverageApprovalMessageV1(f.approval)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(SignedHandoffCoverageApprovalV1{
		Version: 1, Approval: f.approval, Signature: ed25519.Sign(f.private, message),
	})
	if err != nil {
		t.Fatal(err)
	}
	raw = append(raw, '\n')
	if err = os.WriteFile(f.approvalPath, raw, 0600); err != nil {
		t.Fatal(err)
	}
	f.approvalSHA = retirementSHA(raw)
}

func (f retirementApprovalFixture) verify() (SourceRetirementApprovalReportV1, error) {
	return VerifySourceRetirementApprovalFilesV1(
		f.manifest.path, f.manifest.sha, f.approvalPath, f.approvalSHA, f.publicPath, f.publicSHA,
	)
}

func TestVerifySourceRetirementApprovalFilesV1BindsLaterApproval(t *testing.T) {
	f := newRetirementApprovalFixture(t)
	report, err := f.verify()
	if err != nil || report.AuthorizesActivation || report.Manifest.AuthorizesActivation ||
		report.Manifest.Coverage.AuthorizesActivation || report.CoverageApprovalSHA != f.approvalSHA ||
		report.CoverageAuthorityPublicSHA != f.publicSHA ||
		report.ApprovedUnixNano <= report.Manifest.Coverage.LatestWitnessedUnixNano {
		t.Fatal("exact later approval rejected or granted authority", report, err)
	}
}

func TestVerifySourceRetirementApprovalFilesV1FailsClosed(t *testing.T) {
	tests := []struct {
		name   string
		change func(*testing.T, *retirementApprovalFixture)
	}{
		{"approval pin mismatch", func(_ *testing.T, f *retirementApprovalFixture) {
			f.approvalSHA = retirementSHA([]byte("foreign approval"))
		}},
		{"authority pin mismatch", func(_ *testing.T, f *retirementApprovalFixture) {
			f.publicSHA = retirementSHA([]byte("foreign authority"))
		}},
		{"foreign retirement bundle", func(t *testing.T, f *retirementApprovalFixture) {
			f.approval.RetirementBundleSHA = retirementSHA([]byte("foreign bundle"))
			f.rewriteApproval(t)
		}},
		{"approval not later than witness", func(t *testing.T, f *retirementApprovalFixture) {
			report, err := VerifySourceRetirementManifestFileV1(f.manifest.path, f.manifest.sha)
			if err != nil {
				t.Fatal(err)
			}
			f.approval.ApprovedUnixNano = report.Coverage.LatestWitnessedUnixNano
			f.rewriteApproval(t)
		}},
		{"foreign source", func(t *testing.T, f *retirementApprovalFixture) {
			f.approval.SourceSHA = retirementSHA([]byte("foreign source"))
			f.rewriteApproval(t)
		}},
		{"foreign destination", func(t *testing.T, f *retirementApprovalFixture) {
			f.approval.DestinationSHA = retirementSHA([]byte("foreign destination"))
			f.rewriteApproval(t)
		}},
		{"world-readable approval", func(t *testing.T, f *retirementApprovalFixture) {
			if err := os.Chmod(f.approvalPath, 0644); err != nil {
				t.Fatal(err)
			}
		}},
		{"world-readable authority", func(t *testing.T, f *retirementApprovalFixture) {
			if err := os.Chmod(f.publicPath, 0644); err != nil {
				t.Fatal(err)
			}
		}},
		{"symlink approval", func(t *testing.T, f *retirementApprovalFixture) {
			link := filepath.Join(filepath.Dir(f.approvalPath), "approval-link.json")
			if err := os.Symlink(f.approvalPath, link); err != nil {
				t.Fatal(err)
			}
			f.approvalPath = link
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			f := newRetirementApprovalFixture(t)
			test.change(t, &f)
			if _, err := f.verify(); err == nil {
				t.Fatal("unsafe retirement approval accepted")
			}
		})
	}
}
