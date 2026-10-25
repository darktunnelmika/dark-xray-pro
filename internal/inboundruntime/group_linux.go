//go:build linux

package inboundruntime

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"syscall"
	"time"

	"github.com/darktunnelmika/dark-xray-pro/internal/inboundexec"
	"github.com/darktunnelmika/dark-xray-pro/internal/usagecheckpoint"
)

type adapterRequest struct {
	Nonce string `json:"nonce,omitempty"`
}
type adapterResponse struct {
	Version       int                   `json:"version"`
	Engine        string                `json:"engine"`
	Ready         bool                  `json:"ready"`
	AdmissionOpen bool                  `json:"admission_open"`
	Final         bool                  `json:"final"`
	Nonce         string                `json:"nonce,omitempty"`
	Counters      []inboundexec.Counter `json:"counters"`
}

type adapterConfig struct {
	Version              int                               `json:"version"`
	Engine               string                            `json:"engine"`
	Config               json.RawMessage                   `json:"config"`
	Manifest             json.RawMessage                   `json:"manifest"`
	Principals           []inboundexec.Principal           `json:"principals"`
	PublicAliasAuthority *inboundexec.PublicAliasAuthority `json:"public_alias_authority,omitempty"`
}
type externalProcess struct {
	child      *Native
	engine     string
	tag        string
	client     *http.Client
	last       map[string]inboundexec.Counter
	principals []inboundexec.Principal
}

func (e *externalProcess) call(ctx context.Context, op, nonce string) (adapterResponse, error) {
	var out adapterResponse
	if e.child.checkIdentity() != nil {
		return out, ErrIdentity
	}
	raw, _ := json.Marshal(adapterRequest{Nonce: nonce})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://unix/"+op, bytes.NewReader(raw))
	if err != nil {
		return out, err
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := e.client.Do(req)
	if err != nil {
		return out, ErrRuntime
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return out, ErrProof
	}
	d := json.NewDecoder(io.LimitReader(res.Body, 8<<20))
	d.DisallowUnknownFields()
	if d.Decode(&out) != nil || out.Version != 1 || out.Engine != e.engine || e.child.checkIdentity() != nil {
		return out, ErrProof
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return out, ErrProof
	}
	return out, nil
}

func startExternal(ctx context.Context, dir string, binary *Binary, c inboundexec.Component, principals []inboundexec.Principal, deadline time.Time, authorities ...*inboundexec.PublicAliasAuthority) (*externalProcess, error) {
	if binary == nil {
		return nil, ErrRuntime
	}
	birth, err := os.MkdirTemp(dir, "birth-")
	if err != nil {
		return nil, err
	}
	success := false
	defer func() {
		if !success {
			_ = os.RemoveAll(birth)
		}
	}()
	config := adapterConfig{Version: 1, Engine: c.Engine, Config: c.Config, Manifest: c.Manifest, Principals: principals}
	if len(authorities) > 0 {
		config.PublicAliasAuthority = authorities[0]
	}
	raw, err := json.Marshal(config)
	if err != nil {
		return nil, err
	}
	path := filepath.Join(birth, "adapter.json")
	sock := filepath.Join(birth, "control.sock")
	if len(sock) > 100 {
		return nil, errors.New("inbound_runtime_path_too_long")
	}
	if err = os.WriteFile(path, raw, 0600); err != nil {
		return nil, err
	}
	cmd := binary.command(context.Background(), "--config", path, "--control", sock)
	cmd.Dir = birth
	cmd.Env = []string{"PATH=/usr/bin:/bin", "HOME=" + birth}
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	cmd.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGKILL}
	child := &Native{binary: binary, cmd: cmd, done: make(chan struct{}), dir: birth}
	started := make(chan error, 1)
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		e := cmd.Start()
		started <- e
		if e == nil {
			_ = cmd.Wait()
		}
		close(child.done)
	}()
	if err = <-started; err != nil {
		<-child.done
		return nil, ErrRuntime
	}
	child.identity, err = usagecheckpoint.Process(cmd.Process.Pid)
	if err != nil {
		child.Stop()
		return nil, err
	}
	child.configSHA = hashBytes(raw)
	if !child.Renew(deadline) {
		child.Stop()
		return nil, ErrLease
	}
	tr := &http.Transport{Proxy: nil, DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{Timeout: 2 * time.Second}).DialContext(ctx, "unix", sock)
	}}
	e := &externalProcess{child: child, engine: c.Engine, tag: c.Tag, principals: principals, last: map[string]inboundexec.Counter{}, client: &http.Client{Transport: tr, Timeout: 4 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
	wait, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	tick := time.NewTicker(50 * time.Millisecond)
	defer tick.Stop()
	for {
		reply, callErr := e.call(wait, "status", "")
		if callErr == nil && reply.Ready && !reply.AdmissionOpen && !reply.Final {
			success = true
			return e, nil
		}
		select {
		case <-wait.Done():
			child.Stop()
			return nil, ErrRuntime
		case <-child.done:
			child.Stop()
			return nil, ErrRuntime
		case <-tick.C:
		}
	}
}

func (e *externalProcess) values(reply adapterResponse) ([]inboundexec.Counter, error) {
	if len(reply.Counters) != len(e.principals) {
		return nil, ErrProof
	}
	expected := map[string]bool{}
	for _, p := range e.principals {
		expected[p.ID] = true
	}
	seen := map[string]bool{}
	for _, c := range reply.Counters {
		if !expected[c.PrincipalID] || seen[c.PrincipalID] {
			return nil, ErrProof
		}
		seen[c.PrincipalID] = true
		last := e.last[c.PrincipalID]
		if c.Upload < last.Upload || c.Download < last.Download {
			return nil, ErrProof
		}
	}
	for _, c := range reply.Counters {
		e.last[c.PrincipalID] = c
	}
	return reply.Counters, nil
}

type engineGroup struct {
	native     *Native
	external   []*externalProcess
	principals []inboundexec.Principal
}

func validateGroup(ctx context.Context, b *Binary, external map[string]*Binary, bundle inboundexec.Bundle) error {
	if err := guardTUNDNSActivation(ctx, b, bundle); err != nil {
		return err
	}
	var err error
	bundle, err = runtimeBundle(bundle)
	if err != nil {
		return err
	}
	if len(bundle.NativeConfig) > 0 {
		if e := ValidateNative(ctx, b, bundle.NativeConfig); e != nil {
			return e
		}
	}
	for _, c := range bundle.Components {
		binary := external[c.Engine]
		if binary == nil {
			return errors.New("inbound_engine_not_installed")
		}
		if c.Tag == "" || len(c.Config) == 0 || c.SHA256 != hashBytes(c.Config) {
			return ErrProof
		}
		dir, e := os.MkdirTemp("", "dark-pro-adapter-check-")
		if e != nil {
			return e
		}
		config := adapterConfig{Version: 1, Engine: c.Engine, Config: c.Config, Manifest: c.Manifest, Principals: componentPrincipals(bundle.Principals, c.Engine, c.Tag)}
		raw, _ := json.Marshal(config)
		path := filepath.Join(dir, "candidate.json")
		e = os.WriteFile(path, raw, 0600)
		if e == nil {
			check, cancel := context.WithTimeout(ctx, 8*time.Second)
			cmd := binary.command(check, "--check", "--config", path)
			cmd.Dir = dir
			cmd.Env = []string{"PATH=/usr/bin:/bin", "HOME=" + dir}
			cmd.Stdout = io.Discard
			cmd.Stderr = io.Discard
			e = cmd.Run()
			cancel()
		}
		_ = os.RemoveAll(dir)
		if e != nil {
			return ErrRuntime
		}
	}
	return nil
}

func componentPrincipals(principals []inboundexec.Principal, engine, tag string) []inboundexec.Principal {
	out := []inboundexec.Principal{}
	for _, p := range principals {
		if p.Engine == engine && (p.ComponentTag == "" || p.ComponentTag == tag) {
			out = append(out, p)
		}
	}
	return out
}

func startGroup(ctx context.Context, dir string, b *Binary, external map[string]*Binary, bundle inboundexec.Bundle, deadline time.Time, authorities ...map[string]inboundexec.PublicAliasAuthority) (*engineGroup, error) {
	var err error
	bundle, err = runtimeBundle(bundle)
	if err != nil {
		return nil, err
	}
	g := &engineGroup{principals: bundle.Principals}
	ok := false
	defer func() {
		if !ok {
			g.Stop()
		}
	}()
	if len(bundle.NativeConfig) > 0 {
		n, e := StartNative(ctx, dir, b, bundle, deadline)
		if e != nil {
			return nil, e
		}
		g.native = n
	}
	for _, c := range bundle.Components {
		var authority *inboundexec.PublicAliasAuthority
		if len(authorities) > 0 {
			if value, ok := authorities[0][c.Tag]; ok {
				authority = &value
			}
		}
		p, e := startExternal(ctx, dir, external[c.Engine], c, componentPrincipals(bundle.Principals, c.Engine, c.Tag), deadline, authority)
		if e != nil {
			return nil, e
		}
		g.external = append(g.external, p)
	}
	if g.native == nil && len(g.external) == 0 {
		return nil, ErrRuntime
	}
	ok = true
	return g, nil
}

func (g *engineGroup) Stop() {
	if g == nil {
		return
	}
	for _, e := range g.external {
		e.child.Stop()
		e.client.CloseIdleConnections()
	}
	if g.native != nil {
		g.native.Stop()
	}
}
func (g *engineGroup) Renew(t time.Time) bool {
	if g.native != nil && !g.native.Renew(t) {
		return false
	}
	for _, e := range g.external {
		if !e.child.Renew(t) {
			return false
		}
	}
	return true
}
func (g *engineGroup) Alive() bool {
	if g == nil || g.native == nil && len(g.external) == 0 {
		return false
	}
	if g.native != nil && g.native.checkIdentity() != nil {
		return false
	}
	for _, e := range g.external {
		if e.child.checkIdentity() != nil {
			return false
		}
	}
	return true
}
func (g *engineGroup) Identity() string {
	m := map[string]string{}
	if g.native != nil {
		m["xray"] = g.native.Identity()
	}
	for _, e := range g.external {
		m[e.engine+"/"+e.tag] = e.child.Identity()
	}
	raw, _ := json.Marshal(m)
	return string(raw)
}
func (g *engineGroup) BinarySHAs() map[string]string {
	m := map[string]string{}
	if g.native != nil {
		m["xray"] = g.native.binary.SHA256
	}
	for _, e := range g.external {
		m[e.engine] = e.child.binary.SHA256
	}
	return m
}
func (g *engineGroup) Activate(ctx context.Context, nonce string) error {
	if g.native != nil {
		if e := g.native.Activate(ctx, nonce); e != nil {
			return e
		}
	}
	for _, e := range g.external {
		r, err := e.call(ctx, "activate", nonce)
		if err != nil || r.Nonce != nonce || !r.AdmissionOpen || r.Final {
			return ErrProof
		}
	}
	return nil
}
func (g *engineGroup) Health(ctx context.Context) error {
	if g.native != nil {
		if e := g.native.Health(ctx); e != nil {
			return e
		}
	}
	for _, e := range g.external {
		r, err := e.call(ctx, "status", "")
		if err != nil || !r.Ready || !r.AdmissionOpen || r.Final {
			return ErrRuntime
		}
	}
	return nil
}
func (g *engineGroup) Counters(ctx context.Context) ([]inboundexec.Counter, error) {
	out := []inboundexec.Counter{}
	if g.native != nil {
		v, e := g.native.Counters(ctx)
		if e != nil {
			return nil, e
		}
		out = append(out, v...)
	}
	for _, e := range g.external {
		r, err := e.call(ctx, "snapshot", "")
		if err != nil {
			return nil, err
		}
		v, err := e.values(r)
		if err != nil {
			return nil, err
		}
		out = append(out, v...)
	}
	return g.complete(out)
}
func (g *engineGroup) Final(ctx context.Context, nonce string) ([]inboundexec.Counter, error) {
	// Upstream adapters close all ingress and join their relay work first. Only
	// then may the native bridge seal its authoritative payload counters.
	out := []inboundexec.Counter{}
	for _, e := range g.external {
		r, err := e.call(ctx, "final", nonce)
		if err != nil || !r.Final || r.AdmissionOpen || r.Nonce != nonce {
			return nil, ErrProof
		}
		v, err := e.values(r)
		if err != nil {
			return nil, err
		}
		out = append(out, v...)
	}
	if g.native != nil {
		v, e := g.native.Final(ctx, nonce)
		if e != nil {
			return nil, e
		}
		out = append(out, v...)
	}
	return g.complete(out)
}
func (g *engineGroup) complete(values []inboundexec.Counter) ([]inboundexec.Counter, error) {
	if len(values) != len(g.principals) {
		return nil, ErrProof
	}
	seen := map[string]bool{}
	for _, v := range values {
		if seen[v.PrincipalID] {
			return nil, ErrProof
		}
		seen[v.PrincipalID] = true
	}
	for _, p := range g.principals {
		if !seen[p.ID] {
			return nil, ErrProof
		}
	}
	sort.Slice(values, func(i, j int) bool { return values[i].PrincipalID < values[j].PrincipalID })
	return values, nil
}
