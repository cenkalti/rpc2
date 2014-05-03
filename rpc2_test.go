package rpc2

import (
	"net"
	"testing"
)

const (
	network = "tcp4"
	addr    = "127.0.0.1:5000"
)

func TestTCPGOB(t *testing.T) {
	type Args struct{ A, B int }
	type Reply int

	lis, err := net.Listen(network, addr)
	if err != nil {
		t.Fatal(err)
	}

	srv := NewServer()
	srv.Handle("add", func(client *Client, args *Args, reply *Reply) error {
		*reply = Reply(args.A + args.B)

		var rep Reply
		err := client.Call("mult", Args{2, 3}, &rep)
		if err != nil {
			t.Fatal(err)
		}

		if rep != 6 {
			t.Fatalf("not expected: %d", rep)
		}

		return nil
	})
	go srv.Accept(lis)

	conn, err := net.Dial(network, addr)
	if err != nil {
		t.Fatal(err)
	}

	clt := NewClient(conn)
	clt.Handle("mult", func(client *Client, args *Args, reply *Reply) error {
		*reply = Reply(args.A * args.B)
		return nil
	})

	var rep Reply
	err = clt.Call("add", Args{1, 2}, &rep)
	if err != nil {
		t.Fatal(err)
	}

	if rep != 3 {
		t.Fatalf("not expected: %d", rep)
	}
}
