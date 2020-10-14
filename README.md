# pool
[![PkgGoDev](https://pkg.go.dev/badge/github.com/silenceper/pool)](https://pkg.go.dev/github.com/silenceper/pool)
[![Go Report Card](https://goreportcard.com/badge/github.com/silenceper/pool)](https://goreportcard.com/report/github.com/silenceper/pool)


[中文文档](./README_ZH_CN.md)

A golang universal network connection pool.

## Feature：

- More versatile, The connection type in the connection pool is `interface{}`, making it more versatile
- More configurable, The connection supports setting the maximum idle time, the timeout connection will be closed and discarded, which can avoid the problem of automatic connection failure when idle
- Support user setting `ping` method, used to check the connectivity of connection, invalid connection will be discarded
- Support connection waiting, When the connection pool is full, support for connection waiting (like the go db connection pool)

## Basic Usage:

```go

//factory Specify the method to create the connection
factory := func() (interface{}, error) { return net.Dial("tcp", "127.0.0.1:4000") }

//close Specify the method to close the connection
close := func(v interface{}) error { return v.(net.Conn).Close() }

//ping Specify the method to detect whether the connection is invalid
//ping := func(v interface{}) error { return nil }

//Create a connection pool: Initialize the number of connections to 5, the maximum idle connection is 20, and the maximum concurrent connection is 30
poolConfig := &pool.Config{
	InitialCap: 5,
	MaxIdle:   20,
	MaxCap:     30,
	Factory:    factory,
	Close:      close,
	//Ping:       ping,
	//The maximum idle time of the connection, the connection exceeding this time will be closed, which can avoid the problem of automatic failure when connecting to EOF when idle
	IdleTimeout: 15 * time.Second,
}
p, err := pool.NewChannelPool(poolConfig)
if err != nil {
	fmt.Println("err=", err)
}

//Get a connection from the connection pool
v, err := p.Get()

//do something
//conn=v.(net.Conn)

//Put the connection back into the connection pool, when the connection is no longer in use
p.Put(v)

//Release all connections in the connection pool, when resources need to be destroyed
p.Release()

//View the number of connections in the current connection pool
current := p.Len()


```


#### Remarks:
The connection pool implementation refers to pool [https://github.com/fatih/pool](https://github.com/fatih/pool) , thanks.


## License

The MIT License (MIT) - see LICENSE for more details
