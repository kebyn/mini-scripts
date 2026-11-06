# dnsutils

提供包含 DNS 诊断工具（`dig`、`nslookup`、`host`、`nsupdate`）的轻量容器镜像，支持 Ubuntu 与 Alpine 两个版本。

## 构建与使用

```bash
# 构建 Alpine 轻量版
docker build -f alpine.Dockerfile -t dnsutils:alpine .

# 构建 Ubuntu 版
docker build -f Dockerfile -t dnsutils:ubuntu .

# 运行 dig 查询
docker run --rm dnsutils:alpine dig example.com
```
