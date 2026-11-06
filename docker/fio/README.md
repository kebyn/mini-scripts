# fio

基于 AlmaLinux 9 构建的磁盘 I/O 性能压测工具（`fio` + `libaio-devel`）容器镜像。

## 构建与使用

```bash
docker build -t fio:almalinux9 .
docker run --rm fio:almalinux9 fio --version
```
