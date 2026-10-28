// Package hubstate fences all native HUB writers and describes the complete
// offline recovery set. It never activates a restored service.
package hubstate

import (
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

const ProductionRoot = "/var/lib/dark-xray-pro-lab"
const LockName = "hub-state.lock"
const InventoryName = "backup-inventory.json"
const QuarantineName = "restore-quarantine.json"
const EnableEnv = "DARK_PRO_ENABLE_OFFLINE_BACKUP_V1"

// Reserve room for the private inventory and archive framing within the
// encrypted format's 64-GiB expanded-stream budget.
const maxSnapshotPayload int64 = (64 << 30) - (4 << 20)

var ErrUnsafe = errors.New("offline HUB state is unsafe or incomplete")
var ErrBusy = errors.New("HUB state is owned by a running writer")
var ErrQuarantined = errors.New("restored HUB state is quarantined")

type Source struct {
	Name string
	Role string
	Path string
	Size int64
}
type SnapshotFile struct {
	Name string `json:"name"`
	Role string `json:"role"`
	Size int64  `json:"size"`
}
type Resource struct {
	Name     string `json:"name"`
	Role     string `json:"role"`
	Path     string `json:"path"`
	Required bool   `json:"required"`
	Present  bool   `json:"present"`
}
type Inventory struct {
	Version        int               `json:"version"`
	Root           string            `json:"root"`
	CreatedUnix    int64             `json:"created_unix"`
	SourceRevision string            `json:"source_revision"`
	BuildModified  bool              `json:"build_modified"`
	Environment    map[string]string `json:"environment"`
	Flags          map[string]string `json:"flags"`
	Resources      []Resource        `json:"resources"`
	// SnapshotFiles describes every payload except inventory.json itself.
	// Its own authenticated length is in the outer archive manifest.
	SnapshotFiles []SnapshotFile `json:"snapshot_files,omitempty"`
}

// Options are the actual parsed HUB flags, never a directory glob. Environment
// is the effective environment, supplied without logging or shell expansion.
type Options struct {
	Root, Registry, Replacements, Staging, BusinessDB, LabRevisions string
	NodeHistoryDB, AgentTLSCert, AgentTLSKey, OwnerReportConfig     string
	Listen, AgentTLSListen, Static                                  string
	SourceRevision                                                  string
	BuildModified                                                   bool
	Environment                                                     []string
}

var revisionRE = regexp.MustCompile(`^[a-f0-9]{40}$`)
var resources = map[string]string{
	"business": "data/business.sqlite", "registry": "data/registry.json",
	"replacements": "data/replacements.json", "config_stage": "data/configs.json",
	"lab_revisions": "data/lab-revisions.json", "node_history": "data/node-history.sqlite",
	"inbound_runtime": "runtime/state.enc", "owner_report_db": "bot/inbox.sqlite",
	"owner_report_config": "config/owner-bot.json", "owner_report_token": "config/owner-bot-token",
	"agent_tls_cert": "config/agent-cert.pem", "agent_tls_key": "config/agent-key.pem",
}

var knownEnvironment = strings.Fields(`DARK_PRO_LAB_MODE DARK_PRO_LAB_TOKEN
DARK_PRO_ENABLE_BUSINESS_CORE DARK_PRO_ENABLE_USAGE_LEDGER DARK_PRO_ENABLE_NATIVE_INBOUNDS
DARK_PRO_NATIVE_INBOUND_KEY_HEX DARK_PRO_AUTHORING_CORE_PATH DARK_PRO_AUTHORING_CORE_SHA256
DARK_PRO_ENABLE_SELLER_MANAGEMENT_V2 DARK_PRO_ENABLE_VOLUME_REFUNDS_V1 DARK_PRO_ENABLE_SELLER_ORDERS_V1
DARK_PRO_ENABLE_OWNER_REPORT_BOT_V1 DARK_PRO_ENABLE_REMOTE_AGENT DARK_PRO_ENABLE_NODE_HISTORY
DARK_PRO_ENABLE_INBOUND_EXECUTION DARK_PRO_INBOUND_RUNTIME_DIR DARK_PRO_INBOUND_CORE_PATH
DARK_PRO_INBOUND_CORE_SHA256 DARK_PRO_INBOUND_ENGINES_JSON DARK_PRO_ENABLE_REMOTE_METERED_ADMISSION
DARK_PRO_NATIVE_METERED_NODE_ID DARK_PRO_ENABLE_ROUTE_TRANSACTIONS DARK_PRO_ROUTE_NODE_ID
DARK_PRO_ROUTE_INSTALLATION_ID DARK_PRO_ROUTE_XRAY_SHA256 DARK_PRO_ENABLE_INBOUND_PLAN_PROMOTION
DARK_PRO_ENABLE_METERED_ROUTE_TRANSACTIONS DARK_PRO_METERED_ROUTE_NODES_JSON
DARK_PRO_METERED_ROUTE_NODE_ID DARK_PRO_METERED_ROUTE_INSTALLATION_ID DARK_PRO_METERED_ROUTE_XRAY_SHA256
DARK_PRO_ENABLE_INITIAL_APPLY_HEALTH DARK_PRO_ENABLE_METERED_HEALTH DARK_PRO_HEALTH_VERIFIER_JSON
DARK_PRO_ENABLE_METERED_HEALTH_ROUNDS DARK_PRO_ENABLE_LAB_SUBSCRIPTIONS DARK_PRO_ENABLE_CONSUMER_ENDPOINTS
DARK_PRO_CONSUMER_ENDPOINTS_JSON DARK_PRO_ENABLE_CONFIG_STAGE DARK_PRO_STAGE_KEY_HEX
DARK_PRO_ENABLE_LAB_REVISION_ROLLBACK DARK_PRO_ENABLE_OFFLINE_BACKUP_V1`)

func selectedEnvironment(raw []string) (map[string]string, error) {
	known := map[string]bool{}
	for _, k := range knownEnvironment {
		known[k] = true
	}
	out := map[string]string{}
	for _, item := range raw {
		k, v, ok := strings.Cut(item, "=")
		if !ok {
			return nil, ErrUnsafe
		}
		if strings.HasPrefix(k, "DARK_PRO_ENABLE_") && !known[k] && v != "" && v != "0" {
			return nil, ErrUnsafe
		}
		if known[k] {
			if _, duplicate := out[k]; duplicate {
				return nil, ErrUnsafe
			}
			out[k] = v
		}
	}
	return out, nil
}

func inside(root, path string) bool {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path || strings.ContainsAny(path, "\x00\r\n") {
		return false
	}
	rel, e := filepath.Rel(root, path)
	return e == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, "../")
}

func BuildInventory(o Options) (Inventory, error) {
	inv := Inventory{Version: 1, Root: o.Root, CreatedUnix: time.Now().UTC().Unix(), SourceRevision: o.SourceRevision, BuildModified: o.BuildModified}
	var err error
	inv.Environment, err = selectedEnvironment(o.Environment)
	if err != nil {
		return inv, err
	}
	if o.Static != "" {
		return inv, ErrUnsafe
	} // Development UI directories are not recovery data.
	inv.Flags = map[string]string{"listen": o.Listen, "agent-tls-listen": o.AgentTLSListen}
	add := func(role, path string, required bool) error {
		if path == "" {
			return ErrUnsafe
		}
		p, e := filepath.Abs(path)
		if e != nil {
			return ErrUnsafe
		}
		inv.Resources = append(inv.Resources, Resource{Name: resources[role], Role: role, Path: p, Required: required})
		return nil
	}
	for _, r := range []struct {
		role, path string
		required   bool
	}{{"business", o.BusinessDB, true}, {"registry", o.Registry, false}, {"replacements", o.Replacements, false}} {
		if e := add(r.role, r.path, r.required); e != nil {
			return inv, e
		}
	}
	conditional := []struct {
		flag, role, path string
		required         bool
	}{
		{"DARK_PRO_ENABLE_NODE_HISTORY", "node_history", o.NodeHistoryDB, true},
		{"DARK_PRO_ENABLE_CONFIG_STAGE", "config_stage", o.Staging, false},
		{"DARK_PRO_ENABLE_LAB_REVISION_ROLLBACK", "lab_revisions", o.LabRevisions, false},
		{"DARK_PRO_ENABLE_INBOUND_EXECUTION", "inbound_runtime", func() string {
			if inv.Environment["DARK_PRO_INBOUND_RUNTIME_DIR"] == "" {
				return ""
			}
			return filepath.Join(inv.Environment["DARK_PRO_INBOUND_RUNTIME_DIR"], "state.enc")
		}(), true},
	}
	for _, r := range conditional {
		enabled := inv.Environment[r.flag] == "1"
		if enabled {
			if e := add(r.role, r.path, r.required); e != nil {
				return inv, e
			}
			continue
		}
		if r.path == "" {
			continue
		}
		abs, e := filepath.Abs(r.path)
		if e != nil {
			return inv, ErrUnsafe
		}
		if inside(o.Root, abs) {
			if e = add(r.role, abs, false); e != nil {
				return inv, e
			}
		} else if _, e = os.Lstat(abs); e == nil || !errors.Is(e, os.ErrNotExist) {
			return inv, ErrUnsafe
		}
	}
	if o.AgentTLSCert != "" || o.AgentTLSKey != "" {
		if add("agent_tls_cert", o.AgentTLSCert, true) != nil || add("agent_tls_key", o.AgentTLSKey, true) != nil {
			return inv, ErrUnsafe
		}
	}
	botEnabled := inv.Environment["DARK_PRO_ENABLE_OWNER_REPORT_BOT_V1"] == "1"
	botConfigured := o.OwnerReportConfig != ""
	if _, e := os.Lstat(filepath.Join(o.Root, "owner-report-bot")); e == nil {
		botConfigured = true
	} else if !errors.Is(e, os.ErrNotExist) {
		return inv, ErrUnsafe
	}
	if botEnabled || botConfigured {
		if o.OwnerReportConfig != "" && o.OwnerReportConfig != "/etc/dark-xray-pro-lab/owner-report-bot/config.json" || botEnabled && o.OwnerReportConfig == "" {
			return inv, ErrUnsafe
		}
		for _, r := range []struct{ role, path string }{{"owner_report_config", "/etc/dark-xray-pro-lab/owner-report-bot/config.json"}, {"owner_report_token", "/etc/dark-xray-pro-lab/owner-report-bot/token"}, {"owner_report_db", filepath.Join(o.Root, "owner-report-bot/inbox.sqlite")}} {
			if add(r.role, r.path, botEnabled) != nil {
				return inv, ErrUnsafe
			}
		}
	}
	sort.Slice(inv.Resources, func(i, j int) bool { return inv.Resources[i].Name < inv.Resources[j].Name })
	return inv, ValidateArchiveInventory(inv)
}

// ValidateArchiveInventory is purely structural: it NEVER opens original
// source paths from an archive. Restorers must use only fixed archive Names.
func ValidateArchiveInventory(inv Inventory) error {
	if inv.Version != 1 || !filepath.IsAbs(inv.Root) || filepath.Clean(inv.Root) != inv.Root || inv.Root == "/" || inv.CreatedUnix <= 0 || !revisionRE.MatchString(inv.SourceRevision) || len(inv.Resources) > 32 || len(inv.Environment) > len(knownEnvironment) {
		return ErrUnsafe
	}
	if len(inv.Flags) != 2 {
		return ErrUnsafe
	}
	for k, v := range inv.Flags {
		if k != "listen" && k != "agent-tls-listen" || len(v) > 256 || strings.ContainsAny(v, "\x00\r\n") {
			return ErrUnsafe
		}
	}
	raw := []string{}
	for k, v := range inv.Environment {
		if len(v) > 100000 || strings.ContainsRune(v, 0) {
			return ErrUnsafe
		}
		raw = append(raw, k+"="+v)
	}
	e, err := selectedEnvironment(raw)
	if err != nil || len(e) != len(inv.Environment) {
		return ErrUnsafe
	}
	for k, v := range e {
		if strings.HasPrefix(k, "DARK_PRO_ENABLE_") && v != "" && v != "0" && v != "1" {
			return ErrUnsafe
		}
	}
	if e[EnableEnv] != "1" || e["DARK_PRO_LAB_MODE"] != "1" || e["DARK_PRO_ENABLE_BUSINESS_CORE"] != "1" || len(e["DARK_PRO_LAB_TOKEN"]) < 32 {
		return ErrUnsafe
	}
	roles := map[string]Resource{}
	paths := map[string]bool{}
	for _, r := range inv.Resources {
		name, known := resources[r.Role]
		if !known || r.Name != name || !filepath.IsAbs(r.Path) || filepath.Clean(r.Path) != r.Path || strings.ContainsAny(r.Path, "\x00\r\n") {
			return ErrUnsafe
		}
		if _, exists := roles[r.Role]; exists || paths[r.Path] {
			return ErrUnsafe
		}
		roles[r.Role] = r
		paths[r.Path] = true
		external := r.Role == "agent_tls_cert" || r.Role == "agent_tls_key" || r.Role == "owner_report_config" || r.Role == "owner_report_token"
		if !external && !inside(inv.Root, r.Path) {
			return ErrUnsafe
		}
	}
	for _, role := range []string{"business", "registry", "replacements"} {
		if _, ok := roles[role]; !ok {
			return ErrUnsafe
		}
	}
	if !roles["business"].Required || roles["registry"].Required || roles["replacements"].Required {
		return ErrUnsafe
	}
	for _, r := range []struct {
		flag, role string
		required   bool
	}{{"DARK_PRO_ENABLE_NODE_HISTORY", "node_history", true}, {"DARK_PRO_ENABLE_CONFIG_STAGE", "config_stage", false}, {"DARK_PRO_ENABLE_LAB_REVISION_ROLLBACK", "lab_revisions", false}, {"DARK_PRO_ENABLE_INBOUND_EXECUTION", "inbound_runtime", true}, {"DARK_PRO_ENABLE_OWNER_REPORT_BOT_V1", "owner_report_db", true}, {"DARK_PRO_ENABLE_OWNER_REPORT_BOT_V1", "owner_report_config", true}, {"DARK_PRO_ENABLE_OWNER_REPORT_BOT_V1", "owner_report_token", true}} {
		v, present := roles[r.role]
		enabled := e[r.flag] == "1"
		if enabled && !present || present && v.Required != (enabled && r.required) {
			return ErrUnsafe
		}
	}
	_, hasCert := roles["agent_tls_cert"]
	_, hasKey := roles["agent_tls_key"]
	hasAgentListener := inv.Flags["agent-tls-listen"] != ""
	if hasCert != hasKey || hasAgentListener != (hasCert && hasKey) {
		return ErrUnsafe
	}
	if hasAgentListener && (!roles["agent_tls_key"].Required || !roles["agent_tls_cert"].Required || e["DARK_PRO_ENABLE_REMOTE_AGENT"] != "1") {
		return ErrUnsafe
	}
	if e["DARK_PRO_ENABLE_LAB_REVISION_ROLLBACK"] == "1" && e["DARK_PRO_ENABLE_CONFIG_STAGE"] != "1" {
		return ErrUnsafe
	}
	if r, ok := roles["inbound_runtime"]; ok {
		if r.Path != filepath.Join(e["DARK_PRO_INBOUND_RUNTIME_DIR"], "state.enc") {
			return ErrUnsafe
		}
		if e["DARK_PRO_ENABLE_INBOUND_EXECUTION"] == "1" && (e["DARK_PRO_ENABLE_NATIVE_INBOUNDS"] != "1" || e["DARK_PRO_ENABLE_USAGE_LEDGER"] != "1") {
			return ErrUnsafe
		}
	}
	if _, ok := roles["owner_report_db"]; ok {
		if roles["owner_report_config"].Path != "/etc/dark-xray-pro-lab/owner-report-bot/config.json" || roles["owner_report_token"].Path != "/etc/dark-xray-pro-lab/owner-report-bot/token" || roles["owner_report_db"].Path != filepath.Join(inv.Root, "owner-report-bot/inbox.sqlite") {
			return ErrUnsafe
		}
	}
	validKey := func(name string) bool {
		v := e[name]
		b, err := hex.DecodeString(v)
		return err == nil && len(b) == 32 && hex.EncodeToString(b) == v
	}
	if e["DARK_PRO_ENABLE_NATIVE_INBOUNDS"] == "1" || roles["inbound_runtime"].Present {
		if !validKey("DARK_PRO_NATIVE_INBOUND_KEY_HEX") {
			return ErrUnsafe
		}
	}
	if e["DARK_PRO_ENABLE_CONFIG_STAGE"] == "1" || roles["config_stage"].Present || roles["lab_revisions"].Present {
		if !validKey("DARK_PRO_STAGE_KEY_HEX") {
			return ErrUnsafe
		}
	}
	if inv.SnapshotFiles != nil {
		if len(inv.SnapshotFiles) > 4095 {
			return ErrUnsafe
		}
		var total int64
		files := map[string]bool{}
		for _, f := range inv.SnapshotFiles {
			r, ok := roles[f.Role]
			if !ok || f.Name != r.Name || !r.Present || f.Size < 0 || f.Size > maxRoleSize(f.Role) || total > maxSnapshotPayload-f.Size || files[f.Name] {
				return ErrUnsafe
			}
			total += f.Size
			files[f.Name] = true
		}
		for _, r := range inv.Resources {
			if r.Required && !r.Present || r.Present != files[r.Name] {
				return ErrUnsafe
			}
		}
		botConfig, botToken, botDB := roles["owner_report_config"].Present, roles["owner_report_token"].Present, roles["owner_report_db"].Present
		if botConfig != botToken || botDB && !botConfig {
			return ErrUnsafe
		}
	}
	return nil
}
