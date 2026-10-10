package backup

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"testing"
	"time"

	"Tide-Sync/internal/config"
	"Tide-Sync/internal/store"
)

func TestParseDateDir(t *testing.T) {
	re := regexp.MustCompile(`^log-\d{4}-\d{2}-\d{2}$`)
	got, ok := ParseDateDir("log-2024-06-11", re)
	if !ok {
		t.Fatal("应识别为日期目录")
	}
	if got.Format("2006-01-02") != "2024-06-11" {
		t.Fatalf("日期 = %v", got)
	}
	if _, ok := ParseDateDir("notadate", re); ok {
		t.Fatal("非日期目录不应识别")
	}
}

func TestObjectKey(t *testing.T) {
	got := ObjectKey("tide-sync", "web-01", "appA", "log-2024-06-11/a.log")
	want := "tide-sync/web-01/appA/log-2024-06-11/a.log"
	if got != want {
		t.Fatalf("ObjectKey = %q", got)
	}
}

func TestScanExclude(t *testing.T) {
	dir := t.TempDir()
	must := func(rel, content string) {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	must("log-2024-06-10/a.log", "a")
	must("log-2024-06-10/skip.tmp", "b")
	must("log-2024-06-11/c.txt", "c")

	files, err := Scan(dir, ScanOptions{Exclude: []string{"**/*.tmp"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 {
		t.Fatalf("want 2 got %d: %+v", len(files), files)
	}
}

func newTestConfig(t *testing.T, src, mirror string) *config.Config {
	t.Helper()
	return &config.Config{
		Storage: config.Storage{Type: "local", LocalDir: mirror, Prefix: "tide-sync"},
		Defaults: config.Defaults{
			Host:                  "testhost",
			StateFile:             filepath.Join(t.TempDir(), "state.json"),
			Exclude:               []string{"**/*.tmp"},
			Concurrency:           2,
			Retry:                 1,
			SkipCompletedDateDirs: true,
			DateDirRegex:          `^log-\d{4}-\d{2}-\d{2}$`,
		},
		Jobs: []config.Job{{Name: "appA", Source: src}},
	}
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestRunIncrementalMirror(t *testing.T) {
	src := t.TempDir()
	mirror := t.TempDir()

	// 用“今天”的日期目录，保证不会被当作已完成目录跳过
	dateDir := "log-" + time.Now().Format("2006-01-02")
	base := filepath.Join(src, dateDir)
	if err := os.MkdirAll(base, 0o755); err != nil {
		t.Fatal(err)
	}
	f := filepath.Join(base, "a.log")
	if err := os.WriteFile(f, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := newTestConfig(t, src, mirror)
	b, err := store.New(cfg.Storage)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	deps := Deps{Store: b, Logger: discardLogger()}
	st := &State{Jobs: map[string]*JobState{}}

	res := Run(ctx, cfg, cfg.Jobs[0], deps, st)
	if res.Err != nil {
		t.Fatalf("首次运行: %v", res.Err)
	}
	if res.Uploaded != 1 {
		t.Fatalf("首次应上传 1，得到 %d", res.Uploaded)
	}

	// 再次运行：无变化 -> 不上传
	res = Run(ctx, cfg, cfg.Jobs[0], deps, st)
	if res.Uploaded != 0 {
		t.Fatalf("无变化时应上传 0，得到 %d", res.Uploaded)
	}

	// 修改文件 -> 重新上传
	if err := os.WriteFile(f, []byte("hello world"), 0o644); err != nil {
		t.Fatal(err)
	}
	res = Run(ctx, cfg, cfg.Jobs[0], deps, st)
	if res.Uploaded != 1 {
		t.Fatalf("修改后应上传 1，得到 %d", res.Uploaded)
	}

	// 桶内布局断言：<prefix>/<host>/<job>/<date>/a.log
	wantKey := filepath.Join(mirror, "tide-sync", "testhost", "appA", dateDir, "a.log")
	data, err := os.ReadFile(wantKey)
	if err != nil {
		t.Fatalf("读取镜像文件: %v", err)
	}
	if string(data) != "hello world" {
		t.Fatalf("镜像内容 = %q", data)
	}
}

func TestSkipCompletedDateDir(t *testing.T) {
	src := t.TempDir()
	mirror := t.TempDir()

	past := filepath.Join(src, "log-2000-01-01")
	if err := os.MkdirAll(past, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(past, "a.log"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := newTestConfig(t, src, mirror)
	b, _ := store.New(cfg.Storage)
	ctx := context.Background()
	deps := Deps{Store: b, Logger: discardLogger()}
	st := &State{Jobs: map[string]*JobState{}}

	if res := Run(ctx, cfg, cfg.Jobs[0], deps, st); res.Uploaded != 1 {
		t.Fatalf("首次应上传 1，得到 %d", res.Uploaded)
	}

	// 往“已完成”的旧日期目录塞新文件：应被跳过
	if err := os.WriteFile(filepath.Join(past, "b.log"), []byte("y"), 0o644); err != nil {
		t.Fatal(err)
	}
	if res := Run(ctx, cfg, cfg.Jobs[0], deps, st); res.Uploaded != 0 {
		t.Fatalf("已完成目录应被跳过，上传 %d", res.Uploaded)
	}
}

func TestStateRoundTrip(t *testing.T) {
	p := filepath.Join(t.TempDir(), "state.json")
	st := &State{Jobs: map[string]*JobState{}}
	st.Job("appA").Files["a.log"] = FileState{Size: 5, MTime: 123}
	if err := st.Save(p); err != nil {
		t.Fatal(err)
	}
	got, err := LoadState(p)
	if err != nil {
		t.Fatal(err)
	}
	if fs := got.Job("appA").Files["a.log"]; fs.Size != 5 || fs.MTime != 123 {
		t.Fatalf("往返结果 = %+v", fs)
	}
}
