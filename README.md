# pool
Golang 实现的连接池


该连接池参考 [https://github.com/fatih/pool](https://github.com/fatih/pool) 实现，用interface{}替代原有的net.Conn ，使得更加通用。

## 基本使用

```go

//factory 创建连接的方法
factory := func() (interface{}, error) { return net.Dial("tcp", "127.0.0.1:4000") }

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
p.Release()

//查看当前链接中的数量
current := p.Len()


```

## License

The MIT License (MIT) - see LICENSE for more details
