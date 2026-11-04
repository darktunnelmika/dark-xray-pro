package telegrambot

const maxManagedInteger int64 = 1<<53 - 1

// BotIdentity is obtained from Telegram getMe, never from a supplied username.
// It contains no credential or transport details.
type BotIdentity struct {
	BotID    int64  `json:"bot_id"`
	Username string `json:"username"`
}

type ManagedConnection struct {
	BotID      int64  `json:"bot_id"`
	Configured bool   `json:"configured"`
	Username   string `json:"username"`
	State      string `json:"state"`
	Problem    string `json:"problem,omitempty"`
}

type ManagementStatus struct {
	Revision    int64               `json:"revision"`
	Connections []ManagedConnection `json:"connections"`
}
