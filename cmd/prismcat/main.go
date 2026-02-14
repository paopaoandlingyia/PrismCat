package main

import (
	"context"
	"flag"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/prismcat/prismcat/internal/config"
	"github.com/prismcat/prismcat/internal/server"
	"github.com/prismcat/prismcat/internal/storage"
)

const defaultYAML = `
server:
  addr: 0.0.0.0
  port: 8080
  ui_hosts:
    - localhost
    - 127.0.0.1
  proxy_domains:
    - localhost

logging:
  max_request_body: 1048576       # 1MB
  max_response_body: 10485760     # 10MB
  sensitive_headers:
    - Authorization
    - api-key
    - x-api-key
  detach_body_over_bytes: 262144  # 256KB
  body_preview_bytes: 4096        # 4KB

storage:
  database: "data/prismcat.db"
  retention_days: 7
  blob_store: "fs"
  blob_dir: "data/blobs"
`

func main() {
	configPath := flag.String("config", "config.yaml", "配置文件路径")
	flag.Parse()

	// 检查配置文件是否存在
	if _, err := os.Stat(*configPath); os.IsNotExist(err) {
		log.Printf("未找到配置文件 %q，尝试初始化...", *configPath)

		var configData []byte
		// 1. 优先尝试从磁盘上的示例文件读取
		if data, err := os.ReadFile("config.example.yaml"); err == nil {
			log.Printf("使用磁盘上的 config.example.yaml 作为模版")
			configData = data
		} else {
			// 2. 备选方案：使用内置的默认配置字符串
			log.Printf("使用内置默认配置初始化")
			configData = []byte(strings.TrimSpace(defaultYAML))
		}

		// 确保目标路径的父目录存在
		if dir := filepath.Dir(*configPath); dir != "." {
			if err := os.MkdirAll(dir, 0755); err != nil {
				log.Fatalf("创建配置目录失败: %v", err)
			}
		}

		if err := os.WriteFile(*configPath, configData, 0644); err != nil {
			log.Fatalf("写入配置文件失败: %v", err)
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
	sqliteRepo, err := storage.NewSQLiteRepository(cfg.Storage.Database)
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

	// 1. SQLite
	// 2. Detaching：在异步 worker 中将大 Body 写入 blob store（避免阻塞代理热路径）
	// 3. Async：异步写入 SQLite（并保持顺序）
	detachingRepo := storage.NewDetachingRepository(sqliteRepo, blobStore, cfg)
	// 2. 包装一层 Async，用于处理最终的磁盘 IO 写入
	asyncRepo := storage.NewAsyncRepository(detachingRepo, cfg.Storage.AsyncBuffer)
	defer asyncRepo.Close()

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
						if refs, err := sqliteRepo.ListBlobRefs(); err != nil {
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
	srv := server.New(cfg, asyncRepo, blobStore)
	if err := srv.Start(); err != nil {
		log.Fatalf("服务器错误: %v", err)
	}
}
