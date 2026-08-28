#!/bin/bash
# 一键构建并启动所有服务
set -e

cd "$(dirname "$0")"

# Docker 构建上下文不包含 .git，显式把当前提交作为不可变运行时身份传入。
export SENTINELOPS_RUNTIME_VERSION="${SENTINELOPS_RUNTIME_VERSION:-$(git -C ../.. rev-parse HEAD)}"

API_CONTAINER="sentinelops-api"
API_SERVICE="api"
WORKER_SERVICE="worker"
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

echo "==> 构建迁移、api/worker/frontend 镜像..."
docker compose build migrate api worker frontend

echo "==> 执行数据库迁移（包含默认 admin/user1 用户）..."
docker compose --profile migration run --rm migrate

echo "==> 更新 api/worker/frontend 容器..."
docker compose up -d api worker frontend

echo "==> 更新 nginx 容器..."
docker compose up -d nginx

echo "==> 等待 API 就绪（最长 ${MAX_WAIT_SECONDS} 秒）..."
while ! docker exec "$API_CONTAINER" wget -qO- "$HEALTH_URL" > /dev/null 2>&1; do
  if [ "$ELAPSED" -ge "$MAX_WAIT_SECONDS" ]; then
    echo "==> API 启动超时，输出诊断信息..."
    docker compose ps || true
    echo "\n==> API 服务日志："
    docker compose logs "$API_SERVICE" || true
    echo "\n==> Worker 服务日志："
    docker compose logs "$WORKER_SERVICE" || true
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
