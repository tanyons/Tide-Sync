package cmd

import (
	"fmt"
	"path/filepath"

	"github.com/spf13/cobra"

	"Tide-Sync/internal/config"
)

func newBackupCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "backup",
		Short: "强制同步指定源目录（--source），缺省则全部",
		RunE: func(cmd *cobra.Command, _ []string) error {
			app, err := setup(cmd)
			if err != nil {
				return err
			}
			srcPath, _ := cmd.Flags().GetString("source")
			dryRun, _ := cmd.Flags().GetBool("dry-run")

			sources, err := selectSources(app.Cfg.Sources, srcPath)
			if err != nil {
				return err
			}
			return runWithLock(cmd, app, sources, dryRun)
		},
	}
	cmd.Flags().String("source", "", "只同步指定源目录（默认全部）")
	cmd.Flags().Bool("dry-run", false, "只列出将要同步的源目录，不实际上传")
	return cmd
}

func selectSources(all []config.Source, path string) ([]config.Source, error) {
	if path == "" {
		return all, nil
	}
	want := filepath.Clean(path)
	for _, s := range all {
		if filepath.Clean(s.Path) == want {
			return []config.Source{s}, nil
		}
	}
	return nil, fmt.Errorf("未配置该源目录: %q", path)
}
