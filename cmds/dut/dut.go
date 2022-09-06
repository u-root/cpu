// This is a very simple dut program. It builds into one binary to implement
// both client and server. It's just easier to see both sides of the code and test
// that way.
package main

import (
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net"
	"net/rpc"
	"os"
	"time"

	"github.com/u-root/u-root/pkg/ulog"
	"golang.org/x/sys/unix"
)

var (
	debug   = flag.Bool("d", false, "Enable debug prints")
	addr    = flag.String("addr", "192.168.0.1:8080", "DUT addr in addr:port format")
	network = flag.String("net", "tcp", "Network to use")
	klog    = flag.Bool("klog", false, "Direct all logging to klog -- depends on debug")
	pubKey  = flag.String("key", "key.pub", "public key file")
	hostKey = flag.String("hostkey", "", "host key file -- usually empty")
	cpuAddr = flag.String("cpuaddr", ":17010", "cpu address -- IANA port value is ncpu tcp/17010")

	// for debug
	v = func(string, ...interface{}) {}
)

func dutStart(network, addr string) (net.Listener, error) {
	ln, err := net.Listen(network, addr)
	if err != nil {
		log.Print(err)
		return nil, err
	}
	log.Printf("Listening on %v at %v", ln.Addr(), time.Now())
	return ln, nil
}

func dutAccept(l net.Listener) (net.Conn, error) {
	if err := l.(*net.TCPListener).SetDeadline(time.Now().Add(3 * time.Minute)); err != nil {
		return nil, err
	}
	c, err := l.Accept()
	if err != nil {
		log.Printf("Listen failed: %v at %v", err, time.Now())
		log.Print(err)
		return nil, err
	}
	log.Printf("Accepted %v", c)
	return c, nil
}

func dutRPC(network, addr string) error {
	l, err := dutStart(network, addr)
	if err != nil {
		return err
	}
	c, err := dutAccept(l)
	if err != nil {
		return err
	}
	cl := rpc.NewClient(c)
	for _, cmd := range []struct {
		call string
		args interface{}
	}{
		{"Command.Welcome", &RPCWelcome{}},
		{"Command.Reboot", &RPCReboot{}},
	} {
		var r RPCRes
		if err := cl.Call(cmd.call, cmd.args, &r); err != nil {
			return err
		}
		fmt.Printf("%v(%v): %v\n", cmd.call, cmd.args, string(r.C))
	}

	if c, err = dutAccept(l); err != nil {
		return err
	}
	cl = rpc.NewClient(c)
	var r RPCRes
	if err := cl.Call("Command.Welcome", &RPCWelcome{}, &r); err != nil {
		return err
	}
	fmt.Printf("%v(%v): %v\n", "Command.Welcome", nil, string(r.C))

	return nil
}

func dutcpu(network, addr, pubkey, hostkey, cpuaddr string) error {
	var req = &RPCCPU{Network: network, Addr: cpuaddr}
	var err error

	// we send the pubkey and hostkey as the value of the key, not the
	// name of the file.
	// TODO: maybe use ssh_config to find keys? the cpu client can do that.
	// Note: the public key is not optional. That said, we do not test
	// for len(*pubKey) > 0; if it is set to ""< ReadFile will return
	// an error.
	if req.PubKey, err = ioutil.ReadFile(pubkey); err != nil {
		return fmt.Errorf("Reading pubKey:%w", err)
	}
	if len(hostkey) > 0 {
		if req.HostKey, err = ioutil.ReadFile(hostkey); err != nil {
			return fmt.Errorf("Reading hostKey:%w", err)
		}
	}

	l, err := dutStart(network, addr)
	if err != nil {
		return err
	}

	c, err := dutAccept(l)
	if err != nil {
		return err
	}

	cl := rpc.NewClient(c)

	for _, cmd := range []struct {
		call string
		args interface{}
	}{
		{"Command.Welcome", &RPCWelcome{}},
		{"Command.Welcome", &RPCWelcome{}},
		{"Command.CPU", req},
	} {
		var r RPCRes
		if err := cl.Call(cmd.call, cmd.args, &r); err != nil {
			return err
		}
		fmt.Printf("%v(%v): %v\n", cmd.call, cmd.args, string(r.C))
	}
	return err
}

func main() {
	flag.Parse()

	if *debug {
		v = log.Printf
		if *klog {
			ulog.KernelLog.Reinit()
			v = ulog.KernelLog.Printf
		}
	}
	a := flag.Args()
	if len(a) == 0 {
		a = []string{"device"}
	}

	os.Args = a
	var err error
	v("Mode is %v", a[0])
	switch a[0] {
	case "tester":
		err = dutRPC(*network, *addr)
	case "cpu":
		// In this case, we chain the cpu daemon and exit.
		v("pubkey %v", *pubKey)
		if err = dutcpu(*network, *addr, *pubKey, *hostKey, *cpuAddr); err != nil {
			log.Printf("cpu service: %v", err)
		}
	case "device":
		err = uinit(*network, *addr)
		// What to do after a return? Reboot I suppose.
		log.Printf("Device returns with error %v", err)
		// We only reboot if there was an error.
		if err == nil {
			break
		}
		if err = unix.Reboot(int(unix.LINUX_REBOOT_CMD_RESTART)); err != nil {
			log.Printf("Reboot failed, not sure what to do now.")
		}
	default:
		log.Printf("Unknown mode %v", a[0])
	}
	log.Printf("We are now done ......................")
	if err != nil {
		log.Printf("%v", err)
		os.Exit(2)
	}
}
