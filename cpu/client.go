// Copyright 2018-2019 the u-root Authors. All rights reserved
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package cpu

import (
	"fmt"
	"io"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	// We use this ssh because it implements port redirection.
	// It can not, however, unpack password-protected keys yet.
	// TODO: get rid of krpty
	// We use this ssh because it can unpack password-protected private keys.

	"golang.org/x/crypto/ssh"
)

const (
	// From setting up the forward to having the nonce written back to us,
	// we would like to default to 100ms. This is a lot, considering that at this point,
	// the sshd has forked a server for us and it's waiting to be
	// told what to do.
	defaultTimeOut = time.Duration(100 * time.Millisecond)
	defaultPort    = 23
)

// V allows debug printing.
var V = func(string, ...interface{}) {}

// Cmd is a cpu client.
// It implements as much of exec.Command as makes sense.
type Cmd struct {
	config  ssh.ClientConfig
	client  *ssh.Client
	session *ssh.Session
	// CPU-specific options.
	// As in exec.Command, these controls are exposed and can
	// be set directly.
	Host           string
	Args           []string
	Root           string
	HostKeyFile    string
	PrivateKeyFile string
	Port           uint16
	Timeout        time.Duration
	Env            []string
	Stdin          io.WriteCloser
	Stdout         io.Reader
	Stderr         io.Reader

	nonce   nonce
	network string // This is a variable but we expect it will always be tcp
	port9p  uint16 // port on which we serve 9p
	cmd     string // The command is built up, bit by bit, as we configure the client
}

// Command implements exec.Command. The required parameter is a host.
// The args arg args to $SHELL. If there are no args, then starting $SHELL
// is assumed.
func Command(host string, args ...string) *Cmd {
	// TODO: use lookpath, or something like it, but
	// such a test will need awareness of CPU_NAMESPACE
	// n, err := exec.LookPath(cmd)
	// if err != nil {
	// 	return nil, err
	// }
	// By convention, if there are no args, then the remote
	// command is the user's shell
	if len(args) == 0 {
		args = []string{os.Getenv("SHELL")}
	}
	return &Cmd{
		Host:    host,
		Args:    args,
		Port:    defaultPort,
		Timeout: defaultTimeOut,
		config: ssh.ClientConfig{
			User:            os.Getenv("USER"),
			HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		},
		network: "tcp",
		// Safety first: if they want a namespace, they must say so
		Root: "",
		// The command, always, at least, starts with "cpu"
		cmd: "cpud -remote -bin cpud",
	}
}

// WithPrivateKeyFile adds a private key file to a Cmd
func (c *Cmd) WithPrivateKeyFile(key string) *Cmd {
	c.PrivateKeyFile = key
	return c
}

// WithHostKeyFile adds a host key to a Cmd
func (c *Cmd) WithHostKeyFile(key string) *Cmd {
	c.HostKeyFile = key
	return c
}

// WithPort adds a port to a Cmd
func (c *Cmd) WithPort(port uint16) *Cmd {
	c.Port = port
	return c
}

// WithRoot adds a root to a Cmd
func (c *Cmd) WithRoot(root string) *Cmd {
	c.Root = root
	return c
}

// Dial implements ssh.Dial for cpu.
// Additionaly, if Cmd.Root is not "", it
// starts up a server for 9p requests.
func (c *Cmd) Dial() error {
	if err := c.UserKeyConfig(); err != nil {
		return err
	}
	addr := fmt.Sprintf("%s:%d", c.Host, c.Port)
	cl, err := ssh.Dial(c.network, addr, &c.config)
	V("cpu:ssh.Dial(%s, %s, %v): (%v, %v)", c.network, addr, c.config, cl, err)
	if err != nil {
		return fmt.Errorf("Failed to dial: %v", err)
	}

	c.client = cl
	// Specifying a root is required for a remote namespace.
	if len(c.Root) == 0 {
		return nil
	}
	// If the namespace is empty, a nameserver won't be started
	if _, ok := os.LookupEnv("CPU_NAMESPACE"); !ok {
		return fmt.Errorf("Root is set to %q but there is no CPU_NAMESPACE", c.Root)
	}

	// Arrange port forwarding from remote ssh to our server.
	// Request the remote side to open port 5640 on all interfaces.
	// Note: cl.Listen returns a TCP listener with network is "tcp"
	// or variants. This lets us use a listen deadline.
	l, err := cl.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return fmt.Errorf("cpu client listen for forwarded 9p port %v", err)
	}
	V("ssh.listener %v", l.Addr().String())
	ap := strings.Split(l.Addr().String(), ":")
	if len(ap) == 0 {
		return fmt.Errorf("Can't find a port number in %v", l.Addr().String())
	}
	port9p, err := strconv.ParseUint(ap[len(ap)-1], 0, 16)
	if err != nil {
		return fmt.Errorf("Can't find a 16-bit port number in %v", l.Addr().String())
	}
	c.port9p = uint16(port9p)

	V("listener %T %v addr %v port %v", l, l, l.Addr().String(), port9p)

	nonce, err := generateNonce()
	if err != nil {
		log.Fatalf("Getting nonce: %v", err)
	}
	c.Env = append(c.Env, "CPUNONCE="+nonce.String())
	c.nonce = nonce
	go c.srv(l)
	return nil
}

// Start implements exec.Start for CPU.
func (c *Cmd) Start() error {
	var err error
	if c.client == nil {
		return fmt.Errorf("Cmd has no client")
	}
	if c.session, err = c.client.NewSession(); err != nil {
		return err
	}

	c.Env = append(c.Env, "CPUNONCE="+c.nonce.String())
	if err := c.SetEnv(c.Env...); err != nil {
		return err
	}
	if c.Stdin, err = c.session.StdinPipe(); err != nil {
		return err
	}
	if c.Stdout, err = c.session.StdoutPipe(); err != nil {
		return err
	}
	if c.Stderr, err = c.session.StderrPipe(); err != nil {
		return err
	}

	// Unlike the cpu command source, which assumes an SSH-like stdin, SSHStdin in this package will need to be set up explicitly.
	//go c.SSHStdin(i, c.Stdin)
	// N.B.: if a server was needed, it was
	// started in Dial.

	// assemble the command.
	cmd := fmt.Sprintf("%s -port9p %v %q", c.cmd, c.port9p, strings.Join(c.Args, " "))
	V("call session.Start(%s)", cmd)
	if err := c.session.Start(cmd); err != nil {
		return fmt.Errorf("Failed to run %v: %v", c, err.Error())
	}
	return nil
}

// Wait waits for a Cmd to finish.
func (c *Cmd) Wait() error {
	return c.session.Wait()
}

// Run runs a command with Start, and waits for it to finish with Wait.
func (c *Cmd) Run() error {
	if err := c.Start(); err != nil {
		return err
	}
	return c.Wait()
}

// Close ends a cpu session, doing whatever is needed.
func (c *Cmd) Close() (errs []error) {
	if c.session != nil {
		if err := c.session.Close(); err != nil && err != io.EOF {
			errs = append(errs, fmt.Errorf("Closing session: got %v, want nil", err))
		}
	}
	return errs
}
