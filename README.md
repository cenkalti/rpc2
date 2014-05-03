rpc2
====

[![GoDoc](https://godoc.org/github.com/cenkalti/rpc2?status.png)](https://godoc.org/github.com/cenkalti/rpc2)
[![Build Status](https://travis-ci.org/cenkalti/rpc2.png)](https://travis-ci.org/cenkalti/rpc2)

rpc2 is a fork of net/rpc package in the standard library.
The main goal is to add bi-directional support to calls.
That means server can call the methods of client.
This is not possible with net/rpc package.
In order to do this it adds a `*Client` argument to method signatures.

Example Server:
---------------

    srv := NewServer()
    srv.Handle("add", func(client *Client, args *Args, reply *Reply) error {
        // Reversed call (server to client)
        var rep Reply
        client.Call("mult", Args{2, 3}, &rep)
        fmt.Println("mult result:", rep)

        *reply = Reply(args.A + args.B)
        return nil
    })

    lis, _ := net.Listen("tcp", "127.0.0.1:5000")
    srv.Accept(lis)

Example Client:
---------------

    clt := NewClient(conn)
    clt.Handle("mult", func(client *Client, args *Args, reply *Reply) error {
        *reply = Reply(args.A * args.B)
        return nil
    })

    conn, _ := net.Dial("tcp", "127.0.0.1:5000")
    go clt.ServeConn(conn)

    var rep Reply
    clt.Call("add", Args{1, 2}, &rep)
    fmt.Println("add result:", rep)

