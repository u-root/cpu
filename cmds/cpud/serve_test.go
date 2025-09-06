package main

import (
	"errors"
	"fmt"
	"net"
	"os"
	"syscall"
	"testing"
	"time"
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
		// CI no longer lets us do vsock, it seems.
		// {"vsock", "xyz"},
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
			// For any error, if it is vsock, print something
			// and continue.
			if tt.network == "vsock" {
				t.Logf("vsock test fails: %v; ignoring", err)
				continue
			}
			if errors.As(err, &sysErr) && sysErr.Err == syscall.EAFNOSUPPORT {
				t.Logf("%s is not supported; continuing", tt.network)
				// e.g. no ipv4 or vsock
				continue
			}
			// If it is in use, not a lot to do.
			if errors.As(err, &sysErr) && sysErr.Err == syscall.EADDRINUSE {
				t.Logf("%s:%s is in use, so can not test; continuing", tt.network, tt.port)
				// e.g. no ipv4 or vsock
				continue
			}
			t.Errorf("Listen(%v, %v): got %v, want nil", tt.network, tt.port, err)
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

func TestRegister(t *testing.T) {
	// There is not a lot of consistency in errors and error values and messages across kernels.
	// There are a few things we can count on:
	// nobody listens on all of ports 1, 21, or 23 any more. So try to get an error
	// from trying to connect to them and use it in the test.

	var addr string
	for _, p := range []int{1, 21, 23} {
		addr = fmt.Sprintf(":%d", p)
		if _, err := net.DialTimeout("tcp", addr, time.Second); err != nil {
			break
		}
	}
	if len(addr) == 0 {
		t.Skip("Can't get addr which gets econnrefused")
	}

	// Now, interestingly, all errors returned from the test below, even connection refused,
	// do not work with errors.Is with the error return above. Even when Unwrapped, even when Is
	// is used.
	// So ... we just do all the error cases, then work on the
	// non-error cases.
	// All these tests expect err to be non-nil.
	var tests = []struct {
		network string
		addr    string
		timeout time.Duration
	}{
		{network: "tcp", addr: addr, timeout: time.Duration(0)},
		{network: "tcp", addr: addr, timeout: time.Duration(time.Second)},
	}

	v = t.Logf
	// These tests are purely for the error case. We're not listening.
	for _, tt := range tests {
		if err := register(tt.network, tt.addr, tt.timeout); err == nil {
			t.Errorf("register(%v, %v, %v): nil != an error", tt.network, tt.addr, tt.timeout)
			continue
		}
	}
	// See if, once listening, we can register.
	l, err := net.Listen("tcp", ":")
	if err != nil {
		t.Fatalf("Can not listen on tcp: %v", err)
	}
	defer l.Close()
	if err := register("tcp", l.Addr().String(), 0); err != nil {
		t.Fatalf("register(\"tcp\", %v): %v != nil", l.Addr(), err)
	}
	c, err := l.Accept()
	if err != nil {
		t.Fatalf("Accept(\"tcp\", %v): %v != nil", l.Addr().String(), err)
	}
	defer c.Close()
	var ok [8]byte
	n, err := c.Read(ok[:])
	if err != nil {
		t.Fatalf("Read(\"tcp\", %v): %v != nil", l.Addr().String(), err)
	}
	if string(ok[:n]) != "ok" {
		t.Errorf("Read(\"tcp\", %v): %q != %q", l.Addr().String(), ok[:n], "ok")
	}

}
