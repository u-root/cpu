// Copyright 2018-2022 the u-root Authors. All rights reserved
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build !linux
// +build !linux

package server

import (
	"os/exec"
)

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
