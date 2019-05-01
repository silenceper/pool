package pool

import "errors"

var (
	//ErrClosed 连接池已经关闭Error
	ErrClosed = errors.New("pool is closed")
	//ErrTimeoutClosed 連結因timeout關閉
	ErrTimeoutClosed = errors.New("connection is closed by timeout")
)

// Pool 基本方法
type Pool interface {
	Get() (interface{}, error)

	Put(interface{}) error

	Close(interface{}) error

	Release()

	Len() int
}
