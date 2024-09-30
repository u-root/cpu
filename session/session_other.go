// Copyright 2018-2022 the u-root Authors. All rights reserved
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build !linux
// +build !linux

package session

import (
	"fmt"
	"os"
)

// Namespace assembles a NameSpace for this cpud, iff CPU_NAMESPACE
// is set.
// CPU_NAMESPACE can be the empty string.
// It also requires that CPU_NONCE exist.
func (s *Session) Namespace() error {
	return fmt.Errorf("CPUD: 9p mounts are only valid on Linux:%w", os.ErrNotExist)
}

func osMounts() error {
	return nil
}

// runSetup performs kernel-specific operations for starting a Session.
func runSetup() error {
	return nil
}
