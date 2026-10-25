package rpc2_test

import (
	"io"
	"sync"
	"testing"

	"github.com/cenkalti/rpc2"
)

type notifyCodec struct{}

func (notifyCodec) ReadHeader(*rpc2.Request, *rpc2.Response) error { return io.EOF }
func (notifyCodec) ReadRequestBody(interface{}) error              { return nil }
func (notifyCodec) ReadResponseBody(interface{}) error             { return nil }
func (notifyCodec) WriteRequest(*rpc2.Request, interface{}) error  { return nil }
func (notifyCodec) WriteResponse(*rpc2.Response, interface{}) error {
	return nil
}
func (notifyCodec) Close() error { return nil }

func TestNotifyConcurrentClose(t *testing.T) {
	for i := 0; i < 100; i++ {
		client := rpc2.NewClientWithCodec(notifyCodec{})
		start := make(chan struct{})
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			<-start
			if err := client.Notify("event", nil); err != nil && err != rpc2.ErrShutdown {
				t.Errorf("concurrent Notify: %v", err)
			}
		}()
		go func() {
			defer wg.Done()
			<-start
			if err := client.Close(); err != nil {
				t.Errorf("Close: %v", err)
			}
		}()
		close(start)
		wg.Wait()
		if err := client.Notify("event", nil); err != rpc2.ErrShutdown {
			t.Fatalf("Notify after Close: got %v, want ErrShutdown", err)
		}
	}
}

func TestNotifyAfterShutdown(t *testing.T) {
	client := rpc2.NewClientWithCodec(notifyCodec{})
	client.Run()
	if err := client.Notify("event", nil); err != rpc2.ErrShutdown {
		t.Fatalf("Notify after read EOF: got %v, want ErrShutdown", err)
	}
}
