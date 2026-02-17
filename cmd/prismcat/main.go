package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/getlantern/systray"
	"github.com/prismcat/prismcat/internal/config"
	"github.com/prismcat/prismcat/internal/server"
	"github.com/prismcat/prismcat/internal/storage"
	"github.com/skratchdot/open-golang/open"
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

var (
	kernel32         = syscall.NewLazyDLL("kernel32.dll")
	user32           = syscall.NewLazyDLL("user32.dll")
	getConsoleWindow = kernel32.NewProc("GetConsoleWindow")
	showWindow       = user32.NewProc("ShowWindow")
	allocConsole     = kernel32.NewProc("AllocConsole")
)

const (
	SW_HIDE = 0
	SW_SHOW = 5
)

func hideConsole() {
	hwnd, _, _ := getConsoleWindow.Call()
	if hwnd != 0 {
		showWindow.Call(hwnd, SW_HIDE)
	}
}

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

	detachingRepo := storage.NewDetachingRepository(sqliteRepo, blobStore, cfg)
	asyncRepo := storage.NewAsyncRepository(detachingRepo, cfg.Storage.AsyncBuffer)
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

	// Windows 控制台处理
	if runtime.GOOS == "windows" {
		if *showConsole {
			hwnd, _, _ := getConsoleWindow.Call()
			if hwnd == 0 {
				// 如果当前没有控制台（通常是 GUI 模式双击启动），尝试分配一个
				allocConsole.Call()
			} else {
				// 如果已有控制台，确保显示出来
				showWindow.Call(hwnd, SW_SHOW)
			}
		} else {
			// 如果不要求显示，则尝试隐藏现有的
			hideConsole()
		}
	}

	// 运行系统托盘
	systray.Run(func() {
		systray.SetIcon(iconData)
		systray.SetTitle("PrismCat")
		systray.SetTooltip("PrismCat LLM Proxy " + config.Version)

		titleOpen, titleQuit := getTrayLabels()
		mOpen := systray.AddMenuItem(titleOpen, "")
		systray.AddSeparator()
		mQuit := systray.AddMenuItem(titleQuit, "")

		// 托盘菜单事件循环
		go func() {
			for {
				select {
				case <-mOpen.ClickedCh:
					open.Run(fmt.Sprintf("http://localhost:%d", cfg.Server.Port))
				case <-mQuit.ClickedCh:
					systray.Quit()
				}
			}
		}()

		// 在后台启动服务器
		go func() {
			if err := srv.Start(); err != nil {
				log.Printf("服务器错误: %v", err)
				systray.Quit()
			}
		}()

	}, func() {
		log.Printf("PrismCat %s 正在退出...", config.Version)
	})
}

func getTrayLabels() (openTitle, quitTitle string) {
	if runtime.GOOS == "windows" {
		userDefaultUILang := kernel32.NewProc("GetUserDefaultUILanguage")
		ret, _, _ := userDefaultUILang.Call()
		primaryLangId := uint16(ret) & 0x3ff
		if primaryLangId == 0x04 { // LANG_CHINESE
			return "打开仪表盘", "退出"
		}
	}
	return "Open Dashboard", "Exit"
}
