//go:build linux

package managedxray

import (
 "time"
 uc "github.com/darktunnelmika/dark-xray-pro/internal/usagecheckpoint"
)

// ObservationProcess is an in-process supervisor hook, NOT a public API.
// It returns a kernel-bound identity only for the owned, leased Xray child.
// It neither opens a StatsService socket nor enables production metering.
func(r *Runtime)ObservationProcess()(uc.ProcessToken,error){
 r.mu.Lock();defer r.mu.Unlock()
 if r.closed||r.fenced||r.active==nil||r.active.NoListeners||r.process.dead()||!time.Now().Before(r.deadline){return uc.ProcessToken{},uc.ErrEpoch}
 token,err:=uc.Process(r.process.cmd.Process.Pid)
 if err!=nil{return uc.ProcessToken{},err}
 if !r.process.bound(r.active.Listeners){return uc.ProcessToken{},uc.ErrEpoch}
 return token,nil
}
