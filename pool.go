package pool

import (
	"errors"
	"time"
)

var (
	//ErrClosed 连接池已经关闭Error
	ErrClosed = errors.New("pool is closed")
)

//Pool 基本方法
type Pool interface {
	Get() (interface{}, time.Time, error)

	Put(interface{}, time.Time) error

	Close(interface{}) error

	Release()

	Len() int
}
