// Copyright 2018-2019 the u-root Authors. All rights reserved
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"fmt"
	"io"
	"log"
	"net"
	"time"

	"github.com/hugelgupf/p9/p9"
)

// Made harder as you can't set a read deadline on ssh.Conn
func srv(l net.Listener, root string, n nonce, deadline time.Duration) {
	// We only accept once
	defer l.Close()
	var (
		errs = make(chan error)
		c    net.Conn
		err  error
	)
	go func() {
		v("srv: try to accept")
		c, err = l.Accept()
		if err != nil {
			errs <- fmt.Errorf("accept 9p socket: %v", err)
			return
		}
		v("srv got %v", c)
		var rn nonce
		if _, err := io.ReadAtLeast(c, rn[:], len(rn)); err != nil {
			errs <- fmt.Errorf("Reading nonce from remote: %v", err)
			return
		}
		v("srv: read the nonce back got %s", rn)
		if n.String() != rn.String() {
			errs <- fmt.Errorf("nonce mismatch: got %s but want %s", rn, n)
			return
		}
		errs <- nil
	}()

	// This is interesting. If we return an error from the timeout
	// in this select, the Accept above *never* succeeds. It always hangs.
	// If we return at all, for any reason, same result.
	// I have no clue what's up here, since the usage exactly
	// follows most other packages, but I suspect it's some
	// conflicting usage of time with the ssh package. I'm past caring.
	// To be continued ...
	select {
	case <-time.After(deadline):
		log.Fatalf("cpud did not connect for more than %v", deadline)
	case err := <-errs:
		if err != nil {
			log.Fatalf("srv: %v", err)
		}
	}
	if err := p9.NewServer(&cpu9p{path: root}).Handle(c, c); err != nil {
		if err != io.EOF {
			log.Printf("Serving cpu remote: %v", err)
		}
	}
}
