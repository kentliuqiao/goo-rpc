package goorpc

import (
	"encoding/json"
	"errors"
	"fmt"
	"goorpc/codec"
	"io"
	"log"
	"net"
	"reflect"
	"strings"
	"sync"
	"time"
)

const MagicNumber = 0x3bef5c

type Option struct {
	MagicNumber    int           // MagicNumber marks this's a geerpc request
	CodecType      codec.Type    // client may choose different Codec to encode body
	ConnectTimeout time.Duration // 0 means no limit
	HandleTimeout  time.Duration // 0 means no limit
}

var DefaultOption = &Option{
	MagicNumber:    MagicNumber,
	CodecType:      codec.GobType,
	ConnectTimeout: 10 * time.Second,
}

type Server struct {
	serviceMap sync.Map
}

func NewServer() *Server {
	return &Server{
		serviceMap: sync.Map{},
	}
}

var DefaultServer = NewServer()

// request holds all information of a call
type request struct {
	header       *codec.Header // header of request
	argv, replyv reflect.Value // argv and replyv of a request
	mType        *methodType
	svc          *service
}

func (s *Server) Register(rcvr interface{}) error {
	service := newService(rcvr)
	if _, dup := s.serviceMap.LoadOrStore(service.name, service); dup {
		return fmt.Errorf("rpc server: service [%s] already registered", service.name)
	}

	return nil
}

func Register(rcvr interface{}) error {
	return DefaultServer.Register(rcvr)
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

	s.serveCodec(f(conn), &opt)
}

var invalidRequest = struct{}{}

func Accept(lis net.Listener) { DefaultServer.Accept(lis) }

func (s *Server) serveCodec(cd codec.Codec, opt *Option) {
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
		go s.handleRequest(cd, req, sending, wg, opt.HandleTimeout)
	}

	wg.Wait()
	_ = cd.Close()
}

func (s *Server) findService(serviceMethod string) (svc *service, mTyp *methodType, err error) {
	dot := strings.LastIndex(serviceMethod, ".")
	if dot < 0 {
		err = errors.New("rpc server: service/method request ill-formed: " + serviceMethod)
		return
	}
	serviceName, methodName := serviceMethod[:dot], serviceMethod[dot+1:]
	svcI, ok := s.serviceMap.Load(serviceName)
	if !ok {
		err = errors.New("rpc server: cannot find serivice: " + serviceName)
		return
	}

	svc = svcI.(*service)
	mTyp = svc.method[methodName]
	if mTyp == nil {
		err = errors.New("rpc server: cannot find method: " + methodName)
		return
	}

	return
}

func (s *Server) readRequest(cd codec.Codec) (*request, error) {
	header, err := s.readRequestHeader(cd)
	if err != nil {
		return nil, err
	}
	req := &request{header: header}
	req.svc, req.mType, err = s.findService(header.ServiceMethod)
	if err != nil {
		return nil, err
	}
	req.argv, req.replyv = req.mType.newArgv(), req.mType.newReplyv()

	// make sure that argvI is a pointer, since ReadBody needs a pointer
	argvI := req.argv.Interface()
	if req.argv.Type().Kind() != reflect.Ptr {
		argvI = req.argv.Addr().Interface()
	}

	if err = cd.ReadBody(argvI); err != nil {
		log.Println("rpc server: read body err:", err)
		return nil, err
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

func (s *Server) handleRequest(cd codec.Codec, req *request, sending *sync.Mutex, wg *sync.WaitGroup, timeout time.Duration) {
	defer wg.Done()

	called := make(chan struct{})
	sent := make(chan struct{})

	go func() {
		err := req.svc.call(req.mType, req.argv, req.replyv)
		called <- struct{}{}

		if err != nil {
			req.header.Error = err.Error()
			s.sendResponse(cd, req.header, invalidRequest, sending)
			sent <- struct{}{}
			return
		}
		s.sendResponse(cd, req.header, req.replyv.Interface(), sending)
		sent <- struct{}{}
	}()

	if timeout == 0 {
		<-called
		<-sent
		return
	}

	select {
	case <-time.After(timeout):
		req.header.Error = fmt.Sprintf("rpc server: request handle timeout: expect within %s", timeout)
		s.sendResponse(cd, req.header, invalidRequest, sending)
	case <-called:
		<-sent
	}

}

func (s *Server) sendResponse(cd codec.Codec, header *codec.Header, body interface{}, sending *sync.Mutex) {
	sending.Lock()
	defer sending.Unlock()

	if err := cd.Write(header, body); err != nil {
		log.Println("rpc server: write response error:", err)
	}
}
