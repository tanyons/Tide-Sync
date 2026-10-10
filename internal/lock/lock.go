// Package lock 提供单机互斥锁，防止同一台机器上重复执行。
package lock

import (
	"fmt"

	"github.com/gofrs/flock"
)

// Lock 是一个已持有的文件锁。
type Lock struct {
	fl *flock.Flock
}

// Acquire 尝试获取排他锁；已被占用则返回错误。
func Acquire(path string) (*Lock, error) {
	fl := flock.New(path)
	ok, err := fl.TryLock()
	if err != nil {
		return nil, fmt.Errorf("获取锁失败: %w", err)
	}
	if !ok {
		return nil, fmt.Errorf("已有实例在运行（锁: %s）", path)
	}
	return &Lock{fl: fl}, nil
}

// Release 释放锁。
func (l *Lock) Release() {
	if l != nil && l.fl != nil {
		_ = l.fl.Unlock()
	}
}
