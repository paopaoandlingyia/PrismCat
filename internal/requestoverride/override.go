package requestoverride

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"mime"
	"net/http"
	"reflect"
	"strconv"
	"strings"

	"github.com/paopaoandlingyia/PrismCat/internal/config"
)

var (
	ErrBodyTooLarge        = errors.New("request body exceeds request override max_body_bytes")
	ErrUnsupportedEncoding = errors.New("request override only supports uncompressed request bodies")
	ErrUnsupportedContent  = errors.New("request override only supports JSON request bodies")
)

// RequestInfo describes the request metadata used to select override rules.
type RequestInfo struct {
	Upstream        string
	Method          string
	Path            string
	ContentType     string
	ContentEncoding string
}

// HeaderChange records a single header modification for logging.
type HeaderChange struct {
	Op       string `json:"op"`
	Name     string `json:"name"`
	Value    string `json:"value,omitempty"`
	OldValue string `json:"old_value,omitempty"`
}

// Result contains the possibly rewritten body and the names of applied rules.
type Result struct {
	AppliedRuleNames []string
	Body             []byte
	HeaderChanges    []HeaderChange
}

// HasCandidate reports whether the config contains any enabled rule that can
// apply to the request without inspecting the JSON body.
func HasCandidate(cfg config.RequestOverridesConfig, info RequestInfo) bool {
	if !cfg.Enabled {
		return false
	}
	for _, rule := range selectedRules(cfg, info.Upstream) {
		if baseMatches(rule, info) {
			return true
		}
	}
	return false
}

// Apply applies matching request override rules to a JSON request body.
func Apply(cfg config.RequestOverridesConfig, info RequestInfo, body []byte) (Result, error) {
	if !cfg.Enabled {
		return Result{Body: body}, nil
	}
	if !isIdentityEncoding(info.ContentEncoding) {
		return Result{Body: body}, ErrUnsupportedEncoding
	}
	if !isJSONContent(info.ContentType) {
		return Result{Body: body}, ErrUnsupportedContent
	}

	var doc interface{}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	if err := decoder.Decode(&doc); err != nil {
		return Result{Body: body}, fmt.Errorf("parse JSON request body: %w", err)
	}

	applied := make([]string, 0)
	for _, rule := range selectedRules(cfg, info.Upstream) {
		if !baseMatches(rule, info) {
			continue
		}
		if !jsonConditionsMatch(rule.Match.JSON, doc) {
			continue
		}
		next, err := ApplyPatch(doc, rule.Patch)
		if err != nil {
			return Result{Body: body, AppliedRuleNames: applied}, fmt.Errorf("apply request override %q: %w", ruleName(rule), err)
		}
		doc = next
		applied = append(applied, ruleName(rule))
	}

	if len(applied) == 0 {
		return Result{Body: body}, nil
	}

	out, err := json.Marshal(doc)
	if err != nil {
		return Result{Body: body, AppliedRuleNames: applied}, fmt.Errorf("marshal overridden JSON request body: %w", err)
	}
	return Result{AppliedRuleNames: applied, Body: out}, nil
}

// ApplyHeaders applies header override operations from matching rules.
// Header operations only require base matching (method/path); JSON body
// conditions are intentionally ignored so headers can be modified even for
// non-JSON or compressed requests.
func ApplyHeaders(cfg config.RequestOverridesConfig, info RequestInfo, header http.Header) ([]HeaderChange, []string) {
	if !cfg.Enabled {
		return nil, nil
	}
	var changes []HeaderChange
	var ruleNames []string
	for _, rule := range selectedRules(cfg, info.Upstream) {
		if !rule.Enabled || len(rule.Headers) == 0 {
			continue
		}
		if !baseMatchesForHeaders(rule, info) {
			continue
		}
		for _, h := range rule.Headers {
			switch h.Op {
			case "set":
				old := header.Get(h.Name)
				header.Set(h.Name, h.Value)
				changes = append(changes, HeaderChange{Op: "set", Name: h.Name, Value: h.Value, OldValue: old})
			case "remove":
				old := header.Get(h.Name)
				header.Del(h.Name)
				changes = append(changes, HeaderChange{Op: "remove", Name: h.Name, OldValue: old})
			}
		}
		ruleNames = append(ruleNames, ruleName(rule))
	}
	return changes, ruleNames
}

func baseMatchesForHeaders(rule config.RequestOverrideRule, info RequestInfo) bool {
	match := rule.Match
	if len(match.Methods) > 0 && !containsFold(match.Methods, info.Method) {
		return false
	}
	if len(match.Paths) > 0 && !contains(match.Paths, info.Path) {
		return false
	}
	if len(match.PathPrefixes) > 0 {
		ok := false
		for _, prefix := range match.PathPrefixes {
			if strings.HasPrefix(info.Path, prefix) {
				ok = true
				break
			}
		}
		if !ok {
			return false
		}
	}
	return true
}

// ApplyPatch applies a subset of JSON Patch operations to a decoded JSON value.
func ApplyPatch(doc interface{}, ops []config.RequestOverridePatch) (interface{}, error) {
	var current interface{}
	if err := normalizeJSONValue(doc, &current); err != nil {
		return nil, err
	}

	for _, op := range ops {
		name := strings.ToLower(strings.TrimSpace(op.Op))
		switch name {
		case "add":
			value, err := normalizedPatchValue(op.Value)
			if err != nil {
				return nil, err
			}
			next, err := addValue(current, op.Path, value)
			if err != nil {
				return nil, err
			}
			current = next
		case "remove":
			next, err := removeValue(current, op.Path)
			if err != nil {
				return nil, err
			}
			current = next
		case "replace":
			value, err := normalizedPatchValue(op.Value)
			if err != nil {
				return nil, err
			}
			next, err := replaceValue(current, op.Path, value)
			if err != nil {
				return nil, err
			}
			current = next
		case "move":
			if strings.TrimSpace(op.From) == "" {
				return nil, errors.New("move requires from")
			}
			value, err := getValue(current, op.From)
			if err != nil {
				return nil, err
			}
			current, err = removeValue(current, op.From)
			if err != nil {
				return nil, err
			}
			current, err = addValue(current, op.Path, cloneJSONValue(value))
			if err != nil {
				return nil, err
			}
		case "copy":
			if strings.TrimSpace(op.From) == "" {
				return nil, errors.New("copy requires from")
			}
			value, err := getValue(current, op.From)
			if err != nil {
				return nil, err
			}
			current, err = addValue(current, op.Path, cloneJSONValue(value))
			if err != nil {
				return nil, err
			}
		case "test":
			want, err := normalizedPatchValue(op.Value)
			if err != nil {
				return nil, err
			}
			got, err := getValue(current, op.Path)
			if err != nil {
				return nil, err
			}
			if !jsonEqual(got, want) {
				return nil, fmt.Errorf("test failed at %s", pointerLabel(op.Path))
			}
		default:
			return nil, fmt.Errorf("unsupported JSON Patch op %q", op.Op)
		}
	}

	return current, nil
}

func baseMatches(rule config.RequestOverrideRule, info RequestInfo) bool {
	if !rule.Enabled || len(rule.Patch) == 0 {
		return false
	}
	match := rule.Match
	if len(match.Methods) > 0 && !containsFold(match.Methods, info.Method) {
		return false
	}
	if len(match.Paths) > 0 && !contains(match.Paths, info.Path) {
		return false
	}
	if len(match.PathPrefixes) > 0 {
		ok := false
		for _, prefix := range match.PathPrefixes {
			if strings.HasPrefix(info.Path, prefix) {
				ok = true
				break
			}
		}
		if !ok {
			return false
		}
	}
	return true
}

func selectedRules(cfg config.RequestOverridesConfig, upstream string) []config.RequestOverrideRule {
	binding, ok := cfg.Upstreams[strings.ToLower(strings.TrimSpace(upstream))]
	if !ok || !binding.Enabled || len(binding.RuleNames) == 0 {
		return nil
	}

	rulesByName := make(map[string]config.RequestOverrideRule, len(cfg.Rules))
	for _, rule := range cfg.Rules {
		name := strings.TrimSpace(rule.Name)
		if name == "" {
			continue
		}
		rulesByName[name] = rule
	}

	out := make([]config.RequestOverrideRule, 0, len(binding.RuleNames))
	for _, name := range binding.RuleNames {
		if rule, ok := rulesByName[strings.TrimSpace(name)]; ok {
			out = append(out, rule)
		}
	}
	return out
}

func jsonConditionsMatch(conditions []config.RequestOverrideJSONCondition, doc interface{}) bool {
	for _, condition := range conditions {
		value, err := getValue(doc, condition.Path)
		exists := err == nil
		if condition.Exists != nil && *condition.Exists != exists {
			return false
		}
		if !exists {
			return false
		}
		if condition.Equals != nil {
			want, err := normalizedPatchValue(condition.Equals)
			if err != nil || !jsonEqual(value, want) {
				return false
			}
		}
		if condition.StartsWith != "" {
			s, ok := value.(string)
			if !ok || !strings.HasPrefix(s, condition.StartsWith) {
				return false
			}
		}
		if len(condition.In) > 0 {
			ok := false
			for _, item := range condition.In {
				want, err := normalizedPatchValue(item)
				if err == nil && jsonEqual(value, want) {
					ok = true
					break
				}
			}
			if !ok {
				return false
			}
		}
	}
	return true
}

func addValue(doc interface{}, path string, value interface{}) (interface{}, error) {
	tokens, err := parsePointer(path)
	if err != nil {
		return nil, err
	}
	if len(tokens) == 0 {
		return value, nil
	}
	parent, token, err := parentAt(doc, tokens)
	if err != nil {
		return nil, err
	}
	switch p := parent.(type) {
	case map[string]interface{}:
		p[token] = value
		return doc, nil
	case []interface{}:
		idx, err := parseArrayAddIndex(token, len(p))
		if err != nil {
			return nil, err
		}
		p = append(p, nil)
		copy(p[idx+1:], p[idx:])
		p[idx] = value
		return replaceAt(doc, tokens[:len(tokens)-1], p)
	default:
		return nil, fmt.Errorf("cannot add at %s", pointerLabel(path))
	}
}

func removeValue(doc interface{}, path string) (interface{}, error) {
	tokens, err := parsePointer(path)
	if err != nil {
		return nil, err
	}
	if len(tokens) == 0 {
		return nil, nil
	}
	parent, token, err := parentAt(doc, tokens)
	if err != nil {
		return nil, err
	}
	switch p := parent.(type) {
	case map[string]interface{}:
		if _, ok := p[token]; !ok {
			return nil, fmt.Errorf("path does not exist: %s", pointerLabel(path))
		}
		delete(p, token)
		return doc, nil
	case []interface{}:
		idx, err := parseArrayIndex(token, len(p))
		if err != nil {
			return nil, err
		}
		p = append(p[:idx], p[idx+1:]...)
		return replaceAt(doc, tokens[:len(tokens)-1], p)
	default:
		return nil, fmt.Errorf("cannot remove at %s", pointerLabel(path))
	}
}

func replaceValue(doc interface{}, path string, value interface{}) (interface{}, error) {
	tokens, err := parsePointer(path)
	if err != nil {
		return nil, err
	}
	if len(tokens) == 0 {
		return value, nil
	}
	if _, err := getValue(doc, path); err != nil {
		return nil, err
	}
	return replaceAt(doc, tokens, value)
}

func replaceAt(doc interface{}, tokens []string, value interface{}) (interface{}, error) {
	if len(tokens) == 0 {
		return value, nil
	}
	parent, token, err := parentAt(doc, tokens)
	if err != nil {
		return nil, err
	}
	switch p := parent.(type) {
	case map[string]interface{}:
		p[token] = value
		return doc, nil
	case []interface{}:
		idx, err := parseArrayIndex(token, len(p))
		if err != nil {
			return nil, err
		}
		p[idx] = value
		return replaceAt(doc, tokens[:len(tokens)-1], p)
	default:
		return nil, fmt.Errorf("cannot replace at %s", pointerLabel("/"+strings.Join(tokens, "/")))
	}
}

func parentAt(doc interface{}, tokens []string) (interface{}, string, error) {
	if len(tokens) == 0 {
		return nil, "", errors.New("root has no parent")
	}
	current := doc
	for _, token := range tokens[:len(tokens)-1] {
		switch v := current.(type) {
		case map[string]interface{}:
			next, ok := v[token]
			if !ok {
				return nil, "", fmt.Errorf("path does not exist: %s", token)
			}
			current = next
		case []interface{}:
			idx, err := parseArrayIndex(token, len(v))
			if err != nil {
				return nil, "", err
			}
			current = v[idx]
		default:
			return nil, "", fmt.Errorf("cannot traverse through %s", token)
		}
	}
	return current, tokens[len(tokens)-1], nil
}

func getValue(doc interface{}, path string) (interface{}, error) {
	tokens, err := parsePointer(path)
	if err != nil {
		return nil, err
	}
	current := doc
	for _, token := range tokens {
		switch v := current.(type) {
		case map[string]interface{}:
			next, ok := v[token]
			if !ok {
				return nil, fmt.Errorf("path does not exist: %s", pointerLabel(path))
			}
			current = next
		case []interface{}:
			idx, err := parseArrayIndex(token, len(v))
			if err != nil {
				return nil, err
			}
			current = v[idx]
		default:
			return nil, fmt.Errorf("cannot read %s", pointerLabel(path))
		}
	}
	return current, nil
}

func parsePointer(path string) ([]string, error) {
	if path == "" {
		return nil, nil
	}
	if !strings.HasPrefix(path, "/") {
		return nil, fmt.Errorf("invalid JSON pointer %q", path)
	}
	raw := strings.Split(path[1:], "/")
	out := make([]string, len(raw))
	for i, token := range raw {
		token = strings.ReplaceAll(token, "~1", "/")
		token = strings.ReplaceAll(token, "~0", "~")
		out[i] = token
	}
	return out, nil
}

func parseArrayIndex(token string, length int) (int, error) {
	if token == "-" {
		return 0, errors.New("'-' is only valid for add")
	}
	idx, err := strconv.Atoi(token)
	if err != nil || idx < 0 || idx >= length {
		return 0, fmt.Errorf("array index out of range: %s", token)
	}
	return idx, nil
}

func parseArrayAddIndex(token string, length int) (int, error) {
	if token == "-" {
		return length, nil
	}
	idx, err := strconv.Atoi(token)
	if err != nil || idx < 0 || idx > length {
		return 0, fmt.Errorf("array index out of range: %s", token)
	}
	return idx, nil
}

func normalizedPatchValue(value interface{}) (interface{}, error) {
	var out interface{}
	if err := normalizeJSONValue(value, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func normalizeJSONValue(in interface{}, out *interface{}) error {
	data, err := json.Marshal(in)
	if err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	return decoder.Decode(out)
}

func cloneJSONValue(value interface{}) interface{} {
	out, err := normalizedPatchValue(value)
	if err != nil {
		return value
	}
	return out
}

func jsonEqual(left, right interface{}) bool {
	left, _ = normalizedPatchValue(left)
	right, _ = normalizedPatchValue(right)
	return reflect.DeepEqual(left, right)
}

func isIdentityEncoding(contentEncoding string) bool {
	for _, token := range strings.Split(contentEncoding, ",") {
		t := strings.TrimSpace(strings.ToLower(token))
		if t != "" && t != "identity" {
			return false
		}
	}
	return true
}

func isJSONContent(contentType string) bool {
	if contentType == "" {
		return false
	}
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		mediaType = strings.TrimSpace(strings.Split(contentType, ";")[0])
	}
	mediaType = strings.ToLower(mediaType)
	return mediaType == "application/json" || strings.HasSuffix(mediaType, "+json")
}

func contains(in []string, want string) bool {
	for _, item := range in {
		if item == want {
			return true
		}
	}
	return false
}

func containsFold(in []string, want string) bool {
	for _, item := range in {
		if strings.EqualFold(item, want) {
			return true
		}
	}
	return false
}

func ruleName(rule config.RequestOverrideRule) string {
	if strings.TrimSpace(rule.Name) != "" {
		return strings.TrimSpace(rule.Name)
	}
	return "unnamed rule"
}

func pointerLabel(path string) string {
	if path == "" {
		return "/"
	}
	return path
}
