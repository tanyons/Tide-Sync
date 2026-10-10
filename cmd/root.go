// Package cmd 是接口层：解析参数、装配依赖、调用下层、决定退出码。
// 不含业务逻辑。
package cmd

import (
	"errors"
	"log/slog"
	"path/filepath"

	"github.com/spf13/cobra"

	"Tide-Sync/internal/config"
	"Tide-Sync/internal/logx"
	"Tide-Sync/internal/notify"
	"Tide-Sync/internal/store"
)

// Version 是程序版本号。
const Version = "0.1.0"

// DefaultConfigPath 是默认配置文件路径。
const DefaultConfigPath = "/opt/tide-sync/config.toml"

// errFailed 用于让命令以非零退出码结束（供 systemd 判定失败）。
var errFailed = errors.New("有任务失败")

// App 汇总已装配好的依赖，显式传给各子命令。
type App struct {
	ConfigPath string
	Cfg        *config.Config
	Log        *slog.Logger
	Store      store.Backend
	Notifiers  []notify.Notifier
	Host       string
}

// Execute 构建命令树并运行。
func Execute() error {
	root := &cobra.Command{
		Use:           "tide",
		Short:         "Tide-Sync：按文件增量把备份目录同步到 OSS",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.PersistentFlags().StringP("config", "c", DefaultConfigPath, "配置文件路径")
	root.PersistentFlags().StringP("env", "e", "", "环境变量文件路径（默认 <配置目录>/.env）")
	root.PersistentFlags().BoolP("verbose", "v", false, "输出 debug 日志")

	root.AddCommand(
		newRunCmd(),
		newBackupCmd(),
		newRestoreCmd(),
		newLifecycleCmd(),
		newConfigCmd(),
		newVersionCmd(),
	)
	return root.Execute()
}

// loadConfig 是所有命令读配置的唯一入口：先加载 .env，再读配置。
// 必须走这里，配置里的 ${ENV} 才拿得到 .env 里的敏感信息。
func loadConfig(cmd *cobra.Command) (*config.Config, error) {
	cfgPath, _ := cmd.Flags().GetString("config")

	envPath, _ := cmd.Flags().GetString("env")
	if envPath == "" {
		envPath = filepath.Join(filepath.Dir(cfgPath), ".env")
	}
	if err := config.LoadDotEnv(envPath); err != nil {
		return nil, err
	}
	return config.Load(cfgPath)
}

// setup 显式装配依赖：loadConfig -> 建 logger、store、notifier。
func setup(cmd *cobra.Command) (*App, error) {
	cfgPath, _ := cmd.Flags().GetString("config")
	verbose, _ := cmd.Flags().GetBool("verbose")

	cfg, err := loadConfig(cmd)
	if err != nil {
		return nil, err
	}
	if verbose {
		cfg.Logging.Level = "debug"
	}
	log := logx.New(cfg.Logging)

	host, err := cfg.Host()
	if err != nil {
		return nil, err
	}
	st, err := store.New(cfg.Storage)
	if err != nil {
		return nil, err
	}

	return &App{
		ConfigPath: cfgPath,
		Cfg:        cfg,
		Log:        log,
		Store:      st,
		Notifiers:  notify.New(cfg.Notify, host),
		Host:       host,
	}, nil
}
