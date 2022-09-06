package main

import (
	"fmt"
	"log"
	"os"
	"time"

	"golang.org/x/sys/unix"
)

type RPCRes struct {
	C   []byte
	Err string
}

type Command int

type RPCWelcome struct {
}

func (*Command) Welcome(args *RPCWelcome, r *RPCRes) error {
	r.C = []byte(welcome)
	r.Err = ""
	log.Printf("welcome")
	return nil
}

type RPCExit struct {
	When time.Duration
}

func (*Command) Die(args *RPCExit, r *RPCRes) error {
	go func() {
		time.Sleep(args.When)
		log.Printf("die exits")
		os.Exit(0)
	}()
	*r = RPCRes{}
	log.Printf("die returns")
	return nil
}

type RPCReboot struct {
	When time.Duration
}

func (*Command) Reboot(args *RPCReboot, r *RPCRes) error {
	go func() {
		time.Sleep(args.When)
		if err := unix.Reboot(unix.LINUX_REBOOT_CMD_RESTART); err != nil {
			log.Printf("%v\n", err)
		}
	}()
	*r = RPCRes{}
	log.Printf("reboot returns")
	return nil
}

type RPCCPU struct {
	Network string
	Addr    string
	PubKey  []byte
	HostKey []byte
}

func (*Command) CPU(args *RPCCPU, r *RPCRes) error {
	v("CPU")
	res := make(chan error)
	go func(network, addr string, pubKey, hostKey []byte) {
		v("cpu serve(%q, %q,%q,%q)", network, addr, pubKey, hostKey)
		err := serve(network, addr, pubKey, hostKey)
		v("cpu serve returns")
		res <- err
	}(args.Network, args.Addr, args.PubKey, args.HostKey)
	err := <-res
	*r = RPCRes{Err: fmt.Sprintf("%v", err)}
	v("cpud returns")
	return nil
}
