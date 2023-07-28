// Copyright 2018-2022 the u-root Authors. All rights reserved
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package session

import (
	"testing"
)

// Not sure testing this is a great idea but ... it works so ...
func TestDropPrivs(t *testing.T) {
	s := New("", "/bin/true")
	if err := s.DropPrivs(); err != nil {
		t.Fatalf("s.DropPrivs(): %v != nil", err)
	}
}
