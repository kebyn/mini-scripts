# mini-scripts

日常开发、系统运维与容器化场景下的多语言实用小脚本与微工具集合，涵盖 **Bash**、**Docker**、**Go**、**Python** 和 **Rust**。

---

## 目录结构与命名规范

仓库按语言/技术栈划分为 5 个一级目录，并统一遵循各语言的社区命名规范：

| 一级目录 | 语言 / 技术栈 | 命名规范 | 说明 |
| :--- | :--- | :--- | :--- |
| [`bash/`](./bash/) | Bash / Shell | `kebab-case.sh` | Redis 集群管理、纯 TCP HTTP 请求、Docker 镜像批量导入导出及 Registry v2 脚本 |
| [`docker/`](./docker/) | Dockerfile | `Dockerfile` / `<variant>.Dockerfile` | 常用 DNS 诊断与磁盘 I/O 压测工具镜像 |
| [`golang/`](./golang/) | Go (`1.26`) | 子目录 `snake_case`，入口统一为 `main.go` | 独立 Go 实用命令行工具集 |
| [`python/`](./python/) | Python 3 | PEP 8 `snake_case.py` | Discourse API Key 申请、带上传功能的 HTTP 服务、Docker Tag 查询等工具 |
| [`rustlang/`](./rustlang/) | Rust | `snake_case.rs` | Rust 代码片段与文件 I/O 示例 |

---

## 功能索引速查表

### 1. Bash 脚本 (`bash/`)

详见 [`bash/README.md`](./bash/README.md)：

| 脚本文件 | 功能说明 |
| :--- | :--- |
| [`create-redis-cluster.sh`](./bash/create-redis-cluster.sh) | 快速在单机启动、创建、监控、停止及清理 6 节点（3 主 3 从）Redis 集群，支持自定义绑定 IP |
| [`curl.sh`](./bash/curl.sh) | 利用 Bash 内置 `/dev/tcp` 实现的无外部依赖 HTTP GET 客户端 |
| [`operator-util.sh`](./bash/operator-util.sh) | Shell 运维基础函数库：系统识别（`os_type`）、彩色输出（`echo_pass`/`echo_fail`）、指数退避重试（`retry`）与命令执行（`run_cmd`） |
| [`docker-save-load-images.sh`](./bash/docker-save-load-images.sh) | 使用 `xargs -P 4` 并发批量导出（`ds`/`save`）与批量加载（`dl`/`load`）本地 Docker 镜像 |
| [`get-image-manifest.sh`](./bash/get-image-manifest.sh) | 认证并下载 Docker Hub Registry v2 镜像的 Manifest 至 `manifest.json` |
| [`reg-v2-cdn.sh`](./bash/reg-v2-cdn.sh) | 向 Docker Registry v2 推送 10MB 随机 Blob 并连续拉取 10 次以测试 Registry/CDN 性能 |

---

### 2. Go 实用工具 (`golang/`)

每个子目录均为独立的 Go Module（`go 1.26.0`），入口文件统一为 `main.go`：

| 子项目 | 功能说明 |
| :--- | :--- |
| [`bytes2human`](./golang/bytes2human/) | 将字节数（`int64`）转换为人类可读的单位字符串（`B`、`KB`、`MB`、`GB`、`TB`、`PB`） |
| [`cloud_instance`](./golang/cloud_instance/) | 自动创建阿里云 ECS 或腾讯云 CVM 抢占式（Spot）实例（默认 `aliyun`，支持 `-provider tencent`）、分配公网 IP、启动实例并设置定时自动释放/销毁时间 |
| [`delete_expired_file`](./golang/delete_expired_file/) | 递归清理指定目录下超过 N 天（默认 7 天）的过期文件（含多阶段构建 `Dockerfile`） |
| [`examples`](./golang/examples/) | 监听 `:8080` 并返回当前主机/容器非回环 IPv4 地址的轻量 HTTP 服务（常用于 K8s Pod 测试） |
| [`get_local_ip`](./golang/get_local_ip/) | 获取并打印本机局域网 IPv4 地址的控制台小工具 |
| [`images_base64`](./golang/images_base64/) | 将本地图片文件编码为 Base64 Markdown `<img />` 标签并自动写入系统剪贴板 |
| [`password`](./golang/password/) | 随机生成指定长度的密码，支持纯字母数字模式或通过 `-s` 开启特殊字符模式 |
| [`physical_cpu`](./golang/physical_cpu/) | 基于 `gopsutil` 获取当前主机的物理 CPU 核心数 |
| [`qrcode`](./golang/qrcode/) | 命令行二维码生成工具，支持输出 PNG 文件、标准输出或终端字符画（`-t`） |

---

### 3. Python 实用工具 (`python/`)

| 子项目 | 脚本文件 | 功能说明 |
| :--- | :--- | :--- |
| [`discourse_user_api_key`](./python/discourse_user_api_key/) | [`discourse_user_api_key.py`](./python/discourse_user_api_key/discourse_user_api_key.py) | 向 Discourse 站点（如 `linux.do`）申请、RSA 解密、验证并保存 User API Key（支持 `auto`/`device`/`browser`/`paste` 流程） |
| [`simple_http_server_upload`](./python/simple_http_server_upload/) | [`simple_http_server_upload.py`](./python/simple_http_server_upload/simple_http_server_upload.py) | 基于 Python 3 标准库的 HTTP/HTTPS 目录文件服务器，支持网页表单上传文件 |
| [`list_docker_tags`](./python/list_docker_tags/) | [`list_docker_tags.py`](./python/list_docker_tags/list_docker_tags.py) | 通过 Docker Hub Registry v2 API 查询一个或多个镜像的全部 Tag 列表 |
| [`bytes2human`](./python/bytes2human/) | [`bytes2human.py`](./python/bytes2human/bytes2human.py) | 将字节大小整数转换为人类可读的单位字符串（`B`~`PB`） |

---

### 4. Docker 镜像定义 (`docker/`)

| 目录 | 基础镜像 | 功能说明 |
| :--- | :--- | :--- |
| [`docker/dnsutils`](./docker/dnsutils/) | `ubuntu:24.04` / `alpine:3.21` | 封装 `dig`、`nslookup`、`host` 等 DNS 诊断工具镜像 |
| [`docker/fio`](./docker/fio/) | `almalinux:9` | 封装 `fio` 与 `libaio-devel` 磁盘 I/O 性能测试工具镜像 |

---

### 5. Rust 示例 (`rustlang/`)

| 文件 | 功能说明 |
| :--- | :--- |
| [`write_file.rs`](./rustlang/write_file.rs) | 使用 Rust 标准库创建文件并循环写入数据的基础示例（详见 [`rustlang/README.md`](./rustlang/README.md)） |

---

## License

本项目基于 [Apache-2.0 License](./LICENSE) 开源。
