package main

import (
	"fmt"
	"net"

	"github.com/nestgo/pool"
)

func main() {

	//factory 创建连接的方法
	factory := func() (interface{}, error) { return net.Dial("tcp", "127.0.0.1:80") }

	//close 关闭链接的方法
	close := func(v interface{}) error { return v.(net.Conn).Close() }

	//创建一个连接池： 初始化5，最大链接30
	p, err := pool.NewChannelPool(5, 30, factory, close)
	if err != nil {
		fmt.Println("err=", err)
	}

	//从连接池中取得一个链接
	v, err := p.Get()

	//do something
	//conn=v.(net.Conn)

	//将链接放回连接池中
	p.Put(v)

	//释放连接池中的所有链接
	//p.Release()

	//查看当前链接中的数量
	current := p.Len()
	fmt.Println("len=", current)
}
