package goorpc

import (
	"go/ast"
	"log"
	"reflect"
	"sync/atomic"
)

// methodType holds all the information that a method has
type methodType struct {
	method    reflect.Method // method itself
	ArgType   reflect.Type   // method's first argument which is argin
	ReplyType reflect.Type   // method's second argument which is argout
	numCalls  uint64         // for the statistics of the method's number of calling
}

func (m *methodType) NumCalls() uint64 {
	return atomic.LoadUint64(&m.numCalls)
}

func (m *methodType) newArgv() reflect.Value {
	var argv reflect.Value
	if m.ArgType.Kind() == reflect.Ptr {
		argv = reflect.New(m.ArgType.Elem())
	} else {
		argv = reflect.New(m.ArgType).Elem()
	}

	return argv
}

func (m *methodType) newReplyv() reflect.Value {
	replyv := reflect.New(m.ReplyType.Elem())
	switch m.ReplyType.Elem().Kind() {
	case reflect.Map:
		replyv.Elem().Set(reflect.MakeMap(m.ReplyType.Elem()))
	case reflect.Slice:
		replyv.Elem().Set(reflect.MakeSlice(m.ReplyType.Elem(), 0, 0))
	}

	return replyv
}

type service struct {
	name   string                 // name of struct which implements the RPC's interface
	typ    reflect.Type           // type of the struct
	rcvr   reflect.Value          // instance of the struct which is needed as the first argument when calling method
	method map[string]*methodType // all the methods that fit the RPC's interface of the struct
}

func newService(rcvr interface{}) *service {
	s := new(service)
	s.rcvr = reflect.ValueOf(rcvr)
	s.name = reflect.Indirect(s.rcvr).Type().Name()
	s.typ = reflect.TypeOf(rcvr)
	if !ast.IsExported(s.name) {
		log.Fatalf("rpc server: %s is not a valid service name", s.name)
	}
	s.registerMethods()

	return s
}

func (s *service) registerMethods() {
	s.method = map[string]*methodType{}
	for i := 0; i < s.typ.NumMethod(); i++ {
		method := s.typ.Method(i)
		if method.Type.NumIn() != 3 || method.Type.NumOut() != 1 {
			continue
		}
		if method.Type.Out(0) != reflect.TypeOf((*error)(nil)).Elem() {
			continue
		}
		argTyp, replyTyp := method.Type.In(1), method.Type.In(2)
		if !isExportedOrBuiltin(argTyp) || !isExportedOrBuiltin(replyTyp) {
			continue
		}
		s.method[method.Name] = &methodType{
			method:    method,
			ArgType:   argTyp,
			ReplyType: replyTyp,
		}
		log.Printf("rpc server: register %s.%s\n", s.name, method.Name)
	}
}

func (s *service) call(m *methodType, argv, replyv reflect.Value) error {
	atomic.AddUint64(&m.numCalls, 1)
	f := m.method.Func
	returnVals := f.Call([]reflect.Value{s.rcvr, argv, replyv})
	errInterface := returnVals[0].Interface()
	if errInterface != nil {
		return errInterface.(error)
	}

	return nil
}

func isExportedOrBuiltin(arg reflect.Type) bool {
	return ast.IsExported(arg.Name()) || arg.PkgPath() == ""
}
