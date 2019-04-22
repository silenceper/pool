package pool

import (
	"errors"
	"fmt"
	"sync"
	"time"
)

// PoolConfig 连接池相关配置
type PoolConfig struct {
	InitialCap  int                         // 连接池中拥有的最小连接数
	MaxCap      int                         // 连接池中拥有的最大的连接数
	Factory     func() (interface{}, error) // 生成连接的方法
	Close       func(interface{}) error     // 关闭连接的方法
	Ping        func(interface{}) error     // 检查连接是否有效的方法
	IdleTimeout time.Duration               // 连接最大空闲时间，超过该事件则将失效
}

// channelPool 存放连接信息
type channelPool struct {
	mu          sync.Mutex
	conns       chan *idleConn
	factory     func() (interface{}, error)
	close       func(interface{}) error
	ping        func(interface{}) error
	idleTimeout time.Duration
}

type idleConn struct {
	conn interface{}
	t    time.Time
}

// NewChannelPool 初始化连接
func NewChannelPool(poolConfig *PoolConfig) (Pool, error) {
	if poolConfig.InitialCap < 0 || poolConfig.MaxCap <= 0 || poolConfig.InitialCap > poolConfig.MaxCap {
		return nil, errors.New("invalid capacity settings")
	}
	if poolConfig.Factory == nil {
		return nil, errors.New("invalid factory func settings")
	}
	if poolConfig.Close == nil {
		return nil, errors.New("invalid close func settings")
	}

	c := &channelPool{
		conns:       make(chan *idleConn, poolConfig.MaxCap),
		factory:     poolConfig.Factory,
		close:       poolConfig.Close,
		idleTimeout: poolConfig.IdleTimeout,
	}

	if poolConfig.Ping != nil {
		c.ping = poolConfig.Ping
	}

	for i := 0; i < poolConfig.InitialCap; i++ {
		conn, err := c.factory()
		if err != nil {
			c.Release()
			return nil, fmt.Errorf("factory is not able to fill the pool: %s", err)
		}
		c.conns <- &idleConn{conn: conn, t: time.Now()}
	}

	return c, nil
}

// getConns 获取所有连接
func (c *channelPool) getConns() chan *idleConn {
	c.mu.Lock()
	conns := c.conns
	c.mu.Unlock()
	return conns
}

// Get 从pool中取一个连接
func (c *channelPool) Get() (interface{}, error) {
	conns := c.getConns()
	if conns == nil {
		return nil, ErrClosed
	}
	for {
		select {
		case wrapConn := <-conns:
			if wrapConn == nil {
				return nil, ErrClosed
			}
			// 判断是否超时，超时则丢弃
			if timeout := c.idleTimeout; timeout > 0 {
				if wrapConn.t.Add(timeout).Before(time.Now()) {
					//丢弃并关闭该连接
					c.Close(wrapConn.conn)
					continue
				}
			}
			// 判断是否失效，失效则丢弃，如果用户没有设定 ping 方法，就不检查
			if c.ping != nil {
				if err := c.Ping(wrapConn.conn); err != nil {
					fmt.Println("conn is not able to be connected: ", err)
					continue
				}
			}
			return wrapConn.conn, nil
		default:
			return c.Connect()
		}
	}
}

// Conn 重新创建一个连接
func (c *channelPool) Connect() (interface{}, error) {
	if c.factory == nil {
		return nil, errors.New("factory func is nil. rejecting")
	}
	conn, err := c.factory()
	if err != nil {
		return nil, err
	}
	return conn, nil
}

// Put 将连接放回pool中
func (c *channelPool) Put(conn interface{}) error {
	if conn == nil {
		return errors.New("connection is nil. rejecting")
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.conns == nil {
		return c.Close(conn)
	}

	select {
	case c.conns <- &idleConn{conn: conn, t: time.Now()}:
		return nil
	default:
		//连接池已满，直接关闭该连接
		return c.Close(conn)
	}
}

// Close 关闭单条连接
func (c *channelPool) Close(conn interface{}) error {
	if conn == nil {
		return errors.New("connection is nil. rejecting")
	}
	if c.close == nil {
		return errors.New("close func is nil. rejecting")
	}
	return c.close(conn)
}

// Ping 检查单条连接是否有效
func (c *channelPool) Ping(conn interface{}) error {
	if conn == nil {
		return errors.New("connection is nil. rejecting")
	}
	if c.ping == nil {
		return errors.New("ping func is nil. rejecting")
	}
	return c.ping(conn)
}

// Release 释放连接池中所有连接
func (c *channelPool) Release() {
	c.mu.Lock()
	conns := c.conns
	c.conns = nil
	c.factory = nil
	c.ping = nil
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

// Len 连接池中已有的连接
func (c *channelPool) Len() int {
	return len(c.getConns())
}
