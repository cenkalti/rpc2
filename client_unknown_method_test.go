package rpc2_test

import (
	"io"
	"net"
	"testing"
	"time"

	"github.com/cenkalti/rpc2"
	"github.com/cenkalti/rpc2/jsonrpc"
)

func TestUnknownMethodKeepsConnection(t *testing.T) {
	for _, codec := range []struct {
		name string
		new  func(io.ReadWriteCloser) rpc2.Codec
	}{
		{"gob", rpc2.NewGobCodec},
		{"json", jsonrpc.NewJSONCodec},
	} {
		t.Run(codec.name, func(t *testing.T) {
			for _, notification := range []bool{false, true} {
				name := "call"
				if notification {
					name = "notification"
				}
				t.Run(name, func(t *testing.T) {
					serverConn, clientConn := net.Pipe()
					defer serverConn.Close()
					defer clientConn.Close()
					deadline := time.Now().Add(5 * time.Second)
					if err := serverConn.SetDeadline(deadline); err != nil {
						t.Fatal(err)
					}
					if err := clientConn.SetDeadline(deadline); err != nil {
						t.Fatal(err)
					}

					server := rpc2.NewServer()
					server.Handle("echo", func(_ *rpc2.Client, args int, reply *int) error {
						*reply = args
						return nil
					})
					serverDone := make(chan struct{})
					go func() {
						server.ServeCodec(codec.new(serverConn))
						close(serverDone)
					}()
					client := rpc2.NewClientWithCodec(codec.new(clientConn))
					go client.Run()
					defer func() {
						client.Close()
						serverConn.Close()
						<-serverDone
						<-client.DisconnectNotify()
					}()

					for i := 1; i <= 3; i++ {
						if notification {
							if err := client.Notify("unknown", i); err != nil {
								t.Fatal(err)
							}
						} else {
							var reply int
							err := client.Call("unknown", i, &reply)
							if err != rpc2.ServerError("rpc2: can't find method unknown") {
								t.Fatalf("unknown call: got %v", err)
							}
						}

						var reply int
						if err := client.Call("echo", i, &reply); err != nil {
							t.Fatalf("call after unknown method: %v", err)
						}
						if reply != i {
							t.Fatalf("echo reply: got %d, want %d", reply, i)
						}
					}
				})
			}
		})
	}
}
