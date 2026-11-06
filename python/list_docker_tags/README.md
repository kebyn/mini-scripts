# list_docker_tags

通过 Docker Hub Registry v2 API 查询一个或多个镜像的所有标签（Tag）列表，无需本地安装 Docker。

## 使用示例

```bash
# 查询单个官方镜像标签
python3 list_docker_tags.py alpine

# 查询多个镜像并格式化缩进输出 JSON
python3 list_docker_tags.py -i nginx kebyn/tdengine
```
