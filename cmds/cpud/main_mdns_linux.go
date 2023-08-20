// Copyright 2018-2019 the u-root Authors. All rights reserved
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build mDNS

package main

import (
	"flag"
	"fmt"
	"log"
	"strconv"

	// We use this ssh because it implements port redirection.
	// It can not, however, unpack password-protected keys yet.
	"github.com/gliderlabs/ssh"
	"github.com/u-root/cpu/ds"
)

var (
	dsEnabled   = flag.Bool("dnssd", true, "advertise service using DNSSD")
	dsInstance  = flag.String("dsInstance", "", "DNSSD instance name")
	dsDomain    = flag.String("dsDomain", "local", "DNSSD domain")
	dsService   = flag.String("dsService", "_ncpu._tcp", "DNSSD Service Type")
	dsInterface = flag.String("dsInterface", "", "DNSSD Interface")
	dsTxtStr    = flag.String("dsTxt", "", "DNSSD key-value pair string parameterizing advertisement")
	dsTxt       map[string]string
)

func init() {
	modifiers = append(modifiers, &modifier{f: servemDNS, name: "mDNS"})
}

type handleWrapper struct {
	handle func(s ssh.Session)
}

func (w *handleWrapper) handler(s ssh.Session) {
	ds.Tenant(1)
	w.handle(s)
	ds.Tenant(-1)
}

// servemDNS wraps an existing ssh session with an mDNS instance.
func servemDNS(s *ssh.Server) error {
	if *debug {
		ds.Verbose(log.Printf)
	}
	dsTxt = ds.ParseKv(*dsTxtStr)

	v("Advertising w/dnssd %q", dsTxt)
	p, err := strconv.Atoi(*port)
	if err != nil {
		return fmt.Errorf("Could not parse port: %s, %w", *port, err)
	}

	err = ds.Register(*dsInstance, *dsDomain, *dsService, *dsInterface, p, dsTxt)
	if err != nil {
		return fmt.Errorf("Could not advertise with dns-sd: %w", err)
	}
	defer ds.Unregister()

	wrap := &handleWrapper{
		handle: s.Handler,
	}
	s.Handler = wrap.handler

	return nil
}
