# simple_http_server_upload

基于 Python 3 标准库实现的轻量级 HTTP/HTTPS 文件服务器，支持目录浏览、文件下载（`GET`/`HEAD`）以及网页端表单文件上传（`POST`）。

## 使用示例

```bash
# 默认在 0.0.0.0:8000 启动 HTTP 服务
python3 simple_http_server_upload.py

# 指定监听地址与端口
python3 simple_http_server_upload.py -l 127.0.0.1 -p 8080

# 启用 HTTPS（需当前目录存在 crt.pem 和 key.pem）
python3 simple_http_server_upload.py -s -p 8443
```
