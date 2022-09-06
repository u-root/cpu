package main

import (
	"log"
	"net/rpc"
	"testing"
	"time"
)

func TestUinit(t *testing.T) {
	var tests = []struct {
		c   string
		r   interface{}
		err string
	}{
		{c: "Welcome", r: RPCWelcome{}},
		{c: "Reboot", r: RPCReboot{}},
	}
	l, err := dutStart("tcp", ":")
	if err != nil {
		t.Fatal(err)
	}

	a := l.Addr()
	t.Logf("listening on %v", a)
	// Kick off our node.
	go func() {
		time.Sleep(1 * time.Second)
		if err := uinit(a.Network(), a.String()); err != nil {
			log.Printf("starting uinit: got %v, want nil", err)
		}
	}()

	c, err := dutAccept(l)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("Connected on %v", c)

	cl := rpc.NewClient(c)
	for _, tt := range tests {
		t.Run(tt.c, func(t *testing.T) {
			var r RPCRes
			if err = cl.Call("Command."+tt.c, tt.r, &r); err != nil {
				t.Fatalf("Call to %v: got %v, want nil", tt.c, err)
			}
			if r.Err != tt.err {
				t.Errorf("%v: got %v, want %v", tt, r.Err, tt.err)
			}
		})
	}
}
