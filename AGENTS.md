# AGENTS.md

面向**开发者与 AI agent** 的工程指南。用户/运维文档见 [README.md](README.md)。

## 项目定位

Go 编写的单二进制命令行工具：把服务器本地备份目录**增量镜像同步**到对象存储。
一次性运行（`tide run`），由 systemd timer 触发，跑完退出；桶内保留交给 OSS 生命周期。
**可安装在任意目录**，不依赖固定路径。

## 技术栈与依赖

- Go 1.27；模块名 `Tide-Sync`（import 路径 `Tide-Sync/...`）。
- `spf13/cobra`（CLI）、`knadh/koanf/v2`(+`providers/rawbytes`,`parsers/toml`)（配置）、
  `aliyun/aliyun-oss-go-sdk/oss`（存储）、`bmatcuk/doublestar/v4`（`**` 通配）、
  `gofrs/flock`（运行锁）、`gopkg.in/natefinch/lumberjack.v2`（日志滚动）。
- 标准库 `log/slog` 做日志；`.env` 由 `internal/config/dotenv.go` 自实现解析（无额外依赖）。
- **无 cron 依赖**（调度交给 timer）。

## 分层与目录（**单向依赖，只往下**）

```
main.go
cmd/                      接口层：解析参数、装配依赖、调用下层、决定退出码
internal/
  backup/                 用例：编排 scan -> diff -> upload -> state
  restore/                用例：按 host/job/[dir]/[filter] 拉取
  lifecycle/              用例：生成/下发 OSS 生命周期规则
  store/                  构件：Backend 接口 + local.go + oss.go
  config/                 构件：koanf(TOML) 加载 + .env + ${ENV} 展开 + 路径解析 + 校验 + 脱敏
  logx/                   构件：slog 构建
  notify/                 构件：Notifier 接口 + feishu.go + dingtalk
  lock/                   构件：flock 封装
```

| 层 | 只做 | 绝不做 |
|---|---|---|
| `cmd` | 解析参数、调用下层、打日志、定退出码 | 业务逻辑、文件遍历、OSS 调用 |
| `backup/restore/lifecycle` | 编排流程 | 读 flag、直接建 client |
| `store/config/logx/notify/lock` | 单一职责 | 编排、跨层调用 |
| `main` | 建对象、传参 | 业务逻辑 |

## 代码约定（**显式优先**）

1. **只有两个接口**：`store.Backend`（多后端）与 `notify.Notifier`（多渠道，且为测试留缝）。
   其余一律用具体类型 / 普通函数，**不预先抽象**。
2. **固定问题锁定固定位置**（一处实现、一处修改）：

   | 固定问题 | 锁定位置 |
   |---|---|
   | 对象 key 拼接 | `backup.ObjectKey` |
   | 排除规则匹配 | `backup.shouldExclude` |
   | 日期目录识别 | `backup.ParseDateDir` |
   | 增量判断（size+mtime） | `backup.unchanged` |
   | 状态读写 | `backup.LoadState` / `State.Save` |
   | 生命周期 XML/下发 | `internal/lifecycle` |
   | 内网 endpoint 转换 | `store.InternalEndpoint` |
   | `.env` 解析 | `config.LoadDotEnv` |
   | `${ENV}` 展开 | `config.expandEnv` |
   | 配置路径发现 | `cmd.resolveConfigPath` |
   | 相对路径解析（相对配置目录） | `config.resolvePaths` |

3. **显式装配**：`cmd.loadConfig()` 先 `.env` 再 `config.Load`；`cmd.setup()` 建 `logger/store/notifier`
   并传参给用例。**不用全局变量、不用 `init()`、不用反射、不用 DI 框架。**
4. **配置一次解析成强类型**：`config.Load` 返回结构体并 `Validate()`；运行期只读结构体字段。
5. **错误显式上抛**：底层只 `return err`，**只有 `cmd` 层**打日志、定退出码
   （`errFailed` 用于让 systemd 判定失败）。
6. **依赖即参数**：用例函数形如 `backup.Run(ctx, cfg, job, Deps, st)`，无隐藏依赖。
7. **常量即默认值、可被配置覆盖**：所有"魔法数字"集中定义在 `config` 包顶部 `const` 块
   （如 `DefaultExpireDays`、`DefaultNotifyTimeoutSeconds`、`DefaultListPageSize`、
   `DefaultRetryBackoffSeconds`），并在 `applyDefaults()` 中落到结构体字段；
   **不要在业务代码里写死数字**，一律从 `cfg` 取。
8. **不得硬编码安装目录**：代码/文档里不出现 `/opt/tide-sync` 之类的绝对安装路径。
   - 配置文件位置由 `cmd.resolveConfigPath` 发现（`-c` > `./config.toml` > 可执行文件同目录）。
   - 配置里的相对路径由 `config.resolvePaths` 解析到**配置文件所在目录**。
   - systemd 单元用 `deploy/install-systemd.sh` 把 `__TIDE_DIR__` 替换成实际目录。

## 构建与校验

```bash
go build ./...        # 编译
go vet ./...          # 静态检查
go test ./...         # 单元测试 + 本地端到端
gofmt -l -w .         # 格式化
```

- `internal/backup/backup_test.go`：增量、排除、日期目录跳过、状态往返。
- `internal/config/*_test.go`：配置解析、`${ENV}` 展开、`.env` 解析、**相对/绝对路径解析**。
- `cmd/root_test.go`：配置路径发现（`-c` / 当前目录 / 找不到时报错）。
- `internal/lock/lock_test.go`：锁目录自动创建。

## 核心不变量（改动时勿破坏）

- **镜像**：一个源文件 = 一个桶内对象；key = `<prefix>/<host>/<job>/<相对路径>`。
- **增量**：`size + mtime` 相同 → 跳过；上传后复检是否被改写，改写则本轮记失败、下轮重传。
- **不删除**：工具从不删除桶内对象；保留由 OSS 生命周期负责。
- **一次运行**：`tide run` 跑完全部任务后退出，**无内置调度**、不判断到期。
- **状态只在成功后落盘**：`State.Save` 原子写（tmp + rename），仅在任务循环结束后调用一次。
- **敏感信息只从环境来**：配置里只写 `${ENV}`；`.env` 优先级低于真实环境变量。
- **与安装位置无关**：任何路径都从「配置文件位置」派生，不得依赖 CWD 之外写死的位置。

## 如何扩展

- **新增存储后端**（如腾讯云 COS/S3）：在 `internal/store/` 实现 `Backend` 三个方法，
  并在 `store.New` 的 `switch` 增加一个 case；`config.Storage` 加字段与校验。
- **新增通知渠道**（如企业微信）：在 `internal/notify/` 实现 `Notifier`（复用 `postJSON`），
  在 `notify.New` 中按配置构建；请求体校验用 `httptest` 写单测。
- **新增配置项**：在 `config` 加字段（`koanf` tag）+ `const` 默认值 + `applyDefaults()` 落到字段 +
  `Validate()` 校验；再在 `config.example.toml` 补注释。
- **新增子命令**：在 `cmd/` 加 `newXxxCmd()`，在 `cmd/root.go` 的 `root.AddCommand(...)` 注册；
  用例逻辑放进 `internal/` 对应包，`cmd` 保持薄。

## 已知陷阱

- **`.env` 由工具写入会被工作区安全策略拒绝**：不要用工具生成 `.env`（含密文件），
  用 `.env.example` + 复制。程序读取 `.env` 是正常的。
- **`filepath.IsAbs` 在 Windows 上把 `/tmp/x` 判为相对**：跨平台测试要用平台绝对路径
  （`filepath.Join(t.TempDir(), ...)`），别写死 `/tmp/...`。生产目标是 Linux。
- **`skip_completed_datedirs` 代码默认 `false`**（安全全扫），但**示例配置设为 `true`**。
  原因：bool 默认值无法区分「未设置」与「显式 false」，故代码默认保守、由配置显式开启。
- **`restore --dir` 匹配顶层目录名**（如 `log-2024-06-11`），不是纯日期 `2024-06-11`——
  因为实际目录名带前缀。过滤用 `doublestar` 匹配相对路径。
- **`lifecycle --apply` 会保留桶上其它规则**：先 `GetBucketLifecycle` 读回、替换同名 ID（`tide-sync-expire-<N>d`）
  后再整体写回，避免清掉他人规则。
- **`--days/--abort-days` 传 0 表示"用配置"**：`cmd/lifecycle.go` 里 `<=0` 时回落到 `cfg.Lifecycle.*`。
- **路径归一化**：源扫描内部统一用 `/` 分隔的 rel 路径（`filepath.ToSlash`）；
  对象 key 用 `path.Join`；本地/OS 路径用 `filepath`。开发在 Windows、生产在 Linux，注意别混用。
- **`${ENV}` 展开只识别 `${NAME}`**（不处理裸 `$`），避免误伤含 `$` 的密钥；变量缺失直接报错。
- **目录一律自动创建，文档里不要写手工 `mkdir`**：`state.json`（`State.Save`）、local 镜像目录
  （`store/local.go` 的 Put）、日志目录（lumberjack 内部 `MkdirAll`）、锁目录（`lock.Acquire`）都会自动建。
- **systemd 默认 `TimeoutStartSec=90s` 会杀掉长备份**：`Type=oneshot` 下必须显式 `TimeoutStartSec=0`
  （模板已设）。被杀时进程来不及发通知、状态也不落盘，只会被 systemd 标记 failed。
- **通知失败不改变结果**：`sendNotify` 发送失败只写 warn 日志、不影响退出码；
  要"告警的告警"只能靠 systemd `OnFailure=`，不要指望它自己。
