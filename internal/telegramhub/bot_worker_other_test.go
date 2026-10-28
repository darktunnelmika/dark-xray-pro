//go:build !linux

package telegramhub

import "testing"

func TestOwnerBotUnsupportedPlatformKeepsDefaultDisabled(t *testing.T) {
	for _, flag := range []string{"", "0"} {
		worker, err := OpenLABOwnerReportBot(LABOwnerReportBotOptions{EnableFlag: flag, ConfigPath: "/must/not/be/read"}, nil)
		if err != nil || worker != nil {
			t.Fatal("disabled portable startup changed", err)
		}
	}
	if worker, err := OpenLABOwnerReportBot(LABOwnerReportBotOptions{EnableFlag: "1"}, nil); err != ErrBotUnsupported || worker != nil {
		t.Fatal("unsupported platform enabled transport", err)
	}
}
