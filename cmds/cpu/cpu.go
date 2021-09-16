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
	config "github.com/kevinburke/ssh_config"
	"github.com/u-root/u-root/pkg/termios"
	"github.com/u-root/u-root/pkg/ulog"

	// We use this ssh because it can unpack password-protected private keys.
	ossh "golang.org/x/crypto/ssh"
)

const defaultPort = "23"

// a nonce is a [32]byte containing only printable characters, suitable for use as a string
type nonce [32]byte

var (
	defaultKeyFile = filepath.Join(os.Getenv("HOME"), ".ssh/cpu_rsa")
	// For the ssh server part
	bin         = flag.String("bin", "cpud", "path of cpu binary")
	debug       = flag.Bool("d", false, "enable debug prints")
	dbg9p       = flag.Bool("dbg9p", false, "show 9p io")
	dump        = flag.Bool("dump", false, "Dump copious output, including a 9p trace, to a temp file at exit")
	hostKeyFile = flag.String("hk", "" /*"/etc/ssh/ssh_host_rsa_key"*/, "file for host key")
	keyFile     = flag.String("key", "", "key file")
	mountopts   = flag.String("mountopts", "", "Extra options to add to the 9p mount")
	msize       = flag.Int("msize", 1048576, "msize to use")
	network     = flag.String("network", "tcp", "network to use")
	port        = flag.String("sp", "", "cpu default port")
	port9p      = flag.String("port9p", "", "port9p # on remote machine for 9p mount")
	root        = flag.String("root", "/", "9p root")
	timeout9P   = flag.String("timeout9p", "100ms", "time to wait for the 9p mount to happen.")

	v          = func(string, ...interface{}) {}
	pid1       bool
	dumpWriter *os.File
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

func configSSH(kf string) (*ossh.ClientConfig, error) {
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
func runClient(host, a, port, key string) error {
	c, err := configSSH(key)
	if err != nil {
		return err
	}
	cl, err := dial(*network, net.JoinHostPort(host, port), c)
	if err != nil {
		return err
	}
	// Special case: maybe we don't want a namespace. If so, we don't need
	// to open up the socket.
	wantNameSpace := true
	if n, ok := os.LookupEnv("CPU_NAMESPACE"); ok && len(n) == 0 {
		wantNameSpace = false
	}

	var env []string
	cmd := fmt.Sprintf("%v -remote -bin %v", *bin, *bin)
	if wantNameSpace {
		// From setting up the forward to having the nonce written back to us,
		// we only allow 100ms. This is a lot, considering that at this point,
		// the sshd has forked a server for us and it's waiting to be
		// told what to do. We suggest that making the deadline a flag
		// would be a bad move, since people might be tempted to make it
		// large.
		deadline, err := time.ParseDuration(*timeout9P)
		if err != nil {
			return err
		}
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
		port9p := ap[len(ap)-1]
		v("listener %T %v addr %v port %v", l, l, l.Addr().String(), port)

		nonce, err := generateNonce()
		if err != nil {
			log.Fatalf("Getting nonce: %v", err)
		}
		go srv(l, *root, nonce, deadline)
		cmd = fmt.Sprintf("%s -port9p %v", cmd, port9p)
		env = append(env, "CPUNONCE="+nonce.String())
	}
	cmd = fmt.Sprintf("%s %q", cmd, a)
	if err := shell(cl, cmd, env...); err != nil {
		return err
	}
	return nil
}

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

func stdin(s *ossh.Session, w io.WriteCloser, r io.Reader) {
	var newLine, tilde bool
	var t = []byte{'~'}
	var b [1]byte
	for {
		if _, err := r.Read(b[:]); err != nil {
			break
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

func shell(client *ossh.Client, cmd string, envs ...string) error {
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

	v("command is %q", cmd)
	session, err := client.NewSession()
	if err != nil {
		return err
	}
	defer session.Close()
	env(session, envs...)
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

	v("Start remote with command %q", cmd)
	if err := session.Start(cmd); err != nil {
		return fmt.Errorf("Failed to run %v: %v", cmd, err.Error())
	}
	//env(session, "CPUNONCE="+n.String())
	go stdin(session, i, os.Stdin)
	go io.Copy(os.Stdout, o)
	go io.Copy(os.Stderr, e)
	return session.Wait()
}

func flags() {
	flag.Parse()
	if *dump && *debug {
		log.Fatalf("You can only set either dump OR debug")
	}
	if *debug {
		v = log.Printf
	}
	if *dump {
		var err error
		dumpWriter, err = ioutil.TempFile("", "cpu")
		if err != nil {
			log.Fatal(err)
		}
		log.Printf("Logging to %s", dumpWriter.Name())
		*dbg9p = true
		ulog.Log = log.New(dumpWriter, "", log.Ltime|log.Lmicroseconds)
		v = ulog.Log.Printf
	}
}

func setWinsize(f *os.File, w, h int) {
	syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), uintptr(syscall.TIOCSWINSZ),
		uintptr(unsafe.Pointer(&struct{ h, w, x, y uint16 }{uint16(h), uint16(w), 0, 0})))
}

// getKeyFile picks a keyfile if none has been set.
// It will use sshconfig, else use a default.
func getKeyFile(host, kf string) string {
	v("getKeyFile for %q", kf)
	if len(kf) == 0 {
		kf = config.Get(host, "IdentityFile")
		v("key file from config is %q", kf)
		if len(kf) == 0 {
			kf = defaultKeyFile
		}
	}
	// The kf will always be non-zero at this point.
	if strings.HasPrefix(kf, "~") {
		kf = filepath.Join(os.Getenv("HOME"), kf[1:])
	}
	v("getKeyFile returns %q", kf)
	// this is a tad annoying, but the config package doesn't handle ~.
	return kf
}

// getHostName reads the host name from the config file,
// if needed. If it is not found, the host name is returned.
func getHostName(host string) string {
	h := config.Get(host, "HostName")
	if len(h) != 0 {
		host = h
	}
	return host
}

// getPort gets a port.
// The rules here are messy, since config.Get will return "22" if
// there is no entry in .ssh/config. 22 is not allowed. So in the case
// of "22", convert to defaultPort
func getPort(host, port string) string {
	p := port
	v("getPort(%q, %q)", host, port)
	if len(port) == 0 {
		if cp := config.Get(host, "Port"); len(cp) != 0 {
			v("config.Get(%q,%q): %q", host, port, cp)
			p = cp
		}
	}
	if len(p) == 0 || p == "22" {
		p = defaultPort
		v("getPort: return default %q", p)
	}
	v("returns %q", p)
	return p
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
	flags()
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

	kf := getKeyFile(host, *keyFile)
	p := getPort(host, *port)
	hn := getHostName(host)

	if err := runClient(hn, a, p, kf); err != nil {
		e := 1
		log.Printf("SSH error %s", err)
		if x, ok := err.(*ossh.ExitError); ok {
			e = x.ExitStatus()
		}
		defer os.Exit(e)
	}
	if err := termios.SetTermios(0, t); err != nil {
		// Never make this a log.Fatal, it might
		// interfere with the exit handling
		// for errors from the remote process.
		log.Print(err)
	}
}
