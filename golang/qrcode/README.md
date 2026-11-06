# qrcode

基于 [`skip2/go-qrcode`](https://github.com/skip2/go-qrcode) 的命令行二维码生成工具，支持输出 PNG 图片文件、标准输出流或终端字符画（Text-art）。

## 使用示例

```bash
# 生成默认 qrcode.png (1024x1024)
$ ./qrcode "https://github.com/kebyn/mini-scripts"

# 直接在终端输出二维码字符画
$ ./qrcode -t "https://github.com/kebyn/mini-scripts"

# 指定输出文件名前缀与尺寸
$ ./qrcode -o my_qr -s 512 "Hello World"
```
