#!/usr/bin/env bash
# 安装 systemd 单元：把 service 里的 __TIDE_DIR__ 替换成实际安装目录。
#
# 用法：
#   sudo bash deploy/install-systemd.sh                 # 默认用脚本上一级目录（即仓库根）
#   sudo bash deploy/install-systemd.sh /your/dir       # 或显式指定安装目录
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
dir="${1:-$(cd "$here/.." && pwd)}"
name="tide-sync"

if [ ! -x "$dir/tide" ]; then
  echo "警告: $dir/tide 不存在或不可执行，请先在该目录执行 go build -o tide ." >&2
fi
if [ ! -f "$dir/config.toml" ]; then
  echo "警告: $dir/config.toml 不存在，请从 config.example.toml 复制并填写" >&2
fi

sed "s#__TIDE_DIR__#$dir#g" "$here/tide-sync.service" > "/etc/systemd/system/$name.service"
cp "$here/tide-sync.timer" "/etc/systemd/system/$name.timer"

systemctl daemon-reload
systemctl enable --now "$name.timer"

echo "已安装 $name.timer，安装目录: $dir"
systemctl --no-pager list-timers "$name.timer" || true
