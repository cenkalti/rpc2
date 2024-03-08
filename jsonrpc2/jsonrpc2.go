// Package jsonrpc2 implements a JSON-RPC 2.0 ClientCodec and ServerCodec for the rpc2 package.
//
// Beside struct types, JSONCodec allows using positional arguments.
// Use []interface{} as the type of argument when sending and receiving methods.
//
// Positional arguments example:
//
//	server.Handle("add", func(client *rpc2.Client, args []interface{}, result *float64) error {
//		*result = args[0].(float64) + args[1].(float64)
//		return nil
//	})
//
//	var result float64
//	client.Call("add", []interface{}{1, 2}, &result)
package jsonrpc2

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"sync"

	"github.com/cenkalti/rpc2"
)

type jsonCodec struct {
	dec *json.Decoder // for reading JSON values
	enc *json.Encoder // for writing JSON values
	c   io.Closer

	// temporary work space
	msg            message
	serverRequest  serverRequest
	clientResponse clientResponse

	// JSON-RPC clients can use arbitrary json values as request IDs.
	// Package rpc expects uint64 request IDs.
	// We assign uint64 sequence numbers to incoming requests
	// but save the original request ID in the pending map.
	// When rpc responds, we use the sequence number in
	// the response to find the original request ID.
	mutex   sync.Mutex // protects seq, pending
	pending map[uint64]*json.RawMessage
	seq     uint64
}

// NewJSONRPC2Codec returns a new rpc2.Codec using JSON-RPC 2.0 on conn.
func NewJSONRPC2Codec(conn io.ReadWriteCloser) rpc2.Codec {
	return &jsonCodec{
		dec:     json.NewDecoder(conn),
		enc:     json.NewEncoder(conn),
		c:       conn,
		pending: make(map[uint64]*json.RawMessage),
	}
}

// serverRequest and clientResponse combined
type message struct {
	Jsonrpc string           `json:"jsonrpc"`
	Method  string           `json:"method"`
	Params  *json.RawMessage `json:"params"`
	Id      *json.RawMessage `json:"id"`
	Result  *json.RawMessage `json:"result"`
	Error   interface{}      `json:"error"`
}

// Unmarshal to
type serverRequest struct {
	Jsonrpc string           `json:"jsonrpc"`
	Method  string           `json:"method"`
	Params  *json.RawMessage `json:"params"`
	ID      *json.RawMessage `json:"id"`
}
type clientResponse struct {
	Jsonrpc string             `json:"jsonrpc"`
	ID      uint64             `json:"id"`
	Result  *json.RawMessage   `json:"result,omitempty"`
	Error   *rpc2.JsonRpcError `json:"error,omitempty"`
}

// to Marshal
type serverResponse struct {
	Jsonrpc string           `json:"jsonrpc"`
	ID      *json.RawMessage `json:"id"`
	Result  *interface{}     `json:"result,omitempty"`
	Error   *interface{}     `json:"error,omitempty"`
}
type clientRequestArray struct {
	Jsonrpc string        `json:"jsonrpc"`
	Method  string        `json:"method"`
	Params  []interface{} `json:"params"`
	ID      *uint64       `json:"id"`
}

type clientRequestNamed struct {
	Jsonrpc string      `json:"jsonrpc"`
	Method  string      `json:"method"`
	Params  interface{} `json:"params"`
	ID      *uint64     `json:"id"`
}

func (c *jsonCodec) ReadHeader(req *rpc2.Request, resp *rpc2.Response) error {
	c.msg = message{}
	if err := c.dec.Decode(&c.msg); err != nil {
		return err
	}
	if c.msg.Method != "" {
		// request comes to server
		c.serverRequest.Jsonrpc = c.msg.Jsonrpc
		c.serverRequest.ID = c.msg.Id
		c.serverRequest.Method = c.msg.Method
		c.serverRequest.Params = c.msg.Params

		req.Method = c.serverRequest.Method

		// JSON request id can be any JSON value;
		// RPC package expects uint64.  Translate to
		// internal uint64 and save JSON on the side.
		if c.serverRequest.ID == nil {
			// Notification
		} else {
			c.mutex.Lock()
			c.seq++
			c.pending[c.seq] = c.serverRequest.ID
			c.serverRequest.ID = nil
			req.Seq = c.seq
			c.mutex.Unlock()
		}
	} else {
		// response comes to client
		err := json.Unmarshal(*c.msg.Id, &c.clientResponse.ID)
		if err != nil {
			return err
		}
		c.clientResponse.Jsonrpc = "2.0"
		c.clientResponse.Result = c.msg.Result

		resp.Error = nil
		resp.Seq = c.clientResponse.ID
		if c.msg.Error != nil || c.clientResponse.Result == nil {
			errorAsJson, err := json.Marshal(c.msg.Error)
			if err != nil {
				return fmt.Errorf("failed to marshall error %v: %w", c.msg.Error, err)
			}
			jsonRpcError := &rpc2.JsonRpcError{}
			err = json.Unmarshal(errorAsJson, jsonRpcError)
			if err != nil {
				return fmt.Errorf("failed to unmarshall error %v: %w", string(errorAsJson), err)
			}
			log.Println(string(errorAsJson))
			c.clientResponse.Error = jsonRpcError
			resp.Error = jsonRpcError
		}
	}
	return nil
}

var errMissingParams = errors.New("jsonrpc: request body missing params")

func (c *jsonCodec) ReadRequestBody(x interface{}) error {
	if x == nil {
		return nil
	}
	if c.serverRequest.Params == nil {
		return errMissingParams
	}
	var paramsArray *[]interface{}
	var paramsNamed *interface{}
	switch x := x.(type) {
	case *[]interface{}:
		paramsArray = x
	default:
		paramsNamed = &x
	}
	if paramsArray != nil {
		return json.Unmarshal(*c.serverRequest.Params, paramsArray)
	}
	return json.Unmarshal(*c.serverRequest.Params, paramsNamed)
}

func (c *jsonCodec) ReadResponseBody(x interface{}) error {
	if x == nil {
		return nil
	}
	return json.Unmarshal(*c.clientResponse.Result, x)
}

func (c *jsonCodec) WriteRequest(r *rpc2.Request, param interface{}) error {
	switch param := param.(type) {
	case []interface{}:
		req := &clientRequestArray{Jsonrpc: "2.0", Method: r.Method}
		req.Params = param
		if r.Seq == 0 {
			// Notification
			req.ID = nil
		} else {
			seq := r.Seq
			req.ID = &seq
		}
		return c.enc.Encode(req)
	default:
		req := &clientRequestNamed{Jsonrpc: "2.0", Method: r.Method}
		req.Params = param
		if r.Seq == 0 {
			// Notification
			req.ID = nil
		} else {
			seq := r.Seq
			req.ID = &seq
		}
		return c.enc.Encode(req)
	}
}

var null = json.RawMessage([]byte("null"))

func (c *jsonCodec) WriteResponse(r *rpc2.Response, x interface{}) error {
	c.mutex.Lock()
	b, ok := c.pending[r.Seq]
	if !ok {
		c.mutex.Unlock()
		return errors.New("invalid sequence number in response")
	}
	delete(c.pending, r.Seq)
	c.mutex.Unlock()

	if b == nil {
		// Invalid request so no id.  Use JSON null.
		b = &null
	}
	resp := serverResponse{Jsonrpc: "2.0", ID: b}
	if r.Error == nil {
		resp.Error = nil
		resp.Result = &x
	} else {
		var errIf interface{} = r.Error
		resp.Error = &errIf
		resp.Result = nil
	}
	return c.enc.Encode(resp)
}

func (c *jsonCodec) Close() error {
	return c.c.Close()
}

func (c *jsonCodec) IsV2() bool {
	return true
}
