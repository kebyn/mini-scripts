# sandbox

用 QEMU + KVM + passt 启动 Debian 13 / Rocky 10 云镜像的脚本。

## 文件

- `bootstrap.sh`：全新主机上安装编译依赖，从源码编译 QEMU 并安装到 `/usr/local/`。
- `run-vm.sh`：下载基底镜像、重置工作盘、生成 cloud-init seed、启动 QEMU。
- `user-data.yaml.tmpl`：`~/user-data.yaml` 不存在时用于生成默认配置。
- `.gitignore`：镜像、磁盘、seed、日志和密钥不进入仓库。

## 首次准备

```bash
./bootstrap.sh --qemu-version v11.1.2 --jobs "$(nproc)"
```

`bootstrap.sh` 需要 root（apt 安装和写入 `/usr/local/`），非 root 用户请在命令前加 `sudo`。默认使用 LTO。内存较少的主机可以加 `--no-lto`。同一版本已安装时会跳过编译；升级时换一个 tag 重跑：

```bash
./bootstrap.sh --qemu-version latest
```

QEMU 源码和构建目录位于 `/root/qemu-<tag>`。安装成功后默认删除其它 `qemu-*` 构建目录，保留当前版本；`--keep-old-builds` 可跳过清理。

## 启动

```bash
./run-vm.sh --background
```

默认值：

- 发行版：Debian 13
- vCPU：`min(4, nproc)`
- 内存：`min(8192, MemAvailable - 1024)` MiB
- 磁盘：`/data` 所在文件系统剩余空间的 90%
- 端口：SSH `8022`、HTTP `8080`、HTTPS `8443`，监听 IPv4 和 IPv6 的所有地址

登录：

```bash
ssh -p 8022 dev@127.0.0.1
```

如果 `~/user-data.yaml` 已经存在，脚本直接使用它，不修改内容，也不会自动知道其中配置的用户名和密钥；用 `--ssh-user` / `--ssh-key` 调整提示命令。文件不存在时，脚本会生成 `~/user-data.yaml` 和 `/data/id_ed25519`。

前台运行（串口接终端）：

```bash
./run-vm.sh
```

退出 QEMU：`Ctrl-A X`。

后台运行时优先用正常关机：

```bash
./run-vm.sh --stop
```

`--stop` 通过 QEMU monitor 发送 ACPI poweroff 并等待进程退出。直接 `kill` 或 `Ctrl-A X` 会强制结束 QEMU；如果之后要用 `--keep-disk`，未清理的 ext4 可能让 Debian 云镜像丢失 SSH host key，导致下次 `sshd` 无法启动。

## 磁盘语义

默认每次启动都用基底镜像覆盖 `/data/disk.img`，guest 内的改动会丢失。这适合 sandbox。

保留上次的系统盘：

```bash
./run-vm.sh --keep-disk --background
./run-vm.sh --stop
```

强制重新下载基底镜像（工作盘仍按 `--keep-disk` 决定是否保留）：

```bash
./run-vm.sh --fresh-image --background
```

## 常用参数

```bash
./run-vm.sh --distro rocky --cpus 4 --mem 8192 --disk 100G
./run-vm.sh --accel tcg                 # 没有 KVM 时
./run-vm.sh --net-backend user          # passt 不可用时
./run-vm.sh --bind-addr 127.0.0.1       # 只监听本机
./run-vm.sh --dry-run                   # 只打印 QEMU 命令
```

Rocky 镜像约 520 MB，Debian 镜像约 440 MB。`--disk` 默认按剩余空间动态计算，不会预分配全部空间。

## 常见问题

**`passt` 不可用**：确认 `passt` 已安装，且 QEMU 的 `-netdev help` 包含 `passt`。`run-vm.sh` 默认会回退到 user 网络；显式 `--net-backend passt` 会直接报错。

**KVM 不可用**：`/dev/kvm` 必须可读写。没有嵌套虚拟化时用 `--accel tcg`，启动会明显变慢。

**端口被占用**：脚本会在启动前检查 8022/8080/8443 并打印占用进程，可用 `--ssh-port` 等参数改端口。

**cloud-init 修改不生效**：`instance-id` 由 user-data 的哈希生成。修改 user-data 后重启即可；需要强制重建 seed 时加 `--recreate-seed`。
