# delete_expired_file

递归扫描指定目录，自动删除最后修改时间超过指定天数（默认 7 天）的过期文件。

## 使用示例

```bash
# 删除 /tmp 目录下超过 7 天的文件
$ ./delete_expired_file -p /tmp/
2020/12/31 05:10:19 Removed /tmp/v8-compile-cache-0 (modified at 2020-12-08 09:10:03 +0000 UTC)

# 自定义过期天数（如清理超过 30 天的文件）
$ ./delete_expired_file -p /var/log/app -d 30
```
