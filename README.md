# Tide-Sync

按**文件增量**把服务器上的备份目录**镜像同步**到对象存储（默认阿里云 OSS）。
由 systemd timer 定时触发，跑完即退出；桶内保留由 OSS 生命周期规则控制。

> 面向开发者 / AI agent 的架构与代码约定见 [AGENTS.md](AGENTS.md)。

## 它是怎么工作的

- **镜像增量**：每个文件对应桶内一个对象；按 `size + mtime` 判断，只上传新增/变化的文件。
- **按主机隔离**：桶内路径为 `<prefix>/<host>/<job>/<相对路径>`，`host` 默认取本机 `os.Hostname()`。
  多台机器同步到同一个桶时互不干扰。
- **不删除**：源端删除文件不影响桶内；过期清理交给 OSS 生命周期。
- **一次运行**：`tide run` 执行全部任务后退出，运行频率完全由 systemd timer 决定。

典型场景：服务器本地脚本每天在 `/opt/backup/<软件>/` 下生成 `log-YYYY-MM-DD/` 目录，
Tide-Sync 每天把它们增量搬到 OSS，保留 30 天。

## 目录布局

**你只需要放 3 样东西**：可执行文件、`config.toml`、机密的 `.env`。
其余运行期文件（`logs/`、`state.json`、`state.lock`、local 后端镜像目录）**由程序在运行时自动创建**。

```
/opt/tide-sync/
├── tide                 # ① 可执行文件（你放）
├── config.toml          # ② 配置（你放）
├── .env                 # ③ 敏感信息 KEY=VALUE（你放；已被 .gitignore 忽略）
├── logs/                # 自动创建
│   └── tide-sync.log    #   日志（按大小滚动）
├── state.json           # 自动创建：状态账本（已上传文件的 size/mtime）
└── state.lock           # 自动创建：运行锁
```

桶内布局：

```
<prefix>/<host>/<job>/log-YYYY-MM-DD/...
# 例：tide-sync/web-01/appA/log-2024-06-11/xxx/a.log
```

## 配置与环境变量

- **`config.toml`**：所有可变项都在这里（存储、日志、通知、生命周期、并发/重试、排除规则等），
  详见 `config.example.toml` 逐行注释。
- **`.env`**：只放**敏感信息**（`TIDE_OSS_AK/SK`、`FEISHU_WEBHOOK` 等），在配置里用 `${ENV}` 引用。
  - 默认位置：与 `config.toml` **同目录**的 `.env`，程序启动时自动加载；也可用 `-e/--env` 指定。
  - 已存在的真实环境变量优先，`.env` 不会覆盖它们。
  - `.env` 已被 `.gitignore` 忽略，请勿提交。

## 编译

```bash
go build -o tide .
```

## 安装

```bash
# 1) 建目录（只放你要提供的文件；其余子目录程序会自建），并安装可执行文件
sudo mkdir -p /opt/tide-sync
sudo go build -o /opt/tide-sync/tide .        # 也可：go build -o tide . && sudo cp tide /opt/tide-sync/

# 2) 放配置与机密（这两个必须由你提供，程序无法替你生成）
sudo cp config.example.toml /opt/tide-sync/config.toml
sudo cp .env.example /opt/tide-sync/.env
sudo chmod 600 /opt/tide-sync/.env            # 然后编辑这两个文件

# 3) 安装 systemd 单元
sudo cp deploy/tide-sync.service deploy/tide-sync.timer /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable --now tide-sync.timer
```

> 说明：`/opt/tide-sync/` 这个目录本身需要存在（因为要往里放 `config.toml` 和 `.env`）；
> 而其下的 `logs/`、`state.json`、`state.lock` 都会在首次运行时自动创建，**无需手工建，也无需 `cd` 到该目录**。

## 命令

| 命令 | 作用 |
|---|---|
| `tide run` | 执行全部任务（timer 调用） |
| `tide backup [--job appA] [--dry-run]` | 强制执行指定/全部任务 |
| `tide restore --job appA [--dir log-2024-06-11] [--filter '**/*.txt'] --to /tmp/out` | 从桶恢复 |
| `tide lifecycle [--days N] [--abort-days N]` | 打印 OSS 生命周期规则 XML 与命令（默认不改桶） |
| `tide lifecycle --apply` | 直接下发规则到桶（保留桶上其它规则） |
| `tide config check` | 校验并打印生效配置（脱敏） |
| `tide version` | 版本 |

全局参数：

- `-c/--config`：配置文件路径（默认 `/opt/tide-sync/config.toml`）。
- `-e/--env`：环境变量文件路径（默认 `<配置目录>/.env`）。
- `-v/--verbose`：输出 debug 日志。

`restore` 说明：`--dir` 匹配**顶层目录名**（如 `log-2024-06-11`）；`--filter` 用 doublestar 匹配相对路径。
`lifecycle` 的 `--days/--abort-days` 缺省取配置里的 `lifecycle.expire_days/abort_days`。

## 生命周期规则（默认 30 天保留）

`tide lifecycle` 会输出下面这段（天数来自配置），复制到阿里云控制台，或用 ossutil 执行一次即可：

```xml
<LifecycleConfiguration>
  <Rule>
    <ID>tide-sync-expire-30d</ID>
    <Prefix>tide-sync/</Prefix>
    <Status>Enabled</Status>
    <Expiration><Days>30</Days></Expiration>
    <AbortMultipartUpload><DaysAfterInitiation>3</DaysAfterInitiation></AbortMultipartUpload>
  </Rule>
</LifecycleConfiguration>
```

```bash
ossutil lifecycle --method put oss://your-bucket-name lifecycle.xml
```

要点：

- **前缀 `tide-sync/` 限定范围**，不会误伤桶内其它数据。
- **不做存储分层**：归档/冷归档最短存放 60/180 天，与 30 天删除冲突会被补收费用；低频卡边界也易补收。
- 建议**关闭桶版本控制**，避免隐藏旧版本持续计费。
- 上传走**内网 endpoint**（`internal_endpoint = true`），不产生流量费。

## 说明与注意

- 所有"魔法数字"都可由配置覆盖：`lifecycle.expire_days/abort_days`、`notify.timeout_seconds`、
  `storage.list_page_size`、`defaults.retry_backoff_seconds`、`logging.max_age_days/compress` 等。
- `skip_completed_datedirs`：跳过"早于今天且已完成"的日期目录以提速。**前提是旧日期目录不再变化**；
  `config.example.toml` 中默认开启，若旧目录可能追加文件请设为 `false`。
- **一致性**：源目录里正在被写入的文件，上传后会复检 `size/mtime`；若期间被改写则本轮记为失败，下轮重传。
- 源目录不存在或不可读时该任务失败，并以非零退出码结束（systemd 标记为 failed，飞书告警）。
- 用 `storage.type = "local"` 可在完全离线的情况下演练"同步→增量→恢复"。
