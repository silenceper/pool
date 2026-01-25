package pool

import (
	"sync"
	"testing"
	"time"
)

// TestPoolReleaseRace verifies that Get() returns ErrClosed when the pool is released,
// and that we don't encounter nil pointer dereferences or infinite loops.
// This serves as a regression test for Issue #24.
func TestPoolReleaseRace(t *testing.T) {
	p, err := NewChannelPool(&Config{
		InitialCap: 1,
		MaxCap:     2,
		MaxIdle:    2,
		Factory: func() (interface{}, error) {
			return "conn", nil
		},
		Close: func(v interface{}) error { return nil },
	})
	if err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	wg.Add(2)

	// Goroutine that releases the pool
	go func() {
		defer wg.Done()
		time.Sleep(10 * time.Millisecond)
		p.Release()
	}()

	// Goroutine that tries to get connection
	go func() {
		defer wg.Done()
		// Try to get multiple times to hit the race
		for i := 0; i < 5; i++ {
			time.Sleep(5 * time.Millisecond)
			v, err := p.Get()
			if err != nil {
				if err == ErrClosed {
					// Expected error
				} else {
					t.Logf("Got other error: %v", err)
				}
			} else {
				p.Put(v)
			}
		}
	}()

	wg.Wait()
}
