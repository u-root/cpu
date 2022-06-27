// Copyright 2018-2022 the u-root Authors. All rights reserved
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build !plan9
// +build !plan9

package session

import (
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"

	"github.com/hashicorp/go-multierror"
	"github.com/u-root/cpu/mount"
	"github.com/u-root/u-root/pkg/termios"
	"golang.org/x/sys/unix"
)

// Bind defines a bind mount. It records the Local directory,
// e.g. /bin, and the remote directory, e.g. /tmp/cpu/bin.
type Bind struct {
	Local  string
	Remote string
}

// Session is one instance of a cpu session, started by a cpud.
type Session struct {
	restorer *termios.Termios
	Stdin    io.Reader
	Stdout   io.Writer
	Stderr   io.Writer
	binds    []Bind
	// Any function can use fail to mark that something
	// went badly wrong in some step. At that point, if wtf is set,
	// cpud will start it. This is incredibly handy for debugging.
	fail   bool
	msize  int
	mopts  string
	port9p string
	cmd    string
	args   []string
}

var (
	v = func(string, ...interface{}) {}
	// To get debugging when Things Go Wrong, you can run as, e.g., -wtf /bbin/elvish
	// or change the value here to /bbin/elvish.
	// This way, when Things Go Wrong, you'll be dropped into a shell and look around.
	// This is sometimes your only way to debug if there is (e.g.) a Go runtime
	// bug around unsharing. Which has happened.
	// This is compile time only because I'm so uncertain of whether it's dangerous
	wtf string
)

// DropPrivs drops privileges to the level of os.Getuid / os.Getgid
func (s *Session) DropPrivs() error {
	uid := unix.Getuid()
	v("CPUD:dropPrives: uid is %v", uid)
	if uid == 0 {
		v("CPUD:dropPrivs: not dropping privs")
		return nil
	}
	gid := unix.Getgid()
	v("CPUD:dropPrivs: gid is %v", gid)
	if err := unix.Setreuid(-1, uid); err != nil {
		return err
	}
	return unix.Setregid(-1, gid)
}

// Terminal sets up an interactive terminal.
func (s *Session) Terminal() error {
	// for some reason echo is not set.
	t, err := termios.New()
	if err != nil {
		return fmt.Errorf("CPUD:termios.New(): %v", err)
	}
	term, err := t.Get()
	if err != nil {
		return fmt.Errorf("CPUD:t.Get():%v", err)
	}
	s.restorer = term
	old := term.Lflag
	term.Lflag |= unix.ECHO | unix.ECHONL
	if err := t.Set(term); err != nil {
		return fmt.Errorf("CPUD:t.Set(%#x)(i.e. %#x | unix.ECHO | unix.ECHONL): %v", term.Lflag, old, err)
	}
	return nil
}

// TmpMounts sets up directories, and bind mounts, in /tmp/cpu.
// N.B. the /tmp/cpu mount is private assuming this program
// was started correctly with the namespace unshared (on Linux and
// Plan 9; on *BSD or Windows no such guarantees can be made).
//
// See the longer comment (rant) in seesion_linux.go
func (s *Session) TmpMounts() error {
	// It's true we are making this directory while still root.
	// This ought to be safe as it is a private namespace mount.
	// (or we are started with a clean namespace in Plan 9).
	for _, n := range []string{"/tmp/cpu", "/tmp/local", "/tmp/merge", "/tmp/root", "/home"} {
		if err := os.MkdirAll(n, 0666); err != nil && !os.IsExist(err) {
			log.Println(err)
		}
	}

	if err := osMounts(); err != nil {
		log.Println(err)
	}
	return nil
}

// Run starts up a remote cpu session. It is started by a cpu
// daemon via a -remote switch.
//
// This code assumes that cpud is running as init, or that
// an init has started a cpud, and that the code is running
// with a private namespace (CLONE_NEWNS on Linux; RFNAMEG on Plan9).
// On Linux, it starts as uid 0, and once the mount/bind is done,
// calls DropPrivs.
func (s *Session) Run() error {
	var errors error

	if err := runSetup(); err != nil {
		return err
	}
	if err := s.TmpMounts(); err != nil {
		v("CPUD: TmpMounts error: %v", err)
		s.fail = true
		errors = multierror.Append(err)
	}

	v("CPUD: Set up a namespace")
	if b, ok := os.LookupEnv("CPU_NAMESPACE"); ok {
		binds, err := ParseBinds(b)
		if err != nil {
			v("CPUD: ParseBind failed: %v", err)
			s.fail = true
			errors = multierror.Append(errors, err)
		}

		s.binds = binds
	}
	v("CPUD: call s.NameSpace")
	w, err := s.Namespace()
	if err != nil {
		return fmt.Errorf("CPUD:Namespace: warnings %v, err %v", w, multierror.Append(errors, err))
	}
	v("CPUD:warning: %v", w)

	v("CPUD: bind mounts done")

	// The CPU_FSTAB environment variable is, literally, an fstab.
	// Why an environment variable and not a file? We do not
	// want to require any 9p mounts at all. People should be able
	// to do this:
	// CPU_NAMESPACE="" CPU_FSTAB=`cat fstab`
	// and get the mounts they want. The first uses of this will
	// be building namespaces with drive and virtiofs.
	if tab, ok := os.LookupEnv("CPU_FSTAB"); ok {
		if err := mount.Mount(tab); err != nil {
			v("CPUD: fstab mount failure: %v", err)
			// Should we die if the mounts fail? For now, we think not;
			// the user may be able to debug if they have a non-empty
			// CPU_NAMESPACE. Just record that it failed.
			s.fail = true
		}
	}

	if err := s.Terminal(); err != nil {
		s.fail = true
		errors = multierror.Append(err)
	}
	v("CPUD: Terminal ready")
	if s.fail && len(wtf) != 0 {
		c := exec.Command(wtf)
		// Tricky question: should wtf use the stdio files or the ones
		// in the Server ... hmm.
		c.Stdin, c.Stdout, c.Stderr, c.Dir = os.Stdin, os.Stdout, os.Stderr, "/"
		log.Printf("CPUD: WTF: try to run %v", c)
		if err := c.Run(); err != nil {
			log.Printf("CPUD: Running %q failed: %v", wtf, err)
		}
		log.Printf("CPUD: WTF done")
		return errors
	}

	// We don't want to run as the wrong uid.
	if err := s.DropPrivs(); err != nil {
		return multierror.Append(errors, err)
	}

	// While it is true that things have been mounted, we need not
	// worry about unmounting them once the command is done: the
	// unmount happens for free since we unshared.
	v("CPUD:runRemote: command is %q", s.args)
	c := exec.Command(s.cmd, s.args...)
	c.Stdin, c.Stdout, c.Stderr, c.Dir = s.Stdin, s.Stdout, s.Stderr, os.Getenv("PWD")
	err = c.Run()
	v("CPUD:Run %v returns %v", c, err)
	if err != nil {
		if s.fail && len(wtf) != 0 {
			c := exec.Command(wtf)
			c.Stdin, c.Stdout, c.Stderr, c.Dir = os.Stdin, os.Stdout, os.Stderr, "/"
			log.Printf("CPUD: WTF: try to run %v", c)
			if err := c.Run(); err != nil {
				log.Printf("CPUD: Running %q failed: %v", wtf, err)
			}
			log.Printf("CPUD: WTF done: %v", err)
		}
	}
	return err
}

// New returns a New session with defaults set. It requires a port for
// 9p (which can be the empty string, but is usually not) and a
// command name.
func New(port9p, cmd string, args ...string) *Session {
	return &Session{msize: 8192, Stdin: os.Stdin, Stdout: os.Stdout, Stderr: os.Stderr, port9p: port9p, cmd: cmd, args: args}
}
