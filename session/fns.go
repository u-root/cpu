// Copyright 2018-2022 the u-root Authors. All rights reserved
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package session

import (
	"os"
	"os/exec"
	"os/signal"

	"golang.org/x/sys/unix"
)

func verbose(f string, a ...interface{}) {
	v("session:"+f, a...)
}

func runCmd(c *exec.Cmd) error {
	sigChan := make(chan os.Signal, 1)
	defer close(sigChan)
	signal.Notify(sigChan, unix.SIGTERM, unix.SIGINT)
	defer signal.Stop(sigChan)
	errChan := make(chan error, 1)
	defer close(errChan)
	go func() {
		errChan <- c.Run()
	}()
	var err error
loop:
	for {
		select {
		case sig := <-sigChan:
			if sigErr := c.Process.Signal(sig); sigErr != nil {
				verbose("sending %v to %q: %v", sig, c.Args[0], sigErr)
			} else {
				verbose("signal %v sent to %q", sig, c.Args[0])
			}
		case err = <-errChan:
			break loop
		}
	}
	return err
}
