// Copyright 2018-2022 the u-root Authors. All rights reserved
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package server

import (
	"bytes"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gliderlabs/ssh"
	"github.com/u-root/cpu/session"
)

func TestNewServer(t *testing.T) {
	s, err := New("key.pub", "", os.Args[0])
	if err != nil {
		t.Fatalf(`New("key.pub", "", %q): %v != nil`, os.Args[0], err)
	}
	if s.PublicKeyHandler == nil {
		t.Fatalf(`New("key.pub", "", %q) returns a server without a public key handler`, os.Args[0])
	}
	t.Logf("New server: %v", s)
}

func TestNewServerWithoutKey(t *testing.T) {
	s, err := New("", "", os.Args[0])
	if err != nil {
		t.Fatalf(`New("", "", %q): %v != nil`, os.Args[0], err)
	}
	if s.PublicKeyHandler != nil {
		t.Fatalf(`New("", "", %q) returns a server with a public key handler`, os.Args[0])
	}
	t.Logf("New server: %v", s)
}

func TestRemoteNoNameSpace(t *testing.T) {
	if os.Getuid() != 0 {
		t.Skipf("Skipping as we are not root")
	}
	v = t.Logf
	s := session.New("", "date")
	o, e := &bytes.Buffer{}, &bytes.Buffer{}
	s.Stdin, s.Stdout, s.Stderr = nil, o, e
	if err := s.Run(); err != nil {
		t.Fatalf(`s.Run("", "date"): %v != nil`, err)
	}
	t.Logf("%q %q", o, e)
	if len(o.String()) == 0 {
		t.Errorf("no command output: \"\" != non-zero-length string")
	}
	if e.String() != "" {
		t.Errorf("command error: %q != %q", e.String(), "")
	}
}

func gendotssh(dir, config string) (string, error) {
	dotssh := filepath.Join(dir, ".ssh")
	if err := os.MkdirAll(dotssh, 0700); err != nil {
		return "", err
	}

	// https://github.com/kevinburke/ssh_config/issues/2
	hackconfig := fmt.Sprintf(string(sshConfig), filepath.Join(dir, ".ssh"))
	for _, f := range []struct {
		name string
		val  []byte
	}{
		{name: "config", val: []byte(hackconfig)},
		{name: "hostkey", val: hostKey},
		{name: "server", val: privateKey},
		{name: "server.pub", val: publicKey},
	} {
		if err := os.WriteFile(filepath.Join(dotssh, f.name), f.val, 0644); err != nil {
			return "", err
		}
	}
	return hackconfig, nil
}

func TestDaemonStart(t *testing.T) {
	v = t.Logf
	s, err := New("", "", os.Args[0])
	if err != nil {
		t.Fatalf(`New("", "", %q): %v != nil`, os.Args[0], err)
	}

	ln, err := net.Listen("tcp", "")
	if err != nil {
		t.Fatalf("net.Listen(): %v != nil", err)
	}
	t.Logf("Listening on %v", ln.Addr())
	// this is a racy test.
	go func() {
		time.Sleep(5 * time.Second)
		s.Close()
	}()

	if err := s.Serve(ln); err != ssh.ErrServerClosed {
		t.Fatalf("s.Daemon(): %v != %v", err, ssh.ErrServerClosed)
	}
	t.Logf("Daemon returns")
}

func TestDaemonConnectHelper(t *testing.T) {
	if _, ok := os.LookupEnv("GO_WANT_DAEMON_HELPER_PROCESS"); !ok {
		t.Logf("just a helper")
		return
	}
	t.Logf("As a helper, we are supposed to run %q", args)
	s := session.New(port9p, args[0], args[1:]...)
	// Step through the things a server is supposed to do with a session
	if err := s.Run(); err != nil {
		log.Fatalf("CPUD(as remote):%v", err)
	}
}

var (
	args   []string
	port9p string
)

func TestMain(m *testing.M) {
	// Strip out the args after --
	x := -1
	var osargs = os.Args
	for i := range os.Args {
		if x > 0 {
			args = os.Args[i:]
			break
		}
		if os.Args[i] == "--" {
			osargs = os.Args[:i]
			x = i
		}
	}
	// Process any port9p directive
	if len(args) > 1 && args[0] == "-port9p" {
		port9p = args[1]
		args = args[2:]
	}
	os.Args = osargs
	// log.Printf("os.Args %v, args %v", os.Args, args)
	flag.Parse()
	os.Exit(m.Run())
}
