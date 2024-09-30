// Copyright 2018-2022 the u-root Authors. All rights reserved
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build !linux
// +build !linux

package server

import (
	"fmt"
	"net"
	"os"
	"os/exec"
)

type Session struct {
	binds string
	port9p string
}

// Namespace assembles a NameSpace for this cpud, iff CPU_NAMESPACE
// is set.
// CPU_NAMESPACE can be the empty string.
// It also requires that CPU_NONCE exist.
func (s *Session) Namespace() (error, error) {
	var warning error
	// Get the nonce and remove it from the environment.
	// N.B. We do not save the nonce in the cpu struct.
	nonce := os.Getenv("CPUNONCE")
	os.Unsetenv("CPUNONCE")
	verbose("namespace is %q", s.binds)

	// Connect to the socket, return the nonce.
	a := net.JoinHostPort("localhost", s.port9p)
	verbose("Dial %v", a)
	so, err := net.Dial("tcp", a)
	if err != nil {
		return warning, fmt.Errorf("CPUD:Dial 9p port: %v", err)
	}
	verbose("Connected: write nonce %s\n", nonce)
	if _, err := fmt.Fprintf(so, "%s", nonce); err != nil {
		return warning, fmt.Errorf("CPUD:Write nonce: %v", err)
	}
	verbose("Wrote the nonce")
	// Zero it. I realize I am not a crypto person.
	// improvements welcome.
	copy([]byte(nonce), make([]byte, len(nonce)))

	return warning, fmt.Errorf("CPUD: cannot use 9p connection yet")
}

func osMounts() error {
	return nil
}

func logopts() {
}

func command(n string, args ...string) *exec.Cmd {
	cmd := exec.Command(n, args...)
	return cmd
}

// runSetup performs kernel-specific operations for starting a Session.
func runSetup() error {
	return nil
}
