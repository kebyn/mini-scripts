# sandbox

[English](README.md) | 中文

用 QEMU + KVM + passt 启动 Debian 13 / Rocky 10 云镜像。

## 文件

- `sandbox.sh`：唯一实现。子命令 `bootstrap`、`run`、`stop`，支持中文/英文和 `curl | bash`。
- `user-data.yaml.tmpl`：`~/user-data.yaml` 不存在时用于生成默认配置；`sandbox.sh` 内有同样的内嵌副本，供管道模式使用。
- `.gitignore`：镜像、磁盘、seed、日志和密钥不进入仓库。

## 快速开始

### 本地

```bash
./sandbox.sh bootstrap --qemu-version v11.1.2 --jobs "$(nproc)"
./sandbox.sh run --background
./sandbox.sh stop
```

`bootstrap` 需要 root（apt 安装和写入 `/usr/local/`），非 root 用户请在命令前加 `sudo`。默认使用 LTO；内存较少的主机可以加 `--no-lto`。同一版本已安装时会跳过编译；升级时换一个 tag 重跑：

```bash
./sandbox.sh bootstrap --qemu-version latest
```

QEMU 源码和构建目录位于 `/root/qemu-<tag>`。安装成功后默认删除其它 `qemu-*` 构建目录，保留当前版本；`--keep-old-builds` 可跳过清理。

### curl

```bash
# 编译 QEMU（需要 root）
curl -fsSL https://raw.githubusercontent.com/kebyn/sandbox/main/sandbox.sh \
  | sudo bash -s -- bootstrap --qemu-version v11.1.2 --lang zh

# 前台串口（交互式终端）
curl -fsSL https://raw.githubusercontent.com/kebyn/sandbox/main/sandbox.sh \
  | bash -s -- run --foreground --lang zh

# 后台启动
curl -fsSL https://raw.githubusercontent.com/kebyn/sandbox/main/sandbox.sh \
  | bash -s -- run --background --lang zh

# 正常关闭
curl -fsSL https://raw.githubusercontent.com/kebyn/sandbox/main/sandbox.sh \
  | bash -s -- stop
```

管道模式使用脚本内置的 user-data 模板，不依赖本地文件。`curl | bash` 会执行远程代码；生产环境建议先检查对应提交，或下载后运行。

## 语言

```bash
./sandbox.sh run --lang zh --dry-run
./sandbox.sh run --lang en --dry-run
VM_LANG=zh ./sandbox.sh run --dry-run
```

`--lang` 支持 `auto`、`zh`、`en`，默认 `auto`。`auto` 按 `LC_ALL` → `LC_MESSAGES` → `LANG` 的顺序判断，`zh*` 用中文，其余用英文。

## run 默认值

- 发行版：Debian 13
- vCPU：`min(4, nproc)`
- 内存：`min(8192, MemAvailable - 1024)` MiB
- 磁盘：`/data` 所在文件系统剩余空间的 90%
- 端口：SSH `8022`、HTTP `8080`、HTTPS `8443`，默认监听 IPv4 和 IPv6 的所有地址

登录：

```bash
ssh -p 8022 dev@127.0.0.1
```

如果 `~/user-data.yaml` 已经存在，脚本直接使用它，不修改内容，也不会自动知道其中配置的用户名和密钥；用 `--ssh-user` / `--ssh-key` 调整提示命令。文件不存在时，脚本会生成 `~/user-data.yaml` 和 `/data/id_ed25519`。

前台运行需要终端：

```bash
./sandbox.sh run
```

退出 QEMU：`Ctrl-A X`。

## 磁盘语义

默认每次启动都用基底镜像覆盖 `/data/disk.img`，guest 内的改动会丢失。这适合 sandbox。

保留上次的系统盘：

```bash
./sandbox.sh run --keep-disk --background
./sandbox.sh stop
```

强制重新下载基底镜像（工作盘仍按 `--keep-disk` 决定是否保留）：

```bash
./sandbox.sh run --fresh-image --background
```

## 常用参数

```bash
./sandbox.sh run --distro rocky --cpus 4 --mem 8192 --disk 100G
./sandbox.sh run --accel tcg                 # 没有 KVM 时
./sandbox.sh run --net-backend user          # passt 不可用时
./sandbox.sh run --bind-addr 127.0.0.1       # 只监听本机
./sandbox.sh run --foreground                # 管道模式前台串口
./sandbox.sh run --dry-run                   # 只打印 QEMU 命令
./sandbox.sh run --help                      # 全部选项
```

Rocky 镜像约 520 MB，Debian 镜像约 440 MB。`--disk` 默认按剩余空间动态计算，不会预分配全部空间。

## 管道模式与前台串口

- `bash <(curl ...) run`：stdin 仍是终端，直接前台运行：
  ```bash
  bash <(curl -fsSL https://raw.githubusercontent.com/kebyn/sandbox/main/sandbox.sh) run
  ```
- `curl ... | bash -s -- run --foreground`：把 QEMU stdin 接到 `/dev/tty`，需要控制终端和终端输出。`--foreground` 与 `--background` 互斥。
- `curl ... | bash -s -- run`：stdin 不是 TTY 时自动后台，行为和之前一致。
- CI、cron、`setsid` 等没有控制终端的环境使用 `--background`。
- 需要前台串口时，也可以先下载再运行：
  ```bash
  curl -fsSL https://raw.githubusercontent.com/kebyn/sandbox/main/sandbox.sh -o sandbox.sh
  chmod +x sandbox.sh
  ./sandbox.sh run
  ```
- `stop`、`--dry-run` 和 `bootstrap` 在管道模式下照常工作。

## 常见问题

**`passt` 不可用**：确认 `passt` 已安装，且 QEMU 的 `-netdev help` 包含 `passt`。`run` 默认回退到 user 网络；显式 `--net-backend passt` 会直接报错。

**KVM 不可用**：`/dev/kvm` 必须可读写。没有嵌套虚拟化时用 `--accel tcg`，启动会明显变慢。

**端口被占用**：脚本会在启动前检查 8022/8080/8443 并打印占用进程，可用 `--ssh-port` 等参数改端口。

**停止后台 VM**：优先用 `./sandbox.sh stop`，它通过 QEMU monitor 请求 ACPI 关机。直接 `kill` 或 `Ctrl-A X` 会强制结束 QEMU；如果之后要用 `--keep-disk`，未清理的 ext4 可能让 Debian 云镜像丢失 SSH host key，导致下次 `sshd` 无法启动。

**cloud-init 修改不生效**：`instance-id` 由 user-data 的哈希生成。修改 user-data 后重启即可；需要强制重建 seed 时加 `--recreate-seed`。
