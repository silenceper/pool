package pool

import (
	"testing"
	"time"
)

// TestNilConnInChannel verifies that Get() skips nil connections in the channel
// and continues to fetch a valid connection or create a new one.
// This specifically reproduces Issue #24 where a nil value would cause ErrClosed.
func TestNilConnInChannel(t *testing.T) {
	// Create a pool with 1 initial connection
	p, err := NewChannelPool(&Config{
		InitialCap: 1,
		MaxCap:     2,
		MaxIdle:    2,
		Factory: func() (interface{}, error) {
			return "valid-conn", nil
		},
		Close: func(v interface{}) error { return nil },
		IdleTimeout: 10 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer p.Release()

	// Access the private structure to inject a nil connection
	cp, ok := p.(*channelPool)
	if !ok {
		t.Fatal("Pool is not a channelPool")
	}

	// 1. Calculate how many items are effectively in the channel (InitialCap=1)
	// We want to inject a nil.
	// Since InitialCap is 1, there is already 1 item in cp.conns.
	// Let's inject a nil pointer into the channel.
	// The channel buffer is MaxIdle=2.
	
	// Inject a nil *idleConn
	cp.conns <- nil

	// Now the channel has: [validConn, nil] (order depends, but likely validConn first if not consumed)
	// Let's consume the valid one first if present.
	v1, err := p.Get()
	if err != nil {
		t.Fatalf("First Get() failed: %v", err)
	}
	if v1 != "valid-conn" {
		t.Fatalf("First Get() returned unexpected value: %v", v1)
	}

	// Now the next item in channel should be our injected nil.
	// The fix should cause it to skip this nil and either:
	// a) Wait (if no more conns), or
	// b) Create a new one (since openingConns might allow it, or if it loops)
	
	// Wait, if we consume the first one, openingConns is still 1 (we just took it out).
	// If we return it, it goes back.
	
	// Let's Put it back to be safe, so pool has capacity? 
	// No, let's keep it out. openingConns is 1. MaxCap is 2.
	// So Get() usually tries to read from channel.
	// It will read the 'nil' we injected.
	// It should skip it ("continue").
	// Then it hits 'default':
	// It sees openingConns (1) < maxActive (2).
	// It should create a NEW connection.
	
	v2, err := p.Get()
	if err != nil {
		t.Fatalf("Second Get() (expecting to skip nil) failed: %v", err)
	}
	
	if v2 != "valid-conn" {
		t.Fatalf("Second Get() returned unexpected value: %v", v2)
	}
	
	t.Logf("p.Get() returned: conn=%v, err=%v", v2, err)
}

// TestFactoryReturnsNilConn verifies behavior when Factory returns (nil, nil).
// We expect the pool to reject this configuration or return an error,
// ensuring no nil connections enter the system.
func TestFactoryReturnsNilConn(t *testing.T) {
	// Case 1: InitialCap > 0. NewChannelPool should fail if factory returns nil.
	t.Run("InitialCap > 0", func(t *testing.T) {
		_, err := NewChannelPool(&Config{
			InitialCap: 1,
			MaxCap:     2,
			MaxIdle:    2,
			Factory: func() (interface{}, error) {
				return nil, nil
			},
			Close:       func(v interface{}) error { return nil },
			IdleTimeout: 10 * time.Second,
		})
		if err == nil {
			t.Error("Expected error from NewChannelPool when factory returns nil, got nil")
		} else {
			t.Logf("NewChannelPool correctly returned error: %v", err)
		}
	})

	// Case 2: InitialCap = 0. NewChannelPool succeeds. Get() should fail if factory returns nil.
	t.Run("InitialCap = 0", func(t *testing.T) {
		p, err := NewChannelPool(&Config{
			InitialCap: 0,
			MaxCap:     2,
			MaxIdle:    2,
			Factory: func() (interface{}, error) {
				return nil, nil
			},
			Close:       func(v interface{}) error { return nil },
			IdleTimeout: 10 * time.Second,
		})
		if err != nil {
			t.Fatal(err)
		}
		defer p.Release()

		// Get() triggers factory call
		v, err := p.Get()
		if err == nil {
			t.Error("Expected error from Get() when factory returns nil, got nil error")
		} else {
			t.Logf("Get() correctly returned error: %v", err)
		}
		if v != nil {
			t.Errorf("Expected nil connection from Get(), got %v", v)
		}
	})
}
