# fio

基于 CentOS 7 构建的磁盘 I/O 性能压测工具（`fio` + `libaio-devel`）容器镜像。

## 构建与使用

```bash
docker build -t fio:centos7 .
docker run --rm fio:centos7 fio --version
```
