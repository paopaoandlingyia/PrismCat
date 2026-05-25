package usage

import (
	"bufio"
	"bytes"
	"encoding/json"
	"mime"
	"strconv"
	"strings"

	"github.com/paopaoandlingyia/PrismCat/internal/config"
	"github.com/paopaoandlingyia/PrismCat/internal/httpbody"
)

type Result struct {
	InputTokens  *int64
	OutputTokens *int64
	TotalTokens  *int64
	Raw          string
	Source       string
}

func Extract(cfg config.UsageExtractionConfig, upstream string, contentType string, contentEncoding string, body []byte) Result {
	if len(body) == 0 {
		return Result{}
	}

	cfg = config.NormalizeUsageExtraction(cfg)
	if !cfg.Enabled {
		return Result{}
	}
	binding, ok := cfg.Upstreams[strings.ToLower(strings.TrimSpace(upstream))]
	if !ok || !binding.Enabled || len(binding.RuleNames) == 0 {
		return Result{}
	}

	rules := selectedRules(cfg.Rules, binding.RuleNames, contentType)
	if len(rules) == 0 {
		return Result{}
	}

	data := body
	if hasContentEncoding(contentEncoding) {
		formatted := httpbody.FormatForDisplay(contentType, contentEncoding, body, httpbody.FormatOptions{
			MaxOutputBytes: int64(len(body)) * 32,
		})
		if formatted.Text == "" || formatted.Binary {
			return Result{}
		}
		data = []byte(formatted.Text)
	}

	if isSSE(contentType, data) {
		return extractFromSSE(rules, data)
	}
	return extractFromJSONDocument(rules, data)
}

func selectedRules(rules []config.UsageExtractionRule, names []string, contentType string) []config.UsageExtractionRule {
	nameSet := make(map[string]struct{}, len(names))
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name != "" {
			nameSet[name] = struct{}{}
		}
	}

	out := make([]config.UsageExtractionRule, 0, len(rules))
	for _, rule := range rules {
		if !rule.Enabled || strings.TrimSpace(rule.Name) == "" {
			continue
		}
		if _, ok := nameSet[rule.Name]; !ok {
			continue
		}
		if !matchesContentType(rule.Match.ContentTypes, contentType) {
			continue
		}
		out = append(out, rule)
	}
	return out
}

func matchesContentType(allowed []string, contentType string) bool {
	if len(allowed) == 0 {
		return true
	}
	mediaType := normalizedContentType(contentType)
	for _, candidate := range allowed {
		candidate = normalizedContentType(candidate)
		if candidate != "" && strings.EqualFold(candidate, mediaType) {
			return true
		}
	}
	return false
}

func normalizedContentType(contentType string) string {
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		mediaType = strings.TrimSpace(strings.Split(contentType, ";")[0])
	}
	return strings.ToLower(mediaType)
}

func hasContentEncoding(contentEncoding string) bool {
	for _, part := range strings.Split(contentEncoding, ",") {
		token := strings.ToLower(strings.TrimSpace(part))
		if token != "" && token != "identity" {
			return true
		}
	}
	return false
}

func isSSE(contentType string, data []byte) bool {
	if strings.EqualFold(normalizedContentType(contentType), "text/event-stream") {
		return true
	}
	return bytes.Contains(data, []byte("\ndata:")) || bytes.HasPrefix(data, []byte("data:"))
}

func extractFromSSE(rules []config.UsageExtractionRule, data []byte) Result {
	var best Result
	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if !bytes.HasPrefix(line, []byte("data:")) {
			continue
		}
		eventData := bytes.TrimSpace(bytes.TrimPrefix(line, []byte("data:")))
		if len(eventData) == 0 || bytes.Equal(eventData, []byte("[DONE]")) {
			continue
		}
		next := extractFromJSONDocument(rules, eventData)
		if next.Source != "" {
			best = mergeResults(best, next)
		}
	}
	return best
}

func mergeResults(previous Result, next Result) Result {
	if previous.Source == "" {
		return next
	}

	merged := next
	if merged.InputTokens == nil {
		merged.InputTokens = previous.InputTokens
	}
	if merged.OutputTokens == nil {
		merged.OutputTokens = previous.OutputTokens
	}
	if merged.TotalTokens == nil {
		merged.TotalTokens = previous.TotalTokens
	}
	if merged.InputTokens != nil && merged.OutputTokens != nil {
		total := *merged.InputTokens + *merged.OutputTokens
		merged.TotalTokens = &total
	}
	return merged
}

func extractFromJSONDocument(rules []config.UsageExtractionRule, data []byte) Result {
	data = bytes.TrimSpace(data)
	if len(data) == 0 {
		return Result{}
	}

	var doc interface{}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(&doc); err != nil {
		return Result{}
	}

	for _, rule := range rules {
		result := extractWithRule(rule, doc)
		if result.Source != "" {
			return result
		}
	}
	return Result{}
}

func extractWithRule(rule config.UsageExtractionRule, doc interface{}) Result {
	var result Result
	result.InputTokens = firstIntAt(doc, rule.Paths.InputTokens)
	result.OutputTokens = firstIntAt(doc, rule.Paths.OutputTokens)
	result.TotalTokens = firstIntAt(doc, rule.Paths.TotalTokens)
	if rawValue, path, ok := firstValueAt(doc, rule.Paths.RawUsage); ok {
		if b, err := json.Marshal(rawValue); err == nil {
			result.Raw = string(b)
			result.Source = rule.Name + ":" + path
		}
	}
	if result.Source == "" && (result.InputTokens != nil || result.OutputTokens != nil || result.TotalTokens != nil) {
		result.Source = rule.Name
	}
	if result.TotalTokens == nil && result.InputTokens != nil && result.OutputTokens != nil {
		total := *result.InputTokens + *result.OutputTokens
		result.TotalTokens = &total
	}
	return result
}

func firstIntAt(doc interface{}, paths []string) *int64 {
	for _, path := range paths {
		if value, _, ok := firstValueAt(doc, []string{path}); ok {
			if n, ok := numericValue(value); ok {
				return &n
			}
		}
	}
	return nil
}

func firstValueAt(doc interface{}, paths []string) (interface{}, string, bool) {
	for _, path := range paths {
		value, ok := valueAtPointer(doc, path)
		if ok {
			return value, path, true
		}
	}
	return nil, "", false
}

func numericValue(value interface{}) (int64, bool) {
	switch v := value.(type) {
	case float64:
		return int64(v), v >= 0 && v == float64(int64(v))
	case int64:
		return v, true
	case json.Number:
		n, err := v.Int64()
		return n, err == nil
	case string:
		n, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
		return n, err == nil
	default:
		return 0, false
	}
}

func valueAtPointer(doc interface{}, pointer string) (interface{}, bool) {
	if pointer == "" {
		return doc, true
	}
	if !strings.HasPrefix(pointer, "/") {
		return nil, false
	}
	current := doc
	for _, raw := range strings.Split(pointer[1:], "/") {
		token := strings.ReplaceAll(strings.ReplaceAll(raw, "~1", "/"), "~0", "~")
		switch v := current.(type) {
		case map[string]interface{}:
			next, ok := v[token]
			if !ok {
				return nil, false
			}
			current = next
		case []interface{}:
			idx, err := strconv.Atoi(token)
			if err != nil || idx < 0 || idx >= len(v) {
				return nil, false
			}
			current = v[idx]
		default:
			return nil, false
		}
	}
	return current, true
}
