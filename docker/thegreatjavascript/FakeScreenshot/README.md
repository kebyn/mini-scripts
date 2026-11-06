# FakeScreenshot

基于 `node:8-alpine` 构建的 [thegreatjavascript/FakeScreenshot](https://github.com/thegreatjavascript/FakeScreenshot) 容器镜像。

## 快速开始

```bash
docker run --name fakescreenshot --publish 8000:8000 --rm -d kebyn/fakescreenshot
```

启动后通过浏览器访问 `http://<IP>:8000`（注意：需使用 IP 地址访问，不支持直接使用域名）。
