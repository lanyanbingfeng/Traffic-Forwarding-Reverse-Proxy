# 局域网 TCP 53 流量转发工具

本项目提供两种互斥的服务模式：

- **FlClash 直连（推荐）**：服务端运行 `flclash-server.exe`，客户端只需导入 YAML 并启动 FlClash；本地端口保持默认 `7890`。
- **自定义隧道（备用）**：继续使用 `tunnel-client.exe` 和 `tunnel-server.exe`，本地 SOCKS5 端口为 `1080`。

两种服务默认都监听 TCP 53，不能同时启动。以下先介绍推荐的 FlClash 直连方式，原隧道方式保留在后续章节。

## FlClash 直连模式

```text
FlClash(mixed-port 7890) --[SOCKS5 over TLS / TCP 53]--> 跳板机 --> 互联网
```

该模式使用标准 SOCKS5、RFC 1929 用户名密码认证和 TLS。客户端不需要运行本项目的任何程序。

### 两台新电脑的最简流程

服务端电脑首次 clone 后，直接双击 `start-flclash-console.bat`：

1. 脚本自动申请管理员权限；
2. 如果没有 Go，优先通过 Windows `winget` 自动安装；
3. 自动编译并安装服务端；
4. 自动检测服务端局域网 IPv4，确认用户名和密码；
5. 在服务器桌面生成包含服务端 IP、TCP 端口、账号和证书指纹的 `FlClash-direct.yaml`；
6. 服务端进入窗口日志模式并开始监听 TCP 53。

客户端电脑不需要 clone 本项目，只需安装 FlClash。把服务器生成的 `FlClash-direct.yaml` 复制到客户端，导入 FlClash，点击启动并开启“系统代理”即可。

如果服务端以前已经安装过旧版本，但桌面没有 YAML，请更新项目后再次双击 `start-flclash-console.bat`。启动器会保留原来的账号、密码和证书，只询问服务端局域网 IP，然后在桌面补生成 `FlClash-direct.yaml`。

`start-flclash-background.bat` 会自动升级旧计划任务，并在退出前确认后台进程确实已经监听端口。后台任务允许笔记本在使用电池时启动和继续运行；若启动失败，窗口会显示计划任务结果码，并提示改用窗口日志入口查看具体原因。

从项目目录双击任一启动入口时，启动器也会检查源码和已安装 exe 是否一致。`git pull` 获得新版本后无需手动重新安装：需要时会自动编译、停止旧进程、替换程序，再启动所选模式。

构建脚本始终使用项目根目录寻找 `go.mod`，不依赖管理员窗口当前位于哪个目录；任意一个程序构建失败都会立即停止，不再错误显示 `Build OK`。

Windows 防火墙默认仅允许本地子网访问代理端口。客户端和服务端应位于同一局域网子网；如果中间经过路由、VLAN 或公网，不要直接放宽到任意来源，应只添加实际客户端网段。

### 服务端一次性安装（Windows）

以下是需要手动控制时的完整安装方式；使用上面的双击入口时会自动完成编译。

1. 编译三个程序：

```powershell
powershell -ExecutionPolicy Bypass -File build.ps1
```

2. 以管理员身份打开 PowerShell，在项目目录运行：

```powershell
powershell -ExecutionPolicy Bypass -File install-flclash-server.ps1
```

3. 按提示填写：

   - 监听地址，通常直接回车使用 `0.0.0.0:53`；
   - 跳板机的稳定局域网 IP，例如 `192.168.0.109`；
   - SOCKS5 用户名；
   - 至少 12 位的随机密码。

安装脚本会完成以下操作：

- 安装程序与配置到 `%ProgramData%\TunnelProxy`；
- 首次生成并持久保存 `tunnel.local` 自签名证书；
- 锁定配置和私钥的 Windows ACL；
- 注册并启动 `TunnelProxy-FlClash-Server` 开机计划任务；
- 添加对应 TCP 监听端口的 Windows 入站防火墙规则；
- 在当前管理员用户桌面生成 `FlClash-direct.yaml`。

把生成的 YAML 安全地复制到客户端，在 FlClash 中选择“添加配置 → 本地文件”，导入后点击启动，再打开“系统代理”或“虚拟网卡”。YAML 的 `mixed-port` 固定为 `7890`。

安装完成后，服务器桌面会生成两个双击入口：

- `代理服务-后台运行`：无日志窗口，服务在后台长期运行并随系统启动；
- `代理服务-窗口日志`：暂停后台任务，打开一个实时窗口显示客户端访问的域名和端口。

两个模式都使用 TCP 53，启动其中一个时会自动停止另一个。项目目录也保留了 `start-flclash-background.bat` 和 `start-flclash-console.bat`，可以直接双击使用。首次尚未安装时，双击入口会自动进入一次性配置。

### 服务状态管理

```powershell
# 查看状态（只读）
powershell -ExecutionPolicy Bypass -File manage-flclash-server.ps1 -Action status

# 启动或停止
powershell -ExecutionPolicy Bypass -File manage-flclash-server.ps1 -Action start
powershell -ExecutionPolicy Bypass -File manage-flclash-server.ps1 -Action stop
```

服务配置文件字段如下：

```json
{
  "listen": "0.0.0.0:53",
  "username": "flclash",
  "password": "替换为随机密码",
  "cert_file": "C:\\ProgramData\\TunnelProxy\\server.crt",
  "key_file": "C:\\ProgramData\\TunnelProxy\\server.key",
  "handshake_timeout": "15s",
  "idle_timeout": "10m",
  "max_connections": 512
}
```

也可不安装计划任务，直接运行：

```powershell
dist\flclash-server.exe -config "C:\ProgramData\TunnelProxy\server.json"
```

> 第一版仅转发 TCP。YAML 设置了 `udp: false`；网页、HTTPS 和下载可以使用，游戏、语音、QUIC 等依赖 UDP 的流量暂不支持。

## 自定义隧道模式（备用）

利用局域网 DNS 服务端口（**TCP 53**）建立隧道，在本机普通出站端口被封、但 53 端口可用的网络环境下（如校园网、公司网），把本机流量转发到局域网内的一台跳板机，由跳板机真实出网访问互联网。

## 原理

```
本机(普通出站端口被封) --[TCP 53 隧道]--> 跳板机(局域网, 能出网) --[正常出网]--> 互联网
 浏览器设 SOCKS5 代理          反向代理客户端                反向代理服务端
```

- 本机跑**客户端**，监听 `127.0.0.1:1080`（SOCKS5），浏览器/系统代理指向它；
- 客户端把收到的代理流量封装成带完整性校验的加密隧道帧，经 **TCP 53 端口**发往局域网跳板机；
- 跳板机跑**服务端**，监听 53 端口，解帧还原 SOCKS5 请求，建立真实出站连接访问目标，并把响应原路返回。

因为本机与跳板机之间走的是局域网路径，仅端口被限制、内容不检查，所以裸 TCP 走 53 端口吞吐与延迟都接近正常网络，适合日常浏览上网。

## 目录结构

```
├── main.go                    # 双端 CLI 入口
├── build.ps1                  # Windows 一键编译脚本
├── start-client.bat           # 本机客户端双击启动脚本（按提示输入跳板机IP）
├── start-server.bat           # 跳板机服务端双击启动脚本（需管理员运行）
├── internal/
│   ├── transport/             # 53 端口隧道传输层（帧编解码、连接复用、AES-GCM 加密）
│   └── socks/                 # SOCKS5 代理层（本地监听 / 服务端会话处理）
├── install-flclash-server.ps1 # 安装 FlClash 长期服务并生成客户端 YAML
├── manage-flclash-server.ps1  # 查看、启动或停止 Windows 长期服务
└── dist/                      # 编译产物（可选，通常不入库，clone 后自行编译）
    ├── tunnel-client
    └── tunnel-server
```

## 环境要求

- 开发/编译：Go 1.21+（Windows 可 `winget install --id GoLang.Go`，或用官网安装包；`build.ps1` 会自动回退到 `%LOCALAPPDATA%\GoToolchain` 解压的 Go）
- 运行：无需任何依赖，单文件二进制直接运行（Linux/macOS 需 `chmod +x` 赋予执行权限）
- 权限：服务端监听 53 端口（端口 <1024）在 Linux/macOS 需 `root`，Windows 需**以管理员身份运行**
- 若跳板机的 53 端口被系统 DNS 服务占用，需先停用/让出（见下方"注意事项"）

## 从 Clone 到运行（另一台电脑使用）

本工具仅依赖 Go 标准库，**无任何第三方依赖**，clone 后任意系统（Windows / Linux / macOS）都能编译运行。

### 第 1 步：克隆项目

```bash
git clone <仓库地址> 流量转发代理服务
cd 流量转发代理服务
```

> 提示：仓库默认**不包含** `dist/` 编译产物（避免二进制入库），clone 后需自行编译，见下一步。

### 第 2 步：安装 Go（仅首次需要）

- **官网下载**：https://go.dev/dl/ 下载对应系统的 Go 1.21+ 并安装；
- **Windows**：可用 `winget install --id GoLang.Go`；
- 验证安装：终端执行 `go version`，能输出版本号即成功。

### 第 3 步：编译双端二进制

在项目根目录执行：

```bash
# 方式 A：Windows 一键编译
powershell -ExecutionPolicy Bypass -File build.ps1

# 方式 B：任意系统通用编译（Linux / macOS / Windows 均可）
go build -trimpath -ldflags "-s -w -X main.defaultMode=client" -o tunnel-client .
go build -trimpath -ldflags "-s -w -X main.defaultMode=server"  -o tunnel-server .
```

两种方式产物等价。生成的 `tunnel-client` / `tunnel-server` 即可直接运行，无需任何依赖。

Windows `build.ps1` 还会生成 `dist/flclash-server.exe`，用于前述 FlClash 直连模式。

### 第 4 步：部署并运行

把 `tunnel-server` 放到**跳板机**（局域网内能出网的主机），`tunnel-client` 放到**本机**。

**方式 A：命令行（任意系统）**

```bash
# 跳板机（管理员/root 权限）
sudo ./tunnel-server -listen 0.0.0.0:53 -key 你的口令

# 本机（连到跳板机的局域网 IP）
./tunnel-client -server 192.168.1.100:53 -listen 127.0.0.1:1080 -key 你的口令
```

**方式 B：Windows 双击脚本（不用记命令行）**

项目自带两个脚本，双击即可启动：

- **跳板机**：右键 → **以管理员身份运行** `start-server.bat`（监听 53 端口需管理员权限）
- **本机**：双击 `start-client.bat`，按提示**输入跳板机 IP**（如 `192.168.0.109:53`，直接回车用默认值），即可连上跳板机

> 启动脚本会提示输入隧道口令，两端必须完全一致。窗口会保持打开显示日志。

### 第 5 步：本机设置代理

浏览器/系统设置 **SOCKS5 代理 `127.0.0.1:1080`**，即可开始上网（详见下方"快速开始"）。

> 注意：同一时刻只能有一个服务端/客户端在运行。若 `-key` 两端不一致，服务端会拒绝客户端接入并提示握手失败。

## 快速开始

### 1. 编译（若无现成二进制）

```powershell
powershell -ExecutionPolicy Bypass -File build.ps1
```

产物生成到 `dist/`：
- `tunnel-client.exe` —— 默认以**客户端**模式运行
- `tunnel-server.exe` —— 默认以**服务端**模式运行

两个二进制是同一程序，也可用 `-mode client/server` 显式指定角色。

### 2. 服务端（跳板机，管理员权限）

假设跳板机局域网 IP 为 `192.168.1.100`：

```bash
# Linux/macOS (需 root)
sudo ./tunnel-server -listen 0.0.0.0:53 -key 你的口令

# Windows (管理员身份)
tunnel-server.exe -listen 0.0.0.0:53 -key 你的口令
```

### 3. 客户端（本机）

```bash
tunnel-client.exe -server 192.168.1.100:53 -listen 127.0.0.1:1080 -key 你的口令
```

### 4. 本机设置代理

浏览器或系统代理设置为 **SOCKS5 代理 `127.0.0.1:1080`**：

- **Chrome/Edge**：安装支持 SOCKS5 的代理插件，或启动时加 `--proxy-server="socks5://127.0.0.1:1080"`
- **Firefox**：设置 → 网络设置 → 手动代理 → SOCKS 主机 `127.0.0.1`，端口 `1080`
- **系统级**（Windows）：设置 → 网络和 Internet → 代理 → 手动设置，代理地址 `127.0.0.1`，端口 `1080`，并勾选"对本地地址不使用代理"

设置后浏览器流量即经隧道转发到跳板机出网。

## 命令行参数

两端共用参数，通过 `-mode` 区分角色（或用默认角色二进制省略）：

| 参数 | 说明 | 默认 |
|------|------|------|
| `-mode` | 运行模式：`client` 或 `server` | 编译期注入 |
| `-server` | 跳板机地址（仅客户端，如 `192.168.1.100:53`） | 必填 |
| `-listen` | 监听地址：客户端 `127.0.0.1:1080`；服务端 `0.0.0.0:53` | 见说明 |
| `-key` | 隧道加密和认证口令，两端**必须一致**；建议使用至少 16 位随机口令 | 空 |
| `-retry` | 客户端连接失败后的重试间隔（秒） | `5` |
| `-allow-insecure` | 仅服务端：显式允许无口令明文运行，不推荐 | `false` |

示例：

```bash
# 客户端，带口令，指定本地端口
tunnel-client.exe -server 192.168.1.100:53 -listen 127.0.0.1:1080 -key mysecret

# 服务端，带口令
tunnel-server.exe -listen 0.0.0.0:53 -key mysecret
```

## 安全说明

- `-key` 使用 SHA-256 派生 AES-256-GCM 密钥。每个帧都有随机 nonce 和认证标签，可防止流量被直接读取或静默篡改；请使用足够长且不可猜测的口令。
- 服务端默认拒绝空口令，避免意外成为局域网开放代理。只有显式添加 `-allow-insecure` 才能启用无认证明文模式。
- 口令不一致时，服务端会拒绝客户端接入并给出握手失败提示。
- 服务端具备访问其所在网络的能力，请通过主机防火墙只允许受信任的客户端 IP 连接 TCP 53。

## 注意事项

- **53 端口冲突**：如果跳板机开启了系统 DNS 服务占用了 53 端口，服务端无法监听。需要先停用 DNS 服务或用 `-listen` 指定其他端口（此时客户端 `-server` 也要对应改）。
- **管理员权限**：监听 <1024 端口需要 root/管理员权限，请以对应身份运行服务端。
- **本机被限制的只是普通端口**：本机到跳板机的 53 端口必须真实可达，否则隧道无法建立。
- **隧道断开会自动重连**：现有浏览器连接会被关闭，客户端按 `-retry` 指定的间隔重连；连接恢复后刷新网页即可继续使用。
- 仅用于学习与技术验证，请遵守所在网络的规定与当地法律法规。

## 常见问题

**Q：客户端提示"连接跳板机失败"？**
A：确认 `-server` 地址与端口正确、跳板机服务端已启动且 53 端口可达、口令一致。

**Q：浏览器能开但网页加载不出来？**
A：确认代理设置为 SOCKS5（不是 HTTP），并检查服务端日志是否有 `[server] 已连接目标` 打印；若目标连接失败会打印错误。

**Q：如何验证链路是否通了？**
A：可用 `curl --socks5 127.0.0.1:1080 http://example.com` 测试；返回内容即说明隧道正常。
