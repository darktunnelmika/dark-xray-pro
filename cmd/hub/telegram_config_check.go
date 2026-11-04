package main

import (
	"encoding/json"
	"io"
	"os"
	"os/user"
	"runtime"
	"strconv"

	"github.com/darktunnelmika/dark-xray-pro/internal/telegrambot"
)

// Deliberately separate from runtime startup: checking a private config must
// never initialize the business database, Telegram journal, workers or network.
type telegramConfigCheckHooks struct {
	getenv     func(string) string
	euid       func() int
	egid       func() int
	lookupUser func(string) (*user.User, error)
	check      func() (int, error)
}

func telegramConfigCheckRuntime() telegramConfigCheckHooks {
	return telegramConfigCheckHooks{
		getenv: os.Getenv, euid: os.Geteuid, egid: os.Getegid,
		lookupUser: user.Lookup, check: telegrambot.CheckLABConfig,
	}
}

type telegramConfigCheckReport struct {
	Scope          string            `json:"scope"`
	Status         string            `json:"status"`
	Code           string            `json:"code"`
	ConfiguredBots *int              `json:"configured_bots,omitempty"`
	Activation     string            `json:"activation"`
	Acceptance     map[string]string `json:"acceptance"`
}

func runTelegramConfigCheck(args []string, output io.Writer, hooks telegramConfigCheckHooks) int {
	report := telegramConfigCheckReport{
		Scope: "private_config_only", Status: "blocked", Activation: "not_performed",
		Acceptance: map[string]string{
			"bot_identity": "unknown", "exclusive_transport": "unknown",
			"runtime_feature_flags": "unknown", "store_registration": "unknown",
			"native_delivery": "unknown", "stars_payment_and_refund": "unknown",
		},
	}
	finish := func(code string, exit int) int {
		report.Code = code
		if json.NewEncoder(output).Encode(report) != nil {
			return 2
		}
		return exit
	}
	// No optional arguments: credentials, file paths and tokens are never
	// accepted on argv. Errors must not echo caller input or parser errors.
	if len(args) != 0 {
		return finish("invalid_arguments", 2)
	}
	if runtime.GOOS != "linux" {
		return finish("linux_required", 2)
	}
	if hooks.getenv("DARK_PRO_LAB_MODE") != "1" {
		return finish("lab_mode_required", 2)
	}
	uid, gid := hooks.euid(), hooks.egid()
	if uid <= 0 || gid <= 0 {
		return finish("service_identity_required", 2)
	}
	account, err := hooks.lookupUser("darkprolab")
	if err != nil || account == nil || account.Username != "darkprolab" {
		return finish("service_identity_required", 2)
	}
	accountUID, uidErr := strconv.Atoi(account.Uid)
	accountGID, gidErr := strconv.Atoi(account.Gid)
	if uidErr != nil || gidErr != nil || accountUID != uid || accountGID != gid {
		return finish("service_identity_required", 2)
	}
	count, err := hooks.check()
	if err != nil || count < 0 || count > 16 {
		return finish("private_config_refused", 1)
	}
	report.Status = "passed"
	report.ConfiguredBots = &count
	return finish("private_config_valid", 0)
}
