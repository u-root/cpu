// Copyright 2018-2019 the u-root Authors. All rights reserved
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package cpu

import (
	"fmt"
	"log"
	"strings"
	"time"

	// We use this ssh because it implements port redirection.
	// It can not, however, unpack password-protected keys yet.
	// TODO: get rid of krpty
	// We use this ssh because it can unpack password-protected private keys.

	"golang.org/x/crypto/ssh"
)

var v = func(string, ...interface{}) {}

// ClientConfig defines configuration for a client.
// TODO: add 9p timeout and other options.
type ClientConfig struct {
	SSH            *ssh.ClientConfig
	HostkeyFile    string
	PrivateKeyFile string
	root           string
}

// Client is a cpu client.
type Client struct {
	ssh *ssh.Client
}

// Session is a single CPU session.
type Session struct {
	*ssh.Session
}

// NewClient returns a filled-in Client.
func NewClient(root string, opt ...func(s *ssh.ClientConfig) error) (*ClientConfig, error) {
	c := &ClientConfig{root: root, SSH: &ssh.ClientConfig{}}
	for _, f := range opt {
		if err := f(c.SSH); err != nil {
			return nil, err
		}
	}
	return c, nil
}

// Dial implements ssh.Dial for cpu.
func (c *ClientConfig) Dial(n, a string) (*ssh.Client, error) {
	cl, err := ssh.Dial(n, a, c.SSH)
	if err != nil {
		return nil, fmt.Errorf("Failed to dial: %v", err)
	}

	// From setting up the forward to having the nonce written back to us,
	// we only allow 100ms. This is a lot, considering that at this point,
	// the sshd has forked a server for us and it's waiting to be
	// told what to do. We suggest that making the deadline a flag
	// would be a bad move, since people might be tempted to make it
	// large.
	deadline, err := time.ParseDuration("1s") // *timeout9P)
	if err != nil {
		return nil, err
	}

	// Arrange port forwarding from remote ssh to our server.
	// Request the remote side to open port 5640 on all interfaces.
	// Note: cl.Listen returns a TCP listener with network is "tcp"
	// or variants. This lets us use a listen deadline.
	l, err := cl.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("First cl.Listen %v", err)
	}
	ap := strings.Split(l.Addr().String(), ":")
	if len(ap) == 0 {
		return nil, fmt.Errorf("Can't find a port number in %v", l.Addr().String())
	}
	port9p := ap[len(ap)-1]
	v("listener %T %v addr %v port %v", l, l, l.Addr().String(), port9p)

	nonce, err := generateNonce()
	if err != nil {
		log.Fatalf("Getting nonce: %v", err)
	}
	go srv(l, c.root, nonce, deadline)
	//	cmd = fmt.Sprintf("%s -port9p %v", cmd, port9p)
	//	env = append(env, "CPUNONCE="+nonce.String())
	return cl, nil
}

// NewSession implements ssh.NewSession for cpu.
func (c *Client) NewSession() (*Session, error) {
	session, err := c.ssh.NewSession()
	return &Session{Session: session}, err
}
