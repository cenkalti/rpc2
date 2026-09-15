package jsonrpc

import (
	"net"
	"sync"
	"testing"

	"github.com/cenkalti/rpc2"
)

// TestConcurrentResponseWrites checks that responses written by concurrently
// running handlers do not overlap on the codec's encoder.
//
// In non-blocking mode (the default) every incoming request is handled in its
// own goroutine, and each of those goroutines writes its response through the
// connection's single encoder. The connection is closed underneath them so
// that the writes fail: a failing Encode stores the error on the encoder,
// which is what concurrent writers corrupt. Run with -race.
func TestConcurrentResponseWrites(t *testing.T) {
	const n = 50

	lis, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer lis.Close()

	// Hold every handler until all of them have been entered, so that their
	// responses are written at the same time.
	var arrived sync.WaitGroup
	arrived.Add(n)
	release := make(chan struct{})

	srv := rpc2.NewServer()
	srv.Handle("echo", func(_ *rpc2.Client, args []int, reply *int) error {
		*reply = args[0]
		arrived.Done()
		<-release
		return nil
	})

	serverConn := make(chan net.Conn, 1)
	go func() {
		conn, err := lis.Accept()
		if err != nil {
			return
		}
		serverConn <- conn
		srv.ServeCodec(NewJSONCodec(conn))
	}()

	conn, err := net.Dial("tcp4", lis.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	clt := rpc2.NewClientWithCodec(NewJSONCodec(conn))
	go clt.Run()
	defer clt.Close()

	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			var reply int
			// The connection is torn down on purpose, so errors here are
			// expected and not the subject of this test.
			_ = clt.Call("echo", i, &reply)
		}(i)
	}

	arrived.Wait()
	(<-serverConn).Close()
	close(release)
	wg.Wait()
}
