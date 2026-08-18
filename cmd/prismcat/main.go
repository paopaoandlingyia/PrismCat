package main

import (
	"context"
	"errors"
	"flag"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/paopaoandlingyia/PrismCat/internal/archive"
	"github.com/paopaoandlingyia/PrismCat/internal/config"
	"github.com/paopaoandlingyia/PrismCat/internal/server"
	"github.com/paopaoandlingyia/PrismCat/internal/storage"
)

const defaultYAML = `
server:
  addr: 0.0.0.0
  port: 8711
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
  store_base64: false

storage:
  database: "data/prismcat.db"
  retention_days: 7
  max_storage_bytes: 0            # 0 = no limit
  blob_store: "fs"
  blob_dir: "data/blobs"
  body_compression:
    algorithm: zstd
    level: 3

archive:
  enabled: false
  s3:
    endpoint: ""
    region: ""
    bucket: ""
    access_key_id: ""
    secret_access_key: ""
    force_path_style: false
  key_prefix: "backups/prismcat/${yyyy}/${MM}-${dd}"
  schedule_time: "02:00"
  timezone: "Asia/Shanghai"
  zstd_level: 10
  local_retention_hours: 24
  import_retention_hours: 24

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
    - name: OpenAI Responses
      enabled: true
      match:
        content_types: ["application/json", "text/event-stream"]
      paths:
        input_tokens: ["/usage/input_tokens", "/response/usage/input_tokens"]
        output_tokens: ["/usage/output_tokens", "/response/usage/output_tokens"]
        total_tokens: ["/usage/total_tokens", "/response/usage/total_tokens"]
        raw_usage: ["/usage", "/response/usage"]
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
	if err := cfg.EnsureModelPathTemplatesInitialized(); err != nil {
		log.Fatalf("初始化模型日志路径模板失败: %v", err)
	}
	log.Printf("PrismCat %s 启动中...", config.Version)
	log.Printf("配置已加载: body compression=%s level=%d",
		cfg.Storage.BodyCompression.Algorithm, cfg.Storage.BodyCompression.Level)

	// 初始化存储
	sqliteRepo, err := storage.NewSQLiteRepository(cfg.Storage.Database)
	if err != nil {
		log.Fatalf("初始化存储失败: %v", err)
	}

	// Blob store for detached bodies.
	var blobStore storage.BlobStore
	switch cfg.Storage.BlobStore {
	case "", "fs":
		bs, err := storage.NewFileBlobStoreWithCompression(
			cfg.Storage.BlobDir,
			cfg.Storage.BodyCompression.Algorithm,
			cfg.Storage.BodyCompression.Level,
		)
		if err != nil {
			log.Fatalf("初始化 blob 存储失败: %v", err)
		}
		blobStore = bs
	default:
		log.Fatalf("不支持的 blob_store: %s", cfg.Storage.BlobStore)
	}
	if err := sqliteRepo.MigrateLegacyBodies(blobStore); err != nil {
		log.Fatalf("迁移旧版正文存储失败: %v", err)
	}

	asyncRepo := storage.NewAsyncRepository(sqliteRepo, cfg, cfg.Storage.AsyncBuffer, blobStore)
	defer asyncRepo.Close()
	archiveManager, err := archive.NewManager(cfg, asyncRepo, blobStore)
	if err != nil {
		log.Fatalf("初始化归档服务失败: %v", err)
	}

	// Best-effort log retention cleanup.
	stopRetention := make(chan struct{})
	go func() {
		ticker := time.NewTicker(1 * time.Minute)
		defer ticker.Stop()

		var lastRetention time.Time
		var lastBlobGC time.Time
		var lastSizeCheck time.Time
		for {
			storageCfg := cfg.StorageSnapshot()
			var totalDeleted int64

			// 1. Time-based retention.
			archiveEnabled := cfg.ArchiveSnapshot().Enabled
			if !archiveEnabled && storageCfg.RetentionDays > 0 && (lastRetention.IsZero() || time.Since(lastRetention) >= 6*time.Hour) {
				before := time.Now().Add(-time.Duration(storageCfg.RetentionDays) * 24 * time.Hour)
				n, err := asyncRepo.DeleteLogsBefore(before)
				if err != nil {
					log.Printf("log retention cleanup failed: %v", err)
				} else if n > 0 {
					log.Printf("deleted %d logs older than %d days", n, storageCfg.RetentionDays)
				}
				totalDeleted += n
				ignoredDeleted, ignoredErr := asyncRepo.DeleteIgnoredPathsBefore(before)
				if ignoredErr != nil {
					log.Printf("ignored path retention cleanup failed: %v", ignoredErr)
				} else if ignoredDeleted > 0 {
					log.Printf("deleted %d ignored paths older than %d days", ignoredDeleted, storageCfg.RetentionDays)
				}
				totalDeleted += ignoredDeleted
				lastRetention = time.Now()
			}

			// 2. Size-based cleanup. Reclaim orphaned blobs/free DB pages first;
			// delete logs only if the storage limit is still exceeded.
			if !archiveEnabled && storageCfg.MaxStorageBytes > 0 && (lastSizeCheck.IsZero() || time.Since(lastSizeCheck) >= 6*time.Hour) {
				var fsStore *storage.FileBlobStore
				if s, ok := blobStore.(*storage.FileBlobStore); ok {
					fsStore = s
				}
				result, err := cleanupStorageLimit(context.Background(), storageCfg, asyncRepo, sqliteRepo, fsStore, nil, log.Printf)
				if err != nil {
					log.Printf("size-based cleanup failed: %v", err)
				}
				totalDeleted += result.DeletedLogs
				if result.RanBlobGC {
					lastBlobGC = time.Now()
				}
				lastSizeCheck = time.Now()
			}

			// 3. Blob GC (independent of retention setting).
			if fsStore, ok := blobStore.(*storage.FileBlobStore); ok {
				if lastBlobGC.IsZero() || time.Since(lastBlobGC) >= 24*time.Hour {
					if refs, err := sqliteRepo.ListBlobRefs(); err != nil {
						log.Printf("blob GC list refs failed: %v", err)
					} else if n, err := fsStore.GarbageCollect(context.Background(), refs, time.Hour); errors.Is(err, storage.ErrRefSetEmpty) {
						log.Printf("blob GC skipped: %v — check that storage.database points at the database these blobs belong to (blob dir: %s)", err, cfg.Storage.BlobDir)
					} else if err != nil {
						log.Printf("blob GC failed: %v", err)
					} else if n > 0 {
						log.Printf("deleted %d unreferenced blobs", n)
					}
					lastBlobGC = time.Now()
				}
			}

			// 4. WAL checkpoint after significant deletions.
			if totalDeleted >= 500 {
				if err := asyncRepo.WALCheckpoint(); err != nil {
					log.Printf("WAL checkpoint failed: %v", err)
				}
			}

			select {
			case <-ticker.C:
			case <-stopRetention:
				return
			}
		}
	}()
	defer close(stopRetention)

	// Daily S3 backup scheduler plus delayed local cleanup.
	go func() {
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		var lastScheduleAttempt time.Time
		var lastLocalCleanup time.Time
		var lastImportCleanup time.Time
		run := func() {
			archiveCfg := cfg.ArchiveSnapshot()
			loc, err := time.LoadLocation(archiveCfg.Timezone)
			if err != nil {
				return
			}
			now := time.Now().In(loc)
			if archiveCfg.Enabled && now.Format("15:04") >= archiveCfg.ScheduleTime && (lastScheduleAttempt.IsZero() || time.Since(lastScheduleAttempt) >= 5*time.Minute) {
				jobs, _ := asyncRepo.ListArchiveJobs(100)
				cutoff := startOfLocalDay(now, loc).UTC()
				alreadyHandled := scheduledArchiveHandled(jobs, cutoff)
				if !alreadyHandled {
					job, err := archiveManager.StartScheduled(now)
					if err != nil && !errors.Is(err, archive.ErrArchiveBusy) {
						log.Printf("daily backup start failed: %v", err)
					} else if job != nil {
						log.Printf("daily backup job %s accepted with cutoff %s", job.ID, job.Cutoff.Format(time.RFC3339))
					}
				}
				lastScheduleAttempt = time.Now()
			}
			if archiveCfg.Enabled && (lastLocalCleanup.IsZero() || time.Since(lastLocalCleanup) >= 5*time.Minute) {
				if n, err := archiveManager.CleanupEligible(time.Now()); err != nil {
					log.Printf("backed-up log cleanup failed: %v", err)
				} else if n > 0 {
					log.Printf("deleted %d backed-up logs after local retention", n)
				}
				lastLocalCleanup = time.Now()
			}
			if lastImportCleanup.IsZero() || time.Since(lastImportCleanup) >= time.Hour {
				if n, err := archiveManager.DeleteExpiredImports(time.Now()); err != nil {
					log.Printf("archive import TTL cleanup failed: %v", err)
				} else if n > 0 {
					log.Printf("deleted %d expired imported logs", n)
				}
				lastImportCleanup = time.Now()
			}
		}
		run()
		for {
			select {
			case <-ticker.C:
				run()
			case <-stopRetention:
				return
			}
		}
	}()

	// 启动服务器
	srv := server.New(cfg, asyncRepo, blobStore, archiveManager)

	// 平台相关的运行逻辑（Windows: 系统托盘, 其他: 直接运行）
	if err := platformRun(srv, cfg, *showConsole); err != nil {
		log.Fatalf("运行失败: %v", err)
	}
}

func startOfLocalDay(value time.Time, loc *time.Location) time.Time {
	y, m, d := value.In(loc).Date()
	return time.Date(y, m, d, 0, 0, 0, 0, loc)
}

func scheduledArchiveHandled(jobs []storage.ArchiveJob, cutoff time.Time) bool {
	for _, job := range jobs {
		if job.Trigger == "scheduled" && job.Cutoff.Equal(cutoff) && job.Status != "failed" {
			return true
		}
	}
	return false
}
