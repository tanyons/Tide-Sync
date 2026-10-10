package lock

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAcquireCreatesParentDir(t *testing.T) {
	p := filepath.Join(t.TempDir(), "nested", "deep", "state.lock")

	lk, err := Acquire(p)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	lk.Release()

	if _, err := os.Stat(filepath.Dir(p)); err != nil {
		t.Fatalf("父目录未自动创建: %v", err)
	}
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("锁文件未创建: %v", err)
	}
}
