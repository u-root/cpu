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

	debug     = flag.Bool("d", false, "enable debug prints")
	runAsInit = flag.Bool("init", false, "run as init (Debug only; normal test is if we are pid 1")
	v         = func(string, ...interface{}) {}
	remote    = flag.Bool("remote", false, "indicates we are the remote side of the cpu session")
	network   = flag.String("network", "tcp", "network to use")
	keyFile   = flag.String("key", filepath.Join(os.Getenv("HOME"), ".ssh/cpu_rsa"), "key file")
	bin       = flag.String("bin", "cpu", "path of cpu binary")
	port9p    = flag.String("port9p", "", "port9p # on remote machine for 9p mount")
	dbg9p     = flag.String("dbg9p", "0", "show 9p io")
	root      = flag.String("root", "/", "9p root")
	klog      = flag.Bool("klog", false, "Log cpud messages in kernel log, not stdout")

	mountopts = flag.String("mountopts", "", "Extra options to add to the 9p mount")
	msize     = flag.Int("msize", 1048576, "msize to use")
	// To get debugging when Things Go Wrong, you can run as, e.g., -wtf /bbin/elvish
	// or change the value here to /bbin/elvish.
	// This way, when Things Go Wrong, you'll be dropped into a shell and look around.
	// This is sometimes your only way to debug if there is (e.g.) a Go runtime
	// bug around unsharing. Which has happened.
	wtf  = flag.String("wtf", "", "Command to run if setup (e.g. private name space mounts) fail")
	pid1 bool
)

func verbose(f string, a ...interface{}) {
	v("\r\nCPUD:"+f+"\r\n", a...)
}

func dropPrivs() error {
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

func buildCmd(cmd string) []string {
	if len(cmd) == 0 {
		cmd = os.Getenv("SHELL")
		if len(cmd) == 0 {
			cmd = "/bin/sh"
		}
	}
	return strings.Fields(cmd)
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
		log.Printf("CPUD:can't get a termios; oh well; %v", err)
	} else {
		term, err := t.Get()
		if err != nil {
			log.Printf("CPUD:can't get a termios; oh well; %v", err)
		} else {
			term.Lflag |= unix.ECHO | unix.ECHONL
			if err := t.Set(term); err != nil {
				log.Printf("CPUD:can't set a termios; oh well; %v", err)
			}
		}
	}

	bindover := "/lib:/lib64:/usr:/bin:/etc:/home"
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
	v("CPUD:namespace is %q", bindover)
	var fail bool
	if len(bindover) != 0 {
		// Connect to the socket, return the nonce.
		a := net.JoinHostPort("127.0.0.1", port9p)
		v("CPUD:Dial %v", a)
		so, err := net.Dial("tcp4", a)
		if err != nil {
			log.Fatalf("CPUD:Dial 9p port: %v", err)
		}
		v("CPUD:Connected: write nonce %s\n", nonce)
		if _, err := fmt.Fprintf(so, "%s", nonce); err != nil {
			log.Fatalf("CPUD:Write nonce: %v", err)
		}
		v("CPUD:Wrote the nonce")

		// the kernel takes over the socket after the Mount.
		defer so.Close()
		flags := uintptr(unix.MS_NODEV | unix.MS_NOSUID)
		cf, err := so.(*net.TCPConn).File()
		if err != nil {
			log.Fatalf("CPUD:Cannot get fd for %v: %v", so, err)
		}

		fd := cf.Fd()
		v("CPUD:fd is %v", fd)
		// The debug= option is here so you can see how to temporarily set it if needed.
		// It generates copious output so use it sparingly.
		// A useful compromise value is 5.
		opts := fmt.Sprintf("version=9p2000.L,trans=fd,rfdno=%d,wfdno=%d,uname=%v,debug=0,msize=%d", fd, fd, user, *msize)
		if *mountopts != "" {
			opts += "," + *mountopts
		}
		v("CPUD: mount 127.0.0.1 on /tmp/cpu 9p %#x %s", flags, opts)
		if err := unix.Mount("127.0.0.1", "/tmp/cpu", "9p", flags, opts); err != nil {
			return fmt.Errorf("9p mount %v", err)
		}
		v("CPUD: mount done")

		// Further, bind / onto /tmp/local so a non-hacked-on version may be visible.
		if err := unix.Mount("/", "/tmp/local", "", syscall.MS_BIND, ""); err != nil {
			log.Printf("CPUD:Warning: binding / over /tmp/cpu did not work: %v, continuing anyway", err)
		}

		// In some cases if you set LD_LIBRARY_PATH it is ignored.
		// This is disappointing to say the least. We just bind a few things into /
		// bind *may* hide local resources but for now it's the least worst option.
		dirs := strings.Split(bindover, ":")
		for _, n := range dirs {
			l, r := n, n
			// If the value is local=remote, len(c) will be 2.
			// The value might be some weird degenerate form such as
			// =name or name=. That is considered to be an error.
			// The convention is to split on the first =. It is not up
			// to this code to determine that more than one = is an error.
			c := strings.SplitN(n, "=", 2)
			if len(c) == 2 {
				l, r = c[0], c[1]
				if len(r) == 0 {
					return fmt.Errorf("Bad name in %q: zero-length remote name", n)
				}
				if len(l) == 0 {
					return fmt.Errorf("Bad name in %q: zero-length local name", n)
				}
			}
			t := filepath.Join("/tmp/cpu", r)
			v("CPUD: mount %v over %v", t, n)
			if err := unix.Mount(t, l, "", syscall.MS_BIND, ""); err != nil {
				fail = true
				log.Printf("CPUD:Warning: mounting %v on %v failed: %v", t, n, err)
			} else {
				v("CPUD:Mounted %v on %v", t, n)
			}

		}
	}
	v("CPUD: bind mounts done")
	if fail && len(*wtf) != 0 {
		c := exec.Command(*wtf)
		c.Stdin, c.Stdout, c.Stderr, c.Dir = os.Stdin, os.Stdout, os.Stderr, "/"
		log.Printf("CPUD: WTF: try to run %v", c)
		if err := c.Run(); err != nil {
			log.Printf("CPUD: Running %q failed: %v", *wtf, err)
		}
		log.Printf("CPUD: WTF done")
	}
	// We don't want to run as the wrong uid.
	if err := dropPrivs(); err != nil {
		return err
	}
	// The unmount happens for free since we unshared.
	v("CPUD:runRemote: command is %q", cmd)
	f := buildCmd(cmd)
	c := exec.Command(f[0], f[1:]...)
	c.Stdin, c.Stdout, c.Stderr, c.Dir = os.Stdin, os.Stdout, os.Stderr, os.Getenv("PWD")
	err = c.Run()
	v("CPUD:Run %v returns %v", c, err)
	if err != nil {
		if fail && len(*wtf) != 0 {
			c := exec.Command(*wtf)
			c.Stdin, c.Stdout, c.Stderr, c.Dir = os.Stdin, os.Stdout, os.Stderr, "/"
			log.Printf("CPUD: WTF: try to run %v", c)
			if err := c.Run(); err != nil {
				log.Printf("CPUD: Running %q failed: %v", *wtf, err)
			}
			log.Printf("CPUD: WTF done")
		}
	}
	return err
}

// We do flag parsing in init so we can
// Unshare if needed while we are still
// single threaded.
// Note that we can't run tets, dammit, because you can not call flag.Parse() from init,
// but we really need to b/c unshare etc. are broken in earlier versions of go.
// I.e., an unshare in earlier Go only affects one process, not all processes constituting
// the program.
func init() {
	flag.Parse()
	if *runAsInit && *remote {
		log.Fatal("Only use -remote or -init, not both")
	}
	if os.Getpid() == 1 {
		pid1, *runAsInit, *debug = true, true, false
	}
	if *debug {
		v = log.Printf
		if *klog {
			ulog.KernelLog.Reinit()
			v = ulog.KernelLog.Printf
		}
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
		// We assume that this was called via an unshare command or forked by
		// a process with the CLONE_NEWS flag set. This call to Unshare used to work;
		// no longer. We leave this code here as a signpost. Don't enable it.
		// It won't work. Go's green threads and Linux name space code have
		// never gotten along. Fixing it is hard, I've discussed this with the Go
		// core from time to time and it's not a priority for them.
		if false {
			if err := syscall.Unshare(syscall.CLONE_NEWNS); err != nil {
				log.Printf("CPUD:bad Unshare: %v", err)
			}
		}
		// Make / private. This call *is* safe so far for reasons.
		// Probably because, on many systems, we are lucky enough not to have a systemd
		// there screwing up namespaces.
		_, _, err1 := syscall.RawSyscall6(unix.SYS_MOUNT, uintptr(unsafe.Pointer(&none[0])), uintptr(unsafe.Pointer(&slash[0])), 0, flags, 0, 0)
		if err1 != 0 {
			log.Printf("CPUD:Warning: unshare failed (%v). There will be no private 9p mount if systemd is there", err1)
		}
		flags = 0
		if err := unix.Mount("cpu", "/tmp", "tmpfs", flags, ""); err != nil {
			log.Printf("CPUD:Warning: tmpfs mount on /tmp (%v) failed. There will be no 9p mount", err)
		}
	}
}

func setWinsize(f *os.File, w, h int) {
	syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), uintptr(syscall.TIOCSWINSZ),
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
	v("handler: cmd is %v", a)
	cmd := exec.Command(a[0], a[1:]...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Cloneflags: syscall.CLONE_NEWNS}
	cmd.Env = append(cmd.Env, s.Environ()...)
	ptyReq, winCh, isPty := s.Pty()
	if isPty {
		cmd.Env = append(cmd.Env, fmt.Sprintf("TERM=%s", ptyReq.Term))
		f, err := pty.Start(cmd)
		v("command started with pty")
		if err != nil {
			v("CPUD:err %v", err)
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
		v("wait for %v", cmd)
		err = cmd.Wait()
		v("cmd %v returns with %v %v", err, cmd, cmd.ProcessState)
		if errval(err) != nil {
			v("CPUD:child exited with  %v", err)
			s.Exit(cmd.ProcessState.ExitCode())
		}

	} else {
		cmd.Stdin, cmd.Stdout, cmd.Stderr = s, s, s
		v("running command without pty")
		if err := cmd.Run(); errval(err) != nil {
			v("CPUD:err %v", err)
			s.Exit(1)
		}
	}
	verbose("handler exits")
}

func doInit() error {
	if pid1 {
		if err := cpuSetup(); err != nil {
			log.Printf("CPUD:CPU setup error with cpu running as init: %v", err)
		}
		cmds := [][]string{{"/bin/sh"}, {"/bbin/dhclient", "-v", "--retry", "1000"}}
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
				verbose("CPUD:Error starting %v: %v", v, err)
				continue
			}
		}
	}
	verbose("Kicked off startup jobs, now serve ssh")
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
			log.Println("CPUD:Accepted forward", dhost, dport)
			return true
		}),
		Addr:             ":" + *port,
		PublicKeyHandler: publicKeyOption,
		ReversePortForwardingCallback: ssh.ReversePortForwardingCallback(func(ctx ssh.Context, host string, port uint32) bool {
			log.Println("CPUD:attempt to bind", host, port, "granted")
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
	verbose("Start the process reaper")
	go cpuDone(procs)

	server.SetOption(ssh.HostKeyFile(*hostKeyFile))
	log.Println("CPUD:starting ssh server on port " + *port)
	if err := server.ListenAndServe(); err != nil {
		log.Printf("CPUD:err %v", err)
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
			log.Fatalf("CPUD(as init):%v", err)
		}
	case *remote:
		verbose("Running as remote")
		if err := runRemote(strings.Join(args, " "), *port9p); err != nil {
			log.Fatalf("CPUD(as remote):%v", err)
		}
	default:
		log.Fatal("CPUD:can only run as remote or pid 1")
	}
}
