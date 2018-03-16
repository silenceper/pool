package pool

import (
	"errors"
	"fmt"
	"sync"
	"time"
)

//PoolConfig 连接池相关配置
type PoolConfig struct {
	//连接池中拥有的最小连接数
	InitialCap int
	//连接池中拥有的最大的连接数
	MaxCap int
	//生成连接的方法
	Factory func() (interface{}, error)
	//关闭链接的方法
	Close func(interface{}) error
	//链接最大空闲时间，超过该事件则将失效，放回连接池会重新计算
	IdleTimeout time.Duration
	//链接从创建开始的总生存时间，取出放回不影响计时
	Lifetime time.Duration
}

//channelPool 存放链接信息
type channelPool struct {
	mu          sync.Mutex
	conns       chan *idleConn
	factory     func() (interface{}, error)
	close       func(interface{}) error
	idleTimeout time.Duration
	lifetime 	time.Duration
}

type idleConn struct {
	conn interface{}
	t    time.Time
	b    time.Time
}

//NewChannelPool 初始化链接
func NewChannelPool(poolConfig *PoolConfig) (Pool, error) {
	if poolConfig.InitialCap < 0 || poolConfig.MaxCap <= 0 || poolConfig.InitialCap > poolConfig.MaxCap {
		return nil, errors.New("invalid capacity settings")
	}

	c := &channelPool{
		conns:       make(chan *idleConn, poolConfig.MaxCap),
		factory:     poolConfig.Factory,
		close:       poolConfig.Close,
		idleTimeout: poolConfig.IdleTimeout,
		lifetime:    poolConfig.Lifetime,
	}

	for i := 0; i < poolConfig.InitialCap; i++ {
		conn, err := c.factory()
		if err != nil {
			c.Release()
			return nil, fmt.Errorf("factory is not able to fill the pool: %s", err)
		}
		c.conns <- &idleConn{conn: conn, t: time.Now(), b: time.Now()}
	}

	return c, nil
}

//getConns 获取所有连接
func (c *channelPool) getConns() chan *idleConn {
	c.mu.Lock()
	conns := c.conns
	c.mu.Unlock()
	return conns
}

//Get 从pool中取一个连接
func (c *channelPool) Get() (interface{}, time.Time, error) {
	conns := c.getConns()
	if conns == nil {
		return nil, time.Now(), ErrClosed
	}
	for {
		select {
		case wrapConn := <-conns:
			if wrapConn == nil {
				return nil, time.Now(), ErrClosed
			}
			//判断是否超时，超时则丢弃
			if timeout := c.idleTimeout; timeout > 0 {
				if wrapConn.t.Add(timeout).Before(time.Now()) {
					//丢弃并关闭该链接
					c.Close(wrapConn.conn)
					continue
				}
			}
			//判断是否超过lifetime
			if timeout := c.lifetime; timeout > 0 {
				if wrapConn.b.Add(timeout).Before(time.Now()) {
					//丢弃并关闭该链接
					c.Close(wrapConn.conn)
					continue
				}
			}
			return wrapConn.conn, wrapConn.b, nil
		default:
			conn, err := c.factory()
			if err != nil {
				return nil, time.Now(), err
			}

			return conn, time.Now(), nil
		}
	}
}

//Put 将连接放回pool中
func (c *channelPool) Put(conn interface{}, birth time.Time) error {
	if conn == nil {
		return errors.New("connection is nil. rejecting")
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.conns == nil {
		return c.Close(conn)
	}

	select {
	case c.conns <- &idleConn{conn: conn, t: time.Now(), b: birth}:
		return nil
	default:
		//连接池已满，直接关闭该链接
		return c.Close(conn)
	}
}

//Close 关闭单条连接
func (c *channelPool) Close(conn interface{}) error {
	if conn == nil {
		return errors.New("connection is nil. rejecting")
	}
	return c.close(conn)
}

//Release 释放连接池中所有链接
func (c *channelPool) Release() {
	c.mu.Lock()
	conns := c.conns
	c.conns = nil
	c.factory = nil
	closeFun := c.close
	c.close = nil
	c.mu.Unlock()

	if conns == nil {
		return
	}

	close(conns)
	for wrapConn := range conns {
		closeFun(wrapConn.conn)
	}
}

//Len 连接池中已有的连接
func (c *channelPool) Len() int {
	return len(c.getConns())
}
