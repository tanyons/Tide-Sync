# AGENTS.md

面向**开发者与 AI agent** 的工程指南。用户/运维文档见 [README.md](README.md)。

## 项目定位

Go 编写的单二进制命令行工具：把服务器本地目录**增量镜像同步**到对象存储。
**一台主机一份配置**（列出一批源目录），一次性运行（`tide run`），由 systemd timer 触发；
桶内保留交给 OSS 生命周期。**可安装在任意目录**，不依赖固定路径。

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
  restore/                用例：把桶内子树按目录对目录恢复到本地
  lifecycle/              用例：生成/下发 OSS 生命周期规则
  store/                  构件：Backend 接口 + local.go + oss.go
  config/                 构件：koanf(TOML) 加载 + .env + ${ENV} 展开 + 路径解析 + 校验 + 脱敏
  logx/                   构件：slog 构建
  notify/                 构件：Notifier 接口 + feishu 卡片 + dingtalk
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
   | 源路径 → 桶内路径段 | `backup.SourcePrefix` |
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
   | 源目录重叠校验 | `config.nestedIn` |

3. **显式装配**：`cmd.loadConfig()` 先 `.env` 再 `config.Load`；`cmd.setup()` 建 `logger/store/notifier`
   并传参给用例。**不用全局变量、不用 `init()`、不用反射、不用 DI 框架。**
4. **配置一次解析成强类型**：`config.Load` 返回结构体并 `Validate()`；运行期只读结构体字段。
5. **错误显式上抛**：底层只 `return err`，**只有 `cmd` 层**打日志、定退出码
   （`errFailed` 用于让 systemd 判定失败）。
6. **依赖即参数**：用例函数形如 `backup.Run(ctx, cfg, src, Deps, st)`，无隐藏依赖。
7. **常量即默认值、可被配置覆盖**：所有"魔法数字"集中定义在 `config` 包顶部 `const` 块，
   并在 `applyDefaults()` 中落到结构体字段；**不要在业务代码里写死数字**。
8. **不得硬编码安装目录**：配置文件位置由 `cmd.resolveConfigPath` 发现，配置里的相对路径由
   `config.resolvePaths` 解析到配置文件所在目录；systemd 单元用 `deploy/install-systemd.sh`
   把 `__TIDE_DIR__` 替换成实际目录。

## 构建与校验

```bash
go build ./... / go vet ./... / go test ./... / gofmt -l -w .
```

- `internal/backup/backup_test.go`：SourcePrefix、ObjectKey、增量、排除、日期目录跳过、状态往返。
- `internal/config/*_test.go`：配置解析、`${ENV}`、`.env`、相对/绝对路径、**源目录校验（非绝对/嵌套/重复）**。
- `internal/restore/restore_test.go`：子树恢复语义（内容写入 `--to`）、过滤、根目录判定。
- `internal/notify/*_test.go`：卡片结构、成功/失败着色、事件开关。
- `cmd/root_test.go`：配置路径发现；`internal/lock/lock_test.go`：锁目录自动创建。

## 核心不变量（改动时勿破坏）

- **镜像**：一个源文件 = 一个桶内对象；key = `<prefix>/<host>/<源目录绝对路径>/<相对路径>`。
- **路径即身份**：源目录没有 name；改源路径 = 换身份（会重新上传）。
- **增量**：`size + mtime` 相同 → 跳过；上传后复检是否被改写，改写则本轮记失败、下轮重传。
- **不删除**：工具从不删除桶内对象；保留由 OSS 生命周期负责。
- **一次运行**：`tide run` 跑完全部源目录后退出，**无内置调度**、不判断到期。
- **状态只在成功后落盘**：`State.Save` 原子写（tmp + rename），仅在循环结束后调用一次。
- **敏感信息只从环境来**：配置里只写 `${ENV}`；`.env` 优先级低于真实环境变量。
- **与安装位置无关**：路径都从「配置文件位置」派生，不得写死安装目录。

## 如何扩展

- **新增存储后端**（如腾讯云 COS/S3）：在 `internal/store/` 实现 `Backend` 三个方法，
  并在 `store.New` 的 `switch` 增加 case；`config.Storage` 加字段与校验。
- **新增通知渠道**（如企业微信）：在 `internal/notify/` 实现 `Notifier`（复用 `postJSON`），
  在 `notify.New` 中按配置构建；请求体校验用 `httptest` 写单测。
- **新增配置项**：在 `config` 加字段（`koanf` tag）+ `const` 默认值 + `applyDefaults()` + `Validate()`，
  再在 `config.example.toml` 补注释。
- **新增子命令**：在 `cmd/` 加 `newXxxCmd()`，在 `cmd/root.go` 的 `root.AddCommand(...)` 注册；
  用例逻辑放进 `internal/` 对应包，`cmd` 保持薄。

## 已知陷阱

- **`.env` 由工具写入会被工作区安全策略拒绝**：用 `.env.example` + 复制；程序读取 `.env` 正常。
- **`filepath.IsAbs` 在 Windows 上把 `/tmp/x` 判为相对**：配置校验用 `config.isRooted`（同时接受 `/...`）；
  测试要用平台绝对路径（`filepath.Join(t.TempDir(), ...)`）。
- **对象 key 里不能出现 Windows 盘符冒号**：`SourcePrefix` 会去掉 `:`（`C:/x` → `C/x`），
  否则本地后端的路径映射会失败。
- **源目录重叠必须报错**：`/opt/backup` 与 `/opt/backup/appA` 同时配会让同一批文件上传两份。
- **恢复语义 A**：`--path` 是桶内子树，**其内容**写入 `--to`（剥掉前缀），不是保留完整路径。
  默认 `IsRootPath(--to)` 时拒绝，需要 `--allow-root` 才允许。
- **`skip_completed_datedirs` 代码默认 `false`**（安全全扫），但**示例配置设为 `true`**：
  bool 默认值无法区分「未设置」与「显式 false」，故代码默认保守、由配置显式开启。
- **`lifecycle --apply` 会保留桶上其它规则**：先读回、替换同名 ID 后再整体写回。
- **`--days/--abort-days` 传 0 表示"用配置"**：`cmd/lifecycle.go` 里 `<=0` 时回落到 `cfg.Lifecycle.*`。
- **路径归一化**：源扫描内部统一 `/` 分隔（`filepath.ToSlash`）；key 用 `path.Join`；
  本地路径用 `filepath`。开发在 Windows、生产在 Linux，注意别混用。
- **`${ENV}` 展开只识别 `${NAME}`**（不处理裸 `$`）；变量缺失直接报错。
- **目录一律自动创建**：`state.json`（`State.Save`）、local 镜像目录、日志目录、锁目录都会自动建。
- **systemd 默认 `TimeoutStartSec=90s` 会杀掉长备份**：`Type=oneshot` 下已显式设 `TimeoutStartSec=0`。
- **通知失败不改变结果**：`sendNotify` 失败只写 warn 日志、不影响退出码；"告警的告警"只能靠 `OnFailure=`。
