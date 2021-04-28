package goorpc

import (
	"reflect"
	"testing"
)

type Foo int

type Args struct {
	Num1, Num2 int
}

func (f *Foo) Sum(args Args, reply *int) error {
	*reply = args.Num1 + args.Num2

	return nil
}

func (f *Foo) sum(args Args, reply *int) error {
	*reply = args.Num1 + args.Num2

	return nil
}

func _assert(t *testing.T, condition bool, msg string, v ...interface{}) {
	if !condition {
		t.Errorf(msg+"\n", v...)
	}
}

func TestNewService(t *testing.T) {
	var foo Foo
	service := newService(&foo)
	_assert(t, len(service.method) == 1, "wrong method number, expect 1, but got %d", len(service.method))
	mTyp := service.method["Sum"]
	_assert(t, mTyp != nil, "method Sum should not be nil")
}

func TestMethodType_Call(t *testing.T) {
	var foo Foo
	s := newService(&foo)
	m := s.method["Sum"]

	argv := m.newArgv()
	replyv := m.newReplyv()
	argv.Set(reflect.ValueOf(Args{Num1: 1, Num2: 2}))
	err := s.call(m, argv, replyv)
	_assert(t, err == nil && *replyv.Interface().(*int) == 3 && m.numCalls == 1, "failed to call Foo.Sum")
}
