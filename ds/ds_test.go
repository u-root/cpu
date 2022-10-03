// Copyright 2022 the u-root Authors. All rights reserved
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package ds

import (
	"fmt"
	"testing"
	"time"
)

func TestClient(t *testing.T) {
	v = t.Logf

	q := dsQuery{
		Type:   "_nobody._tcp",
		Domain: "local",
	}

	// simple lookup with no server and bad service, it better fail
	_, _, err := Lookup(q)
	if err == nil {
		t.Fatal(fmt.Errorf("Lookup of bad service didn't fail"))
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
	_, _, err = Lookup(q)
	if err != nil {
		t.Error(err)
	}

	// default uri parse
	q, err = Parse(DsDefault)
	if err != nil {
		t.Fatal(err)
	}
}
