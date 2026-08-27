# Proposal: 基于 FFmpeg 的视频下载与转封装方案

- **需求追踪**: [oget 项目支持 M3U8 下载/转码方案](https://tasks.google.com/task/kNtYtjBgeiJmAAIY?sa=DLSA_GEMINI)
- **目标组件**: `pkg/oget`, `cmd/main.go`
- **设计原则**: **拒绝重复造轮子**，专注于以 FFmpeg 为核心媒体引擎，由 `oget` 负责统一的 CLI 体验、网络参数透传、子进程生命周期管控与进度监控。
- **状态**: Draft (草案)

---

## 1. 核心理念与架构定位 (Core Philosophy)

流媒体协议（如 M3U8/HLS、DASH 等）涉及繁杂的音视频容器（TS / fMP4）、多码率自适应、AES 密钥交互以及 PTS/DTS 时间戳重映射同步等问题。

为保持 `oget` 代码精简高效并保证产出视频的最高兼容性：
1. **零 CGO、零纯 Go 重复造轮子**：不手写 TS 解析器与分片拼接器，直接委托工业级成熟的 `ffmpeg` 处理媒体拉取、解密与容器封装。
2. **`oget` 定位**：作为强大的前端编排与监控器，负责：
   - 自动探测视频资源（M3U8 / 常见流媒体链接）。
   - 环境检测与 FFmpeg 依赖校验（缺失时快速报错指引）。
   - 请求头（Headers / Cookies / User-Agent / Referer）、代理与重试参数注入。
   - 捕获 FFmpeg 实时输出并无缝桥接到 `oget` 统一终端进度条。
   - 优雅中断（SIGINT/SIGTERM）与未完成临时文件清理。

---

## 2. 架构设计与数据流 (Architecture & Flow)

```
                     ┌───────────────────────────┐
                     │       oget CLI 入口       │
                     └─────────────┬─────────────┘
                                   │
                 探测资源为 M3U8 / 视频流媒体协议
                                   │
                  FFmpeg 环境检测 (exec.LookPath)
                  ├─ 未安装 ──> 提示安装指南并退出
                  └─ 已就绪 ──> 构建 FFmpeg 执行上下文
                                   │
                      ┌────────────┴────────────┐
                      ▼                         ▼
            【FFmpeg 子进程 (Worker)】     【进度监听器 (Progress Bridge)】
                      │                         │
         1. 注入 HTTP Headers / Proxy       1. 实时读取 `-progress pipe:1`
         2. 流式拉取切片并无损 Remux        2. 解析 `out_time_us`, `speed`, `total_size`
         3. PTS/DTS 自动对齐修复            3. 驱动 oget 统一 Progress Bar & 速率显示
         4. 产出标准 MP4 (faststart)        4. 处理 Context 取消与优雅退出
```

---

## 3. 模块结构设计 (Package Structure)

在 `pkg/oget/` 下新增 FFmpeg 调度相关模块，与现有的 `Prober` / `Fetcher` 架构保持一致：

```
oget/
├── pkg/
│   └── oget/
│       ├── ffmpeg_prober.go    // 视频/M3U8 探测器（实现 Prober 接口）
│       ├── ffmpeg_fetcher.go   // 视频拉取执行器（实现 Fetcher 接口，管理子进程）
│       ├── ffmpeg_wrapper.go   // FFmpeg 命令组装、环境检测与参数透传
│       └── ffmpeg_progress.go  // FFmpeg 结构化进度解析器 (-progress pipe:1)
└── docs/
    └── m3u8-video-download.md  // 本设计提案文档
```

---

## 4. 关键技术实现细节 (Implementation Details)

### 4.1 协议探测与路由 (`ffmpeg_prober.go`)
- 根据 URL 扩展名（`.m3u8`, `.mpd`）、Query 参数或 HTTP 响应头 `Content-Type: application/vnd.apple.mpegurl` 识别流媒体任务。
- 自动路由至 `FFmpegFetcher` 执行。

### 4.2 FFmpeg 命令组装与网络参数透传 (`ffmpeg_wrapper.go`)
将 `oget` 原生的网络与安全参数标准化转换为 FFmpeg CLI 参数：
- **请求头透传**：通过 `-headers $'Header: Value\r\n'` 注入自定义 Header、User-Agent、Referer 及 Cookie。
- **代理支持**：若配置了 `--proxy`，设置 `http_proxy` 环境变量或 `-http_proxy` 参数。
- **网络容错与重连**：
  ```bash
  -reconnect 1 \
  -reconnect_at_eof 1 \
  -reconnect_streamed 1 \
  -reconnect_delay_max 5
  ```
- **无损转封装（Remux Copy）**：
  ```bash
  ffmpeg -y \
    -headers "User-Agent: ...\r\nReferer: ...\r\n" \
    -i "https://example.com/video.m3u8" \
    -c copy \
    -bsf:a aac_adtstoasc \
    -movflags +faststart \
    -progress pipe:1 \
    -nostats \
    "output.mp4"
  ```

### 4.3 实时进度捕获与 UI 桥接 (`ffmpeg_progress.go`)
- 使用 `-progress pipe:1` 获取 FFmpeg 标准化键值对输出：
  ```text
  frame=1240
  fps=0.00
  total_size=15420340
  out_time_us=45230000
  out_time=00:00:45.230000
  speed=8.42x
  progress=continue
  ```
- 解析 `total_size` 与 `out_time_us`，直接反馈给 `task.OnProgress` 与 `oget` 的全局速率/进度条计算引擎。

### 4.4 进程生命周期与优雅中断 (Graceful Shutdown)
- 当用户在终端按下 `Ctrl+C` (SIGINT) 时，向 FFmpeg 子进程发送 `q` 或 `SIGINT`，等待 FFmpeg 完成 MP4 尾部 `moov` index 封装后再退出，避免产出损坏无法播放的视频文件。
- 如果发生不可恢复错误，自动清理未完成的目标文件或临时产物。

---

## 5. 命令行交互示例 (CLI Usage)

```bash
# 1. 基础下载（自动转封装为 MP4）
oget "https://example.com/playlist.m3u8" -o video.mp4

# 2. 携带自定义请求头与 Referer
oget "https://example.com/playlist.m3u8" \
  -H "Referer: https://example.com" \
  -H "User-Agent: Mozilla/5.0 ..." \
  -o video.mp4

# 3. 指定自定义 ffmpeg 路径
oget "https://example.com/playlist.m3u8" --ffmpeg-path /opt/homebrew/bin/ffmpeg
```

### CLI 参数定义

| 参数名 | 类型 | 默认值 | 说明 |
| :--- | :--- | :--- | :--- |
| `--ffmpeg-path` | `string` | `""` | 指定自定义 FFmpeg 可执行文件路径（默认在 PATH 中查找） |
| `--video-copy` | `bool` | `true` | 使用流拷贝直接转封装（`-c copy`，不进行二次重编码） |

---

## 6. 实施路线规划 (Roadmap)

- [ ] **Phase 1: FFmpeg 包装器与环境探测**
  - [ ] 实现 `exec.LookPath("ffmpeg")` 检测与友好报错指引。
  - [ ] 封装 FFmpeg 命令行参数构建器（支持 Headers、Proxy、重连策略）。
- [ ] **Phase 2: 进度管道与 Prober/Fetcher 集成**
  - [ ] 实现 `-progress pipe:1` 键值对解析器。
  - [ ] 对接 `oget` 现有 `Prober` 与 `Fetcher` 接口体系。
- [ ] **Phase 3: 优雅退出与异常处理**
  - [ ] 实现 SIGINT 信号拦截与 FFmpeg 安全收尾（确保 MP4 moov 头完整）。
  - [ ] 补充单元测试与模拟测试。
