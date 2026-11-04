package main

import (
	"errors"

	"github.com/darktunnelmika/dark-xray-pro/internal/telegrambot"
)

// Validate the opt-in before opening any store or touching a credential file.
func telegramBotsEnabled(getenv func(string) string, configPath string) (bool, error) {
	flag := getenv(telegrambot.EnableEnv)
	if flag != "" && flag != "0" && flag != "1" {
		return false, errors.New("DARK_PRO_ENABLE_TELEGRAM_BOTS_V1 must be 0 or 1")
	}
	if flag != "1" {
		if configPath != "" {
			return false, errors.New("Telegram bot configuration requires its explicit enable switch")
		}
		return false, nil
	}
	if getenv("DARK_PRO_LAB_MODE") != "1" || getenv("DARK_PRO_ENABLE_BUSINESS_CORE") != "1" || getenv("DARK_PRO_ENABLE_USAGE_LEDGER") != "1" {
		return false, errors.New("Telegram commerce requires isolated PRO LAB, native business storage and authoritative usage accounting")
	}
	if configPath != telegrambot.LABConfigPath {
		return false, errors.New("Telegram commerce requires its fixed private LAB configuration")
	}
	return true, nil
}
