//go:build !linux

package corebarrierv2

func openFinalCapture(number, binding, config string) (finalCapture, error) { return nil, ErrBarrier }
