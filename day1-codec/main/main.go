package main

import (
	"encoding/json"
	"fmt"
	"goorpc"
	"goorpc/codec"
	"log"
	"net"
	"time"
)

func main() {
	addr := make(chan string)
	go startServer(addr)

	//
	conn, err := net.Dial("tcp", <-addr)
	if err != nil {
		log.Fatalln("dial goorpc error", err)
	}
	defer func() {
		_ = conn.Close()
	}()

	time.Sleep(time.Second)
	// send options
	_ = json.NewEncoder(conn).Encode(goorpc.DefaultOption)
	cd := codec.NewGobCodec(conn)
	for i := 0; i < 5; i++ {
		h := &codec.Header{
			ServiceMethod: "Foo.Bar",
			Seq:           uint64(i),
		}
		_ = cd.Write(h, fmt.Sprintf("goorpc req %d", i))
		_ = cd.ReadHeader(h)
		var reply string
		_ = cd.ReadBody(&reply)
		log.Println("reply:", reply)
	}
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
