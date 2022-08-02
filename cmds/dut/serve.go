// Copyright 2022 the u-root Authors. All rights reserved
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"fmt"
	"io/ioutil"
	"log"
	"net"
	"time"

	// We use this ssh because it implements port redirection.
	// It can not, however, unpack password-protected keys yet.
	"github.com/gliderlabs/ssh" // TODO: get rid of krpty
	"github.com/u-root/cpu/server"
	"golang.org/x/sys/unix"
)

// hang hangs for a VERY long time.
// This aids diagnosis, else you lose all messages in the
// kernel panic as init exits.
func hang() {
	log.Printf("hang")
	time.Sleep(10000 * time.Second)
	log.Printf("done hang")
}

func serve(network, addr string, pubKey, hostKey []byte) error {
	if err := unix.Mount("cpu", "/tmp", "tmpfs", 0, ""); err != nil {
		log.Printf("CPUD:Warning: tmpfs mount on /tmp (%v) failed. There will be no 9p mount", err)
	}

	// Note that the keys are in a private mount; no need for a temp file.
	if err := ioutil.WriteFile("/tmp/key.pub", pubKey, 0644); err != nil {
		return fmt.Errorf("writing pubkey: %w", err)
	}
	if len(hostKey) > 0 {
		if err := ioutil.WriteFile("/tmp/hostkey", hostKey, 0644); err != nil {
			return fmt.Errorf("writing hostkey: %w", err)
		}
	}

	v("Kicked off startup jobs, now serve ssh")
	s, err := server.New("/tmp/key.pub", "/tmp/hostkey")
	if err != nil {
		log.Printf(`New(%q, %q): %v != nil`, "/tmp/key.pub", "/tmp/hostkey", err)
		hang()
	}
	v("Server is %v", s)

	ln, err := net.Listen(network, addr)
	if err != nil {
		log.Printf("net.Listen(): %v != nil", err)
		hang()
	}
	v("Listening on %v", ln.Addr())
	if err := s.Serve(ln); err != ssh.ErrServerClosed {
		log.Printf("s.Daemon(): %v != %v", err, ssh.ErrServerClosed)
		hang()
	}
	v("Daemon returns")
	hang()
	return nil
}
