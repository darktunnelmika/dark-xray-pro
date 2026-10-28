//go:build linux

package telegramhub

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"
)

type ownerBotFileConfig struct {
	Version       int    `json:"version"`
	Purpose       string `json:"purpose"`
	ExpectedBotID int64  `json:"expected_bot_id"`
	OwnerUserID   int64  `json:"owner_user_id"`
	OwnerChatID   int64  `json:"owner_chat_id"`
	BotUsername   string `json:"bot_username"`
	TokenFile     string `json:"token_file"`
	JournalFile   string `json:"journal_file"`
}

// OpenLABOwnerReportBot is the only production constructor. Disabled returns
// before all filesystem/network I/O and before inspecting the reporter. Enabled
// opens a separate private delivery journal; it does not contact Telegram.
// Run verifies getMe before processing persisted work or starting polling.
// Startup owns the shared, already-open business Store passed as reporter.
func OpenLABOwnerReportBot(options LABOwnerReportBotOptions, reporter RepresentativeReporter) (*OwnerReportWorker, error) {
	if options.EnableFlag == "" || options.EnableFlag == "0" {
		return nil, nil
	}
	if options.EnableFlag != "1" || !options.LABMode || !options.BusinessEnabled ||
		options.ConfigPath != OwnerReportLABConfigPath || reporter == nil {
		return nil, ErrBotLABConfig
	}
	data, err := readPrivateBotFile(options.ConfigPath, 4096)
	if err != nil {
		return nil, ErrBotLABConfig
	}
	config, err := parseOwnerBotFileConfig(data)
	if err != nil {
		return nil, err
	}
	tokenBytes, err := readPrivateBotFile(config.TokenFile, 256)
	if err != nil {
		return nil, ErrBotLABConfig
	}
	// A single final LF is convenient for a dedicated secret file. Other
	// whitespace, extra lines and raw environment substitutions are rejected.
	token := string(bytes.TrimSuffix(tokenBytes, []byte("\n")))
	if !botTokenFormat.MatchString(token) {
		return nil, ErrBotLABConfig
	}
	return openOwnerReportWorker(config.binding(), token, config.JournalFile, reporter, nil)
}

func (c ownerBotFileConfig) binding() botBinding {
	return botBinding{BotID: c.ExpectedBotID, OwnerUserID: c.OwnerUserID, OwnerChatID: c.OwnerChatID,
		BotUsername: c.BotUsername}
}

func parseOwnerBotFileConfig(data []byte) (ownerBotFileConfig, error) {
	var config ownerBotFileConfig
	var fields map[string]json.RawMessage
	if len(data) > 4096 || decodeBotJSON(data, &fields) != nil || len(fields) != 8 {
		return config, ErrBotLABConfig
	}
	for _, key := range []string{"version", "purpose", "expected_bot_id", "owner_user_id", "owner_chat_id",
		"bot_username", "token_file", "journal_file"} {
		value, exists := fields[key]
		if !exists || bytes.Equal(value, []byte("null")) {
			return config, ErrBotLABConfig
		}
	}
	if json.Unmarshal(data, &config) != nil || config.Version != 1 || config.Purpose != "isolated-pro-lab-owner-reports" ||
		config.ExpectedBotID <= 0 || config.OwnerUserID <= 0 || config.OwnerChatID <= 0 ||
		!botUsernamePattern.MatchString(config.BotUsername) || config.TokenFile != OwnerReportLABTokenPath ||
		config.JournalFile != OwnerReportLABInboxPath {
		return ownerBotFileConfig{}, ErrBotLABConfig
	}
	return config, nil
}

func readPrivateBotFile(path string, limit int64) ([]byte, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path || limit < 1 || privateBotParent(path) != nil {
		return nil, ErrBotLABConfig
	}
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		return nil, ErrBotLABConfig
	}
	file := os.NewFile(uintptr(fd), "private-owner-bot-file")
	defer file.Close()
	var stat unix.Stat_t
	if unix.Fstat(fd, &stat) != nil || stat.Mode&unix.S_IFMT != unix.S_IFREG ||
		stat.Mode&0077 != 0 || stat.Mode&0111 != 0 || stat.Uid != uint32(os.Geteuid()) ||
		stat.Nlink != 1 || stat.Size < 1 || stat.Size > limit {
		return nil, ErrBotLABConfig
	}
	data, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil || len(data) == 0 || int64(len(data)) > limit {
		return nil, ErrBotLABConfig
	}
	return data, nil
}

func privateBotParent(path string) error {
	parent := filepath.Dir(path)
	current := string(filepath.Separator)
	for _, part := range strings.Split(strings.TrimPrefix(parent, "/"), "/") {
		if part == "" {
			continue
		}
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return ErrBotLABConfig
		}
	}
	var stat unix.Stat_t
	if unix.Lstat(parent, &stat) != nil || stat.Uid != uint32(os.Geteuid()) || stat.Mode&0077 != 0 {
		return ErrBotLABConfig
	}
	return nil
}
