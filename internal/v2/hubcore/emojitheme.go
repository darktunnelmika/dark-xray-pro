package hubcore

import (
	"context"
	"encoding/json"
	"sync"
	"time"
)

// The bots' animated emoji: plain emoji → premium custom emoji id, set by
// the owner from the bot (/emoji) or the panel.

var emojiCache struct {
	sync.Mutex
	at  time.Time
	val map[string]string
}

func (s *Store) EmojiTheme(ctx context.Context) map[string]string {
	emojiCache.Lock()
	defer emojiCache.Unlock()
	if time.Since(emojiCache.at) < 20*time.Second && emojiCache.val != nil {
		return emojiCache.val
	}
	m := map[string]string{}
	_ = json.Unmarshal([]byte(s.Setting(ctx, "tg_emoji")), &m)
	emojiCache.val, emojiCache.at = m, time.Now()
	return m
}

// AddEmoji merges new pairs (an empty map clears the theme when reset).
func (s *Store) AddEmoji(ctx context.Context, add map[string]string, reset bool) (int, error) {
	m := map[string]string{}
	if !reset {
		_ = json.Unmarshal([]byte(s.Setting(ctx, "tg_emoji")), &m)
	}
	for k, v := range add {
		if len(k) > 0 && len(k) <= 40 && len(v) > 0 && len(v) <= 32 {
			m[k] = v
		}
	}
	if len(m) > 300 {
		return len(m), ErrInvalid
	}
	raw, _ := json.Marshal(m)
	if err := s.SetSetting(ctx, "tg_emoji", string(raw)); err != nil {
		return 0, err
	}
	emojiCache.Lock()
	emojiCache.val, emojiCache.at = m, time.Now()
	emojiCache.Unlock()
	return len(m), nil
}

// DelEmoji removes one plain emoji from the theme.
func (s *Store) DelEmoji(ctx context.Context, key string) error {
	m := map[string]string{}
	_ = json.Unmarshal([]byte(s.Setting(ctx, "tg_emoji")), &m)
	delete(m, key)
	raw, _ := json.Marshal(m)
	emojiCache.Lock()
	emojiCache.val, emojiCache.at = m, time.Now()
	emojiCache.Unlock()
	return s.SetSetting(ctx, "tg_emoji", string(raw))
}
