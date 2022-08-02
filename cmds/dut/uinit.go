package main

import (
	"log"
	"net"
	"net/rpc"
	"os"
	"time"

	"github.com/cenkalti/backoff/v4"
)

var (
	welcome = `  ______________
< welcome to DUT >
  --------------
         \   ^__^ 
          \  (oo)\_______
             (__)\       )\/\
                 ||----w |
                 ||     ||
`
)

func uinit(network, addr string) error {
	log.Printf("here we are in uinit")
	log.Printf("UINIT uid is %d", os.Getuid())

	log.Printf("Now dial %v %v", network, addr)
	b := backoff.NewExponentialBackOff()
	// We'll go at it for 5 minutes, then reboot.
	b.MaxElapsedTime = 5 * time.Minute

	var c net.Conn
	f := func() error {
		nc, err := net.Dial(network, addr)
		if err != nil {
			log.Printf("Dial went poorly")
			return err
		}
		c = nc
		return nil
	}
	if err := backoff.Retry(f, b); err != nil {
		return err
	}
	log.Printf("Start the RPC server")
	var Cmd Command
	s := rpc.NewServer()
	log.Printf("rpc server is %v", s)
	if err := s.Register(&Cmd); err != nil {
		log.Printf("register failed: %v", err)
		return err
	}
	log.Printf("Serve and protect")
	s.ServeConn(c)
	log.Printf("And uinit is all done.")
	return nil

}
