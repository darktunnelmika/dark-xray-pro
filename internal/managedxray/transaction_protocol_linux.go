//go:build linux

package managedxray

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"regexp"
	"time"

	"github.com/darktunnelmika/dark-xray-pro/internal/xrayconfig"
)

var ErrTransaction = errors.New("invalid or stale route transaction")
var ErrTransactionInterrupted = errors.New("interrupted route transaction requires explicit restaging")

const maxTransactionSerial = 1<<53 - 1

var transactionID = regexp.MustCompile(`^[a-f0-9]{32}$`)
var transactionHash = regexp.MustCompile(`^[a-f0-9]{64}$`)
var transactionNode = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,63}$`)

// RuntimeCondition binds a command to the state actually reviewed by its
// issuer. A stopped/restarted child is distinct from a live child with the
// same last-good configuration. Lease renewal alone does not change it.
type RuntimeCondition struct {
	Highwater      uint64 `json:"highwater"`
	SavedSequence  uint64 `json:"saved_sequence"`
	SavedSHA       string `json:"saved_sha"`
	ActiveSequence uint64 `json:"active_sequence"`
	ActiveSHA      string `json:"active_sha"`
	ProcessReady   bool   `json:"process_ready"`
}

func Condition(s Snapshot) RuntimeCondition {
	return RuntimeCondition{s.Highwater, s.SavedSequence, s.SavedSHA, s.ActiveSequence, s.ActiveSHA, s.ProcessReady}
}

// TransactionCommand authorizes exactly one action. Serial orders commands,
// while RuntimeSequence independently orders the underlying Xray revisions.
// PayloadSHA authenticates all candidate metadata as well as private bytes.
type TransactionCommand struct {
	Version               int              `json:"version"`
	ID                    string           `json:"id"`
	Serial                uint64           `json:"serial"`
	Action                string           `json:"action"`
	StageID               string           `json:"stage_id"`
	NodeID                string           `json:"node_id"`
	InstallationID        string           `json:"installation_id"`
	RuntimeSequence       uint64           `json:"runtime_sequence"`
	PayloadSHA            string           `json:"payload_sha"`
	BinarySHA             string           `json:"binary_sha"`
	Expected              RuntimeCondition `json:"expected"`
	InterruptedCommandSHA string           `json:"interrupted_command_sha,omitempty"`
	Issued                int64            `json:"issued_unix_nano"`
	Expires               int64            `json:"expires_unix_nano"`
	Lease                 Lease            `json:"lease"`
	Signature             string           `json:"signature"`
}

func transactionMAC(key []byte, domain string, v any) string {
	raw, _ := json.Marshal(v)
	h := hmac.New(sha256.New, key)
	h.Write([]byte(domain + "\x00"))
	h.Write(raw)
	return hex.EncodeToString(h.Sum(nil))
}

func hashJSON(v any) string {
	raw, _ := json.Marshal(v)
	h := sha256.Sum256(raw)
	return hex.EncodeToString(h[:])
}

// TransactionPayloadSHA must be computed before signing the Stage command.
// Config alone does not authenticate listener IDs, kinds or attribution.
func TransactionPayloadSHA(c xrayconfig.Candidate) string {
	return hashJSON(struct {
		Metadata xrayconfig.Candidate `json:"metadata"`
		Config   []byte               `json:"config"`
	}{c, c.Config})
}

func validTransactionCommand(c TransactionCommand) bool {
	if c.Version != 1 || !transactionID.MatchString(c.ID) || !transactionID.MatchString(c.StageID) ||
		!transactionNode.MatchString(c.NodeID) || !transactionID.MatchString(c.InstallationID) ||
		c.Serial == 0 || c.Serial > maxTransactionSerial || c.RuntimeSequence == 0 || c.RuntimeSequence > maxTransactionSerial ||
		!transactionHash.MatchString(c.PayloadSHA) || !transactionHash.MatchString(c.BinarySHA) ||
		c.Issued <= 0 || c.Expires <= c.Issued || c.Expires-c.Issued > int64(2*time.Minute) ||
		(c.InterruptedCommandSHA != "" && !transactionHash.MatchString(c.InterruptedCommandSHA)) {
		return false
	}
	switch c.Action {
	case "stage":
		return c.StageID == c.ID && c.Lease == (Lease{})
	case "apply", "rollback", "renew":
		return c.InterruptedCommandSHA == "" && c.Lease.Sequence == c.RuntimeSequence &&
			c.Lease.NodeID == c.NodeID && c.Lease.InstallationID == c.InstallationID && c.Lease.Expires <= c.Expires
	default:
		return false
	}
}

func SignTransactionCommand(key []byte, c TransactionCommand) (TransactionCommand, error) {
	if len(key) < 32 || !validTransactionCommand(c) {
		return TransactionCommand{}, ErrTransaction
	}
	c.Signature = ""
	c.Signature = transactionMAC(key, "DARK-PRO-ROUTE-COMMAND-v1", c)
	return c, nil
}

func validTransactionSignature(key []byte, c TransactionCommand) bool {
	want, err := SignTransactionCommand(key, c)
	return err == nil && transactionHash.MatchString(c.Signature) && hmac.Equal([]byte(want.Signature), []byte(c.Signature))
}

// TransactionReceipt is a signed HISTORICAL observation of one command. It
// never grants traffic authority or proves billing, a REALITY handshake or
// current liveness. The independent short runtime lease still expires.
type TransactionReceipt struct {
	Version         int      `json:"version"`
	CommandID       string   `json:"command_id"`
	CommandSHA      string   `json:"command_sha"`
	Serial          uint64   `json:"serial"`
	Action          string   `json:"action"`
	StageID         string   `json:"stage_id"`
	NodeID          string   `json:"node_id"`
	InstallationID  string   `json:"installation_id"`
	RuntimeInstance string   `json:"runtime_instance"`
	Outcome         string   `json:"outcome"`
	ObservedAt      int64    `json:"observed_unix_nano"`
	Observed        Snapshot `json:"observed"`
	Signature       string   `json:"signature"`
}

func signTransactionReceipt(key []byte, r TransactionReceipt) TransactionReceipt {
	r.Signature = ""
	r.Signature = transactionMAC(key, "DARK-PRO-ROUTE-RECEIPT-v1", r)
	return r
}

// VerifyTransactionReceipt authenticates the entire result, not just the
// lease that authorized the request. It does not certify a remote Node OS.
func VerifyTransactionReceipt(key []byte, c TransactionCommand, r TransactionReceipt) bool {
	if !validTransactionSignature(key, c) || r.Version != 1 || r.CommandID != c.ID || r.CommandSHA != hashJSON(c) ||
		r.Serial != c.Serial || r.Action != c.Action || r.StageID != c.StageID || r.NodeID != c.NodeID ||
		r.InstallationID != c.InstallationID || !transactionID.MatchString(r.RuntimeInstance) || r.ObservedAt <= 0 ||
		r.Observed.DataPlaneVerified || r.Observed.SubscriptionPublished || r.Observed.Usage != nil ||
		!transactionHash.MatchString(r.Signature) {
		return false
	}
	switch r.Outcome {
	case "staged":
		if c.Action != "stage" {
			return false
		}
	case "applied", "rolled_back", "renewed":
		want := map[string]string{"applied": "apply", "rolled_back": "rollback", "renewed": "renew"}[r.Outcome]
		if c.Action != want || !r.Observed.ProcessReady || r.Observed.ActiveSequence != c.RuntimeSequence || r.Observed.ActiveSHA != c.Lease.ConfigSHA {
			return false
		}
	case "failed_stopped", "failed_previous_retained":
		if c.Action == "stage" {
			return false
		}
	default:
		return false
	}
	want := signTransactionReceipt(key, r)
	return hmac.Equal([]byte(want.Signature), []byte(r.Signature))
}
