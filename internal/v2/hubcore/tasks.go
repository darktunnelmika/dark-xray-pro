package hubcore

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/darktunnelmika/dark-xray-pro/internal/v2/sniscan"
	"github.com/darktunnelmika/dark-xray-pro/internal/v2/wire"
)

const taskSchema = `
CREATE TABLE IF NOT EXISTS node_tasks(
	id TEXT PRIMARY KEY,
	node_id TEXT NOT NULL,
	kind TEXT NOT NULL,
	args TEXT NOT NULL DEFAULT '',
	status TEXT NOT NULL DEFAULT 'queued',
	result TEXT NOT NULL DEFAULT '',
	error TEXT NOT NULL DEFAULT '',
	created_unix INTEGER NOT NULL,
	done_unix INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS node_tasks_open ON node_tasks(node_id,status);
`

// taskTTL: a task a Node has not answered in this time is given up.
const taskTTL = 10 * 60

func (s *Store) ensureTaskSchema() error {
	if _, err := s.db.Exec(taskSchema); err != nil {
		return err
	}
	if err := s.addColumn("nodes", "agent_sha", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	return s.addColumn("nodes", "agent_assets", "TEXT NOT NULL DEFAULT ''")
}

// SetDistDir tells the store where the Node binaries live.
func (s *Store) SetDistDir(dir string) {
	s.distMu.Lock()
	s.distDir = dir
	s.distMu.Unlock()
}

// DistSHA is the sha256 of the HUB's dark-node, cached by mtime; "" if none.
func (s *Store) DistSHA() string {
	s.distMu.Lock()
	defer s.distMu.Unlock()
	if s.distDir == "" {
		return ""
	}
	p := filepath.Join(s.distDir, "dark-node")
	st, err := os.Stat(p)
	if err != nil {
		return ""
	}
	if !st.ModTime().Equal(s.distMod) {
		sum, err := fileSHA(p)
		if err != nil {
			return ""
		}
		s.distSHA, s.distMod = sum, st.ModTime()
	}
	return s.distSHA
}

// distFile resolves a servable distribution file.
func (s *Store) distFile(name string) (string, bool) {
	for _, g := range GeoAssets {
		if name == g {
			p := filepath.Join(s.geoDir(), name)
			_, err := os.Stat(p)
			return p, err == nil
		}
	}
	if name != "dark-node" && name != "xray" {
		return "", false
	}
	s.distMu.Lock()
	dir := s.distDir
	s.distMu.Unlock()
	if dir == "" {
		return "", false
	}
	return filepath.Join(dir, name), true
}

// StartNodeUpdate asks a Node to replace its agent with the HUB's copy.
func (s *Store) StartNodeUpdate(ctx context.Context, nodeID string) (string, error) {
	sum := s.DistSHA()
	if sum == "" {
		return "", fmt.Errorf("%w: no dark-node binary on the HUB", ErrInvalid)
	}
	return s.QueueTask(ctx, nodeID, wire.TaskSelfUpdate, map[string]string{"sha256": sum})
}

// Task is the owner's view of one Node task.
type Task struct {
	ID      string          `json:"id"`
	NodeID  string          `json:"node_id"`
	Kind    string          `json:"kind"`
	Status  string          `json:"status"` // queued, done, failed, expired
	Result  json.RawMessage `json:"result,omitempty"`
	Error   string          `json:"error,omitempty"`
	Created int64           `json:"created"`
	Done    int64           `json:"done,omitempty"`
}

// QueueTask asks a Node to run kind with args on its next sync.
func (s *Store) QueueTask(ctx context.Context, nodeID, kind string, args any) (string, error) {
	var n int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM nodes WHERE id=?`, nodeID).Scan(&n); err != nil || n == 0 {
		return "", ErrNotFound
	}
	raw, err := json.Marshal(args)
	if err != nil {
		return "", err
	}
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	id := "t" + hex.EncodeToString(b)
	now := s.now().Unix()
	_, _ = s.db.ExecContext(ctx, `DELETE FROM node_tasks WHERE created_unix<?`, now-7*86400)
	_, err = s.db.ExecContext(ctx, `INSERT INTO node_tasks(id,node_id,kind,args,created_unix) VALUES(?,?,?,?,?)`, id, nodeID, kind, string(raw), now)
	return id, err
}

// TaskByID returns one task; open tasks past their TTL read as expired.
func (s *Store) TaskByID(ctx context.Context, id string) (Task, error) {
	var t Task
	var res string
	err := s.ro.QueryRowContext(ctx, `SELECT id,node_id,kind,status,result,error,created_unix,done_unix FROM node_tasks WHERE id=?`, id).
		Scan(&t.ID, &t.NodeID, &t.Kind, &t.Status, &res, &t.Error, &t.Created, &t.Done)
	if errors.Is(err, sql.ErrNoRows) {
		return t, ErrNotFound
	}
	if err != nil {
		return t, err
	}
	if res != "" {
		t.Result = json.RawMessage(res)
	}
	if t.Status == "queued" && s.now().Unix()-t.Created > taskTTL {
		t.Status = "expired"
	}
	return t, nil
}

// openTasks lists what a Node should be running.
func (s *Store) openTasks(ctx context.Context, nodeID string) []wire.Task {
	rows, err := s.db.QueryContext(ctx, `SELECT id,kind,args FROM node_tasks WHERE node_id=? AND status='queued' AND created_unix>? ORDER BY created_unix LIMIT 8`,
		nodeID, s.now().Unix()-taskTTL)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []wire.Task
	for rows.Next() {
		var t wire.Task
		var args string
		if rows.Scan(&t.ID, &t.Kind, &args) == nil {
			if args != "" {
				t.Args = json.RawMessage(args)
			}
			out = append(out, t)
		}
	}
	return out
}

// applyTaskResults stores results; only the owning Node can answer, and
// only once.
func (s *Store) applyTaskResults(ctx context.Context, nodeID string, list []wire.TaskResult) {
	now := s.now().Unix()
	for _, r := range list {
		status, res, msg := "done", string(r.Result), r.Error
		if msg != "" {
			status = "failed"
		}
		if len(res) > 4<<20 {
			status, res, msg = "failed", "", "result too large"
		}
		if len(msg) > 500 {
			msg = msg[:500]
		}
		_, _ = s.db.ExecContext(ctx, `UPDATE node_tasks SET status=?,result=?,error=?,done_unix=? WHERE id=? AND node_id=? AND status='queued'`,
			status, res, msg, now, r.ID, nodeID)
	}
}

// StartSNIScan queues a REALITY camouflage scan on a Node. With no domains
// the built-in list is used; subnet also tries neighbours of the Node's
// public address.
func (s *Store) StartSNIScan(ctx context.Context, nodeID string, domains []string, subnet bool) (string, error) {
	a := sniscan.Args{Subnet: subnet}
	for _, d := range domains {
		if d = strings.TrimSpace(d); d != "" {
			a.Domains = append(a.Domains, d)
		}
	}
	if len(a.Domains) == 0 {
		a.Domains = sniscan.Defaults
	}
	if len(a.Domains) > 200 {
		return "", ErrInvalid
	}
	if subnet {
		_ = s.ro.QueryRowContext(ctx, `SELECT public_ip FROM nodes WHERE id=?`, nodeID).Scan(&a.IP)
	}
	return s.QueueTask(ctx, nodeID, wire.TaskSNIScan, a)
}
