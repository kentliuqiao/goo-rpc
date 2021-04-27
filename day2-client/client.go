package goorpc

import (
	"encoding/json"
	"errors"
	"fmt"
	"goorpc/codec"
	"io"
	"log"
	"net"
	"sync"
)

type Call struct {
	Seq           uint64
	ServiceMethod string      // format "<service>.<method>"
	Args          interface{} // arguments to the function
	Reply         interface{} // reply from the function
	Error         error       // if error occurs, it will be set
	Done          chan *Call  // strobes when call is completed
}

// for the support of asyncronous call,
// when the call is finished, done will be called to notify the caller
func (c *Call) done() {
	c.Done <- c
}

// Client represets a RPC client.
//
// There may be multiple outstanding Calls associated
// with a single client, and a client may be used by
// multiple goroutines simultaneously.
type Client struct {
	cd       codec.Codec
	opt      *Option
	sending  sync.Mutex
	header   codec.Header
	mu       sync.Mutex
	seq      uint64
	pending  map[uint64]*Call
	closing  bool // true when user called Close
	shutdown bool // true when server told us to stop
}

var _ io.Closer = (*Client)(nil)

var (
	ErrShutDown         = errors.New("connection has been shut down")
	ErrInvalidCodecType = errors.New("invalid codec type")
)

// Close closes the connection
func (client *Client) Close() error {
	client.mu.Lock()
	defer client.mu.Unlock()

	if client.closing {
		return ErrShutDown
	}
	client.closing = true

	return client.cd.Close()
}

// IsAvailable return true if the client does work
func (client *Client) IsAvailable() bool {
	client.mu.Lock()
	defer client.mu.Unlock()

	return !client.closing && !client.shutdown
}

func (client *Client) registerCall(call *Call) (uint64, error) {
	client.mu.Lock()
	defer client.mu.Unlock()

	if client.closing || client.shutdown {
		return 0, ErrShutDown
	}
	call.Seq = client.seq
	client.pending[call.Seq] = call
	client.seq++

	return call.Seq, nil
}

func (client *Client) removeCall(seq uint64) *Call {
	client.mu.Lock()
	defer client.mu.Unlock()

	call := client.pending[seq]
	delete(client.pending, seq)

	return call
}

// terminateCalls is called when client or server encounter an error
func (client *Client) terminateCalls(err error) {
	client.sending.Lock()
	defer client.sending.Unlock()
	client.mu.Lock()
	defer client.mu.Unlock()

	client.shutdown = true
	for _, call := range client.pending {
		call.Error = err
		call.done() // notify all pending Calls the err message
	}
}

func (client *Client) receive() {
	var err error
	for err == nil {

		log.Printf("client closing: %v, client shutdown: %v\n", client.closing, client.shutdown)

		var header codec.Header
		if err = client.cd.ReadHeader(&header); err != nil {
			log.Println("rpc client: read header error:", err)
			break
		}
		call := client.removeCall(header.Seq)
		switch {
		case call == nil:
			// This usually means that Write was partially failed,
			// and call was removed already.
			// In this case, just discard the body.
			err = client.cd.ReadBody(nil)
		case header.Error != "":
			call.Error = fmt.Errorf(header.Error)
			err = client.cd.ReadBody(nil)
			call.done()
		default:
			err = client.cd.ReadBody(call.Reply)
			if err != nil {
				call.Error = fmt.Errorf("reading body: %s", err.Error())
			}
			call.done()
		}
	}

	// error occurs, so terminate all Calls
	client.terminateCalls(err)
}

func NewClient(conn net.Conn, opt *Option) (*Client, error) {
	f := codec.NewCodeDecFuncMap[opt.CodecType]
	if f == nil {
		log.Println("rpc client: codec error:", ErrInvalidCodecType, opt.CodecType)
		return nil, fmt.Errorf("%s %s", ErrInvalidCodecType.Error(), opt.CodecType)
	}
	if err := json.NewEncoder(conn).Encode(opt); err != nil {
		err := fmt.Errorf("")
		log.Println("rpc client: options error:", err)
		_ = conn.Close()
		return nil, err
	}

	return newClientCodec(f(conn), opt), nil
}

func newClientCodec(cd codec.Codec, opt *Option) *Client {
	client := &Client{
		seq:     1, // seq starts from 1, 0 means invalid call
		opt:     opt,
		cd:      cd,
		pending: map[uint64]*Call{},
	}

	go client.receive()

	return client
}

func parseOptions(opts ...*Option) (*Option, error) {
	if len(opts) == 0 || opts[0] == nil {
		return DefaultOption, nil
	}
	if len(opts) != 1 {
		return nil, errors.New("number of options is more than 1")
	}
	opt := opts[0]
	opt.MagicNumber = DefaultOption.MagicNumber
	if opt.CodecType == "" {
		opt.CodecType = DefaultOption.CodecType
	}

	return opt, nil
}

// Dial connects to a RPC server with the given network address
func Dial(network, address string, opts ...*Option) (client *Client, err error) {
	opt, err := parseOptions(opts...)
	if err != nil {
		return nil, err
	}
	conn, err := net.Dial(network, address)
	if err != nil {
		return nil, err
	}

	defer func() {
		// close the connection if client is nil
		if client == nil {
			_ = conn.Close()
		}
	}()
	return NewClient(conn, opt)
}

func (client *Client) send(call *Call) {
	// make sure that the client will send a complete request
	client.sending.Lock()
	defer client.sending.Unlock()

	// register this call
	seq, err := client.registerCall(call)

	if err != nil {
		call.Error = err
		call.done()
		return
	}

	// prepare request header
	client.header.ServiceMethod = call.ServiceMethod
	client.header.Seq = seq
	client.header.Error = ""

	// encode and send the request
	if err := client.cd.Write(&client.header, call.Args); err != nil {
		call := client.removeCall(seq)
		// call may be nil, it usually means that Write partially failed,
		// client has received the response and handled
		if call != nil {
			call.Error = err
			call.done()
		}
	}
}

// Go invokes the function asyncronously.
// It returns the Call struct representing the invocation.
func (client *Client) Go(serviceMethod string, args, reply interface{}, done chan *Call) *Call {
	if done == nil {
		done = make(chan *Call, 10)
	} else if cap(done) == 0 {
		log.Panic("rpc client: done channel is unbuffered")
	}

	call := &Call{
		ServiceMethod: serviceMethod,
		Args:          args,
		Reply:         reply,
		Done:          done,
	}
	go client.send(call)

	return call
}

// Call invokes the named function, waits for it to complete,
// and returns the error.
func (client *Client) Call(serviceMethod string, args, reply interface{}) error {

	call := <-client.Go(serviceMethod, args, reply, make(chan *Call, 1)).Done

	return call.Error
}
