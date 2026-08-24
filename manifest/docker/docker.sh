#!/bin/bash
# 一键构建并启动所有服务
set -e

cd "$(dirname "$0")"

BACKEND_CONTAINER="sentinelops-backend"
BACKEND_SERVICE="backend"
FRONTEND_SERVICE="frontend"
NGINX_SERVICE="nginx"
HEALTH_URL="http://localhost:8001/api.json"
WAIT_INTERVAL=3
MAX_WAIT_SECONDS="${MAX_WAIT_SECONDS:-600}"
ELAPSED=0

if ! command -v npm >/dev/null 2>&1; then
  echo "==> 未检测到 npm，无法构建最新前端代码"
  exit 1
fi

echo "==> 构建最新前端代码..."
(
  cd ../../web
  npm run build
)

echo "==> 启动基础设施容器..."
docker compose up -d etcd minio standalone attu redis mysql

echo "==> 构建最新 backend/frontend 镜像..."
docker compose build backend frontend

echo "==> 更新 backend/frontend 容器..."
docker compose up -d backend frontend

echo "==> 更新 nginx 容器..."
docker compose up -d nginx

echo "==> 等待 backend 就绪（最长 ${MAX_WAIT_SECONDS} 秒）..."
while ! docker exec "$BACKEND_CONTAINER" wget -qO- "$HEALTH_URL" > /dev/null 2>&1; do
  if [ "$ELAPSED" -ge "$MAX_WAIT_SECONDS" ]; then
    echo "==> backend 启动超时，输出诊断信息..."
    docker compose ps || true
    echo "\n==> backend 服务日志："
    docker compose logs "$BACKEND_SERVICE" || true
    echo "\n==> frontend 服务日志："
    docker compose logs "$FRONTEND_SERVICE" || true
    echo "\n==> nginx 服务日志："
    docker compose logs "$NGINX_SERVICE" || true
    exit 1
  fi
  sleep "$WAIT_INTERVAL"
  ELAPSED=$((ELAPSED + WAIT_INTERVAL))
done

echo "==> 部署完成，访问地址："
echo "    前端页面:  http://localhost"
echo "    后端 API:  http://localhost/api.json"
echo "    Swagger:   http://localhost/swagger"
echo "    Attu:      http://localhost:8000"
