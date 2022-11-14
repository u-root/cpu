package server

import (
	"bytes"
	"errors"
	"io"
	"io/ioutil"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"syscall"
	"testing"

	config "github.com/kevinburke/ssh_config"
	"github.com/u-root/cpu/client"
	"golang.org/x/sys/unix"
)

func TestHelperProcess(t *testing.T) {
	v, ok := os.LookupEnv("GO_WANT_HELPER_PROCESS")
	if !ok {
		t.Logf("just a helper")
		return
	}
	t.Logf("Check %q", v)
	if err := unix.Mount("cpu", v, "tmpfs", 0, ""); err != nil {
		t.Fatalf("unix.Mount(cpu, %q, \"tmpfs\"): %v != nil", v, err)
	}

	vanish := filepath.Join(v, "vanish")
	t.Logf("Mount ok, try to create %q", vanish)
	msg := "This should not be visible in the parent"
	if err := ioutil.WriteFile(vanish, []byte(msg), 0644); err != nil {
		t.Fatalf(`ioutil.WriteFile(%q, %q, 0644): %v != nil`, vanish, msg, err)
	}
	t.Logf("Created %q", vanish)
}

// TestPrivateNameSpace tests if we are privatizing mounts
// correctly. We spawn a child with syscall.CLONE_NEWNS set.
// The child mounts a tmpfs over the directory, and creates a
// file. When it exits, and returns to us, that file should
// not be visible.
func TestPrivateNameSpace(t *testing.T) {
	if os.Getuid() != 0 {
		t.Skipf("Skipping; not root")
	}
	d := t.TempDir()
	t.Logf("Call helper %q", os.Args[0])
	c := exec.Command(os.Args[0], "-test.run=TestHelperProcess", "-test.v")
	c.Env = []string{"GO_WANT_HELPER_PROCESS=" + d}
	c.SysProcAttr = &syscall.SysProcAttr{Unshareflags: syscall.CLONE_NEWNS}
	o, err := c.CombinedOutput()
	t.Logf("out %s", o)
	if err != nil {
		t.Errorf("Error: %v", err)
	}
	vanish := filepath.Join(d, "vanish")
	if _, err := os.Stat(vanish); err == nil {
		t.Logf("Privatization failed ... Try to unmount %v", d)
		if err := unix.Unmount(d, unix.MNT_FORCE); err != nil {
			t.Fatalf("unix.Unmount( %q, %#x): %v != nil", d, unix.MNT_FORCE, err)
		}
		t.Fatalf("os.Stat(%q): nil != err", vanish)
	}
}

// Now the fun begins. We have to be a demon.
func TestDaemonSession(t *testing.T) {
	if os.Getuid() != 0 {
		t.Skipf("Skipping as we are not root")
	}

	runtime.GOMAXPROCS(1)
	v = t.Logf
	d := t.TempDir()
	t.Logf("tempdir is %q", d)
	if err := os.Setenv("HOME", d); err != nil {
		t.Fatalf(`os.Setenv("HOME", %s): %v != nil`, d, err)
	}
	dotSSH := filepath.Join(d, ".ssh")
	hackconfig, err := gendotssh(d, string(sshConfig))
	if err != nil {
		t.Fatalf(`gendotssh(%s): %v != nil`, d, err)
	}

	pubKey := filepath.Join(dotSSH, "server.pub")
	s, err := New(pubKey, "")
	if err != nil {
		t.Fatalf("New(%q, %q): %v != nil", pubKey, "", err)
	}

	ln, err := net.Listen("tcp", "")
	if err != nil {
		t.Fatalf("s.Listen(): %v != nil", err)
	}
	t.Logf("Listening on %v", ln.Addr())
	// this is a racy test.
	// The ssh package really ought to allow you to accept
	// on a socket and then call with that socket. This would be
	// more in line with bsd sockets which let you write a server
	// and client in line, e.g.
	// socket/bind/listen/connect/accept
	// oh well.
	go func(t *testing.T) {
		if err := s.Serve(ln); err != nil {
			t.Errorf("s.Daemon(): %v != nil", err)
		}
	}(t)
	v = t.Logf
	// From this test forward, at least try to get a port.
	// For this test, there must be a key.
	// hack for lack in ssh_config
	// https://github.com/kevinburke/ssh_config/issues/2
	cfg, err := config.Decode(bytes.NewBuffer([]byte(hackconfig)))
	if err != nil {
		t.Fatal(err)
	}
	host, err := cfg.Get("server", "HostName")
	if err != nil || len(host) == 0 {
		t.Fatalf(`cfg.Get("server", "HostName"): (%q, %v) != (localhost, nil`, host, err)
	}
	kf, err := cfg.Get("server", "IdentityFile")
	if err != nil || len(kf) == 0 {
		t.Fatalf(`cfg.Get("server", "IdentityFile"): (%q, %v) != (afilename, nil`, kf, err)
	}
	host, port, err := net.SplitHostPort(ln.Addr().String())
	if err != nil {
		t.Fatalf("net.SplitHostPort(%q): %v != nil", ln.Addr(), err)
	}

	t.Logf("HostName %q, IdentityFile %q, command %v", host, kf, os.Args[0])
	client.V = t.Logf
	c := client.Command(host, os.Args[0], "-remote", "ls", "-l")
	if err := c.SetOptions(client.WithPrivateKeyFile(kf), client.WithPort(port), client.WithRoot(d), client.WithNameSpace(d)); err != nil {
		t.Fatalf("SetOptions: %v != nil", err)
	}
	if err := c.Dial(); err != nil {
		t.Fatalf("Dial: got %v, want nil", err)
	}
	if err = c.Start(); err != nil {
		t.Fatalf("Start: got %v, want nil", err)
	}
	defer func() {
		if err := c.Close(); err != nil {
			t.Fatalf("Close: got %v, want nil", err)
		}
	}()
	if err := c.SessionIn.Close(); err != nil && !errors.Is(err, io.EOF) {
		t.Errorf("Close stdin: Got %v, want nil", err)
	}
	if err := c.Wait(); err != nil {
		t.Fatalf("Wait: got %v, want nil", err)
	}
	r, err := c.Outputs()
	if err != nil {
		t.Errorf("Outputs: got %v, want nil", err)
	}
	t.Logf("c.Run: (%v, %q, %q)", err, r[0].String(), r[1].String())
}

// TestDaemonConnect tests connecting to a daemon and exercising
// minimal operations.
func TestDaemonConnect(t *testing.T) {
	if os.Getuid() != 0 {
		t.Skipf("Skipping as we are not root")
	}
	d := t.TempDir()
	if err := os.Setenv("HOME", d); err != nil {
		t.Fatalf(`os.Setenv("HOME", %s): %v != nil`, d, err)
	}
	hackconfig, err := gendotssh(d, string(sshConfig))
	if err != nil {
		t.Fatalf(`gendotssh(%s): %v != nil`, d, err)
	}

	v = t.Logf
	s, err := New(filepath.Join(d, ".ssh", "server.pub"), filepath.Join(d, ".ssh", "hostkey"))
	if err != nil {
		t.Fatalf(`New(%q, %q): %v != nil`, filepath.Join(d, ".ssh", "server.pub"), filepath.Join(d, ".ssh", "hostkey"), err)
	}

	ln, err := net.Listen("tcp", "")
	if err != nil {
		t.Fatalf(`net.Listen("", ""): %v != nil`, err)
	}
	t.Logf("Listening on %v", ln.Addr())
	_, port, err := net.SplitHostPort(ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	// this is a racy test.
	// The ssh package really ought to allow you to accept
	// on a socket and then call with that socket. This would be
	// more in line with bsd sockets which let you write a server
	// and client in line, e.g.
	// socket/bind/listen/connect/accept
	// oh well.
	go func(t *testing.T) {
		if err := s.Serve(ln); err != nil {
			t.Errorf("s.Daemon(): %v != nil", err)
		}
	}(t)
	v = t.Logf
	// From this test forward, at least try to get a port.
	// For this test, there must be a key.
	// hack for lack in ssh_config
	// https://github.com/kevinburke/ssh_config/issues/2
	cfg, err := config.Decode(bytes.NewBuffer([]byte(hackconfig)))
	if err != nil {
		t.Fatal(err)
	}
	host, err := cfg.Get("server", "HostName")
	if err != nil || len(host) == 0 {
		t.Fatalf(`cfg.Get("server", "HostName"): (%q, %v) != (localhost, nil`, host, err)
	}
	kf, err := cfg.Get("server", "IdentityFile")
	if err != nil || len(kf) == 0 {
		t.Fatalf(`cfg.Get("server", "IdentityFile"): (%q, %v) != (afilename, nil`, kf, err)
	}
	t.Logf("HostName %q, IdentityFile %q", host, kf)
	c := client.Command(host, os.Args[0]+" -test.run TestDaemonConnectHelper -test.v --", "date")
	if err := c.SetOptions(client.WithPrivateKeyFile(kf), client.WithPort(port), client.WithRoot("/"), client.WithNameSpace("")); err != nil {
		t.Fatalf("SetOptions: %v != nil", err)
	}
	c.Env = append(c.Env, "GO_WANT_DAEMON_HELPER_PROCESS=1")
	if err := c.Dial(); err != nil {
		t.Fatalf("Dial: got %v, want nil", err)
	}
	if err = c.Start(); err != nil {
		t.Fatalf("Start: got %v, want nil", err)
	}
	defer func() {
		if err := c.Close(); err != nil {
			t.Fatalf("Close: got %v, want nil", err)
		}
	}()
	if err := c.SessionIn.Close(); err != nil && !errors.Is(err, io.EOF) {
		t.Errorf("Close stdin: Got %v, want nil", err)
	}
	if err := c.Wait(); err != nil {
		t.Fatalf("Wait: got %v, want nil", err)
	}
	r, err := c.Outputs()
	if err != nil {
		t.Errorf("Outputs: got %v, want nil", err)
	}
	t.Logf("c.Run: (%v, %q, %q)", err, r[0].String(), r[1].String())
}
