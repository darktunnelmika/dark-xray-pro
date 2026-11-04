package darkrestore

import uc "github.com/darktunnelmika/dark-xray-pro/internal/usagecheckpoint"

// LocalSourceSettlement describes exactly one closed source epoch. It never
// authorizes destination activation or attests to other serving sources.
type LocalSourceSettlement struct {
	Scope        string     `json:"scope"`
	SourceID     string     `json:"source_id"`
	SourceSHA    string     `json:"source_sha256"`
	AuthoritySHA string     `json:"authority_sha256"`
	JournalSHA   string     `json:"journal_sha256"`
	Terminal     uc.Receipt `json:"terminal_receipt"`
	PriorBytes   int64      `json:"pre_epoch_bytes,string"`
	Up           int64      `json:"epoch_uplink_bytes,string"`
	Down         int64      `json:"epoch_downlink_bytes,string"`
	Historical   int64      `json:"settled_historical_bytes,string"`
	Remaining    int64      `json:"finite_quota_remaining_bytes,string"`
}
