// Copyright 2022 the u-root Authors. All rights reserved
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package client

import (
	"errors"
	"strconv"
	"testing"
)

// vsockIDPort gets a client id and a port from host and port
// The id and port are uint32.
func TestVsockIdPort(t *testing.T) {
	for _, tt := range []struct {
		name string
		host string
		port string
		h    uint32
		p    uint32
		err  error
	}{
		{name: "badhostportn", host: "", port: "", h: 0, p: 0, err: strconv.ErrSyntax},
		{name: "noport", host: "1", port: "", h: 0, p: 0, err: strconv.ErrSyntax},
		{name: "nohost", host: "", port: "1", h: 0, p: 0, err: strconv.ErrSyntax},
		{name: "ok", host: "1", port: "2", h: 1, p: 2, err: nil},
		{name: "badhostnum", host: "z", port: "2", h: 0, p: 0, err: strconv.ErrSyntax},
		{name: "ok", host: "0x42", port: "17010", h: 0x42, p: 17010, err: nil},
	} {
		h, p, err := vsockIDPort(tt.host, tt.port)
		if !errors.Is(err, tt.err) || h != tt.h || p != tt.p {
			t.Errorf("%s:vsockIDPort(%s, %s): (%v, %v, %v) != (%v, %v, %v)", tt.name, tt.host, tt.port, h, p, err, tt.h, tt.p, tt.err)
		}
	}
}

func TestParseBinds(t *testing.T) {
	for _, tt := range []struct {
		namespace string
		fstab     string
		err       error
	}{
		{"", "", nil},
		{"/lib", "/tmp/cpu/lib /lib none defaults,bind 0 0\n", nil},
		{"/lib=/arm/lib", "/tmp/cpu/arm/lib /lib none defaults,bind 0 0\n", nil},
		{"/lib=/arm/lib:/bin", "/tmp/cpu/arm/lib /lib none defaults,bind 0 0\n/tmp/cpu/bin /bin none defaults,bind 0 0\n", nil},
		{"/lib=/arm/lib:/bin=/b/bin", "/tmp/cpu/arm/lib /lib none defaults,bind 0 0\n/tmp/cpu/b/bin /bin none defaults,bind 0 0\n", nil},
		{"/a:/b:/c:/d", "/tmp/cpu/a /a none defaults,bind 0 0\n/tmp/cpu/b /b none defaults,bind 0 0\n/tmp/cpu/c /c none defaults,bind 0 0\n/tmp/cpu/d /d none defaults,bind 0 0\n", nil},
		{"/lib=:/bin=/b/bin", "", strconv.ErrSyntax},
		{"=/lib:/bin=/b/bin", "", strconv.ErrSyntax},
		{"/a::/bin=/b/bin", "", strconv.ErrSyntax},
		// Test the weird case that a bind name can contain an = sign.
		// There is not imaginable case where we need this but ...
		// note also that only remote names contain = signs pending
		// more complex parsing. Perhaps it should not be allowed at all.
		{"/a:/bin==/b/bin", "/tmp/cpu/a /a none defaults,bind 0 0\n/tmp/cpu/=/b/bin /bin none defaults,bind 0 0\n", nil},
	} {
		f, err := parseBinds(tt.namespace)
		if !errors.Is(err, tt.err) || f != tt.fstab {
			t.Errorf("parseBinds(%q): (%q,%v) != (%q,%v)", tt.namespace, f, err, tt.fstab, tt.err)
		}
	}
}

func TestJoinFSTab(t *testing.T) {
	for _, tt := range []struct {
		tables []string
		fstab  string
	}{
		{tables: nil, fstab: ""},
		{tables: []string{""}, fstab: "\n"},
		{tables: []string{"a b c"}, fstab: "a b c\n"},
		{tables: []string{"a b c\n\n", "d e f"}, fstab: "a b c\nd e f\n"},
		{tables: []string{"a b c\n\n", "d e f"}, fstab: "a b c\nd e f\n"},
		{tables: []string{"a b c\n\n", "d e f\n1 2 3"}, fstab: "a b c\nd e f\n1 2 3\n"},
	} {
		if fstab := joinFSTab(tt.tables...); fstab != tt.fstab {
			t.Errorf("joinFSTab(%q): %q != %q", tt.tables, fstab, tt.fstab)
		}
	}
}
