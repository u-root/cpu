// Copyright 2018-2022 the u-root Authors. All rights reserved
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package server

import (
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"
	"unsafe"

	// We use this ssh because it implements port redirection.
	// It can not, however, unpack password-protected keys yet.
	"github.com/creack/pty"
	"github.com/gliderlabs/ssh"

	"github.com/brutella/dnssd"
	"golang.org/x/sys/unix"
)

const (
	defaultPort = "17010"
	dsUpdate    = 60
)

var (
	v          = func(string, ...interface{}) {}
	cancelMdns = func() {}
	tenants    = 0
	tenChan    = make(chan int, 1)
)

func SetVerbose(f func(string, ...interface{})) {
	v = f
}

func verbose(f string, a ...interface{}) {
	v("\r\nCPUD:"+f+"\r\n", a...)
}

func setWinsize(f *os.File, w, h int) {
	unix.Syscall(unix.SYS_IOCTL, f.Fd(), uintptr(unix.TIOCSWINSZ), //nolint
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
	tenChan <- 1
	a := s.Command()
	v("handler: cmd is %q", a)
	cmd := command(a[0], a[1:]...)
	cmd.Env = append(cmd.Env, s.Environ()...)
	ptyReq, winCh, isPty := s.Pty()
	if isPty {
		cmd.Env = append(cmd.Env, fmt.Sprintf("TERM=%s", ptyReq.Term))
		f, err := pty.Start(cmd)
		v("command started with pty")
		if err != nil {
			v("CPUD:err %v", err)
			tenChan <- -1
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
		v("wait for %q", cmd)
		err = cmd.Wait()
		v("cmd %q returns with %v %v", cmd, err, cmd.ProcessState)
		if errval(err) != nil {
			v("CPUD:child exited with  %v", err)
			s.Exit(cmd.ProcessState.ExitCode()) //nolint
		}

	} else {
		cmd.Stdin, cmd.Stdout, cmd.Stderr = s, s, s
		v("running command without pty")
		if err := cmd.Run(); errval(err) != nil {
			v("CPUD:err %v", err)
			s.Exit(1) //nolint
		}
	}
	tenChan <- -1
	verbose("handler exits")
}

func DsUnregister() {
	v("stopping mdns server")
	cancelMdns()
}

func dsDefaultInstance() string {
	hostname, err := os.Hostname()
	if err == nil {
		hostname += "-cpud"
	} else {
		hostname = "cpud"
	}

	return hostname
}

func dsUpdateSysInfo(txtFlag map[string]string) {
	var sysinfo unix.Sysinfo_t
	err := unix.Sysinfo(&sysinfo)

	if err != nil {
		v("Sysinfo call failed ", err)
		return
	}

	txtFlag["mem_avail"] = strconv.FormatUint(uint64(sysinfo.Freeram), 10)
	txtFlag["mem_total"] = strconv.FormatUint(uint64(sysinfo.Totalram), 10)
	txtFlag["mem_unit"] = strconv.FormatUint(uint64(sysinfo.Unit), 10)
	txtFlag["load1"] = strconv.FormatUint(uint64(sysinfo.Loads[0]), 10)
	txtFlag["load5"] = strconv.FormatUint(uint64(sysinfo.Loads[1]), 10)
	txtFlag["load15"] = strconv.FormatUint(uint64(sysinfo.Loads[2]), 10)
	txtFlag["tenants"] = strconv.Itoa(tenants)

	v(" dsUpdateSysInfo ", txtFlag)
}

func dsDefaults(txtFlag map[string]string) {
	if len(txtFlag["arch"]) == 0 {
		txtFlag["arch"] = runtime.GOARCH
	}

	if len(txtFlag["os"]) == 0 {
		txtFlag["os"] = runtime.GOOS
	}

	if len(txtFlag["cores"]) == 0 {
		txtFlag["cores"] = strconv.Itoa(runtime.NumCPU())
	}
}

func DsRegister(instanceFlag, domainFlag, serviceFlag, interfaceFlag string, portFlag int, txtFlag map[string]string) error {
	v("starting mdns server")

	timeFormat := "15:04:05.000"

	v("Advertising: %s.%s.%s.", strings.Trim(instanceFlag, "."), strings.Trim(serviceFlag, "."), strings.Trim(domainFlag, "."))

	ctx, cancel := context.WithCancel(context.Background())
	cancelMdns = cancel

	resp, err := dnssd.NewResponder()
	if err != nil {
		return fmt.Errorf("dnssd newreponder fail: %w", err)
	}

	ifaces := []string{}
	if len(interfaceFlag) > 0 {
		ifaces = append(ifaces, interfaceFlag)
	}

	if len(instanceFlag) == 0 {
		instanceFlag = dsDefaultInstance()
	}

	dsDefaults(txtFlag)
	dsUpdateSysInfo(txtFlag)

	cfg := dnssd.Config{
		Name:   instanceFlag,
		Type:   serviceFlag,
		Domain: domainFlag,
		Port:   portFlag,
		Ifaces: ifaces,
		Text:   txtFlag,
	}
	srv, err := dnssd.NewService(cfg)
	if err != nil {
		return fmt.Errorf("cpud: advertise: New service fail: %w", err)
	}

	go func() {
		time.Sleep(1 * time.Second)
		handle, err := resp.Add(srv)
		if err != nil {
			fmt.Println(err)
		} else {
			v("%s	Got a reply for service %s: Name now registered and active\n", time.Now().Format(timeFormat), handle.Service().ServiceInstanceName())
		}
		go func() {
			for {
				delta := <-tenChan
				tenants += delta
				dsUpdateSysInfo(txtFlag)
				handle.UpdateText(txtFlag, resp)
			}
		}()

		for {
			time.Sleep(dsUpdate * time.Second)
			tenChan <- 0
		}
	}()

	go func() {
		err = resp.Respond(ctx)
		if err != nil {
			fmt.Println(err)
		} else {
			v("cpu mdns responder running exited")
		}
	}()

	return err
}

// New sets up a cpud. cpud is really just an SSH server with a special
// handler and support for port forwarding for the 9p port.
func New(publicKeyFile, hostKeyFile string) (*ssh.Server, error) {
	v("configure SSH server")
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
			v("CPUD:ReversePortForwardingCallback: attempt to bind %v %v granted", host, port)
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
