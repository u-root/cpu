// Copyright 2018-2019 the u-root Authors. All rights reserved
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"context"
	"fmt"
	"log"
	"math"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	// We use this ssh because it implements port redirection.
	// It can not, however, unpack password-protected keys yet.
	"github.com/gliderlabs/ssh"
	"github.com/mdlayher/vsock"
	"github.com/u-root/cpu/server"
	"github.com/u-root/u-root/pkg/ulog"
	"golang.org/x/sys/unix"
)

const any = math.MaxUint32

type modifier struct {
	name string
	f    func(*ssh.Server) error
}

func (m *modifier) String() string {
	return m.name
}

var (
	// modifiers are called once the ssh server is set up and before s.Serve() in serve()
	// is called.
	// There is only one current use, namely, mDNS.
	// Once we better understand how to structure modifiers it may be time for another
	// package.
	// Before the modifier runs, the ssh server is ready for operation. The question remains:
	// what if a modifier fails? For now, code will log errors but continue.
	// This makes debugging failed modifiers far easier.
	// Therefore, it is the responsibility of modifiers not to break the ssh server.
	// They should ensure correctness before commiting changes.
	modifiers []*modifier
)

// hang hangs for a VERY long time.
// This aids diagnosis, else you lose all messages in the
// kernel panic as init exits.
func hang() {
	log.Printf("hang")
	time.Sleep(10000 * time.Second)
	log.Printf("done hang")
}

func commonsetup() error {
	if *debug {
		server.SetVerbose(verbose)
		v = log.Printf
		if *klog {
			ulog.KernelLog.Reinit()
			v = ulog.KernelLog.Printf
		}
	}
	return nil
}

func initsetup() error {
	if err := unix.Mount("cpu", "/tmp", "tmpfs", 0, ""); err != nil {
		log.Printf("CPUD:Warning: tmpfs mount on /tmp (%v) failed. There will be no 9p mount", err)
	}
	if err := cpuSetup(); err != nil {
		log.Printf("CPUD:CPU setup error with cpu running as init: %v", err)
	}
	cmds := [][]string{{"/bin/sh"}, {"/bbin/dhclient", "-v", "--retry", "1000"}}
	verbose("Try to run %v", cmds)

	for _, v := range cmds {
		verbose("Let's try to run %v", v)
		if _, err := os.Stat(v[0]); os.IsNotExist(err) {
			verbose("it's not there")
			continue
		}

		cmd := exec.Command(v[0], v[1:]...)
		cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
		cmd.SysProcAttr = &syscall.SysProcAttr{Setctty: true, Setsid: true}
		verbose("Run %v", cmd)
		if err := cmd.Start(); err != nil {
			verbose("Error starting %v: %v", v, err)
			continue
		}
	}
	verbose("Kicked off startup jobs, now serve cpu sessions")
	return nil
}

func listen(network, port string) (net.Listener, error) {
	// Sadly, vsock is not in the standard Go net package.
	// It should be but ...
	var (
		ln  net.Listener
		err error
	)

	switch network {
	case "vsock":
		var p uint64
		p, err = strconv.ParseUint(port, 0, 16)
		if err != nil {
			return nil, err
		}
		ln, err = vsock.ListenContextID(any, uint32(p), nil)

	case "unix", "unixgram", "unixpacket":
		// net.JoinHostPort really ought to work for UDS, but it's very naive.
		// It does not take the network type as a parameter.
		ln, err = net.Listen(network, port)

	default:
		ln, err = net.Listen(network, net.JoinHostPort("", port))
	}
	return ln, err
}

func register(network, addr string, timeout time.Duration) error {
	if len(addr) == 0 {
		return nil
	}
	// This is a bit of a hack for now, to test the idea.
	// if registeraddr is not empty, Dial it over the network,
	// and send the string "ok".
	// This may fail because the host may have incorrectly requested a registration
	// but may not be listening, OR may just be looking for a cheap way to set a
	// delay between listen and accept for networks that are not well behaved.
	c, err := net.DialTimeout(network, addr, timeout)
	// N.B.: it fails to connect, it fails to connect.
	if err != nil {
		return err
	}
	defer c.Close()
	// if it works, it works. If not, log it and move on.
	if _, err := c.Write([]byte("ok")); err != nil {
		return fmt.Errorf("Writing OK to register address: %w", err)
	}
	return nil
}

func serve(cpud string) error {
	s, err := server.New(*pubKeyFile, *hostKeyFile, cpud)
	if err != nil {
		log.Printf(`New(%q, %q): %v`, *pubKeyFile, *hostKeyFile, err)
		hang()
	}
	verbose("Server is %v", s)

	ln, err := listen(*network, *port)
	if err != nil {
		return err
	}

	log.Printf("Listening on %v", ln.Addr())

	// register can return an error, but it should not block serving.
	if err := register(*network, *registerAddr, *registerTO); err != nil {
		verbose("Register(%v, %v, %d): %v", *network, *registerAddr, *registerTO, err)
	}

	// If there is a hup, we stop serving.
	sigs := make(chan os.Signal, 1)

	signal.Notify(sigs, syscall.SIGHUP)

	go func() {
		sig := <-sigs
		log.Printf("Received %v, Shutdown cpud listen ...", sig)
		if err := s.Shutdown(context.Background()); err != nil {
			log.Printf("Shutdown done")
		}
	}()

	for _, m := range modifiers {
		if err := m.f(s); err != nil {
			log.Printf("Error %v from modifier %s", err, m)
		}
	}

	if err := s.Serve(ln); err != ssh.ErrServerClosed {
		log.Printf("s.Daemon(): %v != %v", err, ssh.ErrServerClosed)
		hang()
	}
	verbose("Daemon returns")
	hang()
	return nil
}
