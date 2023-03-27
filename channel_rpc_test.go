package pool

import (
	"errors"
	"math/rand"
	"net"
	"net/http"
	"net/rpc"
	"sync"
	"testing"
	"time"

	log "github.com/sirupsen/logrus"
)

var (
	InitialCap = 5
	MaxIdleCap = 10
	MaximumCap = 100
	network    = "tcp"
	address    = "127.0.0.1:7777"
	//factory    = func() (interface{}, error) { return net.Dial(network, address) }
	factory = func() (interface{}, error) {
		return rpc.DialHTTP(network, address)
	}
	closeFac = func(v interface{}) error {
		nc := v.(*rpc.Client)
		return nc.Close()
	}
	pingFac = func(v interface{}) error {
		nc := v.(*rpc.Client)
		var ans int
		return nc.Call("Arith.Multiply", Args{A: 1, B: 2}, &ans)
	}
)

func init() {
	// used for factory function
	log.SetLevel(log.InfoLevel)
	go rpcServer()
	time.Sleep(time.Millisecond * 300) // wait until tcp server has been settled

	rand.Seed(time.Now().UTC().UnixNano())
}

func TestNew(t *testing.T) {
	p, err := newChannelPool()
	defer p.Release()
	if err != nil {
		t.Errorf("New error: %s", err)
	}
}
func TestPool_Get_Impl(t *testing.T) {
	p, _ := newChannelPool()
	defer p.Release()

	conn, err := p.Get()
	if err != nil {
		t.Errorf("Get error: %s", err)
	}
	_, ok := conn.(*rpc.Client)
	if !ok {
		t.Errorf("Conn is not of type poolConn")
	}
	p.Put(conn)
}

func TestPool_Get(t *testing.T) {
	p, _ := newChannelPool()
	defer p.Release()

	_, err := p.Get()
	if err != nil {
		t.Errorf("Get error: %s", err)
	}

	// after one get, current capacity should be lowered by one.
	if p.Len() != (InitialCap - 1) {
		t.Errorf("Get error. Expecting %d, got %d",
			(InitialCap - 1), p.Len())
	}

	// get them all
	var wg sync.WaitGroup
	for i := 0; i < (MaximumCap - 2); i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := p.Get()
			if err != nil {
				t.Errorf("Get error: %s", err)
			}
		}()
	}
	wg.Wait()

	if p.Len() != 0 {
		t.Errorf("Get error. Expecting %d, got %d",
			(InitialCap - 1), p.Len())
	}

	go func() {
		c, err := p.Get()
		defer p.Put(c)
		time.Sleep(2 * time.Second)
		if err != nil {
			t.Errorf("Get error: %s", err)
		}
	}()
	time.Sleep(1 * time.Second)
	_, err = p.Get()
	if err != nil {
		t.Errorf("Get error: %s", err)
	}

}

func TestPool_Put(t *testing.T) {
	pconf := Config{InitialCap: InitialCap, MaxCap: MaximumCap, Factory: factory, Close: closeFac, IdleTimeout: time.Second * 20,
		MaxIdle: MaxIdleCap}
	p, err := NewChannelPool(&pconf)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Release()

	// get/create from the pool
	conns := make([]interface{}, MaximumCap)
	for i := 0; i < MaximumCap; i++ {
		conn, _ := p.Get()
		conns[i] = conn
	}

	// now put them all back
	for _, conn := range conns {
		p.Put(conn)
	}

	if p.Len() != MaxIdleCap {
		t.Errorf("Put error len. Expecting %d, got %d",
			1, p.Len())
	}

	p.Release() // close pool

}

func TestPool_UsedCapacity(t *testing.T) {
	p, _ := newChannelPool()
	defer p.Release()

	if p.Len() != InitialCap {
		t.Errorf("InitialCap error. Expecting %d, got %d",
			InitialCap, p.Len())
	}
}

func TestPool_Close(t *testing.T) {
	p, _ := newChannelPool()

	// now close it and test all cases we are expecting.
	p.Release()

	c := p.(*channelPool)

	if c.conns != nil {
		t.Errorf("Close error, conns channel should be nil")
	}

	if c.factory != nil {
		t.Errorf("Close error, factory should be nil")
	}

	_, err := p.Get()
	if err == nil {
		t.Errorf("Close error, get conn should return an error")
	}

	if p.Len() != 0 {
		t.Errorf("Close error used capacity. Expecting 0, got %d", p.Len())
	}
}

func TestPoolConcurrent(t *testing.T) {
	p, _ := newChannelPool()
	pipe := make(chan interface{}, 0)

	go func() {
		p.Release()
	}()

	for i := 0; i < MaximumCap; i++ {
		go func() {
			conn, _ := p.Get()

			pipe <- conn
		}()

		go func() {
			conn := <-pipe
			if conn == nil {
				return
			}
			p.Put(conn)
		}()
	}
}

func TestPoolWriteRead(t *testing.T) {
	//p, _ := NewChannelPool(0, 30, factory)
	p, _ := newChannelPool()
	conn, _ := p.Get()
	cli := conn.(*rpc.Client)
	var resp int
	err := cli.Call("Arith.Multiply", Args{1, 2}, &resp)
	if err != nil {
		t.Error(err)
	}
	if resp != 2 {
		t.Error("rpc.err")
	}
}

func TestPoolConcurrent2(t *testing.T) {
	//p, _ := NewChannelPool(0, 30, factory)
	p, _ := newChannelPool()

	var wg sync.WaitGroup

	go func() {
		for i := 0; i < 10; i++ {
			wg.Add(1)
			go func(i int) {
				conn, _ := p.Get()
				time.Sleep(time.Millisecond * time.Duration(rand.Intn(100)))
				p.Close(conn)
				wg.Done()
			}(i)
		}
	}()

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(i int) {
			conn, _ := p.Get()
			time.Sleep(time.Millisecond * time.Duration(rand.Intn(100)))
			p.Close(conn)
			wg.Done()
		}(i)
	}

	wg.Wait()
}

func TestInValidConfig(t *testing.T) {
	pconf := Config{InitialCap: MaxIdleCap + 1, MaxCap: MaximumCap, Factory: factory, Close: closeFac, IdleTimeout: time.Second * 20,
		MaxIdle: MaxIdleCap, Ping: pingFac}
	if _, err := NewChannelPool(&pconf); err != ErrInvalidCapSetting {
		t.Errorf("err should be %v, but got %v", ErrInvalidCapSetting, err)
	}
	pconf = Config{InitialCap: MaximumCap + 1, MaxCap: MaximumCap, Factory: factory, Close: closeFac, IdleTimeout: time.Second * 20,
		MaxIdle: MaxIdleCap, Ping: pingFac}
	if _, err := NewChannelPool(&pconf); err != ErrInvalidCapSetting {
		t.Errorf("err should be %v, but got %v", ErrInvalidCapSetting, err)
	}
	pconf = Config{InitialCap: -1, MaxCap: MaximumCap, Factory: factory, Close: closeFac, IdleTimeout: time.Second * 20,
		MaxIdle: MaxIdleCap, Ping: pingFac}
	if _, err := NewChannelPool(&pconf); err != ErrInvalidCapSetting {
		t.Errorf("err should be %v, but got %v", ErrInvalidCapSetting, err)
	}
	pconf = Config{InitialCap: InitialCap, MaxCap: MaximumCap, Factory: nil, Close: closeFac, IdleTimeout: time.Second * 20,
		MaxIdle: MaxIdleCap, Ping: pingFac}
	if _, err := NewChannelPool(&pconf); err != ErrInvalidFactoryFunc {
		t.Errorf("err should be %v, but got %v", ErrInvalidFactoryFunc, err)
	}
	pconf = Config{InitialCap: InitialCap, MaxCap: MaximumCap, Factory: factory, Close: nil, IdleTimeout: time.Second * 20,
		MaxIdle: MaxIdleCap, Ping: pingFac}
	if _, err := NewChannelPool(&pconf); err != ErrInvalidCloseFunc {
		t.Errorf("err should be %v, but got %v", ErrInvalidCloseFunc, err)
	}
}

func TestFactoryErr(t *testing.T) {
	error_msg := "invalid factory"
	invalidFactoryFunc := func() (interface{}, error) {
		return nil, errors.New(error_msg)
	}
	pconf := Config{InitialCap: InitialCap, MaxCap: MaximumCap, Factory: invalidFactoryFunc, Close: closeFac, IdleTimeout: time.Second * 20,
		MaxIdle: MaxIdleCap, Ping: pingFac}
	if _, err := NewChannelPool(&pconf); err.Error() != "factory is not able to fill the pool: "+error_msg {
		t.Errorf("err should be %v, but got %v", ErrInvalidCapSetting, err)
	}
}

func TestPingErr(t *testing.T) {
	error_msg := "invalid factory"
	invalidFactoryFunc := func() (interface{}, error) {
		return nil, errors.New(error_msg)
	}
	pconf := Config{InitialCap: InitialCap, MaxCap: MaximumCap, Factory: invalidFactoryFunc, Close: closeFac, IdleTimeout: time.Second * 20,
		MaxIdle: MaxIdleCap, Ping: pingFac}
	if _, err := NewChannelPool(&pconf); err.Error() != "factory is not able to fill the pool: "+error_msg {
		t.Errorf("err should be %v, but got %v", ErrInvalidCapSetting, err)
	}
}

//
//func TestPoolConcurrent3(t *testing.T) {
//	p, _ := NewChannelPool(0, 1, factory)
//
//	var wg sync.WaitGroup
//
//	wg.Add(1)
//	go func() {
//		p.Close()
//		wg.Done()
//	}()
//
//	if conn, err := p.Get(); err == nil {
//		conn.Close()
//	}
//
//	wg.Wait()
//}

func newChannelPool() (Pool, error) {
	pconf := Config{InitialCap: InitialCap, MaxCap: MaximumCap, Factory: factory, Close: closeFac, IdleTimeout: time.Second * 20,
		MaxIdle: MaxIdleCap, Ping: pingFac}
	return NewChannelPool(&pconf)
}

func rpcServer() {
	arith := new(Arith)
	rpc.Register(arith)
	rpc.HandleHTTP()

	l, e := net.Listen("tcp", address)
	if e != nil {
		panic(e)
	}
	go http.Serve(l, nil)
}

type Args struct {
	A, B int
}

type Arith int

func (t *Arith) Multiply(args *Args, reply *int) error {
	*reply = args.A * args.B
	return nil
}
