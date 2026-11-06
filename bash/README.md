# Bash 实用脚本集

本目录包含常用的 Bash / Shell 运维与容器辅助脚本（文件名统一遵循 `kebab-case.sh` 规范）。

---

## 1. `create-redis-cluster.sh` — 快速创建与管理 Redis 集群

基于 Redis 官方 [`create-cluster`](https://github.com/redis/redis/tree/unstable/utils/create-cluster) 改造，支持通过第二个参数指定监听 IP（默认 `127.0.0.1`），快速在单机启动 6 节点（3 主 3 从）Redis 集群。

### 使用方法

```bash
# 在指定 IP (如 10.10.10.10) 启动并创建集群
./create-redis-cluster.sh start 10.10.10.10
./create-redis-cluster.sh create 10.10.10.10

# 查看集群节点状态
./create-redis-cluster.sh watch 10.10.10.10

# 停止集群并清理数据
./create-redis-cluster.sh stop 10.10.10.10
./create-redis-cluster.sh clean
```

可通过同目录下的 `config.sh` 覆盖默认变量（`PORT=7000`、`TIMEOUT=2000`、`NODES=6`、`REPLICAS=1`、`REDISDIR="../../src/"`）。

---

## 2. `curl.sh` — 纯 Bash `/dev/tcp` HTTP 客户端

在没有安装 `curl` 或 `wget` 的精简容器环境中，利用 Bash 内置的 `/dev/tcp` 发送 HTTP GET 请求。

```bash
# 直接执行
./curl.sh http://example.com/index.html

# 或在当前 Shell 中加载 __curl 函数
source ./curl.sh
__curl http://127.0.0.1:8080/healthz
```

---

## 3. `operator-util.sh` — Shell 运维基础函数库

提供操作系统类型识别（`os_type`）、彩色状态输出（`echo_pass`、`echo_fail`、`die`）、指数退避重试（`retry`）以及命令执行封装（`run_cmd`）。

```bash
source ./operator-util.sh

echo_pass "Service started"
echo_fail "Connection failed"
retry 3 curl -fsSL https://example.com
run_cmd "ls -la" "true"
```

---

## 4. `docker-save-load-images.sh` — 并发批量导出/导入 Docker 镜像

使用 `xargs -P 4` 开启 4 并发批量导出本地所有 Docker 镜像为 `.tar.gz`，或批量加载当前目录下的所有镜像归档包。

```bash
# 直接通过子命令执行
./docker-save-load-images.sh save
./docker-save-load-images.sh load

# 或加载函数 ds / dl
source ./docker-save-load-images.sh
ds
dl
```

---

## 5. `get-image-manifest.sh` — 获取 Docker Registry v2 镜像 Manifest

通过 Docker Hub Registry v2 认证接口获取 Bearer Token，并将指定仓库的 v2 Manifest 下载保存至 `manifest.json`（需安装 `curl` 与 `jq`）。

```bash
./get-image-manifest.sh
```

---

## 6. `reg-v2-cdn.sh` — Docker Registry v2 Blob 推拉测速脚本

向 Docker Registry v2 上传一个 10MB 随机数据 Blob，并连续拉取 10 次以测试镜像仓库及 CDN 的上传/下载性能。

```bash
REPONAME="youruser/test-repo" ./reg-v2-cdn.sh
```

---

## 7. `sysinfo.sh` — 离线系统基础信息采集脚本（YABS-lite）

对标 [YABS (Yet Another Bench Script)](https://github.com/masonr/yet-another-bench-script) 的 `Basic System Information` 模块（等效于本地离线版 `curl -sL yabs.sh | bash -s -- -dig`）。无需联网、无需下载、无需 `root` 权限，仅依赖 `/proc` 与基础 `coreutils` 即可输出运行时间、CPU 型号与核心频率、AES-NI / 虚拟化指令集状态、内存/Swap/磁盘总量、发行版、内核版本、虚拟化类型及全局 IPv4/IPv6 地址状态。

```bash
./sysinfo.sh
```
