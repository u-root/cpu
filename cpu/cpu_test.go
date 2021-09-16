// Copyright 2018-2019 the u-root Authors. All rights reserved
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package cpu

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	config "github.com/kevinburke/ssh_config"
)

func TestConfig(t *testing.T) {
	v = t.Logf
	var tconfig = `
Host *.example.com
  Compression yes

Host apu2
	HostName apu22
	Port 2222
	User root
	IdentityFile ~/.ssh/apu2_rsa

`

	cfg, err := config.Decode(strings.NewReader(tconfig))
	if err != nil {
		t.Fatal(err)
	}

	for _, test := range []struct {
		host string
		key  string
		want string
	}{
		{"test.example.com", "Compression", "yes"},
		{"apu2", "IdentityFile", "~/.ssh/apu2_rsa"},
	} {
		val, err := cfg.Get(test.host, test.key)
		if err != nil {
			t.Error(err)
			continue
		}
		if val != test.want {
			t.Errorf("config.Get(%q, %q): got %q, want %q", test.host, test.key, val, test.want)
		}
	}

	h := os.Getenv("HOME")
	for _, test := range []struct {
		host string
		file string
		want string
	}{
		{"apu2", "abc", "abc"},
		{"apu2", "~abc", filepath.Join(h, "abc")},
	} {
		got := getKeyFile(test.host, test.file)
		if got != test.want {
			t.Errorf("getKeyFile(%q, %q): got %q, want %q", test.host, test.file, got, test.want)
		}
	}
	for _, test := range []struct {
		host string
		port string
		want string
	}{
		// Can't really test this atm.
		//{"apu2", "", "2222"},
		{"apu2", "23", "23"},
		// This test ensures we never default to port 22
		{"bogus", "", "23"},
		{"bogus", "2222", "2222"},
	} {
		got := getPort(test.host, test.port)
		if got != test.want {
			t.Errorf("getPort(%q, %q): got %q, want %q", test.host, test.port, got, test.want)
		}
	}
}
