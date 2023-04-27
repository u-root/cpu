// Copyright 2018-2022 the u-root Authors. All rights reserved
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package server

import (
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"os"
	"strings"
	"syscall"
	"unsafe"

	// We use this ssh because it implements port redirection.
	// It can not, however, unpack password-protected keys yet.
	"github.com/creack/pty"
	"github.com/gliderlabs/ssh"
	"golang.org/x/sys/unix"
)

const (
	defaultPort = "17010"
)

var (
	// v allows debug printing.
	// Do not call it directly, call verbose instead.
	v = func(string, ...interface{}) {}
)

// SetVerbose sets the internal verbose function to a function
// f(string, ...interface{}), i.e. a function like log.Printf.
func SetVerbose(f func(string, ...interface{})) {
	v = f
}

func verbose(f string, a ...interface{}) {
	v("server:"+f, a...)
}

func setWinsize(f *os.File, w, h int) {
	syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), uintptr(syscall.TIOCSWINSZ), //nolint
		uintptr(unsafe.Pointer(&struct{ h, w, x, y uint16 }{uint16(h), uint16(w), 0, 0})))
}

// errval can be used to examine errors that we don't consider errors
func errval(err error) error {
	if err == nil {
		return err
	}
	// Our zombie reaper is occasionally sneaking in and grabbing the
	// child's exit state. Looks like our process code still sux.
	if strings.Contains(err.Error(), "no child process") {
		return nil
	}
	return err
}

func handler(s ssh.Session) {
	a := s.Command()
	verbose("handler: cmd is %q", a)
	cmd := command("cpud", append([]string{"-remote"}, a...)...)

	sigChan := make(chan ssh.Signal, 1)
	defer close(sigChan)
	s.Signals(sigChan)
	go func() {
		for signal := range sigChan {
			var err error
			switch signal {
			case ssh.SIGTERM:
				err = cmd.Process.Signal(unix.SIGTERM)
			case ssh.SIGINT:
				err = cmd.Process.Signal(unix.SIGINT)
			default:
				err = fmt.Errorf("unknown signal: %q", signal)
			}
			if err != nil {
				verbose("sending %q to %q: %v", signal, a[0], err)
			}
		}
	}()

	cmd.Env = append(cmd.Env, s.Environ()...)
	ptyReq, winCh, isPty := s.Pty()
	if isPty {
		cmd.Env = append(cmd.Env, fmt.Sprintf("TERM=%s", ptyReq.Term))
		f, err := pty.Start(cmd)
		verbose("command started with pty")
		if err != nil {
			verbose("err %v", err)
			return
		}
		go func() {
			for win := range winCh {
				setWinsize(f, win.Width, win.Height)
			}
		}()
		go func() {
			io.Copy(f, s) //nolint stdin
		}()
		io.Copy(s, f) //nolint stdout
		// Stdout is closed, "there's no more to the show/
		// If you all want to breath right/you all better go"
		// This is going to seem a bit odd, but it is important to
		// only wait for the process started here, not any orphans.
		// In most cases, that process is either a singleton (so the wait
		// will be all we need); a shell (which does all the waiting for
		// its children); or the rare case of a detached process (in which
		// case the reaper will get it).
		// Seen in the wild: were this code to wait for orphans,
		// and the main loop to wait for orphans, they end up
		// competing with each other and the results are odd to say the least.
		// If the command exits, leaving orphans behind, it is the job
		// of the reaper to get them.
		verbose("wait for %q", cmd)
		err = cmd.Wait()
		verbose("cmd %q returns with %v %v", cmd, err, cmd.ProcessState)
		if errval(err) != nil {
			verbose("child exited with  %v", err)
			s.Exit(cmd.ProcessState.ExitCode()) //nolint
		}

	} else {
		cmd.Stdin, cmd.Stdout, cmd.Stderr = s, s, s.Stderr()
		verbose("running command without pty")
		if err := cmd.Run(); errval(err) != nil {
			verbose("err %v", err)
			s.Exit(1) //nolint
		}
	}
	verbose("handler exits")
}

// New sets up a cpud. cpud is really just an SSH server with a special
// handler and support for port forwarding for the 9p port.
func New(publicKeyFile, hostKeyFile string) (*ssh.Server, error) {
	verbose("configure SSH server")
	publicKeyOption := func(ctx ssh.Context, key ssh.PublicKey) bool {
		data, err := ioutil.ReadFile(publicKeyFile)
		if err != nil {
			fmt.Print(err)
			return false
		}
		allowed, _, _, _, err := ssh.ParseAuthorizedKey(data)
		if err != nil {
			fmt.Print(err)
			return false
		}
		return ssh.KeysEqual(key, allowed)
	}

	// Now we run as an ssh server, and each time we get a connection,
	// we run that command after setting things up for it.
	forwardHandler := &ssh.ForwardedTCPHandler{}
	server := &ssh.Server{
		LocalPortForwardingCallback: ssh.LocalPortForwardingCallback(func(ctx ssh.Context, dhost string, dport uint32) bool {
			log.Println("CPUD:Accepted forward", dhost, dport)
			return true
		}),
		// Pick a reasonable default, which can be used for a call to listen and which
		// will be overridden later from a listen.Addr
		Addr:             ":" + defaultPort,
		PublicKeyHandler: publicKeyOption,
		ReversePortForwardingCallback: ssh.ReversePortForwardingCallback(func(ctx ssh.Context, host string, port uint32) bool {
			verbose("ReversePortForwardingCallback: attempt to bind %v %v granted", host, port)
			return true
		}),
		RequestHandlers: map[string]ssh.RequestHandler{
			"tcpip-forward":        forwardHandler.HandleSSHRequest,
			"cancel-tcpip-forward": forwardHandler.HandleSSHRequest,
		},
		Handler: handler,
	}

	// we ignore the SetOption error; if it does not work out, we
	// actually don't care.
	_ = server.SetOption(ssh.HostKeyFile(hostKeyFile))
	return server, nil
}
