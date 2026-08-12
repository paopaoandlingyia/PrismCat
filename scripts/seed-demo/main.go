// Command seed-demo 生成一份用于截图的演示数据库。
//
// 存在的理由:README 首图需要展示"多上游 + 混合状态码 + token 计量",而真实
// 使用中的库往往长时间只打一个上游,一屏全是相同的成功请求,什么都证明不了;
// 而且真实库里的上游地址通常是私有中转,不适合公开。
//
// 这里只写入公开 API 端点(api.openai.com 之类,人人文档里都有),不含任何凭据。
//
//	go run ./scripts/seed-demo              # 写入 ./data/demo.db
//	go run ./scripts/seed-demo -n 400       # 多灌一些
//	go run ./scripts/seed-demo -out /tmp/x.db
//
// 然后把 config.yaml 的 storage.database 指到这个文件、重启,截完图改回来即可。
// 脚本拒绝写入 ./data/prismcat.db,避免误伤真实数据。
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/paopaoandlingyia/PrismCat/internal/storage"
)

const productionDB = "prismcat.db"

type provider struct {
	name    string
	baseURL string
	path    string
	model   string
	// 该上游是否走 OpenAI 兼容的 /v1/models 探活
	hasModelsEndpoint bool
}

var providers = []provider{
	{"openai", "https://api.openai.com", "/v1/chat/completions", "gpt-4o", true},
	{"anthropic", "https://api.anthropic.com", "/v1/messages", "claude-sonnet-4-5", false},
	{"gemini", "https://generativelanguage.googleapis.com", "/v1beta/models/gemini-2.5-flash:generateContent", "gemini-2.5-flash", false},
	{"deepseek", "https://api.deepseek.com", "/v1/chat/completions", "deepseek-chat", true},
	{"groq", "https://api.groq.com", "/openai/v1/chat/completions", "llama-3.3-70b-versatile", true},
	{"openrouter", "https://openrouter.ai", "/api/v1/chat/completions", "qwen/qwen3-235b-a22b", true},
}

// 权重决定每个上游出现的频率 —— 均匀分布看着像假数据,真实场景总有主力上游
var providerWeights = []int{30, 26, 12, 12, 10, 10}

type failure struct {
	status  int
	errType string
	message string
	// 传输层错误(没拿到响应),会写进 error 列
	transport bool
}

var failures = []failure{
	{429, "rate_limit_error", "Rate limit reached for requests", false},
	{429, "rate_limit_error", "You exceeded your current quota", false},
	{401, "authentication_error", "Incorrect API key provided", false},
	{400, "invalid_request_error", "max_tokens is too large: 200000", false},
	{500, "api_error", "The server had an error while processing your request", false},
	{502, "", "upstream connect error: connection reset by peer", true},
	{504, "", "context deadline exceeded (Client.Timeout)", true},
}

var prompts = []string{
	"帮我把这段 Go 代码重构成更符合惯例的写法",
	"Summarize the attached RFC in five bullet points",
	"这段 SQL 为什么会走全表扫描？",
	"Write a unit test for the retry logic below",
	"解释一下 TCP 的拥塞控制和流量控制有什么区别",
	"Translate this changelog into English, keep the markdown",
	"审查这个 PR,重点看并发安全",
	"Given this stack trace, what is the most likely root cause?",
}

var tags = []string{"", "", "", "", "", "batch-eval", "prod", "smoke-test", "regression"}

var overrideRules = [][]string{
	{"strip-origin-for-claude"},
	{"force-temperature"},
	{"strip-origin-for-claude", "inject-user-id"},
}

func main() {
	out := flag.String("out", filepath.Join("data", "demo.db"), "输出数据库路径")
	count := flag.Int("n", 260, "生成多少条日志")
	hours := flag.Int("hours", 72, "时间跨度(小时),从现在往前")
	seed := flag.Int64("seed", 20260813, "随机种子,同一个种子结果可复现")
	force := flag.Bool("force", false, "目标库已有日志时仍然写入")
	flag.Parse()

	if err := run(*out, *count, *hours, *seed, *force); err != nil {
		fmt.Fprintln(os.Stderr, "seed-demo:", err)
		os.Exit(1)
	}
}

func run(out string, count, hours int, seed int64, force bool) error {
	if count <= 0 {
		return fmt.Errorf("-n 必须大于 0")
	}
	abs, err := filepath.Abs(out)
	if err != nil {
		return err
	}
	if strings.EqualFold(filepath.Base(abs), productionDB) {
		return fmt.Errorf("拒绝写入 %s —— 那是真实数据库,换个 -out", productionDB)
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return err
	}

	repo, err := storage.NewSQLiteRepository(abs)
	if err != nil {
		return fmt.Errorf("打开数据库: %w", err)
	}
	defer repo.Close()

	// 复用项目自己的仓库层建表,schema 不会和生产逻辑漂移
	stats, err := repo.GetStats(nil)
	if err != nil {
		return fmt.Errorf("读取现有统计: %w", err)
	}
	if stats.TotalRequests > 0 && !force {
		return fmt.Errorf("%s 里已有 %d 条日志,加 -force 才会继续写入", abs, stats.TotalRequests)
	}

	rng := rand.New(rand.NewSource(seed))
	now := time.Now()
	span := time.Duration(hours) * time.Hour

	for i := 0; i < count; i++ {
		// 时间往近处压,最近几小时更密,像是还在用的样子
		frac := rng.Float64() * rng.Float64()
		createdAt := now.Add(-time.Duration(frac * float64(span)))
		log := makeLog(rng, createdAt)
		if err := repo.SaveLog(log); err != nil {
			return fmt.Errorf("写入第 %d 条: %w", i+1, err)
		}
	}

	// 几条串起来的调用链,让调用链页面不是空的
	for t := 0; t < 3; t++ {
		traceID := fmt.Sprintf("task-%04d", rng.Intn(9000)+1000)
		base := now.Add(-time.Duration(rng.Intn(hours)) * time.Hour)
		steps := rng.Intn(3) + 3
		var parentID string
		for s := 0; s < steps; s++ {
			log := makeLog(rng, base.Add(time.Duration(s)*47*time.Second))
			log.TraceID = traceID
			log.TraceSeq = s + 1
			log.ParentLogID = parentID
			if err := repo.SaveLog(log); err != nil {
				return fmt.Errorf("写入调用链: %w", err)
			}
			parentID = log.ID
		}
	}

	final, err := repo.GetStats(nil)
	if err != nil {
		return err
	}
	fmt.Printf("已写入 %s\n", abs)
	fmt.Printf("  总计 %d 条 / 成功 %d / 错误 %d / 流式 %d / 平均延迟 %.0fms\n",
		final.TotalRequests, final.SuccessCount, final.ErrorCount, final.StreamingCount, final.AvgLatency)
	fmt.Printf("  上游 %d 个:%s\n", len(final.ByUpstream), strings.Join(sortedKeys(final.ByUpstream), ", "))

	// 首图截的就是第一页,先在这儿看一眼它够不够杂 —— 全是 200 就白灌了
	if err := printFirstPage(repo); err != nil {
		return err
	}
	fmt.Println()
	fmt.Println("把 config.yaml 的 storage.database 指到上面这个路径并重启,截完图改回去即可。")
	return nil
}

func printFirstPage(repo *storage.SQLiteRepository) error {
	logs, _, err := repo.ListLogs(storage.LogFilter{Limit: 20})
	if err != nil {
		return fmt.Errorf("预览首页: %w", err)
	}
	seenUpstream := map[string]bool{}
	seenStatus := map[int]bool{}
	for _, l := range logs {
		seenUpstream[l.Upstream] = true
		seenStatus[l.StatusCode] = true
	}
	codes := make([]string, 0, len(seenStatus))
	for _, l := range logs {
		c := fmt.Sprintf("%d", l.StatusCode)
		if !contains(codes, c) {
			codes = append(codes, c)
		}
	}
	fmt.Printf("  首页 20 条里:上游 %d 种、状态码 %s\n", len(seenUpstream), strings.Join(codes, "/"))
	if len(seenUpstream) < 3 || len(seenStatus) < 2 {
		fmt.Println("  ⚠ 首页不够杂,换个 -seed 重新生成")
	}
	return nil
}

func contains(list []string, v string) bool {
	for _, item := range list {
		if item == v {
			return true
		}
	}
	return false
}

func sortedKeys(m map[string]int64) []string {
	keys := make([]string, 0, len(m))
	for _, p := range providers {
		if _, ok := m[p.name]; ok {
			keys = append(keys, p.name)
		}
	}
	return keys
}

func makeLog(rng *rand.Rand, createdAt time.Time) *storage.RequestLog {
	p := pickProvider(rng)

	// 一成左右是探活 GET,其余是对话请求
	if p.hasModelsEndpoint && rng.Intn(10) == 0 {
		return &storage.RequestLog{
			CreatedAt:        createdAt,
			Upstream:         p.name,
			TargetURL:        p.baseURL + "/v1/models",
			Method:           "GET",
			Path:             "/v1/models",
			RequestHeaders:   headers(p, false),
			StatusCode:       200,
			ResponseHeaders:  respHeaders(false),
			ResponseBody:     `{"object":"list","data":[{"id":"` + p.model + `","object":"model"}]}`,
			ResponseBodySize: 96,
			Latency:          int64(rng.Intn(380) + 60),
		}
	}

	streaming := rng.Intn(100) < 62
	prompt := prompts[rng.Intn(len(prompts))]
	reqBody := chatRequestBody(p, prompt, streaming)

	log := &storage.RequestLog{
		CreatedAt:       createdAt,
		Upstream:        p.name,
		TargetURL:       p.baseURL + p.path,
		Method:          "POST",
		Path:            p.path,
		RequestHeaders:  headers(p, streaming),
		RequestBody:     reqBody,
		RequestBodySize: int64(len(reqBody)),
		Streaming:       streaming,
		Tag:             tags[rng.Intn(len(tags))],
	}

	// 多目标预设的上游会记下当次用的是哪个
	if p.name == "anthropic" && rng.Intn(4) == 0 {
		log.UpstreamTarget = "backup"
	}

	// 约一成二失败 —— 全绿的截图看着像假的,而错误可见性正是这个工具的卖点
	if rng.Intn(100) < 12 {
		f := failures[rng.Intn(len(failures))]
		log.StatusCode = f.status
		log.Latency = int64(rng.Intn(520) + 80)
		if f.transport {
			log.Error = f.message
			log.StatusCode = f.status
			log.Streaming = false
			return log
		}
		body, _ := json.Marshal(map[string]any{
			"error": map[string]string{"type": f.errType, "message": f.message},
		})
		log.ResponseHeaders = respHeaders(false)
		log.ResponseBody = string(body)
		log.ResponseBodySize = int64(len(body))
		log.Streaming = false
		return log
	}

	log.StatusCode = 200
	log.ResponseHeaders = respHeaders(streaming)
	if streaming {
		log.Latency = int64(rng.Intn(23000) + 1800)
	} else {
		log.Latency = int64(rng.Intn(3600) + 420)
	}

	in := int64(rng.Intn(29000) + 600)
	outTok := int64(rng.Intn(3800) + 90)
	total := in + outTok
	log.UsageInputTokens = &in
	log.UsageOutputTokens = &outTok
	log.UsageTotalTokens = &total
	log.UsageSource = "builtin:" + p.name
	usageRaw, _ := json.Marshal(map[string]int64{"input_tokens": in, "output_tokens": outTok})
	log.UsageRaw = string(usageRaw)

	respBody := chatResponseBody(p, in, outTok)
	log.ResponseBody = respBody
	log.ResponseBodySize = int64(len(respBody))

	// 少数请求命中了参数覆盖规则
	if rng.Intn(8) == 0 {
		log.RequestOverrideApplied = true
		log.RequestOverrideRules = overrideRules[rng.Intn(len(overrideRules))]
		log.RequestBodyOriginal = reqBody
		log.RequestBodyFinal = reqBody
	}
	return log
}

func pickProvider(rng *rand.Rand) provider {
	sum := 0
	for _, w := range providerWeights {
		sum += w
	}
	n := rng.Intn(sum)
	for i, w := range providerWeights {
		if n < w {
			return providers[i]
		}
		n -= w
	}
	return providers[0]
}

func headers(p provider, streaming bool) map[string][]string {
	h := map[string][]string{
		"Content-Type": {"application/json"},
		"User-Agent":   {"prismcat-demo/1.0"},
		"Accept":       {"application/json"},
	}
	if streaming {
		h["Accept"] = []string{"text/event-stream"}
	}
	switch p.name {
	case "anthropic":
		h["Anthropic-Version"] = []string{"2023-06-01"}
		h["X-Api-Key"] = []string{"sk-ant-***"}
	default:
		h["Authorization"] = []string{"Bearer sk-***"}
	}
	return h
}

func respHeaders(streaming bool) map[string][]string {
	h := map[string][]string{"Content-Type": {"application/json"}}
	if streaming {
		h["Content-Type"] = []string{"text/event-stream"}
		h["Cache-Control"] = []string{"no-cache"}
	}
	return h
}

func chatRequestBody(p provider, prompt string, streaming bool) string {
	payload := map[string]any{
		"model":    p.model,
		"messages": []map[string]string{{"role": "user", "content": prompt}},
		"stream":   streaming,
	}
	if p.name == "anthropic" {
		payload["max_tokens"] = 4096
	}
	b, _ := json.MarshalIndent(payload, "", "  ")
	return string(b)
}

func chatResponseBody(p provider, in, out int64) string {
	text := "这是演示数据,内容为占位文本。"
	if p.name == "anthropic" {
		b, _ := json.MarshalIndent(map[string]any{
			"type":    "message",
			"role":    "assistant",
			"model":   p.model,
			"content": []map[string]string{{"type": "text", "text": text}},
			"usage":   map[string]int64{"input_tokens": in, "output_tokens": out},
		}, "", "  ")
		return string(b)
	}
	b, _ := json.MarshalIndent(map[string]any{
		"object": "chat.completion",
		"model":  p.model,
		"choices": []map[string]any{{
			"index":         0,
			"message":       map[string]string{"role": "assistant", "content": text},
			"finish_reason": "stop",
		}},
		"usage": map[string]int64{"prompt_tokens": in, "completion_tokens": out, "total_tokens": in + out},
	}, "", "  ")
	return string(b)
}
