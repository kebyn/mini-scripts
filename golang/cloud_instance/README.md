# cloud_instance

管理**阿里云 ECS** 或**腾讯云 CVM** 抢占式/竞价（Spot）按量付费实例：
- **创建 (`create`)**：支持自定义最高出价（`-maxprice`），已有公网 IP 时直接使用、无公网 IP 时自动按需分配（按流量计费 100Mbps），自动启动实例并随机生成符合复杂度要求的登录密码；自动释放/定时销毁时间以**实例创建时间**为基准（预留 2 分钟安全缓冲），避免运行时长跨入下一计费周期。
- **列出 (`list`)**：查看地域下实例的状态、公网/内网 IP、机型、创建时间及自动释放/定时销毁时间。
- **延长 (`extend`)**：在当前自动释放时间（或当前计费周期）基础上顺延指定小时数。
- **删除 (`delete`)**：立即强制释放/销毁指定的一个或多个实例。

## 命令行参数

| 参数 | 默认值 | 说明 |
| :--- | :--- | :--- |
| `-provider` | `aliyun` | 云服务商：`aliyun`（阿里云 ECS）或 `tencent`（腾讯云 CVM） |
| `-action` | `create` | 操作类型：`create`（创建）、`list`（列出）、`delete`（删除）、`extend`（延长释放时长） |
| `-region` | `cn-shanghai` / `ap-shanghai` | 地域 ID（`aliyun` 默认 `cn-shanghai`，`tencent` 默认 `ap-shanghai`） |
| `-id` | `""` | 实例 ID，多个 ID 用英文逗号分隔（`delete` / `extend` 必填，`list` 可选用于过滤） |
| `-hostname` | `example` | 实例名称与系统主机名（`create` 时生效） |
| `-releasetime` | `1` | 自动释放/定时销毁时长或延长时长，单位：小时（`create` / `extend` 时生效） |
| `-maxprice` | `0.98` | 抢占式/竞价实例每小时最高出价（`create` 时生效） |

## 环境变量

### 1. 阿里云 (`-provider aliyun`)

```bash
export ALIBABA_CLOUD_ACCESS_KEY_ID="your-access-key-id"
export ALIBABA_CLOUD_ACCESS_KEY_SECRET="your-access-key-secret"

# 仅 -action create 时必需
export ALIBABA_CLOUD_SECURITY_GROUP_ID="sg-xxxxxxxx"
export ALIBABA_CLOUD_VSWITCH_ID="vsw-xxxxxxxx"
```

### 2. 腾讯云 (`-provider tencent`)

```bash
export TENCENTCLOUD_SECRET_ID="your-secret-id"
export TENCENTCLOUD_SECRET_KEY="your-secret-key"

# 仅 -action create 时必需
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
# 1. 创建实例：在阿里云 cn-shanghai 创建最高出价 0.50、1 小时计费周期内自动释放的抢占式 ECS 实例
go run main.go -provider aliyun -region cn-shanghai -hostname dev-box -releasetime 1 -maxprice 0.50

# 在腾讯云 ap-shanghai 创建最高出价 0.60、2 小时计费周期内自动销毁的竞价 CVM 实例
go run main.go -provider tencent -region ap-shanghai -hostname dev-box -releasetime 2 -maxprice 0.60

# 2. 列出实例：查看当前地域下的实例及自动释放时间
go run main.go -provider aliyun -region cn-shanghai -action list
go run main.go -provider tencent -region ap-shanghai -action list

# 3. 延长实例时长：将指定实例的自动释放/销毁时间顺延 2 小时
go run main.go -provider aliyun -region cn-shanghai -action extend -id i-bp1xxxxxx -releasetime 2
go run main.go -provider tencent -region ap-shanghai -action extend -id ins-xxxxxx -releasetime 2

# 4. 删除实例：立即强制释放/销毁一个或多个实例
go run main.go -provider aliyun -region cn-shanghai -action delete -id i-bp1xxxxxx,i-bp1yyyyyy
go run main.go -provider tencent -region ap-shanghai -action delete -id ins-xxxxxx
```
