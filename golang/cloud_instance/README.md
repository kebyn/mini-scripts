# cloud_instance

自动创建**阿里云 ECS** 或**腾讯云 CVM** 抢占式/竞价（Spot）按量付费实例，自动分配公网 IP（按流量计费 100Mbps）、启动实例并设置定时自动释放/销毁时间，同时随机生成符合复杂度要求的登录密码。

## 命令行参数

| 参数 | 默认值 | 说明 |
| :--- | :--- | :--- |
| `-provider` | `aliyun` | 云服务商：`aliyun`（阿里云 ECS）或 `tencent`（腾讯云 CVM） |
| `-region` | `cn-shanghai` / `ap-shanghai` | 地域 ID（`aliyun` 默认 `cn-shanghai`，`tencent` 默认 `ap-shanghai`） |
| `-hostname` | `example` | 实例名称与系统主机名 |
| `-releasetime` | `1` | 自动释放/定时销毁时长（单位：小时） |

## 环境变量

### 1. 阿里云 (`-provider aliyun`)

```bash
export ALIBABA_CLOUD_ACCESS_KEY_ID="your-access-key-id"
export ALIBABA_CLOUD_ACCESS_KEY_SECRET="your-access-key-secret"
export ALIBABA_CLOUD_SECURITY_GROUP_ID="sg-xxxxxxxx"
export ALIBABA_CLOUD_VSWITCH_ID="vsw-xxxxxxxx"
```

### 2. 腾讯云 (`-provider tencent`)

```bash
export TENCENTCLOUD_SECRET_ID="your-secret-id"
export TENCENTCLOUD_SECRET_KEY="your-secret-key"
export TENCENTCLOUD_ZONE="ap-shanghai-2"

# 可选环境变量（未设置时默认使用 DEFAULT 网络、SA2.MEDIUM2 机型及自动匹配 AlmaLinux 公共镜像）
export TENCENTCLOUD_VPC_ID="vpc-xxxxxxxx"
export TENCENTCLOUD_SUBNET_ID="subnet-xxxxxxxx"
export TENCENTCLOUD_SECURITY_GROUP_ID="sg-xxxxxxxx"
export TENCENTCLOUD_INSTANCE_TYPE="SA2.MEDIUM2"
export TENCENTCLOUD_IMAGE_ID="img-xxxxxxxx"
```

## 使用示例

```bash
# 默认在阿里云 cn-shanghai 创建主机名为 dev-box、1 小时后自动释放的抢占式 ECS 实例
go run main.go -region cn-shanghai -hostname dev-box -releasetime 1

# 在腾讯云 ap-shanghai 创建主机名为 dev-box、2 小时后自动销毁的竞价 CVM 实例
go run main.go -provider tencent -region ap-shanghai -hostname dev-box -releasetime 2
```
