package xrayconfig

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

var (
	ErrCoreUnavailable = errors.New("trusted Xray binary is unavailable")
	ErrCoreMismatch = errors.New("Xray executable hash mismatch")
	ErrCoreRejected = errors.New("pinned Xray rejected candidate")
	ErrCandidateInvalid = errors.New("invalid or altered Xray candidate")
)

const maxXrayBinaryBytes = 128 << 20

type Validation struct {
	ConfigSHA256 string `json:"config_sha256"`
	ManifestSHA256 string `json:"manifest_sha256"`
	PinnedXraySHA256 string `json:"pinned_xray_sha256"`
	XraySyntaxValidated bool `json:"xray_syntax_validated"`
	CanActivate bool `json:"can_activate"`
	NeedsDataPlaneProof bool `json:"needs_data_plane_proof"`
	NeedsSubscriptionCommit bool `json:"needs_subscription_commit"`
}

// ValidateOffline runs only "xray run -test -config <private file>" against a
// caller-pinned trusted Xray executable. It neither applies a configuration
// nor touches systemd, old Nodes, active users or customer subscriptions.
// Core output is deliberately discarded: it could contain private keys.
func ValidateOffline(ctx context.Context, c Candidate, binaryPath, expectedBinarySHA string) (Validation,error) {
	var empty Validation
	if c.CanActivate||!c.RequiresXrayTest||!c.RequiresDataPlaneProof||
		!c.RequiresSubscriptionCommit||len(c.Config)==0||
		len(c.Config)>MaxCandidateBytes||len(c.SHA256)!=64||
		len(c.ManifestSHA256)!=64||len(c.Listeners)==0 {
		return empty,ErrCandidateInvalid
	}
	h:=sha256.Sum256(c.Config)
	if hex.EncodeToString(h[:])!=c.SHA256 {
		return empty,ErrCandidateInvalid
	}
	if !filepath.IsAbs(binaryPath)||strings.TrimSpace(binaryPath)!=binaryPath ||
		len(expectedBinarySHA)!=64 {
		return empty,ErrCoreUnavailable
	}
	expected,err:=hex.DecodeString(expectedBinarySHA)
	if err!=nil||len(expected)!=32 {
		return empty,ErrCoreUnavailable
	}
	info,err:=os.Lstat(binaryPath)
	if err!=nil||!info.Mode().IsRegular()||info.Mode().Perm()&0111==0||
		info.Mode().Perm()&0022!=0||info.Size()>maxXrayBinaryBytes {
		return empty,ErrCoreUnavailable
	}
	file,err:=os.Open(binaryPath)
	if err!=nil{return empty,ErrCoreUnavailable}
	hash:=sha256.New()
	_,err=io.Copy(hash,io.LimitReader(file,maxXrayBinaryBytes+1))
	closeErr:=file.Close()
	if err!=nil||closeErr!=nil{return empty,ErrCoreUnavailable}
	if !equalHash(hash.Sum(nil),expected){return empty,ErrCoreMismatch}
	dir,err:=os.MkdirTemp("","dark-pro-xray-offline-")
	if err!=nil{return empty,ErrCoreUnavailable}
	defer os.RemoveAll(dir)
	path:=filepath.Join(dir,"candidate.json")
	if err:=os.WriteFile(path,c.Config,0600);err!=nil{return empty,ErrCoreUnavailable}

	// A caller can impose a shorter deadline. The fixed upper bound means
	// an invalid or deadlocked core never blocks a Node heartbeat indefinitely.
	runCtx,cancel:=context.WithTimeout(ctx,8*time.Second)
	defer cancel()
	command:=exec.CommandContext(runCtx,binaryPath,"run","-test","-config",path)
	command.Dir=dir
	// Bound subprocess and inherited descriptor waits after cancellation.
	command.WaitDelay=250*time.Millisecond
	command.Stdout=io.Discard
	command.Stderr=io.Discard
	// Command runs only a locally pinned executable. No shell expansion.
	if err:=command.Run();err!=nil{return empty,ErrCoreRejected}
	return Validation{
		ConfigSHA256:c.SHA256, ManifestSHA256:c.ManifestSHA256,
		PinnedXraySHA256:expectedBinarySHA,
		XraySyntaxValidated:true,CanActivate:false,
		NeedsDataPlaneProof:true,NeedsSubscriptionCommit:true,
	},nil
}

func equalHash(left,right []byte)bool{
	if len(left)!=len(right){return false}
	var diff byte
	for i:=range left {diff|=left[i]^right[i]}
	return diff==0
}
