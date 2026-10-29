package main

import (
	"errors"

	"github.com/darktunnelmika/dark-xray-pro/internal/api"
	"github.com/darktunnelmika/dark-xray-pro/internal/backupcontrol"
)

func backupControlEnabled(getenv func(string) string) (bool, error) {
	flag := getenv("DARK_PRO_ENABLE_BACKUP_CONTROL_V1")
	if flag == "" || flag == "0" {
		return false, nil
	}
	if flag != "1" {
		return false, errors.New("DARK_PRO_ENABLE_BACKUP_CONTROL_V1 must be 0 or 1")
	}
	for _, key := range []string{"DARK_PRO_LAB_MODE", "DARK_PRO_ENABLE_BUSINESS_CORE", "DARK_PRO_ENABLE_OFFLINE_BACKUP_V1"} {
		if getenv(key) != "1" {
			return false, errors.New("backup controls require isolated LAB, native business and offline backup inventory")
		}
	}
	return true, nil
}

func configureBackupControl(service *api.Server, enabled bool) error {
	if !enabled {
		return nil
	}
	client := backupcontrol.NewClient(backupcontrol.SocketPath)
	return service.EnableBackupControlV1(client)
}
