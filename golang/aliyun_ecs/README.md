# aliyun_ecs

自动创建阿里云抢占式（Spot）按量付费 ECS 实例，自动分配公网 IP、启动实例并设置定时自动释放时间，同时随机生成符合复杂度要求的登录密码。

## 环境变量

运行前需配置以下环境变量：

```bash
export ALIBABA_CLOUD_ACCESS_KEY_ID="your-access-key-id"
export ALIBABA_CLOUD_ACCESS_KEY_SECRET="your-access-key-secret"
export ALIBABA_CLOUD_SECURITY_GROUP_ID="sg-xxxxxxxx"
export ALIBABA_CLOUD_VSWITCH_ID="vsw-xxxxxxxx"
```

## 使用示例

```bash
# 在 cn-shanghai 创建主机名为 dev-box、1 小时后自动释放的 ECS 实例
go run main.go -region cn-shanghai -hostname dev-box -releasetime 1
```
