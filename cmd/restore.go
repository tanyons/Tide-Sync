package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"Tide-Sync/internal/restore"
)

func newRestoreCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "restore",
		Short: "把桶内某个子树恢复到本地目录（目录对目录）",
		RunE: func(cmd *cobra.Command, _ []string) error {
			app, err := setup(cmd)
			if err != nil {
				return err
			}
			host, _ := cmd.Flags().GetString("host")
			sub, _ := cmd.Flags().GetString("path")
			filter, _ := cmd.Flags().GetString("filter")
			to, _ := cmd.Flags().GetString("to")
			allowRoot, _ := cmd.Flags().GetBool("allow-root")

			if host == "" {
				host = app.Host
			}
			if to == "" {
				return fmt.Errorf("必须指定 --to")
			}
			if restore.IsRootPath(to) && !allowRoot {
				return fmt.Errorf("拒绝直接恢复到根目录 %q（危险）；确需如此请加 --allow-root", to)
			}

			n, err := restore.Run(ctxOf(cmd), app.Store, restore.Options{
				Prefix: app.Cfg.Storage.Prefix,
				Host:   host,
				Path:   sub,
				Filter: filter,
				To:     to,
			})
			if err != nil {
				return err
			}
			app.Log.Info("恢复完成", "files", n, "host", host, "path", sub, "to", to)
			return nil
		},
	}
	cmd.Flags().String("host", "", "主机标识（默认本机 hostname）")
	cmd.Flags().String("path", "", "要恢复的桶内子树，如 opt/backup/appA/log-2024-06-11（默认整个主机）")
	cmd.Flags().String("filter", "", "doublestar 过滤相对路径，如 **/*.conf")
	cmd.Flags().String("to", "", "目标目录（必填）：子树内容写入此处")
	cmd.Flags().Bool("allow-root", false, "允许恢复到文件系统根目录（危险）")
	return cmd
}
