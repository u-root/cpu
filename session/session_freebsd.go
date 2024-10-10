// Copyright 2018-2022 the u-root Authors. All rights reserved
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package session

// Namespace does nothing; no 9p on freebsd yet.
func (s *Session) Namespace() error {
	return nil
}

func osMounts() error {
	return nil
}

// runSetup performs kernel-specific operations for starting a Session.
func runSetup() error {
	return nil
}
