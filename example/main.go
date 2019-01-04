package main

import (
	"fmt"
	"github.com/silenceper/pool"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"
)

const addr string = "127.0.0.1:80"

func main() {
	c := make(chan os.Signal)
	signal.Notify(c, os.Interrupt, os.Kill, syscall.SIGUSR1, syscall.SIGUSR2)
	go server()
	//等待tcp server启动
	time.Sleep(2 * time.Second)
	client()
	fmt.Println("使用: ctrl+c 退出服务")
	<-c
	fmt.Println("服务退出")
}

func client() {

	//factory 创建连接的方法
	factory := func() (interface{}, error) { return net.Dial("tcp", addr) }

	//close 关闭连接的方法
	close := func(v interface{}) error { return v.(net.Conn).Close() }

	//创建一个连接池： 初始化5，最大连接30
	poolConfig := &pool.Config {
		InitialCap: 5,
		MaxCap:     30,
		Factory:    factory,
		Close:      close,
		//连接最大空闲时间，超过该时间的连接 将会关闭，可避免空闲时连接EOF，自动失效的问题
		IdleTimeout: 15 * time.Second,
	}
	p, err := pool.NewChannelPool(poolConfig)
	if err != nil {
		fmt.Println("err=", err)
	}

	//从连接池中取得一个连接
	v, err := p.Get()

	//do something
	//conn=v.(net.Conn)

	//将连接放回连接池中
	p.Put(v)

	//释放连接池中的所有连接
	//p.Release()

	//查看当前连接中的数量
	current := p.Len()
	fmt.Println("len=", current)
}

func server() {
	l, err := net.Listen("tcp", addr)
	if err != nil {
		fmt.Println("Error listening: ", err)
		os.Exit(1)
	}
	defer l.Close()
	fmt.Println("Listening on ", addr)
	for {
		conn, err := l.Accept()
		if err != nil {
			fmt.Println("Error accepting: ", err)
		}
		fmt.Printf("Received message %s -> %s \n", conn.RemoteAddr(), conn.LocalAddr())
		//go handleRequest(conn)
	}
}
