# oget

[English](./README.md) | [简体中文](./README_zh.md)

High-performance, parallel network downloader with modern IO techniques (io_uring, mmap, splice).

## Key Features
- **Modern IO Backends**: Support for `io_uring`, `mmap` (Zero-copy), and `splice`.
- **Network Acceleration**: 
  - **BBR** Congestion Control for high-latency networks.
  - **HTTP/3 (QUIC)** & **HTTP/2** support.
- **Reliability**: 
  - **Resume (Breakpoint)** support with state persistence.
  - **Per-chunk SHA-256 Checksum** verification.
  - **Zero-hole (fallocate)** physical pre-allocation.
- **Advanced CLI**: Beautiful progress bars with detailed speed and percentage.
- **Configurable**: Global configuration via `oget.json` or Environment Variables.

## Installation
Install [Go](https://golang.org/doc/install), then run:
```bash
go install github.com/qtopie/oget/cmd@latest
```

## Usage

* Basic usage
```bash
oget <URL1> [URL2] ...
```

* High-concurrency (default 32)
```bash
oget -concurrency 64 <URL>
```

* Save to specific file (single URL)
```bash
oget -file output.zip <URL>
```

* BitTorrent & Magnet Support
```bash
# Download via local torrent file
oget -verbose ./ubuntu.torrent

# Download via remote torrent URL (supports complex parameters)
oget -verbose "https://releases.ubuntu.com/26.04/ubuntu-26.04-live-server-amd64.iso.torrent?_gl=1*11tffb8*_gcl_au*MTcxNDM1NTg2Ni4xNzc4MjUzMjgx"

# Download via Magnet link
oget -verbose "magnet:?xt=urn:btih:..."
```

## Configuration (`oget.json`)
You can place an `oget.json` in your working directory to customize behavior:
```json
{
  "concurrency": 32,
  "storage_type": "uring",
  "state_store_type": "json",
  "proxy_url": "http://127.0.0.1:8080"
}
```
Available `storage_type`: `file` (default), `uring` (Linux 5.1+), `mmap`.

## Performance Tuning
For the best performance on Linux:
- Use `storage_type: "uring"` to leverage asynchronous IO.
- Use `storage_type: "mmap"` for zero-copy file mapping.
- BBR is enabled automatically if supported by your kernel.

## License
MIT License - see [LICENSE](./LICENSE)
