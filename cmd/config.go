package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newConfigCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "config",
		Short: "校验并打印生效配置（脱敏）",
		RunE: func(cmd *cobra.Command, _ []string) error {
			// 走统一的 loadConfig，保证 .env 里的 ${ENV} 也被展开。
			cfg, cfgPath, err := loadConfig(cmd)
			if err != nil {
				return err
			}
			fmt.Printf("配置文件: %s\n", cfgPath)
			fmt.Println("配置校验通过：")
			fmt.Print(cfg.Redacted())
			return nil
		},
	}
}
