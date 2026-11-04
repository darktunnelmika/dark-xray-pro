package main

import (
	"testing"

	"github.com/darktunnelmika/dark-xray-pro/internal/telegrambot"
)

func TestTelegramBotsRequireIndependentExplicitLABConfiguration(t *testing.T) {
	base := map[string]string{telegrambot.EnableEnv: "1", "DARK_PRO_LAB_MODE": "1", "DARK_PRO_ENABLE_BUSINESS_CORE": "1", "DARK_PRO_ENABLE_USAGE_LEDGER": "1"}
	for _, tc := range []struct {
		name, key, value, path string
		want                   bool
		fail                   bool
	}{
		{"valid", "", "", telegrambot.LABConfigPath, true, false},
		{"disabled", telegrambot.EnableEnv, "0", "", false, false},
		{"disabled-with-secret", telegrambot.EnableEnv, "0", telegrambot.LABConfigPath, false, true},
		{"invalid-flag", telegrambot.EnableEnv, "yes", "", false, true},
		{"no-lab", "DARK_PRO_LAB_MODE", "0", telegrambot.LABConfigPath, false, true},
		{"no-business", "DARK_PRO_ENABLE_BUSINESS_CORE", "0", telegrambot.LABConfigPath, false, true},
		{"no-usage", "DARK_PRO_ENABLE_USAGE_LEDGER", "0", telegrambot.LABConfigPath, false, true},
		{"foreign-path", "", "", "/etc/legacy/bot.json", false, true},
		{"missing-path", "", "", "", false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			get := func(k string) string {
				if k == tc.key {
					return tc.value
				}
				return base[k]
			}
			got, err := telegramBotsEnabled(get, tc.path)
			if got != tc.want || (err != nil) != tc.fail {
				t.Fatalf("enabled=%v error=%v", got, err)
			}
		})
	}
}
