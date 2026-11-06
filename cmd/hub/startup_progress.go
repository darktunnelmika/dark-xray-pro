package main

import (
	"log"
	"time"
)

// Startup progress names are fixed in main. Never log configuration values,
// identities, credentials or store errors here. Completion means initialization
// finished; listener health is still verified separately.
type startupProgress struct {
	phase        string
	phaseStarted time.Time
	started      time.Time
}

func newStartupProgress() *startupProgress {
	return &startupProgress{started: time.Now()}
}

func (s *startupProgress) finishPhase() {
	if s.phase == "" {
		return
	}
	log.Printf("PRO startup phase=%s state=complete elapsed_ms=%d", s.phase, time.Since(s.phaseStarted).Milliseconds())
	s.phase = ""
}

func (s *startupProgress) next(phase string) {
	s.finishPhase()
	s.phase = phase
	s.phaseStarted = time.Now()
	log.Printf("PRO startup phase=%s state=begin", phase)
}

func (s *startupProgress) complete() {
	s.finishPhase()
	log.Printf("PRO startup state=initialization_complete elapsed_ms=%d", time.Since(s.started).Milliseconds())
}
