package telegramhub

import "errors"

const (
	OwnerReportLABEnableEnv  = "DARK_PRO_ENABLE_OWNER_REPORT_BOT_V1"
	OwnerReportLABConfigPath = "/etc/dark-xray-pro-lab/owner-report-bot/config.json"
	OwnerReportLABTokenPath  = "/etc/dark-xray-pro-lab/owner-report-bot/token"
	OwnerReportLABInboxPath  = "/var/lib/dark-xray-pro-lab/owner-report-bot/inbox.sqlite"
)

var (
	ErrBotLABConfig   = errors.New("invalid private PRO LAB owner report bot configuration")
	ErrBotWorkerState = errors.New("HUB owner report bot worker is not available")
	ErrBotUnsupported = errors.New("HUB owner report bot requires Linux")
)

// LABOwnerReportBotOptions is supplied by HUB startup. EnableFlag must be the
// dedicated flag, never a general Telegram/bot token environment variable.
// ConfigPath is explicit and must equal OwnerReportLABConfigPath when enabled.
type LABOwnerReportBotOptions struct {
	EnableFlag      string
	LABMode         bool
	BusinessEnabled bool
	ConfigPath      string
}
