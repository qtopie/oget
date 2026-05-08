# oget Performance Benchmarks

This document records the local performance benchmarks for `oget`, comparing different concurrency levels and storage backends.

## Test Environment
- **Date:** Wednesday, May 6, 2026
- **OS:** Linux
- **File Size:** 512 MB (Simulated via `ogettest`)
- **Network:** Local Loopback (127.0.0.1)

## Benchmark Results

| Test Case | Storage Type | Concurrency | Average Speed | Duration |
| :--- | :--- | :--- | :--- | :--- |
| Standard File Write | `file` | 8 | 1174.10 MB/s | 0.436s |
| Standard File Write | `file` | 32 | 1332.27 MB/s | 0.384s |
| **Mmap Zero-Copy** | `mmap` | **32** | **1432.55 MB/s** | **0.357s** |

## Analysis
1. **Concurrency Impact:** Increasing concurrency from 8 to 32 provided a ~13% performance boost even in a local environment, demonstrating the efficiency of the parallel downloader's task scheduling.
2. **Storage Backend:** The `mmap` storage backend outperformed standard file IO by minimizing user-space to kernel-space copying. It achieved the highest throughput of **1.43 GB/s**.
3. **Efficiency:** `oget` is capable of saturating high-speed local IO, making it ideal for high-bandwidth network environments.

## How to Reproduce
Run the provided `local_bench.go` script:
```bash
HTTP_PROXY= HTTPS_PROXY= go run local_bench.go
```
