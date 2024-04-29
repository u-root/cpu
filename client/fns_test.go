// Copyright 2022 the u-root Authors. All rights reserved
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package client

import (
	"errors"
	"path"
	"strconv"
	"strings"
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
	td := func(s string) string {
		// Because cpud only runs on Linux, for now,
		// the name of tmp is /tmp, not os.TempDir.
		// This allows running many of these tests on
		// non-Linux systems.
		return path.Join("/tmp", s)
	}
	for _, tt := range []struct {
		namespace string
		fstab     string
		err       error
	}{
		// N.B.: Even on windows, the separator is OK, b/c this is a path for a Linux
		// fstab. This is also why we must use path.Join, not filepath.Join.
		{"", "", nil},
		{"/lib", td("cpu/lib") + " /lib none defaults,bind 0 0\n", nil},
		{"/lib=/arm/lib", td("cpu/arm/lib") + " /lib none defaults,bind 0 0\n", nil},
		{"/lib=/arm/lib:/bin", td("cpu/arm/lib") + " /lib none defaults,bind 0 0\n" + td("cpu/bin") + " /bin none defaults,bind 0 0\n", nil},
		{"/lib=/arm/lib:/bin=/b/bin", td("cpu/arm/lib") + " /lib none defaults,bind 0 0\n" + td("cpu/b/bin") + " /bin none defaults,bind 0 0\n", nil},
		{"/a:/b:/c:/d", td("cpu/a") + " /a none defaults,bind 0 0\n" + td("cpu/b") + " /b none defaults,bind 0 0\n" + td("cpu/c") + " /c none defaults,bind 0 0\n" + td("cpu/d") + " /d none defaults,bind 0 0\n", nil},
		{"/lib=:/bin=/b/bin", "", strconv.ErrSyntax},
		{"=/lib:/bin=/b/bin", "", strconv.ErrSyntax},
		{"/a::/bin=/b/bin", "", strconv.ErrSyntax},
		// Test the weird case that a bind name can contain an = sign.
		// There is not imaginable case where we need this but ...
		// note also that only remote names contain = signs pending
		// more complex parsing. Perhaps it should not be allowed at all.
		{"/a:/bin==/b/bin", td("cpu/a") + " /a none defaults,bind 0 0\n" + td("cpu/=/b/bin") + " /bin none defaults,bind 0 0\n", nil},
	} {
		f, err := ParseBinds(tt.namespace)
		if !errors.Is(err, tt.err) {
			t.Errorf("ParseBinds(%q): %v != %v", tt.namespace, err, tt.err)
			continue
		}
		if f != tt.fstab {
			t.Errorf("ParseBinds(%q): %q != %q", tt.namespace, f, tt.fstab)
			w := strings.Split(f, "\n")
			g := strings.Split(tt.fstab, "\n")
			if len(w) == len(g) {
				for i := range g {
					t.Errorf("\n%d:%q\n%d:%q", i, g[i], i, w[i])
				}
				continue
			}
			t.Errorf("got %d lines, want %d lines", len(g), len(w))
			for i := range g {
				t.Logf("%d:%q", i, g[i])
			}
			for i := range w {
				t.Logf("%d:%q", i, w[i])
			}
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
		{tables: []string{"a b c\n\n", "\nd e f\n1 2 3"}, fstab: "a b c\nd e f\n1 2 3\n"},
	} {
		if fstab := JoinFSTab(tt.tables...); fstab != tt.fstab {
			t.Errorf("JoinFSTab(%q): %q != %q", tt.tables, fstab, tt.fstab)
		}
	}
}

func TestUserKeyConfigWithDisablePrivateKey(t *testing.T) {
	cmd := &Cmd{
		PrivateKeyFile:    DefaultKeyFile,
		DisablePrivateKey: true,
	}
	if err := cmd.UserKeyConfig(); err != nil {
		t.Fatalf("UserKeyConfig() returns unexpected err: %v", err)
	}
	if len(cmd.config.Auth) != 0 {
		t.Fatalf("cmd.config.Auth: got %v, want []", cmd.config.Auth)
	}
}
