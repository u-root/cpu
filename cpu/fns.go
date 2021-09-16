// Copyright 2018-2019 the u-root Authors. All rights reserved
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package cpu

import (
	"crypto/rand"
	"fmt"
	"io"
	"io/ioutil"
	"log"
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

// Config returns a ClientConfig for cpu.
func Config(keyFile, hostKeyFile string) (*ClientConfig, error) {
	cb := ssh.InsecureIgnoreHostKey()
	//var hostKey ssh.PublicKey
	// A public key may be used to authenticate against the remote
	// server by using an unencrypted PEM-encoded private key file.
	//
	// If you have an encrypted private key, the crypto/x509 package
	// can be used to decrypt it.
	key, err := ioutil.ReadFile(keyFile)
	if err != nil {
		return nil, fmt.Errorf("unable to read private key %v: %v", keyFile, err)
	}

	// Create the Signer for this private key.
	signer, err := ssh.ParsePrivateKey(key)
	if err != nil {
		return nil, fmt.Errorf("ParsePrivateKey %v: %v", keyFile, err)
	}
	if len(hostKeyFile) != 0 {
		hk, err := ioutil.ReadFile(hostKeyFile)
		if err != nil {
			return nil, fmt.Errorf("unable to read host key %v: %v", hostKeyFile, err)
		}
		pk, err := ssh.ParsePublicKey(hk)
		if err != nil {
			return nil, fmt.Errorf("host key %v: %v", string(hk), err)
		}
		cb = ssh.FixedHostKey(pk)
	}
	config := &ClientConfig{
		SSH: &ssh.ClientConfig{
			User: os.Getenv("USER"),
			Auth: []ssh.AuthMethod{
				// Use the PublicKeys method for remote authentication.
				ssh.PublicKeys(signer),
			},
			HostKeyCallback: cb,
		},
	}
	return config, nil
}

// Env sets zero or more environment variables for a Session.
func Env(s *Session, envs ...string) {
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

// Stdin implements an ssh-like reader, honoring ~ commands.
func Stdin(s *Session, w io.WriteCloser, r io.Reader) {
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

// getKeyFile picks a keyfile if none has been set.
// It will use sshconfig, else use a default.
func getKeyFile(host, kf string) string {
	v("getKeyFile for %q", kf)
	if len(kf) == 0 {
		kf = config.Get(host, "IdentityFile")
		v("key file from config is %q", kf)
		if len(kf) == 0 {
			kf = DefaultKeyFile
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
		p = DefaultPort
		v("getPort: return default %q", p)
	}
	v("returns %q", p)
	return p
}
