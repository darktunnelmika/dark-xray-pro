//go:build !linux

package probackup

import (
	"context"
	"io"
)

// Restore has no platform fallback that could replace an existing destination.
func Restore(context.Context, io.Reader, string, string, Limits, func(string, Manifest) error) (Manifest, error) {
	return Manifest{}, ErrUnsupported
}
