//go:build linux

package hubstate

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestTelegramInventoryPinsPairAndAccounting(t *testing.T) {
	o := fixtureOptions(privateTemp(t))
	o.TelegramBotsConfig = "/etc/dark-xray-pro-lab/telegram-bots/config.json"
	o.Environment = append(o.Environment, "DARK_PRO_ENABLE_TELEGRAM_BOTS_V1=1", "DARK_PRO_ENABLE_USAGE_LEDGER=1")
	inv, err := BuildInventory(o)
	if err != nil {
		t.Fatal(err)
	}
	found := map[string]Resource{}
	for _, r := range inv.Resources {
		found[r.Role] = r
	}
	if !found["telegram_bot_config"].Required || !found["telegram_bot_db"].Required {
		t.Fatal("missing required commerce recovery pair")
	}
	if found["telegram_bot_db"].Path != filepath.Join(o.Root, "telegram-bots/journal.sqlite") {
		t.Fatal("unscoped journal")
	}
	for _, change := range []string{"usage", "path", "pair"} {
		t.Run(change, func(t *testing.T) {
			copy := inv
			copy.Resources = append([]Resource(nil), inv.Resources...)
			copy.Environment = map[string]string{}
			for k, v := range inv.Environment {
				copy.Environment[k] = v
			}
			switch change {
			case "usage":
				copy.Environment["DARK_PRO_ENABLE_USAGE_LEDGER"] = "0"
			case "path":
				for i := range copy.Resources {
					if copy.Resources[i].Role == "telegram_bot_db" {
						copy.Resources[i].Path = filepath.Join(o.Root, "wrong.sqlite")
					}
				}
			case "pair":
				for i := range copy.Resources {
					if copy.Resources[i].Role == "telegram_bot_config" {
						copy.Resources = append(copy.Resources[:i], copy.Resources[i+1:]...)
						break
					}
				}
			}
			if ValidateArchiveInventory(copy) == nil {
				t.Fatal("unsafe recovery inventory accepted")
			}
		})
	}
}

func TestBackupFencesBothIndependentTelegramWorkers(t *testing.T) {
	root := privateTemp(t)
	inv := Inventory{}
	for _, role := range []string{"owner_report_db", "telegram_bot_db"} {
		path := filepath.Join(root, role+".sqlite")
		if err := os.WriteFile(path, []byte("synthetic"), 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path+".lock", nil, 0600); err != nil {
			t.Fatal(err)
		}
		inv.Resources = append(inv.Resources, Resource{Role: role, Path: path, Required: true})
	}
	second, err := os.OpenFile(inv.Resources[1].Path+".lock", os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	if err = syscall.Flock(int(second.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		t.Fatal(err)
	}
	if release, err := holdBotStopped(inv); !errors.Is(err, ErrBusy) {
		if release != nil {
			release()
		}
		t.Fatal("running second bot not fenced", err)
	}
	first, err := os.OpenFile(inv.Resources[0].Path+".lock", os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	if err = syscall.Flock(int(first.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		t.Fatal("failed acquisition leaked first lock", err)
	}
	_ = syscall.Flock(int(first.Fd()), syscall.LOCK_UN)
	_ = syscall.Flock(int(second.Fd()), syscall.LOCK_UN)
	release, err := holdBotStopped(inv)
	if err != nil {
		t.Fatal(err)
	}
	if err = syscall.Flock(int(second.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err == nil {
		release()
		t.Fatal("second journal unlocked during snapshot")
	}
	release()
	if err = syscall.Flock(int(second.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		t.Fatal("release left second lock held", err)
	}
	_ = syscall.Flock(int(second.Fd()), syscall.LOCK_UN)
}
