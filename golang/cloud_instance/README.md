# cloud_instance

管理**阿里云 ECS** 或**腾讯云 CVM** 抢占式/竞价（Spot）按量付费实例：
- **创建 (`create`)**：
  - **自动选型与库存查询**：默认按 **1核 1GiB (X86)** 自动筛选有库存且按性价比/实时单价优先级排序的实例规格（阿里云结合 `DescribeInstanceTypes` + `DescribeAvailableResource` + `DescribeSpotPriceHistory` 并自动匹配受支持的系统盘类型；腾讯云结合 `DescribeZoneInstanceConfigInfos` 按折后单价升序选择），支持通过 `-cpu`、`-memory`、`-type`（或对应环境变量）自定义目标核数、内存或优先规格。
  - **跨可用区自动回退与重试**：优先在指定交换机/可用区创建；当目标可用区无库存或遇到 `NoStock` / `ResourcesSoldOut` 时，阿里云自动查询同 VPC 下其他可用区的可用 VSwitch 进行跨可用区回退，腾讯云自动跨可用区与次优规格回退重试。
  - **网络与计费周期对齐**：已有公网 IP 时直接使用、无公网 IP 时自动按需分配（按流量计费 100Mbps），自动启动实例并随机生成符合复杂度要求的登录密码；自动释放/定时销毁时间以**实例创建时间**为基准（预留 2 分钟安全缓冲），避免运行时长跨入下一计费周期。
  - **最新可用区与最新公共镜像自动匹配**：腾讯云默认优先使用当前地域下最新开放的可用区（如 `ap-shanghai` 默认使用 `ap-shanghai-8`）；阿里云与腾讯云均默认自动匹配最新版本的 **AlmaLinux（如 10.x）** X86_64 公共镜像。
- **列出 (`list`)**：查看地域下实例的状态、公网/内网 IP、机型、创建时间及自动释放/定时销毁时间。
- **查询可用区 (`zones`)**：查看地域下可用区列表（`TENCENTCLOUD_ZONE` 可选值）及默认选中的最新可用区。
- **查询公共镜像 (`images`)**：查看地域下的 AlmaLinux 公共镜像列表及默认自动匹配的最新版本镜像。
- **延长 (`extend`)**：在当前自动释放时间（或当前计费周期）基础上顺延指定小时数。
- **删除 (`delete`)**：立即强制释放/销毁指定的一个或多个实例。

## 命令行参数

| 参数 | 默认值 | 说明 |
| :--- | :--- | :--- |
| `-provider` | `aliyun` | 云服务商：`aliyun`（阿里云 ECS）或 `tencent`（腾讯云 CVM） |
| `-action` | `create` | 操作类型：`create`（创建）、`list`（列出实例）、`zones`（查询可用区）、`images`（查询公共镜像）、`delete`（删除）、`extend`（延长释放时长） |
| `-region` | `cn-shanghai` / `ap-shanghai` | 地域 ID（`aliyun` 默认 `cn-shanghai`，`tencent` 默认 `ap-shanghai`） |
| `-id` | `""` | 实例 ID，多个 ID 用英文逗号分隔（`delete` / `extend` 必填，`list` 可选用于过滤） |
| `-hostname` | `example` | 实例名称与系统主机名（`create` 时生效） |
| `-releasetime` | `1` | 自动释放/定时销毁时长或延长时长，单位：小时（`create` / `extend` 时生效） |
| `-maxprice` | `0.98` | 抢占式/竞价实例每小时最高出价（`create` 时生效） |
| `-cpu` | `1` | 自动选型目标 vCPU 核数，仅筛选 X86 架构（`create` 时生效） |
| `-memory` | `1` | 自动选型目标内存大小，单位：GiB（`create` 时生效） |
| `-type` | `""` | 优先使用的指定实例规格（可选；`create` 时有库存优先选用，`images` 时可用于筛选该规格支持的镜像） |

## 环境变量

### 1. 阿里云 (`-provider aliyun`)

```bash
export ALIBABA_CLOUD_ACCESS_KEY_ID="your-access-key-id"
export ALIBABA_CLOUD_ACCESS_KEY_SECRET="your-access-key-secret"

# 仅 -action create 时必需
export ALIBABA_CLOUD_SECURITY_GROUP_ID="sg-xxxxxxxx"

# 可选环境变量：
# 指定首选交换机（若该交换机所在可用区无库存，会自动查询安全组所属 VPC 下其他可用区的可用 VSwitch 回退）
export ALIBABA_CLOUD_VSWITCH_ID="vsw-xxxxxxxx"
export ALIBABA_CLOUD_CPU="1"
export ALIBABA_CLOUD_MEMORY="1"
export ALIBABA_CLOUD_INSTANCE_TYPE="ecs.t6-c1m1.large"
export ALIBABA_CLOUD_IMAGE_ID="almalinux_10_1_x64_20G_alibase_xxxxxxxx.vhd"
```

### 2. 腾讯云 (`-provider tencent`)

```bash
export TENCENTCLOUD_SECRET_ID="your-secret-id"
export TENCENTCLOUD_SECRET_KEY="your-secret-key"

# 可选环境变量（未设置时默认优先使用对应 region 的最新可用区如 ap-shanghai-8、DEFAULT 网络及自动匹配最新 AlmaLinux 10.x 公共镜像）
export TENCENTCLOUD_ZONE="ap-shanghai-8"
export TENCENTCLOUD_VPC_ID="vpc-xxxxxxxx"
export TENCENTCLOUD_SUBNET_ID="subnet-xxxxxxxx"
export TENCENTCLOUD_SECURITY_GROUP_ID="sg-xxxxxxxx"
export TENCENTCLOUD_CPU="1"
export TENCENTCLOUD_MEMORY="1"
export TENCENTCLOUD_INSTANCE_TYPE="SA2.SMALL1"
export TENCENTCLOUD_IMAGE_ID="img-xxxxxxxx"
```

## 使用示例

```bash
# 1. 创建实例：默认按 1核 1GiB (X86) 自动筛选高性价比有库存规格并在无库存时跨可用区回退
go run main.go -provider aliyun -region cn-shanghai -hostname dev-box -releasetime 1 -maxprice 0.50

# 自定义 2核 4GiB 规格创建阿里云或腾讯云竞价实例
go run main.go -provider aliyun -region cn-shanghai -cpu 2 -memory 4 -maxprice 0.80
go run main.go -provider tencent -region ap-shanghai -cpu 2 -memory 4 -releasetime 2 -maxprice 0.60

# 2. 查询可用区（TENCENTCLOUD_ZONE）与最新 AlmaLinux 公共镜像
go run main.go -provider tencent -region ap-shanghai -action zones
go run main.go -provider tencent -region ap-shanghai -action images
go run main.go -provider aliyun -region cn-shanghai -action images

# 3. 列出实例：查看当前地域下的实例及自动释放时间
go run main.go -provider aliyun -region cn-shanghai -action list
go run main.go -provider tencent -region ap-shanghai -action list

# 4. 延长实例时长：将指定实例的自动释放/销毁时间顺延 2 小时
go run main.go -provider aliyun -region cn-shanghai -action extend -id i-bp1xxxxxx -releasetime 2
go run main.go -provider tencent -region ap-shanghai -action extend -id ins-xxxxxx -releasetime 2

# 5. 删除实例：立即强制释放/销毁一个或多个实例
go run main.go -provider aliyun -region cn-shanghai -action delete -id i-bp1xxxxxx,i-bp1yyyyyy
go run main.go -provider tencent -region ap-shanghai -action delete -id ins-xxxxxx
```
