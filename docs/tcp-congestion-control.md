# TCP 拥塞控制算法：从 CUBIC 到 BBR (深度解析)

TCP 拥塞控制是确保互联网稳定运行的核心机制。它通过动态调节数据的发送速率，既能充分利用带宽，又能防止网络因过载而发生“拥塞崩溃”。

---

## 1. 经典四个阶段 (RFC 5681)

大多数拥塞控制算法都遵循以下基础逻辑：

- **慢启动 (Slow Start)**：连接开始时，拥塞窗口 (`cwnd`) 从极小值开始，每收到一个 ACK，`cwnd` 翻倍。呈指数增长，快速占据可用带宽。
- **拥塞避免 (Congestion Avoidance)**：当 `cwnd` 达到阈值 (`ssthresh`)，增长变为线性（每个 RTT 增加 1 MSS），探测带宽边缘。
- **快重传 (Fast Retransmit)**：发送方收到 3 个重复 ACK 时，认为包已丢失，立即重传，而不等待超时。
- **快恢复 (Fast Recovery)**：丢包后不重置为慢启动，而是将 `ssthresh` 减半，`cwnd` 设置为 `ssthresh` 附近，直接进入拥塞避免。

---

## 2. CUBIC 算法 (基于丢包的经典)

CUBIC 是目前 Linux 内核的默认算法。

### 2.1 核心原理
- **基于丢包 (Loss-based)**：CUBIC 认为“丢包即拥塞”。只有当路由器缓冲区溢出导致丢包时，它才会减小发送窗口。
- **三次函数增长**：其 `cwnd` 增长函数是一个三次方程，在远离上次丢包点时增长快，接近时增长慢，试图在稳定吞吐量和带宽探测间取得平衡。

### 2.2 局限性
- **缓冲区膨胀 (Bufferbloat)**：CUBIC 倾向于填满路径上的所有缓冲区。
- **长肥网络 (LFN) 表现差**：在高延迟、高带宽（如跨国光缆）且伴随少量随机丢包的链路上，CUBIC 会因误判拥塞而频繁减窗，导致带宽利用率极低。

---

## 3. BBR 算法 (基于模型的革命)

BBR (Bottleneck Bandwidth and RTT) 由 Google 开发，它彻底改变了拥塞控制的思路。

### 3.1 核心原理：不再等待丢包
- **基于模型 (Model-based)**：BBR 不再通过丢包来判断拥塞。它通过持续测量路径的两个关键物理指标来建模：
  1. **瓶颈带宽 (RTprop)**：路径上能承载的最大速度。
  2. **最小往返时延 (BtlBdp)**：数据包往返的最短时间。
- **运行点**：BBR 试图让网络运行在“延迟最低且吞吐量最高”的黄金交点（Kleinrock's optimal operating point）。

### 3.2 窗口控制 vs. 速率控制 (Pacing)
这是 BBR 与 CUBIC 的本质区别：
- **CUBIC (窗口控制)**：主要控制“在途字节数”。发送行为往往是瞬间爆发的（Micro-bursts），依赖 ACK 触发发送（Self-clocking）。
- **BBR (速率控制)**：显式计算 `Pacing Rate`。它按照 **发送速率 = 增益因子 × 估计带宽** 的频率，均匀地、有节奏地将数据包吐出到网络中。这极大地减少了交换机瞬时丢包的概率。

---

## 4. 跨平台支持现状 (2024 更新)

| 平台 | TCP 支持情况 | 开启/实现方式 |
| :--- | :--- | :--- |
| **Linux** | ✅ 完美支持 | 代码中使用 `setsockopt(fd, ..., "bbr")` |
| **Windows 11** | ✅ 支持 BBRv2 | 需手动开启：`netsh int tcp set supplemental Template=Internet CongestionProvider=bbr2` |
| **macOS** | ❌ 不支持 | 内核暂无实现，默认使用 CUBIC |
| **QUIC (H3)** | ✅ **全平台支持** | 运行在用户态，由应用程序（如 `oget`）直接控制 |

### 4.1 Windows 11 特别说明 (24H2 Bug)
在 Windows 11 24H2 中开启 BBRv2 后，已知会导致 localhost (回环地址) 连接极慢。修复命令：
```powershell
netsh int ipv4 set gl loopbacklargemtu=disable
netsh int ipv6 set gl loopbacklargemtu=disable
```

### 4.2 macOS 的曲线救国
由于 macOS 内核不支持 TCP BBR，`oget` 在该平台上获得加速的最佳路径是使用 **HTTP/3 (QUIC)**。因为 QUIC 的拥塞控制是在用户态实现的，`oget` 可以直接在 macOS 上跑 BBR 逻辑。

---

## 5. 在 oget 中的应用

`oget` 默认在 Linux 上尝试为每个 TCP 连接开启 BBR。在跨国下载、高延迟卫星链路或移动网络下，开启 BBR 相比 CUBIC 通常能提升 **2x - 10x** 的吞吐量。
