// Copyright 2018-2019 the u-root Authors. All rights reserved
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package client

import (
	"bytes"
	"crypto/rand"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"

	// We use this ssh because it implements port redirection.
	// It can not, however, unpack password-protected keys yet.

	// TODO: get rid of krpty
	config "github.com/kevinburke/ssh_config"

	// We use this ssh because it can unpack password-protected private keys.
	ssh "golang.org/x/crypto/ssh"
)

const (
	// DefaultPort is the default cpu port.
	DefaultPort = "23"
)

var (
	// DefaultKeyFile is the default key for cpu users.
	DefaultKeyFile = filepath.Join(os.Getenv("HOME"), ".ssh/cpu_rsa")
	// Debug9p enables 9p debugging.
	Debug9p bool
	// Dump9p enables dumping 9p packets.
	Dump9p bool
	// DumpWriter is an io.Writer to which dump packets are written.
	DumpWriter io.Writer = os.Stderr
)

// a nonce is a [32]byte containing only printable characters, suitable for use as a string
type nonce [32]byte

func verbose(f string, a ...interface{}) {
	V("\r\n"+f+"\r\n", a...)
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

// UserKeyConfig sets up authentication for a User Key.
// It is required in almost all cases.
func (c *Cmd) UserKeyConfig() error {
	kf := c.PrivateKeyFile
	if len(kf) == 0 {
		kf = config.Get(c.Host, "IdentityFile")
		V("key file from config is %q", kf)
		if len(kf) == 0 {
			kf = DefaultKeyFile
		}
	}
	// The kf will always be non-zero at this point.
	if strings.HasPrefix(kf, "~/") {
		kf = filepath.Join(os.Getenv("HOME"), kf[1:])
	}
	key, err := ioutil.ReadFile(kf)
	if err != nil {
		return fmt.Errorf("unable to read private key %q: %v", kf, err)
	}

	signer, err := ssh.ParsePrivateKey(key)
	if err != nil {
		return fmt.Errorf("ParsePrivateKey %q: %v", kf, err)
	}
	c.config.Auth = append(c.config.Auth, ssh.PublicKeys(signer))
	return nil
}

// HostKeyConfig sets the host key. It is optional.
func (c *Cmd) HostKeyConfig(hostKeyFile string) error {
	hk, err := ioutil.ReadFile(hostKeyFile)
	if err != nil {
		return fmt.Errorf("unable to read host key %v: %v", hostKeyFile, err)
	}
	pk, err := ssh.ParsePublicKey(hk)
	if err != nil {
		return fmt.Errorf("host key %v: %v", string(hk), err)
	}
	c.config.HostKeyCallback = ssh.FixedHostKey(pk)
	return nil
}

// SetEnv sets zero or more environment variables for a Session.
func (c *Cmd) SetEnv(envs ...string) error {
	for _, v := range append(os.Environ(), envs...) {
		env := strings.SplitN(v, "=", 2)
		if len(env) == 1 {
			env = append(env, "")
		}
		if err := c.session.Setenv(env[0], env[1]); err != nil {
			return fmt.Errorf("Warning: c.session.Setenv(%q, %q): %v", v, os.Getenv(v), err)
		}
	}
	return nil
}

// SSHStdin implements an ssh-like reader, honoring ~ commands.
func (c *Cmd) SSHStdin(w io.WriteCloser, r io.Reader) {
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
				c.session.Close()
				return
			}
			if _, err := w.Write(b[:]); err != nil {
				return
			}
		}
	}
}

// GetKeyFile picks a keyfile if none has been set.
// It will use ssh config, else use a default.
func GetKeyFile(host, kf string) string {
	V("getKeyFile for %q", kf)
	if len(kf) == 0 {
		kf = config.Get(host, "IdentityFile")
		V("key file from config is %q", kf)
		if len(kf) == 0 {
			kf = DefaultKeyFile
		}
	}
	// The kf will always be non-zero at this point.
	if strings.HasPrefix(kf, "~") {
		kf = filepath.Join(os.Getenv("HOME"), kf[1:])
	}
	V("getKeyFile returns %q", kf)
	// this is a tad annoying, but the config package doesn't handle ~.
	return kf
}

// GetHostName reads the host name from the ssh config file,
// if needed. If it is not found, the host name is returned.
func GetHostName(host string) string {
	h := config.Get(host, "HostName")
	if len(h) != 0 {
		host = h
	}
	return host
}

// GetPort gets a port. It verifies that the port fits in 16-bit space.
// The rules here are messy, since config.Get will return "22" if
// there is no entry in .ssh/config. 22 is not allowed. So in the case
// of "22", convert to defaultPort.
func GetPort(host, port string) (string, error) {
	p := port
	V("getPort(%q, %q)", host, port)
	if len(port) == 0 {
		if cp := config.Get(host, "Port"); len(cp) != 0 {
			V("config.Get(%q,%q): %q", host, port, cp)
			p = cp
		}
	}
	if len(p) == 0 || p == "22" {
		p = DefaultPort
		V("getPort: return default %q", p)
	}
	V("returns %q", p)
	return p, nil
}

// Signal implements ssh.Signal
func (c *Cmd) Signal(s ssh.Signal) error {
	return c.session.Signal(s)
}

// Outputs returns a slice of bytes.Buffer for stdout and stderr,
// and an error if either had trouble being read.
func (c *Cmd) Outputs() ([]bytes.Buffer, error) {
	var r [2]bytes.Buffer
	var err error
	if _, err = io.Copy(&r[0], c.Stdout); err != nil && err != io.EOF {
		err = fmt.Errorf("Stdout: '%v'", err)
	}
	if _, err = io.Copy(&r[1], c.Stderr); err != nil && err != io.EOF {
		err = fmt.Errorf("%sStderr: '%v'", err.Error(), err)
	}
	return r[:], err
}
