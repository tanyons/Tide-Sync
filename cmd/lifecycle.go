package cmd

import (
	"os"

	"github.com/spf13/cobra"

	"Tide-Sync/internal/lifecycle"
)

func newLifecycleCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "lifecycle",
		Short: "生成（默认）或下发（--apply）OSS 生命周期规则",
		RunE: func(cmd *cobra.Command, _ []string) error {
			app, err := setup(cmd)
			if err != nil {
				return err
			}
			apply, _ := cmd.Flags().GetBool("apply")
			days, _ := cmd.Flags().GetInt("days")
			abortDays, _ := cmd.Flags().GetInt("abort-days")

			// 0 表示未指定，取配置里的值。
			if days <= 0 {
				days = app.Cfg.Lifecycle.ExpireDays
			}
			if abortDays <= 0 {
				abortDays = app.Cfg.Lifecycle.AbortDays
			}

			if !apply {
				lifecycle.Print(os.Stdout, app.Cfg, days, abortDays)
				return nil
			}
			if err := lifecycle.Apply(app.Cfg, days, abortDays); err != nil {
				return err
			}
			app.Log.Info("生命周期规则已下发",
				"id", lifecycle.RuleID(days), "expire_days", days, "abort_days", abortDays)
			return nil
		},
	}
	cmd.Flags().Bool("apply", false, "直接下发规则到桶（默认只打印）")
	cmd.Flags().Int("days", 0, "保留天数（默认取配置 lifecycle.expire_days）")
	cmd.Flags().Int("abort-days", 0, "未完成分片中止天数（默认取配置 lifecycle.abort_days）")
	return cmd
}
