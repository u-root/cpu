package main

import (
	"errors"
	"os"
	"syscall"
	"testing"
)

func TestListen(t *testing.T) {
	// The net package predates wrapped errors and such, and
	// as such is inconvenient. So we divide tests into error-full
	// and error-free.

	// All these tests expect err to be non-nil.
	var tests = []struct {
		network string
		port    string
	}{
		{"blarg", "17010"},
		{"vsock", "xyz"},
	}

	for _, tt := range tests {
		_, err := listen(tt.network, tt.port)
		if err == nil {
			t.Errorf("Listen(%v, %v): nil != some error", tt.network, tt.port)
			continue
		}
	}

	// These should all work.
	var oktests = []struct {
		network string
		port    string
	}{
		{"tcp", "17010"},
		{"tcp4", "17010"},
		{"tcp6", "17010"},
		{"vsock", "17010"},
		{"unix", "@foobar"},
	}

	for _, tt := range oktests {
		ln, err := listen(tt.network, tt.port)
		if err != nil {
			var sysErr *os.SyscallError
			if errors.As(err, &sysErr) && sysErr.Err == syscall.EAFNOSUPPORT {
				// e.g. no ipv4 or vsock
				continue;
			}
			t.Errorf("Listen(%v, %v): err != nil", tt.network, tt.port)
			continue
		}
		if ln == nil {
			t.Errorf("Listen(%v, %v): ln is nil, not non-nil", tt.network, tt.port)
			continue
		}
		if err := ln.Close(); err != nil {
			t.Errorf("%v.Close: %v != nil", ln, err)
		}
	}
}
