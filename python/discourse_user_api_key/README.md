# discourse_user_api_key

用于向 Discourse 站点（默认 `https://linux.do`）申请、RSA 解密、验证并保存 User API Key 的命令行工具。支持 `auto`、`device`、`browser` 和 `paste` 四种授权流程。

## 依赖

```bash
pip install cryptography
```

## 使用示例

```bash
# 使用默认配置申请只读 User API Key
python3 discourse_user_api_key.py

# 指定站点、权限范围、授权方式及输出文件路径
python3 discourse_user_api_key.py \
  --site https://linux.do \
  --name "My CLI Tool" \
  --scopes read,notifications \
  --flow auto \
  --expires 30d \
  --output ./discourse-user-api-key.json
```

## 参数说明

| 参数 | 默认值 | 说明 |
| :--- | :--- | :--- |
| `--site` | `https://linux.do` | Discourse 站点 URL |
| `--name` | `Sample Discourse App` | 展示在授权页面的应用名称 |
| `--scopes` | `read` | 逗号分隔的权限列表 |
| `--flow` | `auto` | 授权流程：`auto`、`device`、`browser`、`paste` |
| `--padding` | `oaep` | RSA 填充模式：`oaep` 或 `pkcs1` |
| `--expires` | 无 | 可选有效期，如 `7d`、`12h`、`30m` 或秒数 |
| `--output` | `./discourse-user-api-key.json` | 保存密钥的 JSON 文件路径（权限 `0600`） |
| `--force` | `False` | 覆盖已存在的输出文件 |
