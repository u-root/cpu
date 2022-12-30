// Copyright 2022 the u-root Authors. All rights reserved
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package ds

import (
	"fmt"
	"testing"
	"time"
)

// test parsing logic
func TestParse(t *testing.T) {
	v = t.Logf

	var tus = []struct {
		uri    string
		result dsQuery
		error  bool
	}{
		{"bad", dsQuery{}, true},
		{"dnssd://", dsQuery{Type: "_ncpu._tcp", Domain: "local"}, false},
		{"dnssd://local", dsQuery{Type: "_ncpu._tcp", Domain: "local"}, false},
		{"dnssd://localhost", dsQuery{Type: "_ncpu._tcp", Domain: "localhost"}, false},
		{"dnssd://example.com", dsQuery{Type: "_ncpu._tcp", Domain: "example.com"}, false},
		{"dnssd://_ncpu._tcp", dsQuery{Type: "_ncpu._tcp", Domain: "local"}, false},
		{"dnssd://_nobody._tcp", dsQuery{Type: "_nobody._tcp", Domain: "local"}, false},
		{"dnssd://_nobody", dsQuery{Type: "_nobody", Domain: "local"}, false}, // malformed
		{"dnssd://instance._ncpu._tcp", dsQuery{Instance: "instance", Type: "_ncpu._tcp", Domain: "local"}, false},
		{"dnssd://instance._ncpu._tcp.example.com", dsQuery{Instance: "instance", Type: "_ncpu._tcp", Domain: "example.com"}, false},
		{"dnssd://?sort=cpu.pcnt", dsQuery{Type: "_ncpu._tcp", Domain: "local"}, false},
	}

	for _, x := range tus {
		d, error := Parse(x.uri)
		r := x.result
		if x.error {
			if error == nil {
				t.Fatal(fmt.Errorf("failed to detect error parsing %s", x.uri))
			}
			continue
		} else {
			if error != nil {
				t.Fatal(fmt.Errorf("failed to parse URI %s", x.uri))
			}
		}
		if len(r.Type) != 0 {
			if r.Type != d.Type {
				t.Fatal(fmt.Errorf("parsing %s resulted bad Type parsing %s!=%s", x.uri, r.Type, d.Type))
			}
		}
		if len(r.Domain) != 0 {
			if r.Domain != d.Domain {
				t.Fatal(fmt.Errorf("parsing %s resulted bad Domain parsing %s!=%s", x.uri, r.Domain, d.Type))
			}
		}
		if len(r.Instance) != 0 {
			if r.Instance != d.Instance {
				t.Fatal(fmt.Errorf("parsing %s resulted bad Instance parsing %s!=%s", x.uri, r.Instance, d.Type))
			}
		}
	}
}

func TestClient(t *testing.T) {
	v = t.Logf

	q := dsQuery{
		Type:   "_nobody._tcp",
		Domain: "local",
	}

	// simple lookup with no server and bad service, it better fail
	if _, err := Lookup(q, 1); err == nil {
		t.Fatalf("Lookup of bad service didn't fail: got nil, want an err")
	}
}

func TestDnsSdStart(t *testing.T) {
	v = t.Logf
	dsTxt := make(map[string]string, 0)

	DefaultTxt(dsTxt)
	err := Register("testInstance", "local", "_ncpu._tcp", "", 17010, dsTxt)
	if err != nil {
		t.Fatalf(`Register: %v != nil`, err)
	}
	time.Sleep(10 * time.Second)

	q := dsQuery{
		Type:   "_ncpu._tcp",
		Domain: "local",
	}

	// simple lookup with no server and bad service, it better succeed
	if _, err := Lookup(q, 1); err != nil {
		t.Error(err)
	}

	// default uri parse
	if _, err := Parse(DsDefault); err != nil {
		t.Fatal(err)
	}
}
