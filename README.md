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
