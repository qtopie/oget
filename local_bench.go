package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/qtopie/oget/ogettest"
	"github.com/qtopie/oget/pkg/oget"
)

func main() {
	// 1. 准备本地测试环境
	// 模拟一个 512MB 的文件，既能看出速度差异，又不会跑太久
	fileSizeMB := 512
	server := ogettest.NewLargeRangeServer(fileSizeMB)
	defer server.Close()

	fmt.Printf("=== oget 本地性能测试 ===\n")
	fmt.Printf("测试服务器已启动: %s\n", server.URL)
	fmt.Printf("测试文件大小: %d MB\n\n", fileSizeMB)

	// 2. 测试方案列表
	tests := []struct {
		name        string
		storageType string
		concurrency int
	}{
		{"标准文件写入 (8并发)", "file", 8},
		{"标准文件写入 (32并发)", "file", 32},
		{"Mmap 零拷贝写入 (32并发)", "mmap", 32},
	}

	for _, tc := range tests {
		runTest(tc.name, server.URL, tc.storageType, tc.concurrency, fileSizeMB)
	}

	fmt.Printf("\n测试完成！你可以看到不同存储方式和并发数对速度的影响。\n")
}

func runTest(name, url, storageType string, concurrency int, totalMB int) {
	fmt.Printf("正在执行: %s...\n", name)

	// 配置调整
	cfg := oget.DefaultConfig()
	cfg.StorageType = storageType
	cfg.Concurrency = concurrency
	cfg.AutoTune = false // 固定并发以便对比性能
	cfg.Verbose = true
	cfg.ProxyURL = "" // 禁用代理，防止干扰本地测试

	// 清理旧的下载结果
	outputFile := fmt.Sprintf("test_output_%s.bin", storageType)
	os.Remove(outputFile)
	os.Remove(outputFile + ".oget") // 清理状态文件
	defer os.Remove(outputFile)

	downloader := oget.NewDownloader([]string{url}, concurrency)
	downloader.Config = cfg
	// 覆盖默认文件名
	downloader.URLs = []string{url}
	
	start := time.Now()
	downloader.Download(context.Background())
	duration := time.Since(start)

	speed := float64(totalMB) / duration.Seconds()
	fmt.Printf(">> 耗时: %v | 平均速度: %.2f MB/s\n\n", duration, speed)
}
