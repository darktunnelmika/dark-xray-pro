package tgbot

import (
	"errors"
	"fmt"
	"testing"

	"github.com/darktunnelmika/dark-xray-pro/internal/v2/hubcore"
)

func TestFaErr(t *testing.T) {
	if got := faErr(fmt.Errorf("%w: the client limit (25) is reached", hubcore.ErrPolicy)); got != "به سقف تعداد کاربر (25) رسیده‌اید." {
		t.Fatal(got)
	}
	if got := faErr(errors.New("something else")); got != "something else" {
		t.Fatal(got)
	}
}
