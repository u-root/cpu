// Copyright 2018-2019 the u-root Authors. All rights reserved
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package client

import (
	"fmt"
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
		got := GetKeyFile(test.host, test.file)
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
		{"apu2", "17010", "17010"},
		// This test ensures we never default to port 22
		{"bogus", "", "17010"},
		{"bogus", "2222", "2222"},
	} {
		got, err := GetPort(test.host, test.port)
		if err != nil {
			t.Errorf("getPort(%q, %q): got %v, want nil", test.host, test.port, nil)
		}
		if got != test.want {
			t.Errorf("getPort(%q, %q): got %q, want %q", test.host, test.port, got, test.want)
		}
	}
}

func TestNew(t *testing.T) {
	c := Command("cputest", "ls", "-l")
	if err := c.Close(); err != nil {
		t.Fatalf("Close: got %v, want nil", err)
	}
}

func TestDialNoAuth(t *testing.T) {
	h := GetHostName("cputest")
	if len(h) == 0 {
		t.Skip()
	}
	c := Command(h, "ls", "-l")
	if err := c.Dial(); err == nil {
		t.Errorf("Dial: got nil, want err")
	}
	if err := c.Close(); err != nil {
		t.Fatalf("Close: got %v, want nil", err)
	}
}

// hostkeyport looks up a host, key, port triple.
// failure is not an option.
func hostkeyport() (host string, key string, port string, err error) {
	h := "cputest"
	host = GetHostName(h)
	if len(host) == 0 {
		return
	}
	key = GetKeyFile(h, "")
	if len(key) == 0 {
		err = fmt.Errorf("No key for host %s", h)
		return
	}
	port, err = GetPort(h, "")
	return
}

func TestDialAuth(t *testing.T) {
	t.Skipf("Figure out how to set up a cpud for this test")
	h, k, p, err := hostkeyport()
	if len(h) == 0 {
		t.Skip()
	}
	if err != nil {
		t.Skipf("%v", err)
	}
	// From this test forward, at least try to get a port.
	// For this test, there must be a key.

	c := Command(h, "ls", "-l")
	c.PrivateKeyFile = k
	if err := c.SetOptions(WithNameSpace(DefaultNameSpace), WithPort(""), WithNetwork("tcp")); err != nil {
		t.Fatalf("Options(): %v != nil", err)
	}
	if c.Port != p {
		t.Fatalf("c.Port(%v) != port(%v)", c.Port, p)
	}
	if err := c.Dial(); err != nil {
		t.Fatalf("Dial: got %v, want nil", err)
	}
	if err := c.Close(); err != nil {
		t.Fatalf("Close: got %v, want nil", err)
	}
}

func TestDialRun(t *testing.T) {
	t.Skipf("Figure out how to set up a cpud for this test")
	h, k, p, err := hostkeyport()
	if len(h) == 0 {
		t.Skip()
	}
	if err != nil {
		t.Skipf("%v", err)
	}
	v = t.Logf
	// From this test forward, at least try to get a port.
	// For this test, there must be a key.

	c := Command(h, "ls", "-l")
	if err := c.SetOptions(WithPrivateKeyFile(k), WithPort(p), WithRoot("/"), WithNameSpace(DefaultNameSpace)); err != nil {
		t.Fatalf("SetOptions: %v != nil", err)
	}
	if err := c.Dial(); err != nil {
		t.Fatalf("Dial: got %v, want nil", err)
	}
	if err = c.Start(); err != nil {
		t.Fatalf("Start: got %v, want nil", err)
	}
	if err := c.SessionIn.Close(); err != nil {
		t.Errorf("Close stdin: Got %v, want nil", err)
	}
	if err := c.Wait(); err != nil {
		t.Fatalf("Wait: got %v, want nil", err)
	}

	r, err := c.Outputs()
	if err != nil {
		t.Errorf("Outputs: got %v, want nil", err)
	}
	t.Logf("c.Run: (%v, %q, %q)", err, r[0].String(), r[1].String())
	if err := c.Close(); err != nil {
		t.Fatalf("Close: got %v, want nil", err)
	}
}

// This requires a dial but not a run. It also requires
// that /dev/tty be working.
func TestSetupInteractive(t *testing.T) {
	t.Skipf("Figure out how to set up a cpud for this test")
	h, k, p, err := hostkeyport()
	if len(h) == 0 {
		t.Skip()
	}
	if err != nil {
		t.Skipf("%v", err)
	}
	v = t.Logf
	// From this test forward, at least try to get a port.
	// For this test, there must be a key.

	c := Command(h, "ls", "-l")
	if err := c.SetOptions(WithPrivateKeyFile(k), WithPort(p), WithRoot("/"), WithNameSpace(DefaultNameSpace)); err != nil {
		t.Fatalf("SetOptions: %v != nil ", err)
	}
	if err := c.Dial(); err != nil {
		t.Fatalf("Dial: got %v, want nil", err)
	}
	if err = c.Start(); err != nil {
		t.Fatalf("Start: got %v, want nil", err)
	}
	if err := c.SessionIn.Close(); err != nil {
		t.Errorf("Close stdin: Got %v, want nil", err)
	}
	if err := c.Wait(); err != nil {
		t.Fatalf("Wait: got %v, want nil", err)
	}
	r, err := c.Outputs()
	if err != nil {
		t.Errorf("Outputs: got %v, want nil", err)
	}
	t.Logf("c.Run: (%v, %q, %q)", err, r[0].String(), r[1].String())
	if err := c.Close(); err != nil {
		t.Fatalf("Close: got %v, want nil", err)
	}
}
