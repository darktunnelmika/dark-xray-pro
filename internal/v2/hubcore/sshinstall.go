package hubcore

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
)

// Installing a node from the panel: the HUB signs in to the new server
// over SSH with the password or key the owner typed (used once, never
// stored or logged), runs the same one-line installer the panel shows,
// and streams its output back.

type SSHInstall struct {
	NodeID   string `json:"node_id"`
	Name     string `json:"name"`
	Host     string `json:"host"`
	Port     int    `json:"port"`
	User     string `json:"user"`
	Password string `json:"password"`
	Key      string `json:"key"` // PEM private key (optional, instead of a password)
}

// SSHJobView is what the panel polls.
type SSHJobView struct {
	Lines  []string `json:"lines"`
	Status string   `json:"status"` // running | done | failed
	Error  string   `json:"error,omitempty"`
	NodeID string   `json:"node_id"`
}

type sshJob struct {
	mu sync.Mutex
	SSHJobView
	start time.Time
}

func (j *sshJob) add(line string) {
	j.mu.Lock()
	defer j.mu.Unlock()
	if len(j.Lines) >= 400 {
		j.Lines = j.Lines[1:]
	}
	j.Lines = append(j.Lines, line)
}

func (j *sshJob) finish(err error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.Status = "done"
	if err != nil {
		j.Status, j.Error = "failed", err.Error()
	}
}

func (j *sshJob) view() SSHJobView {
	j.mu.Lock()
	defer j.mu.Unlock()
	v := j.SSHJobView
	v.Lines = append([]string(nil), j.Lines...)
	return v
}

var (
	sshJobsMu sync.Mutex
	sshJobs   = map[string]*sshJob{}
)

// SSHInstallJob returns a running or finished install.
func SSHInstallJob(id string) (SSHJobView, bool) {
	sshJobsMu.Lock()
	j, ok := sshJobs[id]
	sshJobsMu.Unlock()
	if !ok {
		return SSHJobView{}, false
	}
	return j.view(), true
}

// StartSSHInstall registers the node (or re-issues its install) and runs
// the installer over SSH in the background. installURL turns an install
// token into the one-line installer's address.
func (s *Store) StartSSHInstall(ctx context.Context, in SSHInstall, installURL func(tok string) string) (string, error) {
	in.Host, in.User = strings.TrimSpace(in.Host), strings.TrimSpace(in.User)
	if in.Port == 0 {
		in.Port = 22
	}
	if in.User == "" {
		in.User = "root"
	}
	if !idRE.MatchString(in.NodeID) || in.Host == "" || in.Port < 1 || in.Port > 65535 || (in.Password == "" && strings.TrimSpace(in.Key) == "") {
		return "", fmt.Errorf("%w: node id, server address and a password or key are required", ErrInvalid)
	}
	var auth []ssh.AuthMethod
	if k := strings.TrimSpace(in.Key); k != "" {
		signer, err := ssh.ParsePrivateKey([]byte(k))
		if err != nil {
			return "", fmt.Errorf("%w: the private key cannot be read (%v)", ErrInvalid, err)
		}
		auth = append(auth, ssh.PublicKeys(signer))
	}
	if in.Password != "" {
		pw := in.Password
		auth = append(auth, ssh.Password(pw), ssh.KeyboardInteractive(func(_, _ string, qs []string, _ []bool) ([]string, error) {
			out := make([]string, len(qs))
			for i := range qs {
				out[i] = pw
			}
			return out, nil
		}))
	}
	var tok string
	var x int
	var err error
	if s.ro.QueryRowContext(ctx, `SELECT 1 FROM nodes WHERE id=?`, in.NodeID).Scan(&x) == nil {
		tok, err = s.NewInstallToken(ctx, in.NodeID)
	} else {
		name := in.Name
		if name == "" {
			name = in.NodeID
		}
		tok, err = s.CreateNodeInstall(ctx, in.NodeID, name)
	}
	if err != nil {
		return "", err
	}
	cmd := "curl -fsSL " + installURL(tok) + " | bash"
	if in.User != "root" {
		cmd = "sudo -S -p '' bash -c " + strconv.Quote(cmd)
	}
	id := randomHex(8)
	j := &sshJob{SSHJobView: SSHJobView{Status: "running", NodeID: in.NodeID}, start: time.Now()}
	sshJobsMu.Lock()
	for k, old := range sshJobs { // forget installs older than a day
		if time.Since(old.start) > 24*time.Hour {
			delete(sshJobs, k)
		}
	}
	sshJobs[id] = j
	sshJobsMu.Unlock()
	go func() {
		j.finish(runSSHInstall(in, auth, cmd, j))
	}()
	return id, nil
}

func runSSHInstall(in SSHInstall, auth []ssh.AuthMethod, cmd string, j *sshJob) error {
	addr := net.JoinHostPort(in.Host, strconv.Itoa(in.Port))
	j.add("→ " + addr)
	cfg := &ssh.ClientConfig{User: in.User, Auth: auth, Timeout: 20 * time.Second,
		// First contact with a brand-new server: show its key so the owner can compare.
		HostKeyCallback: func(_ string, _ net.Addr, key ssh.PublicKey) error {
			sum := sha256.Sum256(key.Marshal())
			j.add("🔑 " + key.Type() + " SHA256:" + base64.RawStdEncoding.EncodeToString(sum[:]))
			return nil
		}}
	cl, err := ssh.Dial("tcp", addr, cfg)
	if err != nil && strings.Contains(err.Error(), "unable to authenticate") && in.Password != strings.TrimSpace(in.Password) {
		// A pasted password often carries a space or a line break at its end:
		// the exact text was refused, try it without them.
		pw := strings.TrimSpace(in.Password)
		cfg.Auth = []ssh.AuthMethod{ssh.Password(pw), ssh.KeyboardInteractive(func(_, _ string, qs []string, _ []bool) ([]string, error) {
			out := make([]string, len(qs))
			for i := range qs {
				out[i] = pw
			}
			return out, nil
		})}
		in.Password = pw
		cl, err = ssh.Dial("tcp", addr, cfg)
	}
	if err != nil {
		return fmt.Errorf("cannot sign in: %v", err)
	}
	defer cl.Close()
	sess, err := cl.NewSession()
	if err != nil {
		return err
	}
	defer sess.Close()
	out, _ := sess.StdoutPipe()
	errOut, _ := sess.StderrPipe()
	stdin, _ := sess.StdinPipe()
	read := func(r interface{ Read([]byte) (int, error) }) {
		sc := bufio.NewScanner(r)
		sc.Buffer(make([]byte, 64<<10), 1<<20)
		for sc.Scan() {
			if t := strings.TrimRight(sc.Text(), "\r"); t != "" {
				j.add(t)
			}
		}
	}
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); read(out) }()
	go func() { defer wg.Done(); read(errOut) }()
	j.add("$ " + strings.Replace(cmd, "/install/", "/install/…", 1)[:min(len(cmd), 60)] + "…")
	if err := sess.Start(cmd); err != nil {
		return err
	}
	if in.User != "root" && in.Password != "" {
		_, _ = stdin.Write([]byte(in.Password + "\n")) // for sudo
	}
	_ = stdin.Close()
	done := make(chan error, 1)
	go func() { done <- sess.Wait() }()
	select {
	case err = <-done:
	case <-time.After(10 * time.Minute):
		return fmt.Errorf("the installer did not finish in 10 minutes")
	}
	wg.Wait()
	if err != nil {
		return fmt.Errorf("the installer failed: %v", err)
	}
	j.add("✅ " + "done")
	return nil
}
