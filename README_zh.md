# oget

[English](./README.md) | [简体中文](./README_zh.md)

高性能并行网络下载器，利用现代 IO 技术（io_uring, mmap, splice）实现。

## 核心特性
- **现代 IO 后端**: 支持 `io_uring`、`mmap` (零拷贝) 和 `splice`。
- **网络加速**: 
  - **BBR** 拥塞控制，针对高延迟网络优化。
  - 支持 **HTTP/3 (QUIC)** 和 **HTTP/2**。
- **高可靠性**: 
  - 支持 **断点续传** 及其状态持久化。
  - **分片 SHA-256 校验**。
  - **文件预分配 (fallocate)**，防止碎片化。
- **高级命令行体验**: 优美的进度条，显示详细速度和百分比。
- **灵活配置**: 支持通过 `oget.json` 或环境变量进行全局配置。

## 安装
首先安装 [Go](https://golang.org/doc/install)，然后运行：
```bash
go install github.com/qtopie/oget/cmd@latest
```

## 使用说明

* 基本用法
```bash
oget <URL1> [URL2] ...
```

* 高并发模式 (默认 32)
```bash
oget -concurrency 64 <URL>
```

* 指定文件名 (仅限单个 URL)
```bash
oget -file output.zip <URL>
```

* BitTorrent 与磁力链接支持
```bash
# 通过本地种子文件下载
oget -verbose ./ubuntu.torrent

# 通过远程种子链接下载 (支持复杂的查询参数)
oget -verbose "https://releases.ubuntu.com/26.04/ubuntu-26.04-live-server-amd64.iso.torrent?_gl=1*11tffb8*_gcl_au*MTcxNDM1NTg2Ni4xNzc4MjUzMjgx"

# 通过磁力链接下载
oget -verbose "magnet:?xt=urn:btih:..."
```

## 配置项目 (`oget.json`)
您可以在工作目录下放置 `oget.json` 来自定义下载器行为：
```json
{
  "concurrency": 32,
  "storage_type": "uring",
  "state_store_type": "json",
  "proxy_url": "http://127.0.0.1:8080"
}
```
可选 `storage_type`: `file` (默认), `uring` (Linux 5.1+), `mmap`。

## 性能优化建议
为了在 Linux 上获得最佳性能：
- 使用 `storage_type: "uring"` 以利用异步 IO。
- 使用 `storage_type: "mmap"` 实现零拷贝内存映射。
- 如果内核支持，BBR 将会自动启用。

## 许可证
MIT License - 详见 [LICENSE](./LICENSE)
