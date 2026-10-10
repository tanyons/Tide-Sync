package backup

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path"
	"regexp"
	"sync"
	"sync/atomic"
	"time"

	"Tide-Sync/internal/config"
	"Tide-Sync/internal/store"
)

// Deps 是同步任务显式依赖。
type Deps struct {
	Store  store.Backend
	Logger *slog.Logger
}

// Result 是一次任务执行的结果。
type Result struct {
	Job      string
	Scanned  int64
	Uploaded int64
	Bytes    int64
	Failures int64
	Duration time.Duration
	Err      error
}

// ObjectKey 是全项目唯一拼接桶内对象 key 的地方：<prefix>/<host>/<job>/<rel>
func ObjectKey(prefix, host, job, rel string) string {
	return path.Join(prefix, host, job, rel)
}

// Run 执行单个任务：扫描 -> 增量比对 -> 上传 -> 更新状态（内存）。状态由调用方统一落盘。
func Run(ctx context.Context, cfg *config.Config, job config.Job, d Deps, st *State) Result {
	start := time.Now()
	res := Result{Job: job.Name}
	log := d.Logger.With("job", job.Name)

	info, err := os.Stat(job.Source)
	if err != nil {
		res.Err = fmt.Errorf("源目录不可用: %w", err)
		return res
	}
	if !info.IsDir() {
		res.Err = fmt.Errorf("source 不是目录: %s", job.Source)
		return res
	}

	host, err := cfg.Host()
	if err != nil {
		res.Err = err
		return res
	}

	dateRe, err := regexp.Compile(cfg.Defaults.DateDirRegex)
	if err != nil {
		res.Err = fmt.Errorf("date_dir_regex 非法: %w", err)
		return res
	}

	js := st.Job(job.Name)
	now := time.Now()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())

	done := make(map[string]bool, len(js.DoneDateDirs))
	for _, n := range js.DoneDateDirs {
		done[n] = true
	}

	exclude := append(append([]string{}, cfg.Defaults.Exclude...), job.Exclude...)

	files, err := Scan(job.Source, ScanOptions{
		Exclude:        exclude,
		FollowSymlinks: cfg.Defaults.FollowSymlinks,
		SkipDone:       cfg.Defaults.SkipCompletedDateDirs,
		DoneDateDirs:   done,
	})
	if err != nil {
		res.Err = fmt.Errorf("扫描失败: %w", err)
		return res
	}
	res.Scanned = int64(len(files))

	// 只保留新增/变化的文件
	var pending []File
	for _, f := range files {
		if prev, ok := js.Files[f.Rel]; ok && unchanged(prev, f) {
			continue
		}
		pending = append(pending, f)
	}

	concurrency := cfg.Defaults.Concurrency
	if concurrency < 1 {
		concurrency = 1
	}

	var uploaded, bytesUp, failures int64
	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, concurrency)

	for _, f := range pending {
		f := f
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			key := ObjectKey(cfg.Storage.Prefix, host, job.Name, f.Rel)
			uerr := uploadWithRetry(ctx, d.Store, key, f.Abs, cfg.Defaults.Retry, cfg.Defaults.RetryBackoffSeconds)
			if uerr == nil {
				// 上传后复检：文件在上传期间是否又被改写
				if fi, serr := os.Stat(f.Abs); serr == nil {
					if fi.Size() != f.Size || fi.ModTime().UnixNano() != f.MTime.UnixNano() {
						uerr = fmt.Errorf("上传期间文件被改写")
					}
				}
			}
			if uerr != nil {
				log.Warn("上传失败", "file", f.Rel, "err", uerr)
				atomic.AddInt64(&failures, 1)
				return
			}

			mu.Lock()
			js.Files[f.Rel] = FileState{Size: f.Size, MTime: f.MTime.UnixNano()}
			mu.Unlock()

			atomic.AddInt64(&uploaded, 1)
			atomic.AddInt64(&bytesUp, f.Size)
			log.Debug("已上传", "file", f.Rel, "size", f.Size)
		}()
	}
	wg.Wait()

	res.Uploaded = uploaded
	res.Bytes = bytesUp
	res.Failures = failures

	js.LastRun = time.Now()
	if cfg.Defaults.SkipCompletedDateDirs {
		markDoneDateDirs(job.Source, dateRe, today, js)
	}

	res.Duration = time.Since(start)
	if failures > 0 {
		res.Err = fmt.Errorf("%d 个文件上传失败", failures)
	}
	log.Info("任务完成",
		"scanned", res.Scanned, "uploaded", res.Uploaded,
		"bytes", res.Bytes, "failures", res.Failures, "duration", res.Duration.String())
	return res
}

// unchanged 是全项目唯一判断“文件是否未变”的地方。
func unchanged(prev FileState, f File) bool {
	return prev.Size == f.Size && prev.MTime == f.MTime.UnixNano()
}

func uploadWithRetry(ctx context.Context, b store.Backend, key, localPath string, retry, backoffSeconds int) error {
	var err error
	for i := 0; i <= retry; i++ {
		if i > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(time.Duration(i*backoffSeconds) * time.Second):
			}
		}
		if err = b.Put(ctx, key, localPath); err == nil {
			return nil
		}
	}
	return err
}

// markDoneDateDirs 根据当前目录列表，重建“已完成日期目录”集合（只含早于今天的）。
func markDoneDateDirs(root string, re *regexp.Regexp, today time.Time, js *JobState) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return
	}
	seen := map[string]bool{}
	var done []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		t, ok := ParseDateDir(e.Name(), re)
		if !ok || !t.Before(today) {
			continue
		}
		if !seen[e.Name()] {
			seen[e.Name()] = true
			done = append(done, e.Name())
		}
	}
	js.DoneDateDirs = done
}
