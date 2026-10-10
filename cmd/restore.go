package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"Tide-Sync/internal/restore"
)

func newRestoreCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "restore",
		Short: "从 OSS 恢复指定主机/任务的文件",
		RunE: func(cmd *cobra.Command, _ []string) error {
			app, err := setup(cmd)
			if err != nil {
				return err
			}
			host, _ := cmd.Flags().GetString("host")
			job, _ := cmd.Flags().GetString("job")
			dir, _ := cmd.Flags().GetString("dir")
			filter, _ := cmd.Flags().GetString("filter")
			to, _ := cmd.Flags().GetString("to")

			if host == "" {
				host = app.Host
			}
			if job == "" {
				return fmt.Errorf("必须指定 --job")
			}
			if to == "" {
				return fmt.Errorf("必须指定 --to")
			}

			n, err := restore.Run(ctxOf(cmd), app.Store, restore.Options{
				Prefix: app.Cfg.Storage.Prefix,
				Host:   host,
				Job:    job,
				Dir:    dir,
				Filter: filter,
				To:     to,
			})
			if err != nil {
				return err
			}
			app.Log.Info("恢复完成", "files", n, "host", host, "job", job, "to", to)
			return nil
		},
	}
	cmd.Flags().String("host", "", "主机标识（默认本机 hostname）")
	cmd.Flags().String("job", "", "任务名（必填）")
	cmd.Flags().String("dir", "", "只恢复某个顶层目录，如 log-2024-06-11")
	cmd.Flags().String("filter", "", "doublestar 过滤相对路径，如 **/*.conf")
	cmd.Flags().String("to", "", "目标目录（必填）")
	return cmd
}
