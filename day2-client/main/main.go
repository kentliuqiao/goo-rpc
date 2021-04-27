package main

import (
	"fmt"
	"goorpc"
	"log"
	"net"
	"sync"
	"time"
)

func main() {
	addr := make(chan string)
	go startServer(addr)

	client, err := goorpc.Dial("tcp", <-addr)
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

			args := fmt.Sprintf("goorpc req %d", i+1)
			var reply string
			if err := client.Call("Foo.Bar", args, &reply); err != nil {
				log.Fatal("call Foo.Bar error:", err)
			}

			log.Println("reply:", reply)
		}(i)
	}
	wg.Wait()
}

func startServer(addr chan string) {
	l, err := net.Listen("tcp", ":0")
	if err != nil {
		log.Fatal("network error", err)
	}
	log.Println("start rpc server on", l.Addr())
	addr <- l.Addr().String()
	goorpc.Accept(l)
}
