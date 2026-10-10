rpc2
====

[![GoDoc](https://godoc.org/github.com/cenkalti/rpc2?status.png)](https://godoc.org/github.com/cenkalti/rpc2)

rpc2 is a fork of the standard library's net/rpc package.
With net/rpc, only the client can call the server.
With rpc2, the server can call the client too.
Every handler gets a `*Client` argument to make these calls.

Install
--------

    go get github.com/cenkalti/rpc2

Example
-------

The server calls back into the client to report progress while it handles
the client's request.

```go
package main

import (
	"fmt"
	"log"
	"net"

	"github.com/cenkalti/rpc2"
)

func main() {
	serverConn, clientConn := net.Pipe()

	srv := rpc2.NewServer()
	srv.Handle("process", func(client *rpc2.Client, files []string, reply *int) error {
		for _, f := range files {
			// Call back into the client to report progress.
			if err := client.Call("progress", f, nil); err != nil {
				return err
			}
		}
		*reply = len(files)
		return nil
	})
	go srv.ServeConn(serverConn)

	clt := rpc2.NewClient(clientConn)
	clt.Handle("progress", func(client *rpc2.Client, file string, _ *struct{}) error {
		fmt.Println("processed", file)
		return nil
	})
	go clt.Run()

	var n int
	if err := clt.Call("process", []string{"a.jpg", "b.jpg", "c.jpg"}, &n); err != nil {
		log.Fatal(err)
	}
	fmt.Println("done:", n)
}
```

Output:

    processed a.jpg
    processed b.jpg
    processed c.jpg
    done: 3
