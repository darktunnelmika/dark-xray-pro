package hubcore

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
)

// A trial reseller panel runs for the owner's number of days. A day before
// the end its Telegram owners are reminded; at the end it is frozen: its
// users are switched off and it may not make, renew or edit any until it is
// charged. The first paid top-up (ApproveRepOrder) or the owner makes it a
// normal panel again and switches back on exactly the users the freeze
// switched off.

func (s *Store) startRepTrial(ctx context.Context, rep string, t RepTrial) {
	if t.Days > 0 {
		_ = s.SetSetting(ctx, "rep_trial_end:"+rep, strconv.FormatInt(s.now().Unix()+t.Days*86400, 10))
	}
	if t.MaxClients > 0 {
		if pol, err := s.RepPolicyOf(ctx, rep); err == nil {
			pol.MaxClients = t.MaxClients
			_ = s.SetRepPolicy(ctx, rep, pol)
			_ = s.SetSetting(ctx, "rep_trial_cap:"+rep, strconv.FormatInt(t.MaxClients, 10))
		}
	}
}

func (s *Store) delSetting(ctx context.Context, key string) {
	_, _ = s.db.ExecContext(ctx, `DELETE FROM settings WHERE key=?`, key)
}

// RepTrialState is a trial panel's state for its owner and the bot.
type RepTrialState struct {
	Trial  bool  `json:"trial"`
	End    int64 `json:"end,omitempty"` // 0 = no end
	Frozen bool  `json:"frozen,omitempty"`
}

func (s *Store) RepTrialStateOf(ctx context.Context, rep string) RepTrialState {
	if rep == "" || !s.IsTrialPanel(ctx, rep) {
		return RepTrialState{}
	}
	end, _ := strconv.ParseInt(s.Setting(ctx, "rep_trial_end:"+rep), 10, 64)
	return RepTrialState{Trial: true, End: end, Frozen: s.Setting(ctx, "rep_trial_frozen:"+rep) != ""}
}

// RepTrialsDue lists the trial panels to remind (ending within a day) and
// to freeze (ended).
func (s *Store) RepTrialsDue(ctx context.Context) (remind, freeze []string) {
	rows, err := s.ro.QueryContext(ctx, `SELECT key,value FROM settings WHERE key LIKE 'rep_trial_end:%'`)
	if err != nil {
		return nil, nil
	}
	type due struct {
		rep string
		end int64
	}
	var all []due
	for rows.Next() {
		var k, v string
		if rows.Scan(&k, &v) == nil {
			end, _ := strconv.ParseInt(v, 10, 64)
			all = append(all, due{strings.TrimPrefix(k, "rep_trial_end:"), end})
		}
	}
	rows.Close()
	now := s.now().Unix()
	for _, d := range all {
		st := s.RepTrialStateOf(ctx, d.rep)
		switch {
		case !st.Trial || st.Frozen || d.end == 0:
		case d.end <= now:
			freeze = append(freeze, d.rep)
		case d.end-now <= 86400 && s.Setting(ctx, "rep_trial_rem:"+d.rep) == "":
			remind = append(remind, d.rep)
		}
	}
	return remind, freeze
}

// RepTrialReminded records the day-before reminder.
func (s *Store) RepTrialReminded(ctx context.Context, rep string) {
	_ = s.SetSetting(ctx, "rep_trial_rem:"+rep, strconv.FormatInt(s.now().Unix(), 10))
}

type trialFreeze struct {
	Emails []string  `json:"emails"`
	Policy RepPolicy `json:"policy"`
}

// FreezeRepTrial ends a trial panel: its users go off and it may not make,
// renew or edit users until it is charged.
func (s *Store) FreezeRepTrial(ctx context.Context, rep string) error {
	st := s.RepTrialStateOf(ctx, rep)
	if !st.Trial || st.Frozen {
		return nil
	}
	pol, err := s.RepPolicyOf(ctx, rep)
	if err != nil {
		return err
	}
	var emails []string
	rows, err := s.ro.QueryContext(ctx, `SELECT email FROM clients WHERE rep_id=? AND enabled=1`, rep)
	if err != nil {
		return err
	}
	for rows.Next() {
		var e string
		if rows.Scan(&e) == nil {
			emails = append(emails, e)
		}
	}
	rows.Close()
	b, _ := json.Marshal(trialFreeze{Emails: emails, Policy: pol})
	if err := s.SetSetting(ctx, "rep_trial_frozen:"+rep, string(b)); err != nil {
		return err
	}
	locked := pol
	locked.DenyCreate, locked.DenyRenew, locked.DenyEdit = true, true, true
	if err := s.SetRepPolicy(ctx, rep, locked); err != nil {
		return err
	}
	if len(emails) > 0 {
		off := false
		if _, err := s.BulkExtend(ctx, BulkRequest{Emails: emails, Enable: &off}); err != nil {
			return err
		}
	}
	return nil
}

// ConvertRepTrial makes a trial panel a normal one: the trial's end, its
// user cap and, when frozen, the freeze are lifted.
func (s *Store) ConvertRepTrial(ctx context.Context, rep string) error {
	st := s.RepTrialStateOf(ctx, rep)
	if !st.Trial {
		return nil
	}
	if v := s.Setting(ctx, "rep_trial_frozen:"+rep); v != "" {
		var f trialFreeze
		if json.Unmarshal([]byte(v), &f) == nil {
			if err := s.SetRepPolicy(ctx, rep, f.Policy); err != nil {
				return err
			}
			if len(f.Emails) > 0 {
				on := true
				if _, err := s.BulkExtend(ctx, BulkRequest{Emails: f.Emails, Enable: &on}); err != nil {
					return err
				}
			}
		}
	}
	if capS := s.Setting(ctx, "rep_trial_cap:"+rep); capS != "" {
		if pol, err := s.RepPolicyOf(ctx, rep); err == nil && strconv.FormatInt(pol.MaxClients, 10) == capS {
			pol.MaxClients = 0
			_ = s.SetRepPolicy(ctx, rep, pol)
		}
	}
	for _, k := range []string{"rep_trial_of:", "rep_trial_end:", "rep_trial_frozen:", "rep_trial_rem:", "rep_trial_cap:"} {
		s.delSetting(ctx, k+rep)
	}
	return nil
}

// RepTrialStats counts the trial panels for the owner.
func (s *Store) RepTrialStats(ctx context.Context) map[string]int {
	out := map[string]int{"running": 0, "frozen": 0}
	rows, err := s.ro.QueryContext(ctx, `SELECT key FROM settings WHERE key LIKE 'rep_trial_of:%'`)
	if err != nil {
		return out
	}
	var reps []string
	for rows.Next() {
		var k string
		if rows.Scan(&k) == nil {
			reps = append(reps, strings.TrimPrefix(k, "rep_trial_of:"))
		}
	}
	rows.Close()
	for _, r := range reps {
		if s.RepTrialStateOf(ctx, r).Frozen {
			out["frozen"]++
		} else {
			out["running"]++
		}
	}
	return out
}
