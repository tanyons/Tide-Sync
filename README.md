# Tide-Sync

把服务器上的备份目录**增量同步**到阿里云 OSS：只传新增/变化的文件，按主机隔离；
systemd timer 定时触发，桶内保留 30 天。

## 安装

```bash
sudo git clone https://github.com/tanyons/Tide-Sync.git /opt/tide-sync && cd /opt/tide-sync
sudo go build -o tide .

sudo cp config.example.toml config.toml
sudo cp .env.example .env && sudo chmod 600 .env
sudo vi config.toml .env        # config.toml=要备份什么；.env=OSS 密钥、飞书 webhook

sudo bash deploy/install-systemd.sh   # 注册并启用 systemd timer（每天 03:00）
```

升级：`git pull && go build -o tide .`（装到任意目录均可，路径自动跟随）

## 配置

`config.toml` 至少要改这几项（其余看 `config.example.toml` 注释）：

```toml
[storage]
bucket = "my-bucket"
prefix = "tide-sync"            # 桶内根路径

[[jobs]]
name = "appA"
source = "/opt/backup/appA"     # 要备份的目录，可写多个 [[jobs]]
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
| `tide run` | 执行全部任务（timer 调用的就是它） |
| `tide backup --dry-run` | 演练：只列出将要上传的文件 |
| `tide backup --job appA` | 只跑某个任务 |
| `tide restore --job appA --to /tmp/out` | 从桶恢复到本地 |
| `tide config check` | 查看生效配置（密钥脱敏） |
| `tide lifecycle [--apply]` | 打印 / 下发 OSS 保留规则 |

## 桶内路径

```
<prefix>/<host>/<job>/log-YYYY-MM-DD/...
```

## 保留 30 天

在 OSS 上配一次即可（`tide lifecycle` 打印规则，`tide lifecycle --apply` 直接下发）：

> 删除前缀 `tide-sync/` 下、创建满 30 天的对象。

## 说明

- 只增不删：源端删文件不影响桶内，过期清理交给上面的生命周期规则。
- 桶版本控制建议关闭，上传走内网 endpoint（`internal_endpoint = true`）不产生流量费。

> 架构与开发说明见 [AGENTS.md](AGENTS.md)。
