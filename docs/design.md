# oget Design & Architecture

`oget` 是一个高性能、并发、支持多协议（HTTP/1.1, H2, H3）的高级下载工具，旨在利用现代操作系统的 IO 特性和网络协议优化来最大化传输效率。

## 1. 核心流程 (Process Flow)

```text
Arguments (CLI) --> Configuration (Viper) --> Downloader (Orchestrator) --> Requester (Probes) --> ChunkTasks (Units of Work)
```

- **Main 控制流**：启动任务、监控整体进度、处理中断信号并持久化状态。
- **任务切分**：每个 URL 由一个 `Requester` 负责，它首先进行 HEAD/Range 探测，然后将文件切分为固定大小的 `ChunkTask` (默认 1MB)。

## 2. 系统组件 (System Components)

### 2.1 Downloader (调度中心)
- **Host-based Queues**：为每个域名维护独立的任务队列，优化连接复用（Keep-Alive）。
- **Work Stealing**：Worker 协程优先处理当前主机的任务，在空闲时从其他主机队列“窃取”任务，确保 CPU 和带宽利用率。
- **Adaptive Concurrency**：动态调整并发数。基于实时下载速率的反馈，自动增加或减少 Worker 数量。

### 2.2 Requester & State Management
- **Download State**：使用位图（Bitset）或状态映射记录分片的完成情况。
- **Resume (断点续传)**：通过 `.oget` JSON 状态文件记录 ETag 和已完成分片的校验和。
- **Physical Pre-allocation**：使用 `fallocate` 预分配物理空间，减少磁盘碎片并提升写入性能。

### 2.3 Fetcher (传输层)
- **Hybrid Transport**：支持 HTTP/3 (QUIC) 优先，并能自动降级至 H2/H1.1。
- **BBR Congestion Control**：在支持的内核上强制开启 TCP BBR，优化长距离网络下的吞吐。

## 3. 存储后端 (Storage Backends)

支持通过配置切换不同的 IO 策略：
- **Standard**：传统的带缓冲写入。
- **Mmap**：利用内存映射减少用户态拷贝。
- **Splice (Zero-copy)**：利用 Linux `splice` 系统调用，实现数据在内核态直接从 Socket 传输到文件描述符。

## 4. 进度与速率计算 (Metrics)

- **速率计算**：采用滑动窗口或 Tick 差异计算。
  - 公式：`Rate = delta(processed_bytes) / delta(time)`
- **原子计数器**：使用 `sync/atomic` 确保在多协程环境下进度的准确统计。

## 5. 待办事项 (TODO)

- [x] 实现分片 SHA-256 校验。
- [x] 实现基于 BBR 的网络加速。
- [ ] 优化速率计算的平滑度。
- [ ] 增加更多 IO 调度算法的支持。
