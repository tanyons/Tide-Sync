package backup

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"Tide-Sync/internal/config"
	"Tide-Sync/internal/store"
)

func TestSourcePrefix(t *testing.T) {
	cases := map[string]string{
		"/opt/backup":       "opt/backup",
		"/opt/backup/":      "opt/backup",
		"/opt/backup/appA/": "opt/backup/appA",
	}
	for in, want := range cases {
		if got := SourcePrefix(in); got != filepath.ToSlash(want) {
			t.Errorf("SourcePrefix(%q) = %q，期望 %q", in, got, want)
		}
	}
	// Windows 盘符不应出现在桶内路径里
	if got := SourcePrefix(filepath.Join(t.TempDir(), "x")); strings.Contains(got, ":") {
		t.Errorf("SourcePrefix 不应含冒号: %q", got)
	}
}

func TestObjectKey(t *testing.T) {
	got := ObjectKey("tide-sync", "web-01", "/opt/backup", "appA/log-2024-06-11/a.log")
	want := "tide-sync/web-01/opt/backup/appA/log-2024-06-11/a.log"
	if got != want {
		t.Fatalf("ObjectKey = %q，期望 %q", got, want)
	}
}

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

func newTestConfig(mirror string, sources ...config.Source) *config.Config {
	return &config.Config{
		Storage: config.Storage{Type: "local", LocalDir: mirror, Prefix: "tide-sync"},
		Defaults: config.Defaults{
			Host:                  "testhost",
			StateFile:             filepath.Join(mirror, "state.json"),
			Exclude:               []string{"**/*.tmp"},
			Concurrency:           2,
			Retry:                 1,
			RetryBackoffSeconds:   1,
			SkipCompletedDateDirs: true,
			DateDirRegex:          `^log-\d{4}-\d{2}-\d{2}$`,
		},
		Sources: sources,
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

	cfg := newTestConfig(mirror, config.Source{Path: src})
	b, err := store.New(cfg.Storage)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	deps := Deps{Store: b, Logger: discardLogger()}
	st := &State{Sources: map[string]*SourceState{}}

	if res := Run(ctx, cfg, cfg.Sources[0], deps, st); res.Uploaded != 1 {
		t.Fatalf("首次应上传 1，得到 %d（err=%v）", res.Uploaded, res.Err)
	}
	if res := Run(ctx, cfg, cfg.Sources[0], deps, st); res.Uploaded != 0 {
		t.Fatalf("无变化时应上传 0，得到 %d", res.Uploaded)
	}

	if err := os.WriteFile(f, []byte("hello world"), 0o644); err != nil {
		t.Fatal(err)
	}
	if res := Run(ctx, cfg, cfg.Sources[0], deps, st); res.Uploaded != 1 {
		t.Fatalf("修改后应上传 1，得到 %d", res.Uploaded)
	}

	// 桶内布局：<prefix>/<host>/<源绝对路径>/<日期>/a.log
	wantKey := filepath.Join(mirror, "tide-sync", "testhost",
		filepath.FromSlash(SourcePrefix(src)), dateDir, "a.log")
	data, err := os.ReadFile(wantKey)
	if err != nil {
		t.Fatalf("读取镜像文件 %s: %v", wantKey, err)
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

	cfg := newTestConfig(mirror, config.Source{Path: src})
	b, _ := store.New(cfg.Storage)
	ctx := context.Background()
	deps := Deps{Store: b, Logger: discardLogger()}
	st := &State{Sources: map[string]*SourceState{}}

	if res := Run(ctx, cfg, cfg.Sources[0], deps, st); res.Uploaded != 1 {
		t.Fatalf("首次应上传 1，得到 %d", res.Uploaded)
	}

	// 往“已完成”的旧日期目录塞新文件：应被跳过
	if err := os.WriteFile(filepath.Join(past, "b.log"), []byte("y"), 0o644); err != nil {
		t.Fatal(err)
	}
	if res := Run(ctx, cfg, cfg.Sources[0], deps, st); res.Uploaded != 0 {
		t.Fatalf("已完成目录应被跳过，上传 %d", res.Uploaded)
	}
}

func TestStateRoundTrip(t *testing.T) {
	p := filepath.Join(t.TempDir(), "state.json")
	st := &State{Sources: map[string]*SourceState{}}
	st.Source("opt/backup").Files["a.log"] = FileState{Size: 5, MTime: 123}
	if err := st.Save(p); err != nil {
		t.Fatal(err)
	}
	got, err := LoadState(p)
	if err != nil {
		t.Fatal(err)
	}
	if fs := got.Source("opt/backup").Files["a.log"]; fs.Size != 5 || fs.MTime != 123 {
		t.Fatalf("往返结果 = %+v", fs)
	}
}
