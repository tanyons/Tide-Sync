# Tide-Sync

把服务器上的备份目录**增量同步**到阿里云 OSS：只传新增/变化的文件，按主机隔离；
systemd timer 定时触发，桶内保留 30 天。

**一台主机一份配置**：把本机要备份的目录列出来即可，不用逐个应用配。

## 安装

```bash
git clone https://github.com/tanyons/Tide-Sync.git && cd Tide-Sync
go build -o tide .

cp config.example.toml config.toml
cp .env.example .env && chmod 600 .env
vi config.toml .env        # config.toml=备份哪些目录；.env=OSS 密钥、飞书 webhook

sudo bash deploy/install-systemd.sh   # 注册并启用 systemd timer（每天 03:00）
```

clone 到哪就装在哪（systemd 单元自动填绝对路径）。升级：`git pull && go build -o tide .`

## 配置

`config.toml` 至少要改这几项（其余看 `config.example.toml` 注释）：

```toml
[storage]
bucket = "my-bucket"
prefix = "tide-sync"            # 桶内根路径

[[sources]]
path = "/opt/backup"            # 要备份的目录（可写多个 [[sources]]）
```

敏感信息只放 `.env`，配置里用 `${变量名}` 引用：

```bash
TIDE_OSS_AK=xxx
TIDE_OSS_SK=xxx
FEISHU_WEBHOOK=https://open.feishu.cn/open-apis/bot/v2/hook/xxx
```

## 命令

| 命令 | 说明 |
|---|---|
| `tide run` | 同步全部源目录（timer 调用的就是它） |
| `tide backup --dry-run` | 演练：只列出将要同步的源目录 |
| `tide backup --source /opt/backup` | 只同步某个源目录 |
| `tide restore --path opt/backup/appA/log-2024-06-11 --to /tmp/restore` | 把桶内子树恢复到本地目录 |
| `tide config check` | 查看生效配置（密钥脱敏） |
| `tide lifecycle [--apply]` | 打印 / 下发 OSS 保留规则 |

## 桶内路径

源目录的绝对路径会被原样保留在桶里（去掉开头的 `/`）：

```
<prefix>/<host>/<源目录绝对路径>/...
# 源 /opt/backup 下的 appA/log-2024-06-11/a.log →
# tide-sync/web-01/opt/backup/appA/log-2024-06-11/a.log
```

恢复是**目录对目录**：`--path` 指定桶内子树，该子树的内容写入 `--to`。
（例如上面那条命令会得到 `/tmp/restore/log-2024-06-11/...`。）

## 保留 30 天

在 OSS 上配一次即可（`tide lifecycle` 打印规则，`tide lifecycle --apply` 直接下发）：

> 删除前缀 `tide-sync/` 下、创建满 30 天的对象。

## 说明

- 只增不删：源端删文件不影响桶内，过期清理交给上面的生命周期规则。
- 恢复默认拒绝写入文件系统根目录；确需如此要显式加 `--allow-root`。
- 桶版本控制建议关闭，上传走内网 endpoint（`internal_endpoint = true`）不产生流量费。

> 架构与开发说明见 [AGENTS.md](AGENTS.md)。
