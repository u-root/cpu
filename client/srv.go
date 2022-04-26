// Copyright 2018-2019 the u-root Authors. All rights reserved
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package client

import (
	"fmt"
	"io"
	"log"
	"net"
	"time"

	"github.com/hugelgupf/p9/p9"
	"github.com/u-root/u-root/pkg/ulog"
)

// Made harder as you can't set a read deadline on ssh.Conn
func (c *Cmd) srv(l net.Listener) error {
	// We only accept once
	defer l.Close()
	var (
		errs = make(chan error)
		s    net.Conn
		err  error
	)
	go func() {
		V("srv: try to accept l %v", l)
		s, err = l.Accept()
		V("Accept: %v %v", s, err)
		if err != nil {
			errs <- fmt.Errorf("accept 9p socket: %v", err)
			return
		}
		V("srv got %v", s)
		var rn nonce
		if _, err := io.ReadAtLeast(s, rn[:], len(rn)); err != nil {
			errs <- fmt.Errorf("Reading nonce from remote: %v", err)
			return
		}
		V("srv: read the nonce back got %s", rn)
		if c.nonce.String() != rn.String() {
			errs <- fmt.Errorf("nonce mismatch: got %s but want %s", rn, c.nonce)
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
	case <-time.After(c.Timeout):
		return fmt.Errorf("cpud did not connect for more than %v", c.Timeout)
	case err := <-errs:
		if err != nil {
			return fmt.Errorf("srv: %v", err)
		}
	}
	// If we are debugging, add the option to trace records.
	V("Start serving on %v", c.Root)
	if Debug9p {
		if Dump9p {
			log.SetOutput(DumpWriter)
			log.SetFlags(log.Ltime | log.Lmicroseconds)
			ulog.Log = log.New(DumpWriter, "9p", log.Ltime|log.Lmicroseconds)
		}
		if err := p9.NewServer(&cpu9p{path: c.Root}, p9.WithServerLogger(ulog.Log)).Handle(s, s); err != nil {
			if err != io.EOF {
				log.Printf("Serving cpu remote: %v", err)
				return err
			}
		}
		return nil
	}
	if err := p9.NewServer(&cpu9p{path: c.Root}).Handle(s, s); err != nil {
		if err != io.EOF {
			log.Printf("Serving cpu remote: %v", err)
			return err
		}
	}
	return nil
}
