//go:build linux

package telegrambot

import (
	"context"
	"errors"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

type managedLoop struct {
	cancel context.CancelFunc
	done   chan struct{}
}

// Manager owns the private credentials and one shared journal for all managed
// bots. Management operations are serialized separately from status reads and
// independently running bot loops. It never exports tokens in its snapshots.
type Manager struct {
	operation chan struct{}
	mu        sync.Mutex
	config    fileConfig
	digest    [32]byte
	path      string
	transport http.RoundTripper
	worker    *Worker
	loops     map[int64]*managedLoop
	life      context.Context
	cancel    context.CancelFunc
	started   bool
	stopping  bool
	closed    bool
	done      chan struct{}
	runError  error
	closeOnce sync.Once
	closeErr  error
}

// OpenManager performs no network I/O. Enabled startup materializes a real,
// empty managed configuration/journal pair so panel onboarding needs no token
// file supplied by the operator. Credentials use only the fixed LAB path.
func OpenManager(options Options, backend Backend) (*Manager, error) {
	if options.EnableFlag == "" || options.EnableFlag == "0" {
		return nil, nil
	}
	if options.EnableFlag != "1" || !options.LABMode || options.ConfigPath != LABConfigPath || backend == nil {
		return nil, ErrConfig
	}
	return openManager(LABConfigPath, LABJournalPath, backend, nil)
}

// Alternate paths and transport are restricted to package tests.
func openManager(configPath, journalPath string, backend Backend, transport http.RoundTripper) (*Manager, error) {
	if backend == nil || ensureManagedParent(configPath) != nil || ensureManagedParent(journalPath) != nil {
		return nil, ErrConfig
	}
	config, digest, exists, err := readManagedConfig(configPath)
	if err != nil {
		return nil, err
	}
	if !exists {
		config = fileConfig{Version: 2, Purpose: ConfigPurpose, Revision: 1, Bots: []botConfig{}}
	} else if config.Version == 2 {
		// A managed file is always published after initial journal creation.
		// Recreating a missing journal could erase previously captured money.
		if _, err := os.Lstat(journalPath); err != nil {
			return nil, ErrJournal
		}
	}
	ids := make([]int64, 0, len(config.Bots))
	apis := make(map[int64]*api, len(config.Bots))
	for _, bot := range config.Bots {
		a, err := newAPI(bot.BotID, bot.Token, transport)
		if err != nil {
			for _, a := range apis {
				a.close()
			}
			return nil, ErrConfig
		}
		apis[bot.BotID] = a
		ids = append(ids, bot.BotID)
	}
	j, err := openManagedJournal(journalPath, ids)
	if err != nil {
		for _, a := range apis {
			a.close()
		}
		return nil, err
	}
	if !exists {
		digest, _, err = publishManagedConfig(configPath, config, nil)
		if err != nil {
			j.Close()
			return nil, err
		}
	}
	life, cancel := context.WithCancel(context.Background())
	w := &Worker{backend: backend, journal: j, apis: apis, statuses: map[int64]BotStatus{}}
	return &Manager{operation: make(chan struct{}, 1), config: config, digest: digest, path: configPath, transport: transport,
		worker: w, loops: map[int64]*managedLoop{}, life: life, cancel: cancel, done: make(chan struct{})}, nil
}

func (m *Manager) begin(ctx context.Context) (context.Context, func(), error) {
	if m == nil || ctx == nil {
		return nil, nil, ErrState
	}
	select {
	case <-ctx.Done():
		return nil, nil, ErrState
	case <-m.life.Done():
		return nil, nil, ErrState
	case m.operation <- struct{}{}:
	}
	m.mu.Lock()
	stopped := m.closed || m.stopping || m.life.Err() != nil
	m.mu.Unlock()
	if stopped || ctx.Err() != nil {
		<-m.operation
		return nil, nil, ErrState
	}
	opCtx, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(m.life, cancel)
	return opCtx, func() { stop(); cancel(); <-m.operation }, nil
}

func (m *Manager) candidate(ctx context.Context, token string, expected int64) (*api, BotIdentity, error) {
	if !tokenPattern.MatchString(token) {
		return nil, BotIdentity{}, ErrConfig
	}
	id, err := strconv.ParseInt(strings.SplitN(token, ":", 2)[0], 10, 64)
	if err != nil || id <= 0 || id > maxManagedInteger {
		return nil, BotIdentity{}, ErrConfig
	}
	if expected != 0 && id != expected {
		return nil, BotIdentity{}, ErrIdentity
	}
	a, err := newAPI(id, token, m.transport)
	if err != nil {
		return nil, BotIdentity{}, ErrConfig
	}
	verifyCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	if err := a.verify(verifyCtx); err != nil {
		a.close()
		return nil, BotIdentity{}, err
	}
	return a, BotIdentity{BotID: id, Username: a.username}, nil
}

// Verify calls only getMe and getWebhookInfo on the fixed Telegram origin.
// It does not persist credentials, delete webhooks or consume updates.
func (m *Manager) Verify(ctx context.Context, token string) (BotIdentity, error) {
	ctx, finish, err := m.begin(ctx)
	if err != nil {
		return BotIdentity{}, err
	}
	defer finish()
	a, identity, err := m.candidate(ctx, token, 0)
	if a != nil {
		a.close()
	}
	return identity, err
}

func (m *Manager) Check(ctx context.Context, botID int64) (BotIdentity, error) {
	ctx, finish, err := m.begin(ctx)
	if err != nil {
		return BotIdentity{}, err
	}
	defer finish()
	m.mu.Lock()
	var token string
	for _, bot := range m.config.Bots {
		if bot.BotID == botID {
			token = bot.Token
			break
		}
	}
	m.mu.Unlock()
	if token == "" {
		return BotIdentity{}, ErrConfig
	}
	a, identity, err := m.candidate(ctx, token, botID)
	if a != nil {
		a.close()
	}
	return identity, err
}

func (m *Manager) Configure(ctx context.Context, botID int64, token string, expectedRevision int64) (ManagementStatus, error) {
	return m.ConfigureAuthorized(ctx, botID, token, expectedRevision, nil)
}

// ConfigureAuthorized rechecks caller authority after network verification and
// after the old loop has joined, immediately before publishing credentials.
// A guard failure is redacted. Empty token retries the stored credential.
func (m *Manager) ConfigureAuthorized(ctx context.Context, botID int64, token string, expectedRevision int64, guard func(context.Context) error) (ManagementStatus, error) {
	ctx, finish, err := m.begin(ctx)
	if err != nil {
		return ManagementStatus{}, err
	}
	defer finish()
	m.mu.Lock()
	before := m.config
	before.Bots = append([]botConfig{}, m.config.Bots...)
	expectedDigest := m.digest
	m.mu.Unlock()
	if expectedRevision != before.Revision {
		return m.Management(), ErrConflict
	}
	if botID <= 0 || botID > maxManagedInteger || before.Revision >= maxManagedInteger {
		return m.Management(), ErrConfig
	}
	index := -1
	for i, bot := range before.Bots {
		if bot.BotID == botID {
			index = i
			if token == "" {
				token = bot.Token
			}
			break
		}
	}
	if index < 0 && len(before.Bots) >= maxBots {
		return m.Management(), ErrCapacity
	}
	a, identity, err := m.candidate(ctx, token, botID)
	if err != nil {
		return m.Management(), err
	}
	retained := false
	defer func() {
		if !retained {
			a.close()
		}
	}()
	next := fileConfig{Version: 2, Purpose: ConfigPurpose, Revision: before.Revision + 1, Bots: append([]botConfig{}, before.Bots...)}
	bot := botConfig{BotID: botID, Token: token, Username: identity.Username}
	if index < 0 {
		next.Bots = append(next.Bots, bot)
	} else {
		next.Bots[index] = bot
	}
	sort.Slice(next.Bots, func(i, j int) bool { return next.Bots[i].BotID < next.Bots[j].BotID })
	m.stopBot(botID)
	// Canceled input processing resumes from durable state. An interrupted
	// dispatch is retained as unknown and never silently retried.
	if index >= 0 {
		recoverCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		err := m.worker.journal.recoverManagedBot(recoverCtx, botID)
		cancel()
		if err != nil {
			m.worker.status(botID, "error", 0)
			m.worker.problem(botID, "journal_unavailable")
			return m.Management(), err
		}
	}
	resume := func() {
		if index >= 0 {
			m.resumeBot(before.Bots[index])
		}
	}
	if ctx.Err() != nil {
		resume()
		return m.Management(), ErrState
	}
	if guard != nil && guard(ctx) != nil {
		resume()
		return m.Management(), ErrAccess
	}
	if ctx.Err() != nil {
		resume()
		return m.Management(), ErrState
	}
	digest, published, err := publishManagedConfig(m.path, next, &expectedDigest)
	if !published {
		resume()
		return m.Management(), err
	}
	m.mu.Lock()
	m.config, m.digest = next, digest
	m.worker.mu.Lock()
	old := m.worker.apis[botID]
	m.worker.apis[botID] = a
	m.worker.mu.Unlock()
	m.mu.Unlock()
	if old != nil {
		old.close()
	}
	retained = true
	if err != nil {
		m.worker.status(botID, "error", 0)
		m.worker.problem(botID, "connection_unavailable")
		return m.Management(), err
	}
	// Publish first: if this transaction fails, restart/retry can safely add
	// the never-polled cursor from the saved config. The reverse order could
	// leave an empty onboarding config with unexplained historical bot IDs.
	commitCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	err = m.worker.journal.ensureManagedBot(commitCtx, botID)
	cancel()
	if err != nil {
		m.worker.status(botID, "error", 0)
		m.worker.problem(botID, "journal_unavailable")
		return m.Management(), err
	}
	m.worker.status(botID, "configured", 0)
	m.worker.problem(botID, "")
	m.mu.Lock()
	m.startBotLocked(botID, a)
	m.mu.Unlock()
	return m.Management(), nil
}

func (m *Manager) stopBot(botID int64) {
	m.mu.Lock()
	loop := m.loops[botID]
	m.mu.Unlock()
	if loop != nil {
		loop.cancel()
		<-loop.done
	}
}

func (m *Manager) resumeBot(bot botConfig) {
	a, err := newAPI(bot.BotID, bot.Token, m.transport)
	if err != nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed || m.stopping || m.life.Err() != nil {
		a.close()
		return
	}
	m.worker.mu.Lock()
	old := m.worker.apis[bot.BotID]
	m.worker.apis[bot.BotID] = a
	m.worker.mu.Unlock()
	if old != nil {
		old.close()
	}
	m.worker.status(bot.BotID, "configured", 0)
	m.worker.problem(bot.BotID, "")
	m.startBotLocked(bot.BotID, a)
}

// Requires m.mu and no previous live loop for this bot.
func (m *Manager) startBotLocked(botID int64, a *api) {
	if !m.started || m.closed || m.stopping || m.life.Err() != nil {
		return
	}
	ctx, cancel := context.WithCancel(m.life)
	loop := &managedLoop{cancel: cancel, done: make(chan struct{})}
	m.loops[botID] = loop
	go func() {
		defer close(loop.done)
		defer a.close()
		err := m.worker.runBot(ctx, a)
		if ctx.Err() != nil {
			m.worker.status(botID, "stopped", 0)
		} else {
			m.worker.status(botID, "error", 0)
			m.worker.problem(botID, managementProblem(err))
		}
	}()
}

func managementProblem(err error) string {
	switch {
	case errors.Is(err, ErrIdentity):
		return "identity_mismatch"
	case errors.Is(err, ErrWebhook):
		return "webhook_exists"
	case errors.Is(err, ErrConfig):
		return "credentials_invalid"
	case errors.Is(err, ErrJournal), errors.Is(err, ErrConflict), errors.Is(err, ErrCapacity):
		return "journal_unavailable"
	case errors.Is(err, ErrTransport), errors.Is(err, ErrProtocol):
		return "transport_unavailable"
	default:
		var apiError *APIError
		if errors.As(err, &apiError) {
			return "transport_unavailable"
		}
		return "worker_stopped"
	}
}

func (m *Manager) Management() ManagementStatus {
	if m == nil {
		return ManagementStatus{Connections: []ManagedConnection{}}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	statuses := map[int64]BotStatus{}
	for _, status := range m.worker.Statuses() {
		statuses[status.BotID] = status
	}
	result := ManagementStatus{Revision: m.config.Revision, Connections: make([]ManagedConnection, 0, len(m.config.Bots))}
	for _, bot := range m.config.Bots {
		status := statuses[bot.BotID]
		state := status.State
		if state == "" {
			state = "configured"
		}
		if m.closed || m.stopping {
			state = "stopped"
		}
		result.Connections = append(result.Connections, ManagedConnection{BotID: bot.BotID, Configured: true, Username: bot.Username, State: state, Problem: status.LastError})
	}
	sort.Slice(result.Connections, func(i, j int) bool { return result.Connections[i].BotID < result.Connections[j].BotID })
	return result
}

func (m *Manager) Statuses() []BotStatus {
	if m == nil {
		return nil
	}
	return m.worker.Statuses()
}

func (m *Manager) Run(ctx context.Context) error {
	if m == nil || ctx == nil {
		return ErrState
	}
	// Serialize startup with a configuration made before Run: a token change
	// cannot join an absent loop and then race startup of the old credential.
	m.operation <- struct{}{}
	m.mu.Lock()
	if m.started || m.closed || m.stopping {
		m.mu.Unlock()
		<-m.operation
		return ErrState
	}
	m.started = true
	stop := context.AfterFunc(ctx, m.cancel)
	if ctx.Err() != nil {
		m.cancel()
	}
	for id, a := range m.worker.apis {
		m.startBotLocked(id, a)
	}
	m.mu.Unlock()
	<-m.operation
	defer stop()
	// Empty managers and managers whose bots stopped remain alive so panel
	// onboarding and a verified retry can start independent bot loops later.
	<-m.life.Done()
	m.mu.Lock()
	m.stopping = true
	m.mu.Unlock()
	m.operation <- struct{}{}
	defer func() { <-m.operation }()
	m.mu.Lock()
	loops := make([]*managedLoop, 0, len(m.loops))
	for _, loop := range m.loops {
		loop.cancel()
		loops = append(loops, loop)
	}
	m.mu.Unlock()
	for _, loop := range loops {
		<-loop.done
	}
	result := ctx.Err()
	if result == nil {
		result = context.Canceled
	}
	m.mu.Lock()
	m.runError = result
	close(m.done)
	m.mu.Unlock()
	return result
}

func (m *Manager) Wait(ctx context.Context) error {
	if m == nil || ctx == nil {
		return ErrState
	}
	m.mu.Lock()
	started, closed := m.started, m.closed
	m.mu.Unlock()
	if !started && !closed {
		return ErrState
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-m.done:
		m.mu.Lock()
		defer m.mu.Unlock()
		return m.runError
	}
}

func (m *Manager) Close() error {
	if m == nil {
		return nil
	}
	m.closeOnce.Do(func() {
		m.mu.Lock()
		m.closed = true
		started := m.started
		m.cancel()
		m.mu.Unlock()
		if started {
			<-m.done
		}
		m.operation <- struct{}{}
		defer func() { <-m.operation }()
		for _, a := range m.worker.apis {
			a.close()
		}
		m.closeErr = m.worker.journal.Close()
		if !started {
			m.mu.Lock()
			close(m.done)
			m.mu.Unlock()
		}
	})
	return m.closeErr
}
