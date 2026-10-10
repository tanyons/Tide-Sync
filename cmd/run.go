package cmd

import (
	"context"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"Tide-Sync/internal/backup"
	"Tide-Sync/internal/config"
	"Tide-Sync/internal/lock"
	"Tide-Sync/internal/notify"
)

func newRunCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "run",
		Short: "执行全部任务（供 systemd timer 调用）",
		RunE: func(cmd *cobra.Command, _ []string) error {
			app, err := setup(cmd)
			if err != nil {
				return err
			}
			return runWithLock(cmd, app, app.Cfg.Jobs, false)
		},
	}
}

// lockPath 把状态文件同目录下的 state.lock 作为锁文件。
func lockPath(cfg *config.Config) string {
	return filepath.Join(filepath.Dir(cfg.Defaults.StateFile), "state.lock")
}

// runWithLock 加锁后执行任务列表，最后统一落盘状态。返回 errFailed 表示有失败。
func runWithLock(cmd *cobra.Command, app *App, jobs []config.Job, dryRun bool) error {
	lk, err := lock.Acquire(lockPath(app.Cfg))
	if err != nil {
		app.Log.Error("获取锁失败", "err", err)
		return err
	}
	defer lk.Release()

	ctx := ctxOf(cmd)
	return executeJobs(ctx, app, jobs, dryRun)
}

func executeJobs(ctx context.Context, app *App, jobs []config.Job, dryRun bool) error {
	st, err := backup.LoadState(app.Cfg.Defaults.StateFile)
	if err != nil {
		app.Log.Error("读取状态失败", "err", err)
		return err
	}
	deps := backup.Deps{Store: app.Store, Logger: app.Log}

	anyFail := false
	for _, job := range jobs {
		if dryRun {
			app.Log.Info("dry-run：将执行", "job", job.Name, "source", job.Source)
			continue
		}

		started := time.Now()
		res := backup.Run(ctx, app.Cfg, job, deps, st)

		status := "success"
		ev := notify.Event{
			Host: app.Host, Job: job.Name, Status: status,
			StartedAt: started, EndedAt: time.Now(),
			Scanned: res.Scanned, Uploaded: res.Uploaded, Failures: res.Failures,
			Bytes: res.Bytes,
		}
		if res.Err != nil {
			anyFail = true
			ev.Status = "failure"
			ev.Err = res.Err.Error()
		}
		sendNotify(ctx, app, ev)
	}

	if !dryRun {
		if err := st.Save(app.Cfg.Defaults.StateFile); err != nil {
			app.Log.Error("保存状态失败", "err", err)
			return err
		}
	}
	if anyFail {
		return errFailed
	}
	return nil
}

func sendNotify(ctx context.Context, app *App, ev notify.Event) {
	for _, n := range app.Notifiers {
		if err := n.Send(ctx, ev); err != nil {
			app.Log.Warn("通知发送失败", "channel", n.Name(), "err", err)
		}
	}
}

// ctxOf 返回命令上下文；cobra 未设置时回退到 Background。
func ctxOf(cmd *cobra.Command) context.Context {
	if ctx := cmd.Context(); ctx != nil {
		return ctx
	}
	return context.Background()
}
