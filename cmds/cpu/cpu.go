// Copyright 2018-2019 the u-root Authors. All rights reserved
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"bytes"
	"crypto/rand"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unsafe"

	// We use this ssh because it implements port redirection.
	// It can not, however, unpack password-protected keys yet.

	// TODO: get rid of krpty
	"github.com/u-root/u-root/pkg/termios"

	// We use this ssh because it can unpack password-protected private keys.
	ossh "golang.org/x/crypto/ssh"
)

// a nonce is a [32]byte containing only printable characters, suitable for use as a string
type nonce [32]byte

var (
	// For the ssh server part
	hostKeyFile = flag.String("hk", "" /*"/etc/ssh/ssh_host_rsa_key"*/, "file for host key")
	pubKeyFile  = flag.String("pk", "key.pub", "file for public key")
	port        = flag.String("sp", "23", "cpu default port")

	debug     = flag.Bool("d", false, "enable debug prints")
	v         = func(string, ...interface{}) {}
	network   = flag.String("network", "tcp", "network to use")
	keyFile   = flag.String("key", filepath.Join(os.Getenv("HOME"), ".ssh/cpu_rsa"), "key file")
	bin       = flag.String("bin", "cpud", "path of cpu binary")
	port9p    = flag.String("port9p", "", "port9p # on remote machine for 9p mount")
	dbg9p     = flag.Bool("dbg9p", false, "show 9p io")
	root      = flag.String("root", "/", "9p root")
	bindover  = flag.String("bindover", "/lib:/lib64:/lib32:/usr:/bin:/etc:/home", ": separated list of directories in /tmp/cpu to bind over /")
	mountopts = flag.String("mountopts", "", "Extra options to add to the 9p mount")
	msize     = flag.Int("msize", 1048576, "msize to use")
	pid1      bool
)

func verbose(f string, a ...interface{}) {
	v("\r\n"+f+"\r\n", a...)
}

// generateNonce returns a nonce, or an error if random reader fails.
func generateNonce() (nonce, error) {
	var b [len(nonce{}) / 2]byte
	if _, err := rand.Read(b[:]); err != nil {
		return nonce{}, err
	}
	var n nonce
	copy(n[:], fmt.Sprintf("%02x", b))
	return n, nil
}

// String is a Stringer for nonce.
func (n nonce) String() string {
	return string(n[:])
}

func dial(n, a string, config *ossh.ClientConfig) (*ossh.Client, error) {
	client, err := ossh.Dial(n, a, config)
	if err != nil {
		return nil, fmt.Errorf("Failed to dial: %v", err)
	}
	return client, nil
}

func config(kf string) (*ossh.ClientConfig, error) {
	cb := ossh.InsecureIgnoreHostKey()
	//var hostKey ssh.PublicKey
	// A public key may be used to authenticate against the remote
	// server by using an unencrypted PEM-encoded private key file.
	//
	// If you have an encrypted private key, the crypto/x509 package
	// can be used to decrypt it.
	key, err := ioutil.ReadFile(kf)
	if err != nil {
		return nil, fmt.Errorf("unable to read private key %v: %v", kf, err)
	}

	// Create the Signer for this private key.
	signer, err := ossh.ParsePrivateKey(key)
	if err != nil {
		return nil, fmt.Errorf("ParsePrivateKey %v: %v", kf, err)
	}
	if *hostKeyFile != "" {
		hk, err := ioutil.ReadFile(*hostKeyFile)
		if err != nil {
			return nil, fmt.Errorf("unable to read host key %v: %v", *hostKeyFile, err)
		}
		pk, err := ossh.ParsePublicKey(hk)
		if err != nil {
			return nil, fmt.Errorf("host key %v: %v", string(hk), err)
		}
		cb = ossh.FixedHostKey(pk)
	}
	config := &ossh.ClientConfig{
		User: os.Getenv("USER"),
		Auth: []ossh.AuthMethod{
			// Use the PublicKeys method for remote authentication.
			ossh.PublicKeys(signer),
		},
		HostKeyCallback: cb,
	}
	return config, nil
}

func cmd(client *ossh.Client, s string) ([]byte, error) {
	session, err := client.NewSession()
	if err != nil {
		return nil, fmt.Errorf("Failed to create session: %v", err)
	}
	defer session.Close()

	var b bytes.Buffer
	session.Stdout = &b
	if err := session.Run(s); err != nil {
		return nil, fmt.Errorf("Failed to run %v: %v", s, err.Error())
	}
	return b.Bytes(), nil
}

// To make sure defer gets run and you tty is sane on exit
func runClient(host, a string) error {
	c, err := config(*keyFile)
	if err != nil {
		return err
	}
	cl, err := dial(*network, net.JoinHostPort(host, *port), c)
	if err != nil {
		return err
	}
	// From setting up the forward to having the nonce written back to us,
	// we only allow 10ms. This is a lot, considering that at this point,
	// the sshd has forked a server for us and it's waiting to be
	// told what to do. We suggest that making the deadline a flag
	// would be a bad move, since people might be tempted to make it
	// large.
	deadline := 10 * time.Millisecond

	// Arrange port forwarding from remote ssh to our server.
	// Request the remote side to open port 5640 on all interfaces.
	// Note: cl.Listen returns a TCP listener with network is "tcp"
	// or variants. This lets us use a listen deadline.
	l, err := cl.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return fmt.Errorf("First cl.Listen %v", err)
	}
	ap := strings.Split(l.Addr().String(), ":")
	if len(ap) == 0 {
		return fmt.Errorf("Can't find a port number in %v", l.Addr().String())
	}
	port := ap[len(ap)-1]
	v("listener %T %v addr %v port %v", l, l, l.Addr().String(), port)

	nonce, err := generateNonce()
	if err != nil {
		log.Fatalf("Getting nonce: %v", err)
	}
	go srv(l, *root, nonce, deadline)
	// now run stuff.
	if err := shell(cl, nonce, a, port); err != nil {
		return err
	}
	return nil
}

// env sets environment variables. While we might think we ought to set
// HOME and PATH, it's possibly not a great idea. We leave them here as markers
// to remind ourselves not to try it later.
func env(s *ossh.Session, envs ...string) {
	for _, v := range append(os.Environ(), envs...) {
		env := strings.SplitN(v, "=", 2)
		if len(env) == 1 {
			env = append(env, "")
		}
		if err := s.Setenv(env[0], env[1]); err != nil {
			log.Printf("Warning: s.Setenv(%q, %q): %v", v, os.Getenv(v), err)
		}
	}
}

func shell(client *ossh.Client, n nonce, a, port9p string) error {
	t, err := termios.New()
	if err != nil {
		return err
	}
	r, err := t.Raw()
	if err != nil {
		return err
	}
	defer t.Set(r)
	if *bin == "" {
		if *bin, err = exec.LookPath("cpu"); err != nil {
			return err
		}
	}
	a = fmt.Sprintf("%v -remote -port9p %v -bin %v %v", *bin, port9p, *bin, a)
	v("command is %q", a)
	session, err := client.NewSession()
	if err != nil {
		return err
	}
	defer session.Close()
	env(session, "CPUNONCE="+n.String())
	// Set up terminal modes
	modes := ossh.TerminalModes{
		ossh.ECHO:          0,     // disable echoing
		ossh.TTY_OP_ISPEED: 14400, // input speed = 14.4kbaud
		ossh.TTY_OP_OSPEED: 14400, // output speed = 14.4kbaud
	}
	// Request pseudo terminal
	if err := session.RequestPty("ansi", 40, 80, modes); err != nil {
		log.Fatal("request for pseudo terminal failed: ", err)
	}
	i, err := session.StdinPipe()
	if err != nil {
		return err
	}
	o, err := session.StdoutPipe()
	if err != nil {
		return err
	}
	e, err := session.StderrPipe()
	if err != nil {
		return err
	}

	// sshd doesn't want to set us set the HOME and PATH via the normal
	// request route. So we do this nasty hack to ensure we can find
	// the cpu binary. We append our paths to the one the shell has.
	// This should suffice for u-root systems with paths including
	// /bbin and /ubin as well as more conventional systems.
	// The only possible flaw in this approach is elvish, which
	// has a very odd PATH syntax. For elvish, the PATH= is ignored,
	// so does no harm. Our use case for elvish is u-root, and
	// we will have the right path anyway, so it will still work.
	// It is working well in testing.
	//	cmd := fmt.Sprintf("PATH=$PATH:%s %s", os.Getenv("PATH"), a)
	cmd := a
	v("Start remote with command %q", cmd)
	if err := session.Start(cmd); err != nil {
		return fmt.Errorf("Failed to run %v: %v", a, err.Error())
	}
	//env(session, "CPUNONCE="+n.String())
	go io.Copy(i, os.Stdin)
	go io.Copy(os.Stdout, o)
	go io.Copy(os.Stderr, e)
	return session.Wait()
}

// We do flag parsing in init so we can
// Unshare if needed while we are still
// single threaded.
func init() {
	flag.Parse()
	if *debug {
		v = log.Printf
	}
}

func setWinsize(f *os.File, w, h int) {
	syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), uintptr(syscall.TIOCSWINSZ),
		uintptr(unsafe.Pointer(&struct{ h, w, x, y uint16 }{uint16(h), uint16(w), 0, 0})))
}

// TODO: we've been tryinmg to figure out the right way to do usage for years.
// If this is a good way, it belongs in the uroot package.
func usage() {
	var b bytes.Buffer
	flag.CommandLine.SetOutput(&b)
	flag.PrintDefaults()
	log.Fatalf("Usage: cpu [options] host [shell command]:\n%v", b.String())
}

func main() {
	args := flag.Args()
	if len(args) == 0 {
		usage()
	}
	host := args[0]
	a := strings.Join(args[1:], " ")
	verbose("Running as client")
	if a == "" {
		a = os.Getenv("SHELL")
	}
	t, err := termios.GetTermios(0)
	if err != nil {
		log.Fatal("Getting Termios")
	}
	if err := runClient(host, a); err != nil {
		log.Print(err)
	}
	if err := termios.SetTermios(0, t); err != nil {
		log.Print(err)
	}

}
