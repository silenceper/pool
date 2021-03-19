package pool

import (
	"errors"
	"fmt"
	"sync"
	"time"
	//"reflect"
)

var (
	//ErrMaxActiveConnReached 连接池超限
	ErrMaxActiveConnReached = errors.New("MaxActiveConnReached")
)

// Config 连接池相关配置
type Config struct {
	//连接池中拥有的最小连接数
	InitialCap int
	//最大并发存活连接数
	MaxCap int
	//最大空闲连接
	MaxIdle int
	//生成连接的方法
	Factory func() (interface{}, error)
	//关闭连接的方法
	Close func(interface{}) error
	//检查连接是否有效的方法
	Ping func(interface{}) error
	//连接最大空闲时间，超过该事件则将失效
	IdleTimeout time.Duration
}

type connReq struct {
	idleConn *idleConn
}

// channelPool 存放连接信息
type channelPool struct {
	mu                       sync.RWMutex
	conns                    chan *idleConn
	factory                  func() (interface{}, error)
	close                    func(interface{}) error
	ping                     func(interface{}) error
	idleTimeout, waitTimeOut time.Duration
	maxActive                int
	openingConns             int
	connReqs                 []chan connReq
	openerCh                 chan struct{}
}

type idleConn struct {
	conn interface{}
	t    time.Time
}

var connectionRequestQueueSize = 1000000

// NewChannelPool 初始化连接
func NewChannelPool(poolConfig *Config) (Pool, error) {
	if !(poolConfig.InitialCap <= poolConfig.MaxIdle && poolConfig.MaxCap >= poolConfig.MaxIdle && poolConfig.InitialCap >= 0) {
		return nil, errors.New("invalid capacity settings")
	}
	if poolConfig.Factory == nil {
		return nil, errors.New("invalid factory func settings")
	}
	if poolConfig.Close == nil {
		return nil, errors.New("invalid close func settings")
	}

	c := &channelPool{
		conns:        make(chan *idleConn, poolConfig.MaxIdle),
		factory:      poolConfig.Factory,
		close:        poolConfig.Close,
		idleTimeout:  poolConfig.IdleTimeout,
		maxActive:    poolConfig.MaxCap,
		openingConns: poolConfig.InitialCap,
		openerCh:     make(chan struct{}, connectionRequestQueueSize),
	}

	if poolConfig.Ping != nil {
		c.ping = poolConfig.Ping
	}

	go c.connectionOpener()

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

// connectionOpener separate goroutine for opening new connection
func (c *channelPool) connectionOpener() {
	for {
		select {
		case _, ok := <-c.openerCh:
			if !ok {
				return
			}
			c.openNewConnection()
		}
	}
}

// openNewConnection Open one new connection
func (c *channelPool) openNewConnection() {
	conn, err := c.factory()
	if err != nil {
		c.mu.Lock()
		c.openingConns--
		c.maybeOpenNewConnections()
		c.mu.Unlock()

		// put nil connection into pool to wake up pending channel fetch
		c.Put(nil)
		return
	}

	c.Put(conn)
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
			//判断是否超时，超时则丢弃
			if timeout := c.idleTimeout; timeout > 0 {
				if wrapConn.t.Add(timeout).Before(time.Now()) {
					//丢弃并关闭该连接
					c.Close(wrapConn.conn)
					continue
				}
			}
			//判断是否失效，失效则丢弃，如果用户没有设定 ping 方法，就不检查
			if c.ping != nil {
				if err := c.Ping(wrapConn.conn); err != nil {
					c.Close(wrapConn.conn)
					continue
				}
			}
			return wrapConn.conn, nil
		default:
			c.mu.Lock()
			if c.openingConns >= c.maxActive {
				req := make(chan connReq, 1)
				c.connReqs = append(c.connReqs, req)
				c.mu.Unlock()
				ret, ok := <-req
				if !ok {
					return nil, ErrMaxActiveConnReached
				}
				if ret.idleConn.conn == nil {
					return nil, errors.New("failed to create a new connection")
				}
				if timeout := c.idleTimeout; timeout > 0 {
					if ret.idleConn.t.Add(timeout).Before(time.Now()) {
						//丢弃并关闭该连接
						c.Close(ret.idleConn.conn)
						continue
					}
				}
				return ret.idleConn.conn, nil
			}
			if c.factory == nil {
				c.mu.Unlock()
				return nil, ErrClosed
			}

			// c.factory 耗时较长，采用乐观策略，先增加，失败后再减少
			c.openingConns++
			c.mu.Unlock()
			conn, err := c.factory()
			if err != nil {
				c.mu.Lock()
				c.openingConns--
				c.mu.Unlock()
				return nil, err
			}
			return conn, nil
		}
	}
}

// Put 将连接放回pool中
func (c *channelPool) Put(conn interface{}) error {
	c.mu.Lock()
	if c.conns == nil && conn != nil {
		c.mu.Unlock()
		return c.Close(conn)
	}

	if l := len(c.connReqs); l > 0 {
		req := c.connReqs[0]
		copy(c.connReqs, c.connReqs[1:])
		c.connReqs = c.connReqs[:l-1]
		if conn == nil {
			req <- connReq{idleConn: nil}
			//return errors.New("connection is nil. rejecting")
		} else {
			req <- connReq{
				idleConn: &idleConn{conn: conn, t: time.Now()},
			}
		}
		c.mu.Unlock()
		return nil
	} else if conn != nil {
		select {
		case c.conns <- &idleConn{conn: conn, t: time.Now()}:
			c.mu.Unlock()
			return nil
		default:
			c.mu.Unlock()
			//连接池已满，直接关闭该连接
			return c.Close(conn)
		}
	}

	c.mu.Unlock()
	return errors.New("connection is nil, rejecting")
}

// maybeOpenNewConnections 如果有请求在，并且池里的连接上限未达到时，开启新的连接
// Assumes c.mu is locked
func (c *channelPool) maybeOpenNewConnections() {
	numRequest := len(c.connReqs)

	if c.maxActive > 0 {
		numCanOpen := c.maxActive - c.openingConns
		if numRequest > numCanOpen {
			numRequest = numCanOpen
		}
	}
	for numRequest > 0 {
		c.openingConns++
		numRequest--
		c.openerCh <- struct{}{}
	}
}

// Close 关闭单条连接
func (c *channelPool) Close(conn interface{}) error {
	if conn == nil {
		return errors.New("connection is nil. rejecting")
	}
	if c.close == nil {
		return nil
	}

	var err error
	err = c.close(conn)

	c.mu.Lock()
	c.openingConns--
	c.maybeOpenNewConnections()
	c.mu.Unlock()
	return err
}

// Ping 检查单条连接是否有效
func (c *channelPool) Ping(conn interface{}) error {
	if conn == nil {
		return errors.New("connection is nil. rejecting")
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
	openerCh := c.openerCh
	c.openerCh = nil
	c.mu.Unlock()

	if conns == nil {
		return
	}

	// close channels
	close(conns)
	close(openerCh)

	for wrapConn := range conns {
		//log.Printf("Type %v\n",reflect.TypeOf(wrapConn.conn))
		closeFun(wrapConn.conn)
	}
}

// Len 连接池中已有的连接
func (c *channelPool) Len() int {
	return len(c.getConns())
}
