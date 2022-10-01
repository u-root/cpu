// Copyright 2022 the u-root Authors. All rights reserved
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package ds

import (
	"testing"
	"time"
)

// TODO: test client (will be easier once server is here because we'll need to launch it)

func TestClient(t *testing.T) {
	v = t.Logf

	q := dsQuery{
		Type:   "_ncpu,_tcp",
		Domain: "local",
	}

	// simple lookup, no meta data
	_, _, err := Lookup(q)
	if err != nil {
		// TODO: this is expected until we have a server in place
		// t.Fatal(err)
	}

	// TODO: lookup with standard requirement

	// TODO: lookup with standard requirement + sort

	// TODO: lookup with nonsense service (no result expected)

	// TODO: lookup with unsatisfiable requirement (no result expected)

	// TODO: lookup with poorly formed URI

	// TODO: lookup with poorly formed requirement

	// TODO: lookup with poorly formed sort
}

func TestDnsSdStart(t *testing.T) {
	v = t.Logf
	dsTxt := make(map[string]string, 0)
	err := Register("testInstance", "local", "_ncpu._tcp", "", 17010, dsTxt)
	if err != nil {
		t.Fatalf(`Register: %v != nil`, err)
	}
	time.Sleep(5 * time.Second)
}