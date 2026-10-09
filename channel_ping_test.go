package pool

import (
	"errors"
	"sync"
	"testing"
	"time"
)

func TestPoolGetPing(t *testing.T) {
	for _, path := range []string{"idle", "waiting"} {
		for _, health := range []string{"healthy", "unhealthy", "disabled"} {
			t.Run(path+"/"+health, func(t *testing.T) {
				created := 0
				var checked, closed []int
				config := &Config{
					MaxCap:  1,
					MaxIdle: 1,
					Factory: func() (interface{}, error) {
						created++
						return created, nil
					},
					Close: func(conn interface{}) error {
						closed = append(closed, conn.(int))
						return nil
					},
				}
				if health != "disabled" {
					config.Ping = func(conn interface{}) error {
						checked = append(checked, conn.(int))
						if health == "unhealthy" {
							return errors.New("connection is unhealthy")
						}
						return nil
					}
				}
				p, err := NewChannelPool(config)
				if err != nil {
					t.Fatal(err)
				}
				defer p.Release()
				conn, err := p.Get()
				if err != nil {
					t.Fatal(err)
				}
				if path == "idle" {
					if err := p.Put(conn); err != nil {
						t.Fatal(err)
					}
				}
				type result struct {
					conn interface{}
					err  error
				}
				done := make(chan result, 1)
				go func() {
					conn, err := p.Get()
					done <- result{conn, err}
				}()
				if path == "waiting" {
					c := p.(*channelPool)
					deadline := time.Now().Add(time.Second)
					for {
						c.mu.Lock()
						waiting := len(c.connReqs)
						c.mu.Unlock()
						if waiting == 1 {
							break
						}
						if time.Now().After(deadline) {
							t.Fatal("Get did not wait for a connection")
						}
						time.Sleep(time.Millisecond)
					}
					if err := p.Put(conn); err != nil {
						t.Fatal(err)
					}
				}
				var got result
				select {
				case got = <-done:
				case <-time.After(time.Second):
					t.Fatal("Get did not return a connection")
				}
				if got.err != nil {
					t.Fatal(got.err)
				}
				wantConn := 1
				if health == "unhealthy" {
					wantConn = 2
				}
				if got.conn != wantConn {
					t.Errorf("Get returned %v, want %d", got.conn, wantConn)
				}
				if created != wantConn {
					t.Errorf("factory created %d connections, want %d", created, wantConn)
				}
				if health == "disabled" {
					if len(checked) != 0 {
						t.Errorf("Ping checked %v, want no calls", checked)
					}
				} else if len(checked) != 1 || checked[0] != 1 {
					t.Errorf("Ping checked %v, want [1]", checked)
				}
				if health == "unhealthy" {
					if len(closed) != 1 || closed[0] != 1 {
						t.Errorf("Close received %v, want [1]", closed)
					}
				} else if len(closed) != 0 {
					t.Errorf("Close received %v, want no calls", closed)
				}
				if err := p.Put(got.conn); err != nil {
					t.Fatal(err)
				}
			})
		}
	}
}

func TestPoolGetPingDuringRelease(t *testing.T) {
	for i := 0; i < 100; i++ {
		p, err := NewChannelPool(&Config{
			InitialCap: 1,
			MaxCap:     1,
			MaxIdle:    1,
			Factory: func() (interface{}, error) {
				return 1, nil
			},
			Close: func(interface{}) error { return nil },
			Ping:  func(interface{}) error { return nil },
		})
		if err != nil {
			t.Fatal(err)
		}
		start := make(chan struct{})
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			<-start
			if _, err := p.Get(); err != nil && err != ErrClosed {
				t.Errorf("Get returned %v", err)
			}
		}()
		go func() {
			defer wg.Done()
			<-start
			p.Release()
		}()
		close(start)
		wg.Wait()
	}
}

func TestPoolGetFailedPingDuringRelease(t *testing.T) {
	for _, path := range []string{"idle", "waiting"} {
		t.Run(path, func(t *testing.T) {
			conn := new(int)
			pingStarted := make(chan struct{})
			finishPing := make(chan struct{})
			closed := make(chan interface{}, 2)
			config := &Config{
				MaxCap:  1,
				MaxIdle: 1,
				Factory: func() (interface{}, error) { return conn, nil },
				Close: func(conn interface{}) error {
					closed <- conn
					return nil
				},
				Ping: func(interface{}) error {
					close(pingStarted)
					<-finishPing
					return errors.New("connection is unhealthy")
				},
			}
			if path == "idle" {
				config.InitialCap = 1
			}
			p, err := NewChannelPool(config)
			if err != nil {
				t.Fatal(err)
			}
			defer p.Release()
			defer func() {
				select {
				case <-finishPing:
				default:
					close(finishPing)
				}
			}()
			if path == "waiting" {
				if _, err := p.Get(); err != nil {
					t.Fatal(err)
				}
			}
			type result struct {
				conn interface{}
				err  error
			}
			done := make(chan result, 1)
			go func() {
				conn, err := p.Get()
				done <- result{conn, err}
			}()
			if path == "waiting" {
				c := p.(*channelPool)
				deadline := time.Now().Add(time.Second)
				for {
					c.mu.RLock()
					waiting := len(c.connReqs)
					c.mu.RUnlock()
					if waiting == 1 {
						break
					}
					if time.Now().After(deadline) {
						t.Fatal("Get did not wait for a connection")
					}
					time.Sleep(time.Millisecond)
				}
				if err := p.Put(conn); err != nil {
					t.Fatal(err)
				}
			}
			select {
			case <-pingStarted:
			case <-time.After(time.Second):
				t.Fatal("Get did not start Ping")
			}
			p.Release()
			close(finishPing)
			select {
			case got := <-done:
				if got.conn != nil || got.err != ErrClosed {
					t.Fatalf("Get returned (%v, %v), want (nil, ErrClosed)", got.conn, got.err)
				}
			case <-time.After(time.Second):
				t.Fatal("Get did not finish after Release")
			}
			if len(closed) != 1 {
				t.Fatalf("Close called %d times, want once", len(closed))
			}
			if got := <-closed; got != conn {
				t.Errorf("Close received %v, want %v", got, conn)
			}
			p.Release()
			if len(closed) != 0 {
				t.Error("Release closed the connection again")
			}
		})
	}
}
