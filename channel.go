package pool

import (
	"errors"
	"fmt"
	"sync"
)

//Factory 创建连接方法
type Factory func() (interface{}, error)

//Close 关闭链接方法
type Close func(interface{}) error

//channelPool 存放链接信息
type channelPool struct {
	mu    sync.Mutex
	conns chan interface{}

	factory Factory
	close   Close
}

//NewChannelPool 初始化链接
func NewChannelPool(initialCap, maxCap int, factory Factory, close Close) (Pool, error) {
	if initialCap < 0 || maxCap <= 0 || initialCap > maxCap {
		return nil, errors.New("invalid capacity settings")
	}

	c := &channelPool{
		conns:   make(chan interface{}, maxCap),
		factory: factory,
		close:   close,
	}

	for i := 0; i < initialCap; i++ {
		conn, err := factory()
		if err != nil {
			c.Release()
			return nil, fmt.Errorf("factory is not able to fill the pool: %s", err)
		}
		c.conns <- conn
	}

	return c, nil
}

//getConns 获取所有连接
func (c *channelPool) getConns() chan interface{} {
	c.mu.Lock()
	conns := c.conns
	c.mu.Unlock()
	return conns
}

//Get 从pool中取一个连接
func (c *channelPool) Get() (interface{}, error) {
	conns := c.getConns()
	if conns == nil {
		return nil, ErrClosed
	}

	select {
	case conn := <-conns:
		if conn == nil {
			return nil, ErrClosed
		}

		return conn, nil
	default:
		conn, err := c.factory()
		if err != nil {
			return nil, err
		}

		return conn, nil
	}
}

//Put 将连接放回pool中
func (c *channelPool) Put(conn interface{}) error {
	if conn == nil {
		return errors.New("connection is nil. rejecting")
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.conns == nil {
		return c.close(conn)
	}

	// put the resource back into the pool. If the pool is full, this will
	// block and the default case will be executed.
	select {
	case c.conns <- conn:
		return nil
	default:
		// pool is full, close passed connection
		return c.close(conn)
	}
}

//Release 释放连接池中所有链接
func (c *channelPool) Release() {
	c.mu.Lock()
	conns := c.conns
	c.conns = nil
	c.factory = nil
	c.close = nil
	c.mu.Unlock()

	if conns == nil {
		return
	}

	close(conns)
	for conn := range conns {
		c.close(conn)
	}
}

//Len 连接池中已有的连接
func (c *channelPool) Len() int {
	return len(c.getConns())
}
