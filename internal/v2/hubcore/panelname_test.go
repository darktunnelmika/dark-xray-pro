package hubcore

import (
	"context"
	"testing"
)

func TestPanelName(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()
	if s.PanelName(ctx) != "Dark Xray Pro" {
		t.Fatal("default name")
	}
	if err := s.SetPanelName(ctx, "  My   Panel "); err != nil || s.PanelName(ctx) != "My Panel" {
		t.Fatal("set", err, s.PanelName(ctx))
	}
	if err := s.SetPanelName(ctx, "<b>x</b>"); err == nil {
		t.Fatal("markup must be refused")
	}
	if err := s.SetPanelName(ctx, ""); err != nil || s.PanelName(ctx) != "Dark Xray Pro" {
		t.Fatal("empty goes back to the default")
	}
}
