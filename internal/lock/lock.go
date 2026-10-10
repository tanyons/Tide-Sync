// Package lock 提供单机互斥锁，防止同一台机器上重复执行。
package lock

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/gofrs/flock"
)

// Acquire 获取排他锁并返回释放函数；已被占用则返回错误。
func Acquire(path string) (func(), error) {
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("创建锁目录失败: %w", err)
		}
	}
	fl := flock.New(path)
	ok, err := fl.TryLock()
	if err != nil {
		return nil, fmt.Errorf("获取锁失败: %w", err)
	}
	if !ok {
		return nil, fmt.Errorf("已有实例在运行（锁: %s）", path)
	}
	return func() { _ = fl.Unlock() }, nil
}
