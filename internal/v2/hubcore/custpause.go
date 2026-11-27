package hubcore

import (
	"context"
	"fmt"
	"strconv"
)

// The customer's own on/off for one of its services: a pause turns the
// service off (its time keeps running); only a pause the customer made can
// be undone by the customer — a service the owner or a reseller turned off
// stays off.

func pauseKey(email string) string { return "cpause:" + email }

// CustomerPaused reports whether the customer turned the service off itself.
func (s *Store) CustomerPaused(ctx context.Context, email string) bool {
	return s.Setting(ctx, pauseKey(email)) != ""
}

// CustomerSetOn pauses (on=false) or resumes (on=true) a service for its
// customer.
func (s *Store) CustomerSetOn(ctx context.Context, email string, on bool) error {
	c, err := s.ClientByEmail(ctx, email)
	if err != nil {
		return err
	}
	paused := s.CustomerPaused(ctx, email)
	switch {
	case on && c.Enabled:
		s.delSetting(ctx, pauseKey(email))
		return nil
	case on && !paused:
		return fmt.Errorf("%w: this service was turned off by support", ErrPolicy)
	case !on && !c.Enabled:
		return nil
	}
	if _, err := s.BulkExtend(ctx, BulkRequest{Emails: []string{email}, Enable: &on}); err != nil {
		return err
	}
	if on {
		s.delSetting(ctx, pauseKey(email))
		return nil
	}
	return s.SetSetting(ctx, pauseKey(email), strconv.FormatInt(s.now().Unix(), 10))
}
