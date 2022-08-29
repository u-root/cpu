// Copyright 2018-2019 the u-root Authors. All rights reserved
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package client

import (
	"errors"
	"strconv"
	"testing"
)

func TestBadVsockHost(t *testing.T) {
	var want = strconv.ErrSyntax

	if _, _, err := vsockDial("z", "0"); !errors.Is(err, want) {
		t.Fatalf("Dial: got %v, want %v", err, want)
	}
	if _, _, err := vsockDial("42", "z"); !errors.Is(err, want) {
		t.Fatalf("Dial: got %v, want %v", err, want)
	}
}
