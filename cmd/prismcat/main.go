package main

import (
	"context"
	"flag"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/paopaoandlingyia/PrismCat/internal/config"
	"github.com/paopaoandlingyia/PrismCat/internal/server"
	"github.com/paopaoandlingyia/PrismCat/internal/storage"
)

const defaultYAML = `
server:
  addr: 0.0.0.0
  port: 8080
  ui_hosts:
    - localhost
    - 127.0.0.1
  ui_password: ""
  proxy_domains:
    - localhost

logging:
  max_request_body: 5242880       # 5MB
  max_response_body: 33554432     # 32MB
  sensitive_headers:
    - Authorization
    - api-key
    - x-api-key
  early_request_body_snapshot: false
  detach_body_over_bytes: 2097152 # 2MB
  body_preview_bytes: 524288      # 512KB

storage:
  database: "data/prismcat.db"
  retention_days: 7
  blob_store: "fs"
  blob_dir: "data/blobs"

usage_extraction:
  enabled: false
  upstreams: {}
  rules:
    - name: OpenAI compatible
      enabled: true
      match:
        content_types: ["application/json", "text/event-stream"]
      paths:
        input_tokens: ["/usage/prompt_tokens", "/usage/input_tokens"]
        output_tokens: ["/usage/completion_tokens", "/usage/output_tokens"]
        total_tokens: ["/usage/total_tokens"]
        raw_usage: ["/usage"]
    - name: Anthropic
      enabled: true
      match:
        content_types: ["application/json", "text/event-stream"]
      paths:
        input_tokens: ["/usage/input_tokens", "/message/usage/input_tokens"]
        output_tokens: ["/usage/output_tokens", "/message/usage/output_tokens"]
        raw_usage: ["/usage", "/message/usage"]
    - name: Gemini
      enabled: true
      match:
        content_types: ["application/json", "text/event-stream"]
      paths:
        input_tokens: ["/usageMetadata/promptTokenCount"]
        output_tokens: ["/usageMetadata/candidatesTokenCount"]
        total_tokens: ["/usageMetadata/totalTokenCount"]
        raw_usage: ["/usageMetadata"]
`

func main() {
	defaultPath := filepath.Join("data", "config.yaml")
	configPath := flag.String("config", defaultPath, "配置文件路径")
	showConsole := flag.Bool("console", false, "是否显示控制台窗口")
	flag.Parse()

	// 统一路径处理：如果要使用的是默认路径，但老路径 config.yaml 存在，则尝试迁移或提示
	if *configPath == defaultPath {
		if _, err := os.Stat("config.yaml"); err == nil {
			if _, err := os.Stat(defaultPath); os.IsNotExist(err) {
				log.Printf("检测到旧版配置文件 config.yaml，正在迁移到 data 目录...")
				if err := os.MkdirAll("data", 0755); err == nil {
					if err := os.Rename("config.yaml", defaultPath); err == nil {
						log.Printf("迁移成功: config.yaml -> %s", defaultPath)
					} else {
						log.Printf("迁移失败: %v，将继续使用默认配置初始化", err)
					}
				}
			}
		}
	}

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
	log.Printf("PrismCat %s 启动中...", config.Version)
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

	asyncRepo := storage.NewAsyncRepository(sqliteRepo, cfg, cfg.Storage.AsyncBuffer, blobStore)
	defer asyncRepo.Close()

	// Best-effort log retention cleanup.
	stopRetention := make(chan struct{})
	go func() {
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

	// 平台相关的运行逻辑（Windows: 系统托盘, 其他: 直接运行）
	if err := platformRun(srv, cfg, *showConsole); err != nil {
		log.Fatalf("运行失败: %v", err)
	}
}
