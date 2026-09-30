package jsonrpc

import (
	"errors"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cenkalti/rpc2"
)

// countingCodec wraps a Codec and signals wg once per WriteRequest or
// WriteResponse call. The race tests below close the connection out from
// under in-flight writes, so the calls fail quickly; without this, the test
// function can return - and the process can exit - before every racy Encode
// has actually run, letting -race miss it.
type countingCodec struct {
	rpc2.Codec
	wg *sync.WaitGroup
}

func (c *countingCodec) WriteRequest(r *rpc2.Request, body interface{}) error {
	defer c.wg.Done()
	return c.Codec.WriteRequest(r, body)
}

func (c *countingCodec) WriteResponse(r *rpc2.Response, body interface{}) error {
	defer c.wg.Done()
	return c.Codec.WriteResponse(r, body)
}

// waitOrTimeout waits for wg, failing the test if it takes longer than d.
func waitOrTimeout(t *testing.T, wg *sync.WaitGroup, d time.Duration, msg string) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(d):
		t.Fatal(msg)
	}
}

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

	// Signaled once per WriteResponse, so the test can wait for every
	// handler's response encode to actually run before it returns.
	var written sync.WaitGroup
	written.Add(n)

	srv := rpc2.NewServer()
	srv.Handle("echo", func(_ *rpc2.Client, args []int, reply *int) error {
		*reply = args[0]
		arrived.Done()
		<-release
		return nil
	})

	serverConn := make(chan net.Conn, 1)
	acceptErr := make(chan error, 1)
	go func() {
		conn, err := lis.Accept()
		if err != nil {
			acceptErr <- err
			return
		}
		serverConn <- conn
		srv.ServeCodec(&countingCodec{Codec: NewJSONCodec(conn), wg: &written})
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

	arrivedCh := make(chan struct{})
	go func() {
		arrived.Wait()
		close(arrivedCh)
	}()
	select {
	case <-arrivedCh:
	case err := <-acceptErr:
		t.Fatalf("accept: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for handlers to start")
	}

	(<-serverConn).Close()
	close(release)
	waitOrTimeout(t, &written, 5*time.Second, "timed out waiting for responses to be written")
	wg.Wait()
}

// breakableConn wraps a net.Conn and, once broken, fails every Write without
// touching the underlying connection or Read. A plain conn.Close would also
// make rpc2.Client's read loop see EOF and set its own shutdown flag, which
// makes Client.Notify/Call refuse to even reach the codec - so this lets
// writes fail while reads stay blocked, keeping the client's outbound path
// live.
type breakableConn struct {
	net.Conn
	broken atomic.Bool
}

func (c *breakableConn) Write(b []byte) (int, error) {
	if c.broken.Load() {
		return 0, errors.New("breakableConn: broken")
	}
	return c.Conn.Write(b)
}

// TestConcurrentRequestAndResponseWrites checks that a handler's own
// response write does not race with a reverse notification's request write
// on the same codec - the other trigger for the underlying race, alongside
// two handlers responding at once (see TestConcurrentResponseWrites). Run
// with -race.
func TestConcurrentRequestAndResponseWrites(t *testing.T) {
	const n = 50

	lis, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer lis.Close()

	var arrived sync.WaitGroup
	arrived.Add(n)
	release := make(chan struct{})

	// One WriteResponse and one WriteRequest (the reverse notification)
	// per handler.
	var written sync.WaitGroup
	written.Add(2 * n)

	srv := rpc2.NewServer()
	srv.Handle("echo", func(c *rpc2.Client, args []int, reply *int) error {
		*reply = args[0]
		arrived.Done()
		<-release
		// Race this handler's own response encode against a reverse
		// notification's request encode on the same codec.
		go func() {
			_ = c.Notify("event", args[0])
		}()
		return nil
	})

	acceptErr := make(chan error, 1)
	bconn := make(chan *breakableConn, 1)
	go func() {
		conn, err := lis.Accept()
		if err != nil {
			acceptErr <- err
			return
		}
		bc := &breakableConn{Conn: conn}
		bconn <- bc
		srv.ServeCodec(&countingCodec{Codec: NewJSONCodec(bc), wg: &written})
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
			// The connection is broken on purpose, so errors here are
			// expected and not the subject of this test.
			_ = clt.Call("echo", i, &reply)
		}(i)
	}

	arrivedCh := make(chan struct{})
	go func() {
		arrived.Wait()
		close(arrivedCh)
	}()
	select {
	case <-arrivedCh:
	case err := <-acceptErr:
		t.Fatalf("accept: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for handlers to start")
	}

	bc := <-bconn
	bc.broken.Store(true)
	close(release)
	waitOrTimeout(t, &written, 5*time.Second, "timed out waiting for writes to complete")

	// Now that every racy write has been counted, actually close the
	// connection so the client's pending, response-less Calls unblock.
	bc.Close()
	wg.Wait()
}
