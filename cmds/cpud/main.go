// Copyright 2018-2019 the u-root Authors. All rights reserved
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"bytes"
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
	"unsafe"

	// We use this ssh because it implements port redirection.
	// It can not, however, unpack password-protected keys yet.
	"github.com/gliderlabs/ssh"
	"github.com/kr/pty" // TODO: get rid of krpty
	"github.com/u-root/u-root/pkg/libinit"
	"github.com/u-root/u-root/pkg/termios"
	"github.com/u-root/u-root/pkg/ulog"
	"golang.org/x/sys/unix"
)

// a nonce is a [32]byte containing only printable characters, suitable for use as a string
type nonce [32]byte

var (
	// For the ssh server part
	hostKeyFile = flag.String("hk", "" /*"/etc/ssh/ssh_host_rsa_key"*/, "file for host key")
	pubKeyFile  = flag.String("pk", "key.pub", "file for public key")
	port        = flag.String("sp", "23", "cpu default port")

	debug     = flag.Bool("d", true, "enable debug prints")
	runAsInit = flag.Bool("init", false, "run as init (Debug only; normal test is if we are pid 1")
	v         = func(string, ...interface{}) {}
	remote    = flag.Bool("remote", false, "indicates we are the remote side of the cpu session")
	network   = flag.String("network", "tcp", "network to use")
	keyFile   = flag.String("key", filepath.Join(os.Getenv("HOME"), ".ssh/cpu_rsa"), "key file")
	bin       = flag.String("bin", "cpu", "path of cpu binary")
	port9p    = flag.String("port9p", "", "port9p # on remote machine for 9p mount")
	dbg9p     = flag.String("dbg9p", "0", "show 9p io")
	root      = flag.String("root", "/", "9p root")

	mountopts = flag.String("mountopts", "", "Extra options to add to the 9p mount")
	msize     = flag.Int("msize", 1048576, "msize to use")
	pid1      bool
)

func verbose(f string, a ...interface{}) {
	v("\r\n"+f+"\r\n", a...)
}

func dropPrivs() error {
	uid := unix.Getuid()
	v("dropPrives: uid is %v", uid)
	if uid == 0 {
		v("dropPrivs: not dropping privs")
		return nil
	}
	gid := unix.Getgid()
	v("dropPrivs: gid is %v", gid)
	if err := unix.Setreuid(-1, uid); err != nil {
		return err
	}
	return unix.Setregid(-1, gid)
}

// start up a namespace. We must
// mkdir /tmp/cpu on the remote machine
// issue the mount command
// test via an ls of /tmp/cpu
// TODO: unshare first
// We enter here as uid 0 and once the mount is done, back down.
func runRemote(cmd, port9p string) error {
	// Get the nonce and remove it from the environment.
	nonce := os.Getenv("CPUNONCE")
	os.Unsetenv("CPUNONCE")
	// for some reason echo is not set.
	t, err := termios.New()
	if err != nil {
		log.Printf("can't get a termios; oh well; %v", err)
	} else {
		term, err := t.Get()
		if err != nil {
			log.Printf("can't get a termios; oh well; %v", err)
		} else {
			term.Lflag |= unix.ECHO | unix.ECHONL
			if err := t.Set(term); err != nil {
				log.Printf("can't set a termios; oh well; %v", err)
			}
		}
	}

	bindover := "/lib:/lib64:/lib32:/usr:/bin:/etc:/home"
	if s, ok := os.LookupEnv("CPU_NAMESPACE"); ok {
		bindover = s
	}

	user := os.Getenv("USER")
	if user == "" {
		user = "nouser"
	}

	// It's true we are making this directory while still root.
	// This ought to be safe as it is a private namespace mount.
	for _, n := range []string{"/tmp/cpu", "/tmp/local", "/tmp/merge", "/tmp/root", "/home"} {
		if err := os.MkdirAll(n, 0666); err != nil && !os.IsExist(err) {
			log.Println(err)
		}
	}
	if len(bindover) != 0 {
		// Connect to the socket, return the nonce.
		a := net.JoinHostPort("127.0.0.1", port9p)
		v("remote:Dial %v", a)
		so, err := net.Dial("tcp4", a)
		if err != nil {
			log.Fatalf("Dial 9p port: %v", err)
		}
		v("remote:Connected: write nonce %s\n", nonce)
		if _, err := fmt.Fprintf(so, "%s", nonce); err != nil {
			log.Fatalf("Write nonce: %v", err)
		}
		v("remote:Wrote the nonce")

		// the kernel takes over the socket after the Mount.
		defer so.Close()
		flags := uintptr(unix.MS_NODEV | unix.MS_NOSUID)
		cf, err := so.(*net.TCPConn).File()
		if err != nil {
			log.Fatalf("Cannot get fd for %v: %v", so, err)
		}

		fd := cf.Fd()
		v("remote:fd is %v", fd)
		opts := fmt.Sprintf("version=9p2000.L,trans=fd,rfdno=%d,wfdno=%d,uname=%v,debug=0,msize=%d", fd, fd, user, *msize)
		if *mountopts != "" {
			opts += "," + *mountopts
		}
		v("remote; mount 127.0.0.1 on /tmp/cpu 9p %#x %s", flags, opts)
		if err := unix.Mount("127.0.0.1", "/tmp/cpu", "9p", flags, opts); err != nil {
			return fmt.Errorf("9p mount %v", err)
		}
		v("remote: mount done")

		// Further, bind / onto /tmp/local so a non-hacked-on version may be visible.
		if err := unix.Mount("/", "/tmp/local", "", syscall.MS_BIND, ""); err != nil {
			log.Printf("Warning: binding / over /tmp/cpu did not work: %v, continuing anyway", err)
		}

		// In some cases if you set LD_LIBRARY_PATH it is ignored.
		// This is disappointing to say the least. We just bind a few things into /
		// bind *may* hide local resources but for now it's the least worst option.
		dirs := strings.Split(bindover, ":")
		for _, n := range dirs {
			t := filepath.Join("/tmp/cpu", n)
			v("remote: mount %v over %v", t, n)
			if err := unix.Mount(t, n, "", syscall.MS_BIND, ""); err != nil {
				v("Warning: mounting %v on %v failed: %v", t, n, err)
			} else {
				v("Mounted %v on %v", t, n)
			}

		}
	}
	v("remote: bind mounts done")
	// We don't want to run as the wrong uid.
	if err := dropPrivs(); err != nil {
		return err
	}
	// The unmount happens for free since we unshared.
	v("remote:runRemote: command is %q", cmd)
	f := strings.Fields(cmd)
	c := exec.Command(f[0], f[1:]...)
	c.Stdin, c.Stdout, c.Stderr, c.Dir = os.Stdin, os.Stdout, os.Stderr, os.Getenv("PWD")
	return c.Run()
}

// We do flag parsing in init so we can
// Unshare if needed while we are still
// single threaded.
func init() {
	flag.Parse()
	if os.Getpid() == 1 {
		pid1, *runAsInit, *debug = true, true, false
	}
	if *debug {
		ulog.KernelLog.Reinit()
		v = log.Printf
		v = ulog.KernelLog.Printf
	}
	if *remote {
		// The unshare system call in Linux doesn't unshare mount points
		// mounted with --shared. Systemd mounts / with --shared. For a
		// long discussion of the pros and cons of this see debian bug 739593.
		// The Go model of unsharing is more like Plan 9, where you ask
		// to unshare and the namespaces are unconditionally unshared.
		// To make this model work we must further mark / as MS_PRIVATE.
		// This is what the standard unshare command does.
		var (
			none  = [...]byte{'n', 'o', 'n', 'e', 0}
			slash = [...]byte{'/', 0}
			flags = uintptr(unix.MS_PRIVATE | unix.MS_REC) // Thanks for nothing Linux.
		)
		if err := syscall.Unshare(syscall.CLONE_NEWNS); err != nil {
			log.Printf("bad Unshare: %v", err)
		}
		_, _, err1 := syscall.RawSyscall6(unix.SYS_MOUNT, uintptr(unsafe.Pointer(&none[0])), uintptr(unsafe.Pointer(&slash[0])), 0, flags, 0, 0)
		if err1 != 0 {
			log.Printf("Warning: unshare failed (%v). There will be no private 9p mount", err1)
		}
		flags = 0
		if err := unix.Mount("cpu", "/tmp", "tmpfs", flags, ""); err != nil {
			log.Printf("Warning: tmpfs mount on /tmp (%v) failed. There will be no 9p mount", err)
		}
	}
}

func setWinsize(f *os.File, w, h int) {
	syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), uintptr(syscall.TIOCSWINSZ),
		uintptr(unsafe.Pointer(&struct{ h, w, x, y uint16 }{uint16(h), uint16(w), 0, 0})))
}

func handler(s ssh.Session) {
	a := s.Command()
	verbose("the handler is here, cmd is %v", a)
	cmd := exec.Command(a[0], a[1:]...)
	cmd.Env = append(cmd.Env, s.Environ()...)
	ptyReq, winCh, isPty := s.Pty()
	verbose("the command is %v", *cmd)
	if isPty {
		cmd.Env = append(cmd.Env, fmt.Sprintf("TERM=%s", ptyReq.Term))
		f, err := pty.Start(cmd)
		verbose("command started with pty")
		if err != nil {
			log.Print(err)
			return
		}
		go func() {
			for win := range winCh {
				setWinsize(f, win.Width, win.Height)
			}
		}()
		go func() {
			io.Copy(f, s) // stdin
		}()
		io.Copy(s, f) // stdout
		libinit.WaitOrphans()
	} else {
		cmd.Stdin, cmd.Stdout, cmd.Stderr = s, s, s
		verbose("running command without pty")
		if err := cmd.Run(); err != nil {
			log.Print(err)
			return
		}
	}
	verbose("handler exits")
}

func doInit() error {
	if pid1 {
		if err := cpuSetup(); err != nil {
			log.Printf("CPU setup error with cpu running as init: %v", err)
		}
		cmds := [][]string{{"/bin/sh"}, {"/bbin/dhclient", "-v"}}
		verbose("Try to run %v", cmds)

		for _, v := range cmds {
			verbose("Let's try to run %v", v)
			if _, err := os.Stat(v[0]); os.IsNotExist(err) {
				verbose("it's not there")
				continue
			}

			// I *love* special cases. Evaluate just the top-most symlink.
			//
			// In source mode, this would be a symlink like
			// /buildbin/defaultsh -> /buildbin/elvish ->
			// /buildbin/installcommand.
			//
			// To actually get the command to build, argv[0] has to end
			// with /elvish, so we resolve one level of symlink.
			if filepath.Base(v[0]) == "defaultsh" {
				s, err := os.Readlink(v[0])
				if err == nil {
					v[0] = s
				}
				verbose("readlink of %v returns %v", v[0], s)
				// and, well, it might be a relative link.
				// We must go deeper.
				d, b := filepath.Split(v[0])
				d = filepath.Base(d)
				v[0] = filepath.Join("/", os.Getenv("UROOT_ROOT"), d, b)
				verbose("is now %v", v[0])
			}

			cmd := exec.Command(v[0], v[1:]...)
			cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
			cmd.SysProcAttr = &syscall.SysProcAttr{Setctty: true, Setsid: true}
			verbose("Run %v", cmd)
			if err := cmd.Start(); err != nil {
				log.Printf("Error starting %v: %v", v, err)
				continue
			}
		}
	}
	publicKeyOption := func(ctx ssh.Context, key ssh.PublicKey) bool {
		// Glob the users's home directory for all the
		// possible keys?
		data, err := ioutil.ReadFile(*pubKeyFile)
		if err != nil {
			fmt.Print(err)
			return false
		}
		allowed, _, _, _, _ := ssh.ParseAuthorizedKey(data)
		return ssh.KeysEqual(key, allowed)
	}

	// Now we run as an ssh server, and each time we get a connection,
	// we run that command after setting things up for it.
	forwardHandler := &ssh.ForwardedTCPHandler{}
	server := ssh.Server{
		LocalPortForwardingCallback: ssh.LocalPortForwardingCallback(func(ctx ssh.Context, dhost string, dport uint32) bool {
			log.Println("Accepted forward", dhost, dport)
			return true
		}),
		Addr:             ":" + *port,
		PublicKeyHandler: publicKeyOption,
		ReversePortForwardingCallback: ssh.ReversePortForwardingCallback(func(ctx ssh.Context, host string, port uint32) bool {
			log.Println("attempt to bind", host, port, "granted")
			return true
		}),
		RequestHandlers: map[string]ssh.RequestHandler{
			"tcpip-forward":        forwardHandler.HandleSSHRequest,
			"cancel-tcpip-forward": forwardHandler.HandleSSHRequest,
		},
		Handler: handler,
	}

	// start the process reaper
	procs := make(chan uint)
	go cpuDone(procs)

	server.SetOption(ssh.HostKeyFile(*hostKeyFile))
	log.Println("starting ssh server on port " + *port)
	if err := server.ListenAndServe(); err != nil {
		log.Print(err)
	}
	verbose("server.ListenAndServer returned")

	numprocs := <-procs
	verbose("Reaped %d procs", numprocs)
	return nil
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
	verbose("Args %v pid %d *runasinit %v *remote %v", os.Args, os.Getpid(), *runAsInit, *remote)
	args := flag.Args()
	switch {
	case *runAsInit:
		verbose("Running as Init")
		if err := doInit(); err != nil {
			log.Fatal(err)
		}
	case *remote:
		verbose("Running as remote")
		if err := runRemote(strings.Join(args, " "), *port9p); err != nil {
			log.Fatal(err)
		}
	default:
		log.Fatal("can only run as remote or pid 1")
	}
}
