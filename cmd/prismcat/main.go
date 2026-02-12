package main

import (
	"context"
	"flag"
	"log"
	"os"
	"time"

	"github.com/prismcat/prismcat/internal/config"
	"github.com/prismcat/prismcat/internal/server"
	"github.com/prismcat/prismcat/internal/storage"
)

func main() {
	configPath := flag.String("config", "config.yaml", "配置文件路径")
	flag.Parse()

	// 检查配置文件是否存在
	if _, err := os.Stat(*configPath); os.IsNotExist(err) {
		// 尝试使用示例配置
		if _, err := os.Stat("config.example.yaml"); err == nil {
			log.Println("未找到 config.yaml，正在从 config.example.yaml 复制...")
			data, _ := os.ReadFile("config.example.yaml")
			os.WriteFile("config.yaml", data, 0644)
			*configPath = "config.yaml"
		} else {
			log.Fatal("配置文件不存在: ", *configPath)
		}
	}

	// 加载配置
	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatalf("加载配置失败: %v", err)
	}
	log.Printf("配置已加载: DetachBodyOverBytes=%d, BodyPreviewBytes=%d", 
		cfg.Logging.DetachBodyOverBytes, cfg.Logging.BodyPreviewBytes)

	// 初始化存储
	repo, err := storage.NewSQLiteRepository(cfg.Storage.Database)
	if err != nil {
		log.Fatalf("初始化存储失败: %v", err)
	}

	// Blob store for detached bodies.
	var blobStore storage.BlobStore
	switch cfg.Storage.BlobStore {
	case "", "fs":
		bs, err := storage.NewFileBlobStore(cfg.Storage.BlobDir)
		if err != nil {
			log.Fatalf("初始化 blob 存储失败: %v", err)
		}
		blobStore = bs
	default:
		log.Fatalf("不支持的 blob_store: %s", cfg.Storage.BlobStore)
	}

	// 1. 最底层是 SQLite
	// 2. 包装一层 Async，用于处理最终的磁盘 IO 写入
	asyncRepo := storage.NewAsyncRepository(repo, 4096)
	defer asyncRepo.Close()

	// 3. 最外层是 Detaching，确保在进入异步队列前就已经完成了大 Body 的脱离处理
	// 这样可以保证 UI 在任何时候查到的都是处理（截断）后的对象
	detachedRepo := storage.NewDetachingRepository(asyncRepo, blobStore, cfg)

	// Best-effort log retention cleanup.
	stopRetention := make(chan struct{})
	go func() {
		// Check frequently so config changes take effect without restart,
		// but only run the DELETE job at most every 6 hours.
		ticker := time.NewTicker(1 * time.Minute)
		defer ticker.Stop()

		var lastCleanup time.Time
		var lastBlobGC time.Time
		for {
			retentionDays := cfg.StorageSnapshot().RetentionDays
			if retentionDays > 0 && (lastCleanup.IsZero() || time.Since(lastCleanup) >= 6*time.Hour) {
				before := time.Now().Add(-time.Duration(retentionDays) * 24 * time.Hour)
				deleted, err := asyncRepo.DeleteLogsBefore(before)
				if err != nil {
					log.Printf("log retention cleanup failed: %v", err)
				} else if deleted > 0 {
					log.Printf("deleted %d logs older than %d days", deleted, retentionDays)
				}

				// Best-effort blob GC for filesystem blob store.
				if fsStore, ok := blobStore.(*storage.FileBlobStore); ok {
					if lastBlobGC.IsZero() || time.Since(lastBlobGC) >= 24*time.Hour {
						if refs, err := repo.ListBlobRefs(); err != nil {
							log.Printf("blob GC list refs failed: %v", err)
						} else if n, err := fsStore.GarbageCollect(context.Background(), refs, time.Hour); err != nil {
							log.Printf("blob GC failed: %v", err)
						} else if n > 0 {
							log.Printf("deleted %d unreferenced blobs", n)
						}
						lastBlobGC = time.Now()
					}
				}

				lastCleanup = time.Now()
			}

			select {
			case <-ticker.C:
			case <-stopRetention:
				return
			}
		}
	}()
	defer close(stopRetention)

	// 启动服务器
	srv := server.New(cfg, detachedRepo, blobStore)
	if err := srv.Start(); err != nil {
		log.Fatalf("服务器错误: %v", err)
	}
}
