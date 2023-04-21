// Copyright 2018-2022 the u-root Authors. All rights reserved
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build !plan9
// +build !plan9

package session

import (
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"

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
	// Any function can use fail to mark that something
	// went badly wrong in some step. At that point, if wtf is set,
	// cpud will start it. This is incredibly handy for debugging.
	fail   bool
	msize  int
	mopts  string
	port9p string
	cmd    string
	args   []string
	tmpMnt string
}

var (
	// v allows debug printing.
	// Do not call it directly, call verbose instead.
	v = func(string, ...interface{}) {}
	// To get debugging when Things Go Wrong, you can run as, e.g., -wtf /bbin/elvish
	// or change the value here to /bbin/elvish.
	// This way, when Things Go Wrong, you'll be dropped into a shell and look around.
	// This is sometimes your only way to debug if there is (e.g.) a Go runtime
	// bug around unsharing. Which has happened.
	// This is compile time only because I'm so uncertain of whether it's dangerous
	wtf string
)

// SetVerbose sets the verbose printing function.
// e.g., one might call SetVerbose(log.Printf)
func SetVerbose(f func(string, ...interface{})) {
	v = f
}

// DropPrivs drops privileges to the level of os.Getuid / os.Getgid
func (s *Session) DropPrivs() error {
	uid := unix.Getuid()
	verbose("dropPrives: uid is %v", uid)
	if uid == 0 {
		verbose("dropPrivs: not dropping privs")
		return nil
	}
	gid := unix.Getgid()
	verbose("dropPrivs: gid is %v", gid)
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
// See the longer comment (rant) in session_linux.go
func (s *Session) TmpMounts() error {
	// It's true we are making this directory while still root.
	// This ought to be safe as it is a private namespace mount.
	// (or we are started with a clean namespace in Plan 9).
	for _, n := range []string{
		filepath.Join(s.tmpMnt, "cpu"),
		filepath.Join(s.tmpMnt, "local"),
		filepath.Join(s.tmpMnt, "merge"),
		filepath.Join(s.tmpMnt, "root"),
		"/home"} {
		if err := os.MkdirAll(n, 0666); err != nil && !os.IsExist(err) {
			log.Println(err)
		}
	}

	if err := osMounts(s.tmpMnt); err != nil {
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
	var errs error

	if err := runSetup(s.tmpMnt); err != nil {
		return err
	}
	if err := s.TmpMounts(); err != nil {
		verbose("TmpMounts error: %v", err)
		s.fail = true
		errs = errors.Join(errs, err)
	}

	verbose("call s.NameSpace")
	err := s.Namespace()
	if err != nil {
		return errors.Join(errs, fmt.Errorf("CPUD:Namespace: %v", err))
	}

	// The CPU_FSTAB environment variable is, literally, an fstab.
	// Why an environment variable and not a file? We do not
	// want to require any 9p mounts at all. People should be able
	// to do this:
	// CPU_FSTAB=`cat fstab`
	// and get the mounts they want. The first uses of this will
	// be building namespaces with drive and virtiofs.

	// In some cases if you set LD_LIBRARY_PATH it is ignored.
	// This is disappointing to say the least. We just bind a few things into /
	// bind *may* hide local resources but for now it's the least worst option.
	if tab, ok := os.LookupEnv("CPU_FSTAB"); ok {
		verbose("Mounting %q", tab)
		if err := mount.Mount(tab); err != nil {
			verbose("fstab mount failure: %v", err)
			// Should we die if the mounts fail? For now, we think not;
			// the user may be able to debug. Just record that it failed.
			s.fail = true
		}
	}

	if err := s.Terminal(); err != nil {
		s.fail = true
		errs = errors.Join(errs, err)
	}
	verbose("Terminal ready")
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
		return errs
	}

	// We don't want to run as the wrong uid.
	if err := s.DropPrivs(); err != nil {
		return errors.Join(errs, err)
	}

	// While it is true that things have been mounted, we need not
	// worry about unmounting them once the command is done: the
	// unmount happens for free since we unshared.
	verbose("runRemote: command is %q", s.args)
	c := exec.Command(s.cmd, s.args...)
	c.Stdin, c.Stdout, c.Stderr, c.Dir = s.Stdin, s.Stdout, s.Stderr, os.Getenv("PWD")
	dirInfo, err := os.Stat(c.Dir)
	if err != nil || !dirInfo.IsDir() {
		log.Printf("CPUD: your $PWD %s is not in the remote namespace", c.Dir)
		return os.ErrNotExist
	}
	err = runCmd(c)
	verbose("Run %v returns %v", c, err)
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
func New(port9p, tmpMnt, cmd string, args ...string) *Session {
	// Measurement has shown that 64K is a good number. 8K is too small.
	// History: why was it ever 8K? You have to go back to the 90s and see:
	// Page size on 68K Sun systems was 8K
	// There was this amazing new thing called Jumbo Packets
	// The 9P designers had the wisdom to make msize negotiation part of session initiation,
	// so we had a way out!
	return &Session{msize: 64 * 1024, Stdin: os.Stdin, Stdout: os.Stdout, Stderr: os.Stderr, port9p: port9p, tmpMnt: tmpMnt, cmd: cmd, args: args}
}
