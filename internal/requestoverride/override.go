package requestoverride

import (
	"encoding/json"
	"errors"
	"fmt"
	"mime"
	"net/http"
	"strings"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"

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
	if !gjson.ValidBytes(body) {
		return Result{Body: body}, fmt.Errorf("parse JSON request body: invalid JSON")
	}

	current := string(body)
	applied := make([]string, 0)
	for _, rule := range selectedRules(cfg, info.Upstream) {
		if !baseMatches(rule, info) {
			continue
		}
		if !jsonConditionsMatch(rule.Match.JSON, current) {
			continue
		}
		next, err := ApplyPatch(current, rule.Patch)
		if err != nil {
			return Result{Body: body, AppliedRuleNames: applied}, fmt.Errorf("apply request override %q: %w", ruleName(rule), err)
		}
		current = next
		applied = append(applied, ruleName(rule))
	}

	if len(applied) == 0 {
		return Result{Body: body}, nil
	}
	return Result{AppliedRuleNames: applied, Body: []byte(current)}, nil
}

// ApplyHeaders applies header override operations from matching rules.
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

// ApplyPatch applies a sequence of override operations to a JSON document.
//
// Supported ops:
//   - set     : set value at path (auto-creates parent objects/arrays)
//   - remove  : delete value at path
//   - default : set value at path only if path does not exist
//   - append  : append value to the array at path (creates array if missing)
//   - prepend : prepend value to the array at path (creates array if missing)
//
// Paths use the gjson/sjson dot-notation: "metadata.user_id", "system.0.type".
func ApplyPatch(jsonStr string, ops []config.RequestOverridePatch) (string, error) {
	current := jsonStr
	for _, op := range ops {
		name := strings.ToLower(strings.TrimSpace(op.Op))
		path := strings.TrimSpace(op.Path)
		if path == "" {
			return current, fmt.Errorf("%s requires path", name)
		}
		switch name {
		case "set":
			next, err := sjson.Set(current, path, op.Value)
			if err != nil {
				return current, fmt.Errorf("set %s: %w", path, err)
			}
			current = next
		case "remove":
			next, err := sjson.Delete(current, path)
			if err != nil {
				return current, fmt.Errorf("remove %s: %w", path, err)
			}
			current = next
		case "default":
			if gjson.Get(current, path).Exists() {
				continue
			}
			next, err := sjson.Set(current, path, op.Value)
			if err != nil {
				return current, fmt.Errorf("default %s: %w", path, err)
			}
			current = next
		case "append":
			next, err := arrayAppend(current, path, op.Value)
			if err != nil {
				return current, fmt.Errorf("append %s: %w", path, err)
			}
			current = next
		case "prepend":
			next, err := arrayPrepend(current, path, op.Value)
			if err != nil {
				return current, fmt.Errorf("prepend %s: %w", path, err)
			}
			current = next
		default:
			return current, fmt.Errorf("unsupported op %q (supported: set, remove, default, append, prepend)", op.Op)
		}
	}
	return current, nil
}

func arrayAppend(jsonStr, path string, value interface{}) (string, error) {
	existing := gjson.Get(jsonStr, path)
	if !existing.Exists() {
		return sjson.Set(jsonStr, path, []interface{}{value})
	}
	if !existing.IsArray() {
		return jsonStr, fmt.Errorf("path %s is not an array", path)
	}
	// sjson uses -1 to append to an array.
	return sjson.Set(jsonStr, path+".-1", value)
}

func arrayPrepend(jsonStr, path string, value interface{}) (string, error) {
	existing := gjson.Get(jsonStr, path)
	if !existing.Exists() {
		return sjson.Set(jsonStr, path, []interface{}{value})
	}
	if !existing.IsArray() {
		return jsonStr, fmt.Errorf("path %s is not an array", path)
	}
	current := existing.Array()
	next := make([]interface{}, 0, len(current)+1)
	next = append(next, value)
	for _, item := range current {
		next = append(next, item.Value())
	}
	return sjson.Set(jsonStr, path, next)
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

func jsonConditionsMatch(conditions []config.RequestOverrideJSONCondition, jsonStr string) bool {
	for _, condition := range conditions {
		result := gjson.Get(jsonStr, condition.Path)
		exists := result.Exists()
		if condition.Exists != nil && *condition.Exists != exists {
			return false
		}
		if !exists {
			return false
		}
		if condition.Equals != nil {
			if !gjsonValueEquals(result, condition.Equals) {
				return false
			}
		}
		if condition.StartsWith != "" {
			if result.Type != gjson.String || !strings.HasPrefix(result.String(), condition.StartsWith) {
				return false
			}
		}
		if len(condition.In) > 0 {
			ok := false
			for _, want := range condition.In {
				if gjsonValueEquals(result, want) {
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

func gjsonValueEquals(result gjson.Result, want interface{}) bool {
	wantBytes, err := json.Marshal(want)
	if err != nil {
		return false
	}
	gotBytes := []byte(result.Raw)
	if !gjson.ValidBytes(gotBytes) {
		return false
	}
	// Normalize both sides through a round-trip to compare values semantically.
	var gotVal, wantVal interface{}
	if err := json.Unmarshal(gotBytes, &gotVal); err != nil {
		return false
	}
	if err := json.Unmarshal(wantBytes, &wantVal); err != nil {
		return false
	}
	return jsonDeepEqual(gotVal, wantVal)
}

func jsonDeepEqual(a, b interface{}) bool {
	ab, err1 := json.Marshal(a)
	bb, err2 := json.Marshal(b)
	if err1 != nil || err2 != nil {
		return false
	}
	return string(ab) == string(bb)
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
