//go:build linux

// Package xraycanary runs a PINNED Xray candidate on ephemeral LOOPBACK ports,
// then terminates that isolated process. It NEVER replaces or restarts the
// system Xray service, preserves old Nodes and cannot publish subscriptions.
package xraycanary

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"github.com/darktunnelmika/dark-xray-pro/internal/xrayconfig"
)

var (
	ErrCandidate = errors.New("invalid or unsafe canary candidate")
	ErrCore = errors.New("pinned offline Xray validation failed")
	ErrStart = errors.New("private loopback Xray canary failed to start")
	ErrNotReady = errors.New("private loopback Xray canary listeners not ready")
)

const maxListeners = 64
const startupTimeout = 6 * time.Second

// Proof is intentionally NEVER a healthy user protocol/data-plane verdict.
// Even a fully bound canary must be shut down and all original routes remain
// unchanged; a real VLESS/REALITY handshake is a separate release gate.
type Proof struct {
	ConfigSHA256 string `json:"config_sha256"`
	ManifestSHA256 string `json:"manifest_sha256"`
	XrayBinarySHA256 string `json:"xray_binary_sha256"`
	SyntaxValidated bool `json:"syntax_validated"`
	LoopbackBound bool `json:"loopback_bound"`
	RemappedListeners int `json:"remapped_listeners"`
	CanActivate bool `json:"can_activate"`
	DataPlaneVerified bool `json:"data_plane_verified"`
	NeedsRollbackPlan bool `json:"needs_rollback_plan"`
	NeedsSubscriptionCommit bool `json:"needs_subscription_commit"`
	Status string `json:"status"`
}

func starting(c xrayconfig.Candidate, binarySHA string) Proof {
	return Proof{
		ConfigSHA256:c.SHA256,ManifestSHA256:c.ManifestSHA256,
		XrayBinarySHA256:binarySHA,CanActivate:false,DataPlaneVerified:false,
		NeedsRollbackPlan:true,NeedsSubscriptionCommit:true,
		Status:"not_tested_not_active",
	}
}

// remap validates the exact renderer-only profile and rewrites EVERY listener
// to a fresh 127.0.0.1 port. It does not modify reality credentials or mutate
// the caller's original bytes. Original inbound ports cannot be bound by canary.
func remap(c xrayconfig.Candidate)([]byte,[]int,error){
	if c.CanActivate || !c.RequiresXrayTest || !c.RequiresDataPlaneProof ||
		!c.RequiresSubscriptionCommit || len(c.Config)==0 ||
		len(c.Config)>xrayconfig.MaxCandidateBytes || len(c.Listeners)==0 ||
		len(c.Listeners)>maxListeners || len(c.SHA256)!=64 {
		return nil,nil,ErrCandidate
	}
	hash:=sha256.Sum256(c.Config)
	if hex.EncodeToString(hash[:])!=c.SHA256{return nil,nil,ErrCandidate}
	var root map[string]json.RawMessage
	if err:=json.Unmarshal(c.Config,&root);err!=nil{return nil,nil,ErrCandidate}
	allowed:=map[string]bool{"log":true,"stats":true,"policy":true,"inbounds":true,"outbounds":true}
	if len(root)!=len(allowed){return nil,nil,ErrCandidate}
	for k:=range root{if !allowed[k]{return nil,nil,ErrCandidate}}
	var outbounds []struct{Tag string `json:"tag"`;Protocol string `json:"protocol"`}
	if err:=json.Unmarshal(root["outbounds"],&outbounds);err!=nil ||
		len(outbounds)!=1 || outbounds[0].Tag!="direct" || outbounds[0].Protocol!="freedom"{
		return nil,nil,ErrCandidate
	}
	var inbounds []map[string]json.RawMessage
	if err:=json.Unmarshal(root["inbounds"],&inbounds);err!=nil ||
		len(inbounds)!=len(c.Listeners) {return nil,nil,ErrCandidate}
	expected:=make(map[string]uint16,len(c.Listeners))
	for _,l:=range c.Listeners{
		if l.Tag==""||l.NodePort==0||l.InboundID==0||expected[l.Tag]!=0{
			return nil,nil,ErrCandidate
		}
		switch l.Kind {case "direct","swap","shared":default:return nil,nil,ErrCandidate}
		expected[l.Tag]=l.NodePort
	}
	// Reserve all local ephemeral endpoints at once, release directly before
	// launching Xray. Any port allocation race fails closed, never affects old.
	reservations:=make([]net.Listener,0,len(inbounds))
	defer func(){for _,l:=range reservations{_ = l.Close()}}()
	ports:=make([]int,0,len(inbounds))
	for _,in:=range inbounds{
		if len(in)!=6{return nil,nil,ErrCandidate}
		for k:=range in{
			switch k{case "listen","port","tag","protocol","settings","streamSettings":
			default:return nil,nil,ErrCandidate}
		}
		var tag,listen,protocol string
		var oldPort uint16
		if json.Unmarshal(in["tag"],&tag)!=nil ||
			json.Unmarshal(in["listen"],&listen)!=nil ||
			json.Unmarshal(in["protocol"],&protocol)!=nil ||
			json.Unmarshal(in["port"],&oldPort)!=nil ||
			tag==""||listen!="0.0.0.0"||protocol!="vless"||
			expected[tag]!=oldPort||oldPort==0{
			return nil,nil,ErrCandidate
		}
		delete(expected,tag)
		var stream struct{Network string `json:"network"`;Security string `json:"security"`}
		if json.Unmarshal(in["streamSettings"],&stream)!=nil||
			stream.Security!="reality"|| (stream.Network!="tcp"&&stream.Network!="grpc"){
			return nil,nil,ErrCandidate
		}
		listener,err:=net.Listen("tcp4","127.0.0.1:0")
		if err!=nil{return nil,nil,ErrNotReady}
		reservations=append(reservations,listener)
		port:=listener.Addr().(*net.TCPAddr).Port
		ports=append(ports,port)
		in["port"],_=json.Marshal(port)
		in["listen"],_=json.Marshal("127.0.0.1")
	}
	if len(expected)!=0{return nil,nil,ErrCandidate}
	root["inbounds"],_=json.Marshal(inbounds)
	private,err:=json.Marshal(root)
	if err!=nil||len(private)>xrayconfig.MaxCandidateBytes{return nil,nil,ErrCandidate}
	// Reservations must release before Xray can bind. Close failures are
	// checked, and canary validation never uses any original active Node port.
	for _,l:=range reservations{if err:=l.Close();err!=nil{return nil,nil,ErrNotReady}}
	return private,ports,nil
}

// Run verifies syntax with the independently pinned executable before
// running a disposable Xray process on ONLY ephemeral loopback listeners.
// It never executes systemctl, writes active configs, or publishes accounts.
func Run(ctx context.Context,c xrayconfig.Candidate,binaryPath,sha string)(Proof,error){
	proof:=starting(c,sha)
	if _,err:=xrayconfig.ValidateOffline(ctx,c,binaryPath,sha);err!=nil{
		return proof,fmt.Errorf("%w: %v",ErrCore,err)
	}
	proof.SyntaxValidated=true
	private,ports,err:=remap(c)
	if err!=nil{return proof,err}
	temp,err:=os.MkdirTemp("","dark-pro-canary-")
	if err!=nil{return proof,ErrStart}
	defer os.RemoveAll(temp)
	path:=filepath.Join(temp,"candidate.json")
	if err:=os.WriteFile(path,private,0600);err!=nil{return proof,ErrStart}
	// The two-stage validator enforces a private, correct-hash executable.
	// Retest output is never surfaced, as it may reveal Reality secrets.
	runCtx,cancel:=context.WithTimeout(ctx,startupTimeout)
	defer cancel()
	cmd:=exec.CommandContext(runCtx,binaryPath,"run","-config",path)
	cmd.Dir=temp
	cmd.Env=[]string{"PATH=/usr/bin:/bin","HOME="+temp}
	cmd.Stdout=io.Discard
	cmd.Stderr=io.Discard
	cmd.WaitDelay=250*time.Millisecond
	cmd.SysProcAttr=&syscall.SysProcAttr{Setpgid:true}
	if err:=cmd.Start();err!=nil{return proof,ErrStart}
	done:=make(chan error,1)
	go func(){done<-cmd.Wait()}()
	defer func(){
		// Kill the whole disposable process group so shell/Go subprocess
		// fixtures cannot outlive the private config on cancelled attempts.
		_ = syscall.Kill(-cmd.Process.Pid,syscall.SIGKILL)
		_ = cmd.Process.Kill()
		select{case <-done:case <-time.After(1500*time.Millisecond):}
	}()
	alive:=map[int]bool{}
	ticker:=time.NewTicker(60*time.Millisecond)
	defer ticker.Stop()
	for {
		for _,port:=range ports{
			if alive[port]{continue}
			conn,dialErr:=net.DialTimeout("tcp4",net.JoinHostPort("127.0.0.1",strconv.Itoa(port)),80*time.Millisecond)
			if dialErr==nil{
				alive[port]=true
				_ = conn.Close()
			}
		}
		if len(alive)==len(ports){
			proof.LoopbackBound=true
			proof.RemappedListeners=len(ports)
			proof.Status="loopback_canary_bound_then_stopped_not_active"
			return proof,nil
		}
		select{
		case <-runCtx.Done():
			return proof,ErrNotReady
		case <-done:
			return proof,ErrNotReady
		case <-ticker.C:
		}
	}
}

// No caller can use the canary's remapped addresses as a subscription link.
// Repeated calls always create a fresh ephemeral isolated process.
func ValidateRemappedOnly(c xrayconfig.Candidate)(bool,error){
	private,ports,err:=remap(c)
	if err!=nil{return false,err}
	if len(ports)==0{return false,ErrCandidate}
	var root struct{
		Inbounds []struct{Listen string `json:"listen"`;Port int `json:"port"`} `json:"inbounds"`
	}
	if err:=json.NewDecoder(bytes.NewReader(private)).Decode(&root);err!=nil{return false,ErrCandidate}
	for i,in:=range root.Inbounds{
		if in.Listen!="127.0.0.1"||in.Port!=ports[i] {return false,ErrCandidate}
	}
	return true,nil
}
