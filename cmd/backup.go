package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"Tide-Sync/internal/config"
)

func newBackupCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "backup",
		Short: "强制执行指定任务（--job），缺省则全部",
		RunE: func(cmd *cobra.Command, _ []string) error {
			app, err := setup(cmd)
			if err != nil {
				return err
			}
			jobName, _ := cmd.Flags().GetString("job")
			dryRun, _ := cmd.Flags().GetBool("dry-run")

			jobs, err := selectJobs(app.Cfg.Jobs, jobName)
			if err != nil {
				return err
			}
			return runWithLock(cmd, app, jobs, dryRun)
		},
	}
	cmd.Flags().String("job", "", "只执行指定任务（默认全部）")
	cmd.Flags().Bool("dry-run", false, "只列出将要执行的任务，不实际上传")
	return cmd
}

func selectJobs(all []config.Job, name string) ([]config.Job, error) {
	if name == "" {
		return all, nil
	}
	for _, j := range all {
		if j.Name == name {
			return []config.Job{j}, nil
		}
	}
	return nil, fmt.Errorf("未找到任务: %q", name)
}
