package restore

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"Tide-Sync/internal/config"
	"Tide-Sync/internal/store"
)

func put(t *testing.T, root, key, content string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(key))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestRunRestoresSubtreeContents(t *testing.T) {
	root := t.TempDir()
	b, err := store.New(config.Storage{Type: "local", LocalDir: root})
	if err != nil {
		t.Fatal(err)
	}

	// 目标子树
	put(t, root, "tide-sync/web-01/opt/backup/appA/log-2024-06-01/a.log", "hi")
	put(t, root, "tide-sync/web-01/opt/backup/appA/log-2024-06-01/sub/b.txt", "yo")
	// 另一个子树，不应被恢复；同主机下也不应串
	put(t, root, "tide-sync/web-01/srv/data/x.txt", "no")
	// 另一台主机，不应被恢复
	put(t, root, "tide-sync/web-02/opt/backup/appA/log-2024-06-01/c.log", "no")

	to := t.TempDir()
	n, err := Run(context.Background(), b, Options{
		Prefix: "tide-sync", Host: "web-01", Path: "opt/backup/appA", To: to,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if n != 2 {
		t.Fatalf("恢复文件数 = %d，期望 2", n)
	}

	// 语义 A：子树内容写入 --to（剥掉 --path 前缀）
	for _, rel := range []string{"log-2024-06-01/a.log", "log-2024-06-01/sub/b.txt"} {
		if _, err := os.Stat(filepath.Join(to, filepath.FromSlash(rel))); err != nil {
			t.Errorf("缺少恢复文件 %s: %v", rel, err)
		}
	}
	if _, err := os.Stat(filepath.Join(to, "srv")); err == nil {
		t.Error("不应恢复其它子树")
	}
}

func TestRunFilter(t *testing.T) {
	root := t.TempDir()
	b, _ := store.New(config.Storage{Type: "local", LocalDir: root})
	put(t, root, "tide-sync/h1/opt/app/log-1/a.log", "1")
	put(t, root, "tide-sync/h1/opt/app/log-1/b.txt", "2")

	to := t.TempDir()
	n, err := Run(context.Background(), b, Options{
		Prefix: "tide-sync", Host: "h1", Path: "opt/app", Filter: "**/*.txt", To: to,
	})
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("过滤后恢复数 = %d，期望 1", n)
	}
}

func TestIsRootPath(t *testing.T) {
	if !IsRootPath("/") {
		t.Fatal("/ 应判为根目录")
	}
	if IsRootPath(filepath.Join(t.TempDir(), "out")) {
		t.Fatal("普通目录不应判为根目录")
	}
}
