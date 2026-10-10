# Tide-Sync

把服务器上的备份目录**增量同步**到阿里云 OSS：每个文件对应桶内一个对象，只上传新增/变化的文件；
按主机隔离；由 systemd timer 每天触发，跑完退出；桶内保留 30 天。

典型用法：本地脚本每天在 `/opt/backup/某软件/` 下生成 `log-YYYY-MM-DD/` 目录，Tide-Sync 把它们增量搬到 OSS。

> **可装在任意目录**（下面以 `/opt/tide-sync` 为例）：程序自己找配置，配置里的相对路径都相对配置文件解析。

## 安装

需要 `git` 与 Go（版本见 `go.mod`）。

```bash
sudo git clone https://github.com/tanyons/Tide-Sync.git /opt/tide-sync
cd /opt/tide-sync
sudo go build -o tide .

sudo cp config.example.toml config.toml          # 填：bucket、prefix、要备份的目录
sudo cp .env.example .env && sudo chmod 600 .env # 填：OSS AK/SK、飞书 webhook
sudo vi config.toml .env

sudo bash deploy/install-systemd.sh              # 生成并启用 systemd timer（自动填入安装目录）
```

关于路径：

- 程序在**当前目录**或**可执行文件同目录**查找 `config.toml`；也可用 `-c` 指定。
- 配置里的**相对路径**（`state_file`、`logging.file`、`storage.local_dir`）都相对 `config.toml` 所在目录解析，
  所以装到哪都能用；`logs/`、`state.json`、`state.lock` 等运行期文件会自动创建。

升级：`cd <安装目录> && sudo git pull && sudo go build -o tide .`

## 使用

```bash
tide run                    # 执行全部任务（timer 调用的就是它）
tide backup --dry-run       # 演练：只列出将要上传的文件，不实际上传
tide backup --job appA      # 只跑某个任务
tide restore --job appA --to /tmp/out    # 从桶恢复
tide config check           # 校验并打印配置（脱敏）
```

- 配置项见 `config.example.toml`（每项都有注释）。
- 敏感信息只放 `.env`，配置里用 `${ENV}` 引用；程序自动加载配置同目录的 `.env`。
- 常用参数：`-c` 配置路径、`-e` .env 路径、`-v` debug 日志。

## 桶内结构

```
<prefix>/<host>/<job>/log-YYYY-MM-DD/...
# 例：tide-sync/web-01/appA/log-2024-06-11/xxx/a.log
```

## 保留 30 天

清理交给 OSS 生命周期，执行一次即可（`tide lifecycle` 会打印这段）：

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
ossutil lifecycle --method put oss://your-bucket-name lifecycle.xml   # 或用阿里云控制台
tide lifecycle --apply                                                # 也可让工具直接下发
```

## 说明

- 工具**从不删除**桶内对象，保留天数由生命周期控制（`lifecycle.expire_days`）。
- 上传走内网 endpoint（`internal_endpoint = true`）不产生流量费；建议关闭桶版本控制。
- `tide run` 不做定时判断，频率完全由 systemd timer 决定（默认每天 03:00）。
