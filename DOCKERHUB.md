# VoHive

4G/5G 模组管理平台 - 支持移远 EC20/EC25/RM500Q 等移远模组的统一管理与代理服务。

## 🚀 快速开始

### 1. 创建配置目录

```bash
mkdir -p vohive/{config,data,logs}
cd vohive
```

### 2. 创建配置文件

```bash
cat > config/config.yaml << 'EOF'
server:
  port: 7575
  debug: false

# 可管理设备数上限，0 表示无限制
free_device_limit: 5

web:
  username: admin
  # 首次登录后请在 Web 界面修改密码
  password: admin123

EOF
```

### 3. 使用 Docker Compose 启动

创建 `docker-compose.yml`:

```yaml
services:
  vohive:
    image: ghcr.io/zanescope/vohive:latest
    container_name: vohive
    restart: unless-stopped
    ports:
      - "7575:7575"
    volumes:
      # 配置文件 (首次运行需创建)
      - ./config:/app/config
      - ./data:/app/data
      # 日志目录
      - ./logs:/app/logs
    environment:
      - TZ=Asia/Shanghai
      - CONFIG_PATH=/app/config/config.yaml
      # 代理服务器,可选
      - HTTPS_PROXY=http://proxy-ip:port
    # 需要访问宿主机设备时启用以下配置
    privileged: true
    devices:
      # USB 设备透传
      - /dev/:/dev/
    network_mode: host
```

启动服务：

```bash
docker-compose up -d
```

### 4. 访问 Web 界面

打开浏览器访问: `http://YOUR_IP:7575`

默认账号: `admin` / `admin123`

## 📦 镜像标签

| 标签 | 说明 |
|------|------|
| `latest` | 最新稳定版 |
| `vX.X.X` | 指定版本号 |
| `main` | 开发版 (可能不稳定) |

## 🔧 环境变量

| 变量 | 默认值 | 说明 |
|------|--------|------|
| `TZ` | `UTC` | 时区 |
| `CONFIG_PATH` | `/app/config/config.yaml` | 配置文件路径 |

## 📁 数据卷

| 路径 | 说明 |
|------|------|
| `/app/config` | 配置文件目录 |
| `/app/data` | 数据库存储 |
| `/app/logs` | 日志文件 |

## 🤖 Telegram Bot

支持通过 Telegram Bot 远程管理设备。在 Web 界面 **设置 → 通知** 中配置。

### 配置步骤

1. 通过 [@BotFather](https://t.me/BotFather) 创建 Bot，获取 Token
2. 获取你的 Chat ID（可通过 [@userinfobot](https://t.me/userinfobot) 查询）
3. 在 VoHive 设置页面填入 Bot Token 和 Chat ID
4. 如服务器无法直连 Telegram，填写 TG API 代理地址

### 支持的命令

| 命令 | 说明 |
|------|------|
| `/list` | 列出设备列表 |
| `/rotate <设备>` | 重置设备 IP |
| `/sms <设备>` | 查看最近短信 |
| `/send <设备> <号码> <内容>` | 发送短信 |

### 代理配置

中国大陆服务器需要配置代理才能访问 Telegram API：

```yaml
environment:
  - HTTPS_PROXY=http://your-proxy:port
```

或在 Web 界面的 **TG API 代理** 字段填写 Cloudflare Worker 地址。

## 🖥️ 支持架构

- `linux/amd64` (x86_64)
- `linux/arm64` (ARM64/aarch64)

## 📖 文档

完整文档请访问: [GitHub](https://github.com/zanescope/vohive)

## 📝 License

PolyForm Noncommercial License 1.0.0; see [LICENSE](LICENSE).
