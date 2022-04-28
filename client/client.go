// Copyright 2018-2019 the u-root Authors. All rights reserved
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package client

import (
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/hashicorp/go-multierror"
	"github.com/u-root/u-root/pkg/termios"
	"golang.org/x/crypto/ssh"
)

const (
	// From setting up the forward to having the nonce written back to us,
	// we would like to default to 100ms. This is a lot, considering that at this point,
	// the sshd has forked a server for us and it's waiting to be
	// told what to do.
	defaultTimeOut   = time.Duration(100 * time.Millisecond)
	defaultPort      = "23"
	DefaultNameSpace = "/lib:/lib64:/usr:/bin:/etc:/home"
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
	Host string
	// HostName as found in .ssh/config; set to Host if not found
	HostName       string
	Args           []string
	Root           string
	HostKeyFile    string
	PrivateKeyFile string
	Port           string
	Timeout        time.Duration
	Env            []string
	Stdin          io.WriteCloser
	Stdout         io.Reader
	Stderr         io.Reader
	Row            int
	Col            int
	t              *termios.TTYIO
	interactive    bool // Set if there are no arguments.
	// NameSpace is a string as defined in the cpu documentation.
	NameSpace string
	// FSTab is an fstab(5)-format string
	FSTab string
	// Ninep determines if client will run a 9P server
	Ninep bool

	nonce   nonce
	network string // This is a variable but we expect it will always be tcp
	port9p  uint16 // port on which we serve 9p
	cmd     string // The command is built up, bit by bit, as we configure the client
	closers []func() error
}

// Command implements exec.Command. The required parameter is a host.
// The args arg args to $SHELL. If there are no args, then starting $SHELL
// is assumed.
func Command(host string, args ...string) *Cmd {
	var interactive bool
	if len(args) == 0 {
		interactive = true
		shell, ok := os.LookupEnv("SHELL")
		// We've found in some cases SHELL is not set!
		if !ok {
			shell = "/bin/sh"
		}
		args = []string{shell}
	}

	col, row := 80, 40
	if w, err := termios.GetWinSize(0); err != nil {
		V("Can not get winsize: %v; assuming %dx%d", err, col, row)
	} else {
		col, row = int(w.Col), int(w.Row)
	}

	return &Cmd{
		Host:     host,
		HostName: GetHostName(host),
		Args:     args,
		Port:     defaultPort,
		Timeout:  defaultTimeOut,
		Row:      row,
		Col:      col,
		config: ssh.ClientConfig{
			User:            os.Getenv("USER"),
			HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		},
		interactive: interactive,
		network:     "tcp",
		// Safety first: if they want a namespace, they must say so
		Root: "",
		// The command, always, at least, starts with "cpu"
		// We ship this command because it does allow for
		// using non-cpud to start cpud in --remote mode.
		// We are kind of stuck with this default for now,
		// as the original cpu implementation requires it.
		// Also, there is the nagging concern that we're not
		// totally proper yet on the security issues
		// around letting users run arbitrary binaries.
		cmd: "cpud -remote",
	}
}

// With9P enables the 9P2000 server in cpu.
// The server is by default disabled. Ninep is sticky; if set by,
// e.g., WithNameSpace, the Principle of Least Confusion argues
// that it should remain set. Hence, we || it with its current value.
func (c *Cmd) With9P(p9 bool) *Cmd {
	c.Ninep = p9 || c.Ninep
	return c
}

// WithNameSpace sets the namespace to Cmd.There is no default: having some default
// violates the principle of least surprise for package users. If ns is non-empty
// the Ninep is forced on.
func (c *Cmd) WithNameSpace(ns string) *Cmd {
	c.NameSpace = ns
	if len(ns) > 0 {
		c.Ninep = true
	}
	return c
}

// AddFSTab reads a file for the FSTab member.
func (c *Cmd) AddFSTab(fstab string) error {
	if len(fstab) == 0 {
		return nil
	}
	b, err := ioutil.ReadFile(fstab)
	if err != nil {
		return fmt.Errorf("Reading fstab: %w", err)
	}
	c.FSTab = string(b)
	return nil
}

// SetTimeout sets the 9p timeout.
func (c *Cmd) SetTimeout(timeout string) error {
	d, err := time.ParseDuration(timeout)
	if err != nil {
		return err
	}
	c.Timeout = d
	return nil
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
func (c *Cmd) WithPort(port string) *Cmd {
	c.Port = port
	return c
}

// WithRoot adds a root to a Cmd
func (c *Cmd) WithRoot(root string) *Cmd {
	c.Root = root
	return c
}

// WithCpudCommand sets the initial command to run on the
// remote side. This is extremely helpful when testing new
// implementations of cpud, of little use otherwise.
func (c *Cmd) WithCpudCommand(cmd string) *Cmd {
	c.cmd = cmd
	return c
}

// SetPort sets the port in the Cmd.
// It calls GetPort with the passed-in port
// before assigning it.
func (c *Cmd) SetPort(port string) error {
	c.Port = port
	p, err := GetPort(c.HostName, c.Port)
	if err == nil {
		c.Port = p
	}
	return err
}

// Dial implements ssh.Dial for cpu.
// Additionaly, if Cmd.Root is not "", it
// starts up a server for 9p requests.
func (c *Cmd) Dial() error {
	if err := c.UserKeyConfig(); err != nil {
		return err
	}
	addr := net.JoinHostPort(c.HostName, c.Port)
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

	// Arrange port forwarding from remote ssh to our server.
	// Note: cl.Listen returns a TCP listener with network "tcp"
	// or variants. This lets us use a listen deadline.
	if c.Ninep {
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
		c.nonce = nonce
		c.Env = append(c.Env, "CPUNONCE="+nonce.String())
		if len(c.NameSpace) > 0 {
			c.Env = append(c.Env, "CPU_NAMESPACE="+c.NameSpace)
		}
		V("Set NONCE; set NAMESPACE to %q", "CPU_NAMESPACE="+c.NameSpace)
		go func(l net.Listener) {
			if err := c.srv(l); err != nil {
				log.Printf("9p server error: %v", err)
			}
		}(l)
	}
	if len(c.FSTab) > 0 {
		c.Env = append(c.Env, "CPU_FSTAB="+c.FSTab)
	}

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
	// Set up terminal modes
	modes := ssh.TerminalModes{
		ssh.ECHO:          0,     // disable echoing
		ssh.TTY_OP_ISPEED: 14400, // input speed = 14.4kbaud
		ssh.TTY_OP_OSPEED: 14400, // output speed = 14.4kbaud
	}

	// Request pseudo terminal
	V("c.session.RequestPty(\"ansi\", %v, %v, %#x", c.Row, c.Col, modes)
	if err := c.session.RequestPty("ansi", c.Row, c.Col, modes); err != nil {
		return fmt.Errorf("request for pseudo terminal failed: %v", err)
	}

	c.closers = append(c.closers, func() error {
		if err := c.session.Close(); err != nil && err != io.EOF {
			return fmt.Errorf("Closing session: got %v, want nil", err)
		}
		return nil
	})

	if err := c.SetEnv(c.Env...); err != nil {
		return err
	}
	if c.Stdin, err = c.session.StdinPipe(); err != nil {
		return err
	}
	c.closers = append([]func() error{func() error {
		c.Stdin.Close()
		return nil
	}}, c.closers...)

	if c.Stdout, err = c.session.StdoutPipe(); err != nil {
		return err
	}
	if c.Stderr, err = c.session.StderrPipe(); err != nil {
		return err
	}

	// Unlike the cpu command source, which assumes an SSH-like stdin,
	// but very much like es/exec, users of Stdin in this package
	// will need to set the IO.
	// e.g.,
	// go c.SSHStdin(i, c.Stdin)
	// N.B.: if a 9p server was needed, it was started in Dial.

	cmd := c.cmd
	if c.port9p != 0 {
		cmd += fmt.Sprintf(" -port9p %v", c.port9p)
	}
	// The ABI for ssh.Start uses a string, not a []string
	// On the other end, it splits the string back up
	// as needed, claiming to do proper unquote handling.
	// This means we have to take care about quotes on
	// our side.
	//
	// Be careful here: you want to use
	// %v, not %q. %q will quote the string, and when
	// ssh server unpacks it, this will look like one arg.
	// This will manifest as weird problems when you
	// cpu host ls -l and such. The ls -l will end up being
	// a single arg. Why does this happen on cpu and not ssh?
	// cpu, unlike ssh, does not pass the arguments to a shell.
	// Unlike Plan 9 shells, Linux shells do gargantuan amounts
	// of file IO for each command, and it's a very noticable
	// performance hit.
	// TODO:
	// Possibly the correct thing here is to loop over
	// c.Args and print each argument as %q.
	cmd += fmt.Sprintf(" %v", strings.Join(c.Args, " "))

	V("call session.Start(%s)", cmd)
	if err := c.session.Start(cmd); err != nil {
		return fmt.Errorf("Failed to run %v: %v", c, err.Error())
	}
	if err := c.SetupInteractive(); err != nil {
		return err
	}
	go c.TTYIn(c.session, c.Stdin, os.Stdin)
	go io.Copy(os.Stdout, c.Stdout)
	go io.Copy(os.Stderr, c.Stderr)

	return nil
}

// Wait waits for a Cmd to finish.
func (c *Cmd) Wait() error {
	err := c.session.Wait()
	return err
}

// Run runs a command with Start, and waits for it to finish with Wait.
func (c *Cmd) Run() error {
	if err := c.Start(); err != nil {
		return err
	}
	return c.Wait()
}

// TTYIn manages tty input for a cpu session.
// It exists mainly to deal with ~.
func (c *Cmd) TTYIn(s *ssh.Session, w io.WriteCloser, r io.Reader) {
	var newLine, tilde bool
	var t = []byte{'~'}
	var b [1]byte
	for {
		if _, err := r.Read(b[:]); err != nil {
			return
		}
		switch b[0] {
		default:
			newLine = false
			if tilde {
				if _, err := w.Write(t[:]); err != nil {
					return
				}
				tilde = false
			}
			if _, err := w.Write(b[:]); err != nil {
				return
			}
		case '\n', '\r':
			newLine = true
			if _, err := w.Write(b[:]); err != nil {
				return
			}
		case '~':
			if newLine {
				newLine = false
				tilde = true
				break
			}
			if _, err := w.Write(t[:]); err != nil {
				return
			}
		case '.':
			if tilde {
				s.Close()
				return
			}
			if _, err := w.Write(b[:]); err != nil {
				return
			}
		}
	}
}

// SetupInteractive sets up a cpu client for interactive access.
// It returns a function to be run when the session ends.
func (c *Cmd) SetupInteractive() error {
	t, err := termios.New()
	if err != nil {
		return err
	}
	// FIXME: getting a restorer from t.Raw doesn't work.
	// Still not sure what I'm doing wrong.
	// Always restores raw settings as traced in ioctl.
	r, err := t.Get()
	if err != nil {
		return err
	}
	if _, err = t.Raw(); err != nil {
		return err
	}
	c.closers = append(c.closers, func() error {
		if err := t.Set(r); err != nil {
			return err
		}
		return nil
	})

	return nil
}

// Close ends a cpu session, doing whatever is needed.
func (c *Cmd) Close() error {
	var err error
	for _, f := range c.closers {
		if e := f(); e != nil {
			err = multierror.Append(err, e)
		}
	}
	return err
}
