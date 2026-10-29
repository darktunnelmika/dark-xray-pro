//go:build linux

package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/darktunnelmika/dark-xray-pro/internal/backupcontrol"
	"github.com/darktunnelmika/dark-xray-pro/internal/backupimport"
	"github.com/darktunnelmika/dark-xray-pro/internal/backupworker"
)

func run(args []string) int {
	if len(args) != 1 || (args[0] != "run" && args[0] != "recover") {
		fmt.Fprintln(os.Stderr, "backup worker requires run or recover; configuration path is fixed")
		return 2
	}
	syscall.Umask(0077)
	config, e := backupworker.LoadConfig()
	if e != nil {
		fmt.Fprintln(os.Stderr, "backup worker configuration refused")
		return 3
	}
	store, e := backupcontrol.OpenStore(backupworker.StateDirectory, config.PublicRecipient)
	if e != nil {
		fmt.Fprintln(os.Stderr, "backup worker journal unavailable")
		return 3
	}
	defer store.Close()
	runtime, e := backupworker.NewRecoveryRuntime(config)
	if e != nil {
		fmt.Fprintln(os.Stderr, "backup worker runtime refused")
		return 3
	}
	worker := backupworker.New(store, runtime, runtime, config)
	// Fulfil an existing resume duty before validating the optional writer,
	// archive filesystem, or starting the control socket.
	recoverCtx, recoverCancel := context.WithTimeout(context.Background(), time.Duration(config.ResumeTimeoutSeconds+10)*time.Second)
	recoverErr := worker.RecoverService(recoverCtx)
	recoverCancel()
	if recoverErr != nil {
		fmt.Fprintln(os.Stderr, "backup worker recovery requires attention")
		return 4
	}
	if args[0] == "recover" {
		return 0
	}
	runtime, e = backupworker.NewLinuxRuntime(config)
	if e != nil {
		fmt.Fprintln(os.Stderr, "backup worker writer unavailable; service recovery completed")
		return 3
	}
	worker = backupworker.New(store, runtime, runtime, config)
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	options := backupcontrol.HandlerOptions{RunnerStatus: worker.Status, OpenArchive: runtime.OpenArchive}
	// An unavailable upload inbox must never prevent service recovery or
	// disable the existing create/download control plane.
	inbox, inboxErr := backupimport.OpenStore(backupimport.Directory)
	if inboxErr == nil {
		defer inbox.Close()
		options.Uploads = inbox
	} else {
		fmt.Fprintln(os.Stderr, "backup upload inbox unavailable; existing backup controls remain available")
	}
	handler := backupcontrol.NewHandler(store, options)
	serverDone := make(chan error, 1)
	go func() { serverDone <- backupcontrol.ServeUnix(ctx, backupcontrol.SocketPath, runtime.UID(), handler) }()
	// Establish the local command/status surface before a queued job can stop
	// HUB. No asynchronous command runs from an HTTP request context.
	client := backupcontrol.NewClient(backupcontrol.SocketPath)
	ready := false
	for attempt := 0; attempt < 40; attempt++ {
		select {
		case <-serverDone:
			cancel()
			return 4
		default:
		}
		check, done := context.WithTimeout(ctx, 200*time.Millisecond)
		_, err := client.Status(check)
		done()
		if err == nil {
			ready = true
			break
		}
		if ctx.Err() != nil {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !ready {
		cancel()
		<-serverDone
		fmt.Fprintln(os.Stderr, "backup worker socket unavailable")
		return 4
	}
	workDone := make(chan error, 1)
	go func() { workDone <- worker.Run(ctx) }()
	var workerErr error
	serverExited := false
	serverFailed := false
	select {
	case workerErr = <-workDone:
		cancel()
	case serverErr := <-serverDone:
		serverExited = true
		serverFailed = serverErr != nil || ctx.Err() == nil
		cancel()
		workerErr = <-workDone
	case <-ctx.Done():
		workerErr = <-workDone
	}
	cancel()
	if !serverExited {
		serverFailed = <-serverDone != nil
	}
	// Normal cancellation also gets a final independent recovery attempt.
	recovery, cancelRecovery := context.WithTimeout(context.Background(), time.Duration(config.ResumeTimeoutSeconds+10)*time.Second)
	recoveryErr := worker.RecoverService(recovery)
	cancelRecovery()
	if workerErr != nil || recoveryErr != nil || serverFailed {
		fmt.Fprintln(os.Stderr, "backup worker stopped with an incomplete operation; durable recovery state retained")
		return 4
	}
	return 0
}

func main() { os.Exit(run(os.Args[1:])) }
