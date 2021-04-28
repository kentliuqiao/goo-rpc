package main

import (
	"context"
	"goorpc"
	"log"
	"net"
	"net/http"
	"sync"
	"time"
)

type Foo int

type Args struct {
	Num1, Num2 int
}

func (f *Foo) Sum(args Args, reply *int) error {
	*reply = args.Num1 + args.Num2

	return nil
}

func main() {
	log.SetFlags(0)
	addr := make(chan string)
	go call(addr)
	startServer(addr)
}

func call(addr chan string) {
	client, err := goorpc.DialHTTP("tcp", <-addr)
	if err != nil {
		log.Fatalln("client dial error", err)
	}
	defer func() {
		_ = client.Close()
	}()

	time.Sleep(time.Second)
	// send options
	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()

			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()

			args := &Args{i + 1, i + 2}
			var reply int
			if err := client.Call(ctx, "Foo.Sum", args, &reply); err != nil {
				log.Fatal("call Foo.Sum error:", err)
			}

			log.Println("reply:", reply)
		}(i)
	}
	wg.Wait()
}

func startServer(addr chan string) {
	var foo Foo
	if err := goorpc.Register(&foo); err != nil {
		log.Fatal("register error", err)
	}
	// pick a free port
	l, err := net.Listen("tcp", ":9999")
	if err != nil {
		log.Fatal("network error", err)
	}
	goorpc.HandleHTTP()
	log.Println("start rpc server on", l.Addr())
	addr <- l.Addr().String()
	_ = http.Serve(l, nil)
}
