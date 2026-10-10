// Package cmd 是接口层：解析参数、装配依赖、调用下层、决定退出码。
// 不含业务逻辑。
package cmd

import (
	"errors"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"Tide-Sync/internal/config"
	"Tide-Sync/internal/logx"
	"Tide-Sync/internal/notify"
	"Tide-Sync/internal/store"
)

// Version 是程序版本号。
const Version = "0.1.0"

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
	root.PersistentFlags().StringP("config", "c", "",
		"配置文件路径（默认：./config.toml，其次可执行文件同目录的 config.toml）")
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

// resolveConfigPath 决定用哪个配置文件，与安装目录无关：
// 1) -c/--config 显式指定；2) 当前工作目录的 config.toml；3) 可执行文件同目录的 config.toml。
func resolveConfigPath(cmd *cobra.Command) (string, error) {
	if p, _ := cmd.Flags().GetString("config"); p != "" {
		return p, nil
	}
	if _, err := os.Stat("config.toml"); err == nil {
		return "config.toml", nil
	}
	if exe, err := os.Executable(); err == nil {
		p := filepath.Join(filepath.Dir(exe), "config.toml")
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}
	return "", errors.New("找不到 config.toml：请用 -c 指定，或把 config.toml 放在当前目录 / 可执行文件同目录")
}

// loadConfig 是所有命令读配置的唯一入口：先加载 .env，再读配置。
// 返回配置与它最终使用的路径（供日志与状态路径使用）。
func loadConfig(cmd *cobra.Command) (*config.Config, string, error) {
	cfgPath, err := resolveConfigPath(cmd)
	if err != nil {
		return nil, "", err
	}

	envPath, _ := cmd.Flags().GetString("env")
	if envPath == "" {
		envPath = filepath.Join(filepath.Dir(cfgPath), ".env")
	}
	if err := config.LoadDotEnv(envPath); err != nil {
		return nil, "", err
	}

	cfg, err := config.Load(cfgPath)
	if err != nil {
		return nil, "", err
	}
	return cfg, cfgPath, nil
}

// setup 显式装配依赖：loadConfig -> 建 logger、store、notifier。
func setup(cmd *cobra.Command) (*App, error) {
	verbose, _ := cmd.Flags().GetBool("verbose")

	cfg, cfgPath, err := loadConfig(cmd)
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
