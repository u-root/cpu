// Copyright 2022 the u-root Authors. All rights reserved
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package client

import (
	"errors"
	"strconv"
	"testing"
)

// vsockIdPort gets a client id and a port from host and port
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
		h, p, err := vsockIdPort(tt.host, tt.port)
		if !errors.Is(err, tt.err) || h != tt.h || p != tt.p {
			t.Errorf("%s:vsockIdPort(%s, %s): (%v, %v, %v) != (%v, %v, %v)", tt.name, tt.host, tt.port, h, p, err, tt.h, tt.p, tt.err)
		}
	}
}
