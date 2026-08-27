#!/usr/bin/env bash
# 校验锁定的 Dockerfile 工具链，并输出 Compose image inventory。
set -euo pipefail
IFS=$'\n\t'

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)"
REPO_ROOT="$(cd -- "${SCRIPT_DIR}/../.." && pwd -P)"
cd "${REPO_ROOT}"

PROJECT="sentinelops-e2e"
DO_BUILD=1
OUTPUT_DIR="${SENTINELOPS_P41_ARTIFACT_DIR:-${REPO_ROOT}/.artifacts/p41/image-contract}"
GO_PROXY="${SENTINELOPS_CI_GOPROXY:-}"

while (($# > 0)); do
  case "$1" in
    --no-build)
      DO_BUILD=0
      shift
      ;;
    --output-dir)
      (($# >= 2)) || { echo "ERROR: --output-dir requires a path" >&2; exit 2; }
      OUTPUT_DIR="$2"
      shift 2
      ;;
    --project)
      (($# >= 2)) || { echo "ERROR: --project is fixed to sentinelops-e2e" >&2; exit 2; }
      [[ "$2" == "$PROJECT" ]] || { echo "ERROR: refusing project other than ${PROJECT}" >&2; exit 2; }
      shift 2
      ;;
    --help)
      echo "usage: scripts/ci/image-contract.sh [--no-build] [--output-dir PATH] [--project sentinelops-e2e]"
      exit 0
      ;;
    *)
      echo "ERROR: unknown argument: $1" >&2
      exit 2
      ;;
  esac
done

command -v docker >/dev/null 2>&1 || { echo "FAIL: docker is required" >&2; exit 1; }
docker info >/dev/null 2>&1 || { echo "FAIL: docker daemon is unavailable" >&2; exit 1; }
if [[ "${GO_PROXY}" == *"@"* || "${GO_PROXY}" =~ [[:space:]] ]]; then
  echo "FAIL: SENTINELOPS_CI_GOPROXY must not contain credentials or whitespace" >&2
  exit 1
fi

mkdir -p "${OUTPUT_DIR}"
COMPOSE_JSON="${OUTPUT_DIR}/compose-config.json"
BUILD_LOG="${OUTPUT_DIR}/build.log"
VENDOR_LOG="${OUTPUT_DIR}/vendor.log"
INVENTORY="${OUTPUT_DIR}/image-inventory.json"

COMPOSE=(docker compose -p "${PROJECT}" -f manifest/docker/docker-compose.yml -f manifest/docker/docker-compose.test.yml)
"${COMPOSE[@]}" config --format json >"${COMPOSE_JSON}"

python3 - "${COMPOSE_JSON}" "${PROJECT}" <<'PY'
import json
import pathlib
import sys

path = pathlib.Path(sys.argv[1])
project = sys.argv[2]
data = json.loads(path.read_text())
if data.get("name") != project:
    raise SystemExit(f"compose project is {data.get('name')!r}, want {project!r}")

required = {"api", "worker", "frontend", "migrate", "mysql", "redis", "standalone", "provider-double", "nginx"}
services = data.get("services", {})
missing = sorted(required - set(services))
if missing:
    raise SystemExit(f"compose services missing: {missing}")

for name, service in services.items():
    if service.get("container_name"):
        raise SystemExit(f"fixed container_name is forbidden in isolated service {name}")
    for mount in service.get("volumes", []):
        if isinstance(mount, dict) and mount.get("type") == "bind":
            source = str(mount.get("source", ""))
            if source != "/var/run/docker.sock":
                raise SystemExit(f"host bind mount is forbidden: {name}:{source}")
    for port in service.get("ports", []):
        host_ip = port.get("host_ip", "") if isinstance(port, dict) else ""
        if host_ip != "127.0.0.1":
            raise SystemExit(f"non-loopback published port in isolated stack: {name}:{port}")

for name, network in data.get("networks", {}).items():
    if network.get("external"):
        raise SystemExit(f"external network is forbidden: {name}")
    network_name = str(network.get("name", ""))
    if network_name in {"milvus", "sentinelops"}:
        raise SystemExit(f"shared production network is forbidden: {network_name}")

for name, volume in data.get("volumes", {}).items():
    if not name.startswith("sentinelops_e2e_"):
        raise SystemExit(f"non-isolated named volume: {name}")
    if volume.get("external"):
        raise SystemExit(f"external volume is forbidden: {name}")

pathlib.Path(path.parent / "isolation-pass").write_text("PASS\n")
PY

check_from_version() {
  local file="$1" expected="$2"
  [[ -f "$file" ]] || { echo "FAIL: missing Dockerfile ${file}" >&2; exit 1; }
  if ! grep -Eq "^FROM ${expected}([[:space:]]|-)" "$file"; then
    echo "FAIL: ${file} does not declare ${expected}" >&2
    exit 1
  fi
}

check_from_version manifest/docker/Dockerfile.backend golang:1.27.0
check_from_version manifest/docker/Dockerfile.backend.e2e golang:1.27.0
check_from_version manifest/docker/Dockerfile.migrate golang:1.27.0
check_from_version manifest/docker/Dockerfile.frontend node:24.19.0

if grep -En '^FROM (golang|node):' manifest/docker/Dockerfile.* | grep -Ev 'golang:1\.27\.0|node:24\.19\.0' >/dev/null; then
  echo "FAIL: an application builder uses an unlocked Go/Node version" >&2
  exit 1
fi

ensure_vendor_tree() {
  if [[ -f vendor/modules.txt ]]; then
    printf '%s\n' 'vendor/modules.txt already exists; skipped go mod vendor' >"${VENDOR_LOG}"
    return 0
  fi

  command -v go >/dev/null 2>&1 || {
    echo "FAIL: go is required to generate the vendor tree; see ${VENDOR_LOG}" >&2
    return 1
  }

  if [[ -n "${GO_PROXY}" ]]; then
    if ! GOPROXY="${GO_PROXY}" go mod vendor >"${VENDOR_LOG}" 2>&1; then
      echo "FAIL: go mod vendor failed; see ${VENDOR_LOG}" >&2
      return 1
    fi
  elif ! go mod vendor >"${VENDOR_LOG}" 2>&1; then
    echo "FAIL: go mod vendor failed; see ${VENDOR_LOG}" >&2
    return 1
  fi
}

if ((DO_BUILD)); then
  ensure_vendor_tree || exit 1
  build_args=()
  if [[ -n "${GO_PROXY}" ]]; then
    build_args+=(--build-arg "GOPROXY=${GO_PROXY}")
  fi
  if ! "${COMPOSE[@]}" build --pull "${build_args[@]}" api worker frontend migrate provider-double nginx >"${BUILD_LOG}" 2>&1; then
    echo "FAIL: required image build failed; see ${BUILD_LOG}" >&2
    exit 1
  fi
fi

services=(api worker frontend migrate provider-double nginx)
service_ids_file="${OUTPUT_DIR}/service-ids.tsv"
: >"${service_ids_file}"
for service in "${services[@]}"; do
  image_id="$("${COMPOSE[@]}" images -q "${service}" | head -n 1)"
  if [[ -z "${image_id}" ]]; then
    image_ref="${PROJECT}-${service}"
    image_id="$(docker image inspect --format '{{.Id}}' "${image_ref}" 2>/dev/null || true)"
  fi
  [[ -n "${image_id}" ]] || { echo "FAIL: image for service ${service} is unavailable" >&2; exit 1; }
  printf '%s\t%s\n' "${service}" "${image_id}" >>"${service_ids_file}"
done

base_refs_file="${OUTPUT_DIR}/base-refs.txt"
grep -hE '^FROM ' manifest/docker/Dockerfile.* | awk '{print $2}' | sort -u >"${base_refs_file}"
while IFS= read -r base_ref; do
  [[ -n "${base_ref}" ]] || continue
  if ! docker image inspect "${base_ref}" >/dev/null 2>&1; then
    docker pull "${base_ref}" >>"${BUILD_LOG}" 2>&1
  fi
done <"${base_refs_file}"

python3 - "${COMPOSE_JSON}" "${service_ids_file}" "${base_refs_file}" "${INVENTORY}" "${PROJECT}" <<'PY'
import json
import pathlib
import subprocess
import sys

compose_path = pathlib.Path(sys.argv[1])
services_path = pathlib.Path(sys.argv[2])
refs_path = pathlib.Path(sys.argv[3])
output_path = pathlib.Path(sys.argv[4])
project = sys.argv[5]

service_ids = {}
for line in services_path.read_text().splitlines():
    if line.strip():
        name, image_id = line.split("\t", 1)
        service_ids[name] = image_id

def inspect(ref):
    raw = subprocess.check_output(
        ["docker", "image", "inspect", "--format", "{{json .}}", ref],
        text=True,
        stderr=subprocess.DEVNULL,
    )
    image = json.loads(raw)
    return {
        "reference": ref,
        "id": image.get("Id", ""),
        "repo_tags": image.get("RepoTags") or [],
        "repo_digests": image.get("RepoDigests") or [],
    }

base_images = []
for ref in refs_path.read_text().splitlines():
    ref = ref.strip()
    if not ref:
        continue
    try:
        base_images.append(inspect(ref))
    except subprocess.CalledProcessError as exc:
        raise SystemExit(f"base image is not locally inspectable: {ref}") from exc

application_images = []
for service, image_id in service_ids.items():
    application_images.append({"service": service, **inspect(image_id)})

compose = json.loads(compose_path.read_text())
inventory = {
    "schema": "sentinelops/ci-image-inventory/v1",
    "project": project,
    "compose_services": sorted(compose.get("services", {})),
    "dockerfiles": {"go": "1.27.0", "node": "24.19.0"},
    "base_images": base_images,
    "application_images": application_images,
}
output_path.write_text(json.dumps(inventory, indent=2, sort_keys=True) + "\n")
PY

echo "PASS: image/version contract; inventory=${INVENTORY}"
