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

func TestQuoteArg(t *testing.T) {
	var tests = []struct {
		in  string
		out string
	}{
		{in: "", out: "''"},
		{in: "arg", out: "'arg'"},
		{in: "arg space", out: "'arg space'"},
		{in: "\"", out: "'\"'"},
		{in: "'", out: "''\"'\"''"},
		{in: "'a'", out: "''\"'\"'a'\"'\"''"},
	}
	for i, tt := range tests {
		result := quoteArg(tt.in)
		if result != tt.out {
			t.Errorf("%d: quoteArg(%s) = %s, expected %s", i, tt.in, result, tt.out)
		}
	}
}

func TestCmdWithDisablePrivateKey(t *testing.T) {
	c := Command("someserver", "bash")
	if c.DisablePrivateKey {
		t.Fatal("DisablePrivateKey of Cmd created by Command() is expected to be false, got true")
	}
	if err := c.SetOptions(WithDisablePrivateKey(true)); err != nil {
		t.Fatalf("WithDisablePrivateKey returns unexpected err %v", err)
	}
	if !c.DisablePrivateKey {
		t.Fatal("WithDisablePrivateKey(true) should set DisablePrivateKey to true, got false")
	}
}
