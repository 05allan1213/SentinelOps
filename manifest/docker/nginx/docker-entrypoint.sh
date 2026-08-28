#!/bin/sh
# 确保黑名单文件存在
mkdir -p /app/nginx_blocklist
if [ ! -f /app/nginx_blocklist/blocked_ips.conf ]; then
    echo "# AI 运维动态 IP 黑名单（由 block_ip 工具自动写入）" > /app/nginx_blocklist/blocked_ips.conf
fi

# 执行原始 nginx entrypoint
exec /docker-entrypoint.sh "$@"
