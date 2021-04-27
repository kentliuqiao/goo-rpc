package goorpc

import (
	"encoding/json"
	"fmt"
	"goorpc/codec"
	"io"
	"log"
	"net"
	"reflect"
	"sync"
)

const MagicNumber = 0x3bef5c

type Option struct {
	MagicNumber int        // MagicNumber marks this's a geerpc request
	CodecType   codec.Type // client may choose different Codec to encode body
}

var DefaultOption = &Option{
	MagicNumber: MagicNumber,
	CodecType:   codec.GobType,
}

type Server struct{}

func NewServer() *Server {
	return &Server{}
}

var DefaultServer = NewServer()

type request struct {
	header       *codec.Header
	argv, replyv reflect.Value
}

func (s *Server) Accept(lis net.Listener) {
	for {
		conn, err := lis.Accept()
		if err != nil {
			log.Println("rpc server: accept error:", err)
			return
		}
		go s.ServeConn(conn)
	}
}

func (s *Server) ServeConn(conn io.ReadWriteCloser) {
	defer func() {
		_ = conn.Close()
	}()
	var opt Option
	if err := json.NewDecoder(conn).Decode(&opt); err != nil {
		log.Println("rpc server: options error:", err)
		return
	}

	log.Printf("rpc server: option received: %+v\n", opt)

	if opt.MagicNumber != MagicNumber {
		log.Printf("rpc server: invalid magic number %x", opt.MagicNumber)
		return
	}
	f, ok := codec.NewCodeDecFuncMap[opt.CodecType]
	if !ok || f == nil {
		log.Printf("rpc server: invalid codec type: %s", opt.CodecType)
		return
	}

	s.serveCodec(f(conn))
}

var invalidRequest = struct{}{}

func Accept(lis net.Listener) { DefaultServer.Accept(lis) }

func (s *Server) serveCodec(cd codec.Codec) {
	sending := new(sync.Mutex)
	wg := new(sync.WaitGroup)
	for {
		req, err := s.readRequest(cd)
		if err != nil {
			if req == nil {
				break
			}
			req.header.Error = err.Error()
			s.sendResponse(cd, req.header, invalidRequest, sending)
			continue
		}
		wg.Add(1)
		go s.handleRequest(cd, req, sending, wg)
	}

	wg.Wait()
	_ = cd.Close()
}

func (s *Server) readRequest(cd codec.Codec) (*request, error) {
	header, err := s.readRequestHeader(cd)
	if err != nil {
		return nil, err
	}
	req := &request{header: header}
	// TODO: now we don't know the type of argv
	// day1 just suppose it's string
	req.argv = reflect.New(reflect.TypeOf(""))
	if err := cd.ReadBody(req.argv.Interface()); err != nil {
		log.Println("rpc server: read argv error:", err)
	}

	return req, nil
}

func (s *Server) readRequestHeader(cd codec.Codec) (*codec.Header, error) {
	var h codec.Header
	if err := cd.ReadHeader(&h); err != nil {
		if err != io.EOF || err != io.ErrUnexpectedEOF {
			log.Println("rpc server: read header error:", err)
		}
		return nil, err
	}

	return &h, nil
}

func (s *Server) handleRequest(cd codec.Codec, req *request, sending *sync.Mutex, wg *sync.WaitGroup) {
	defer wg.Done()

	// TODO: shoul call registered rpc methods to get the right replyv
	// day1 just print argv and send a hello message
	log.Println(req.header, req.argv.Elem())
	req.replyv = reflect.ValueOf(fmt.Sprintf("goorpc resp %d", req.header.Seq))
	s.sendResponse(cd, req.header, req.replyv.Interface(), sending)
}

func (s *Server) sendResponse(cd codec.Codec, header *codec.Header, body interface{}, sending *sync.Mutex) {
	sending.Lock()
	defer sending.Unlock()

	if err := cd.Write(header, body); err != nil {
		log.Println("rpc server: write response error:", err)
	}
}
