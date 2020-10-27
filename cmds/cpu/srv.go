// Copyright 2018-2019 the u-root Authors. All rights reserved
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"time"

	"github.com/hugelgupf/p9/p9"
)

// Made harder as you can't set a read deadline on ssh.Conn
func srv(l net.Listener, root string, n nonce, deadline time.Time) {
	// We only accept once
	defer l.Close()
	ctx, cancel := context.WithDeadline(context.Background(), deadline)
	defer cancel()
	var (
		errs chan error
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
		// Without this cancel, the select seems to stick on the context. Fix me.
		cancel()
		errs <- nil
	}()

	select {
	case <-ctx.Done():
		if ctx.Err() != context.Canceled {
			log.Fatalf("Timeout on nonce: %v", ctx.Err())
		}
	case err := <-errs:
		if err != nil {
			log.Fatalf("srv: %v", err)
		}
	}
	if err := p9.NewServer(&cpu9p{path: root}).Handle(c, c); err != nil {
		log.Fatalf("Serving cpu remote: %v", err)
	}
}
