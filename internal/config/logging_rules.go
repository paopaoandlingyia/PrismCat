package config

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"path"
	"regexp"
	"strings"
	"sync"
)

const (
	LoggingModeAll       = "all"
	LoggingModeAllowlist = "allowlist"
	LoggingModeDenylist  = "denylist"

	PathMatcherAnt   = "ant"
	PathMatcherRegex = "regex"
)

//go:embed ai_models.json
var systemAIModelsJSON []byte

type LoggingPathRule struct {
	Matcher string `yaml:"matcher" json:"matcher"`
	Pattern string `yaml:"pattern" json:"pattern"`

	compiled *regexp.Regexp
}

type LoggingPathFilterConfig struct {
	Mode  string            `yaml:"mode" json:"mode"`
	Rules []LoggingPathRule `yaml:"rules" json:"rules"`
}

type ModelPathTemplate struct {
	Tag   string            `yaml:"tag" json:"tag"`
	Rules []LoggingPathRule `yaml:"rules" json:"rules"`
}

type SystemModelPathTemplate struct {
	Tag         string            `json:"tag"`
	DisplayName string            `json:"display_name"`
	Description string            `json:"description"`
	Provider    string            `json:"provider"`
	Category    string            `json:"category"`
	Initialize  bool              `json:"initialize"`
	Includes    []string          `json:"includes,omitempty"`
	Rules       []LoggingPathRule `json:"rules"`
}

type systemAIModelCatalog struct {
	SchemaVersion int `json:"schema_version"`
	Templates     []struct {
		Tag         string   `json:"tag"`
		DisplayName string   `json:"display_name"`
		Description string   `json:"description"`
		Provider    string   `json:"provider"`
		Category    string   `json:"category"`
		Initialize  bool     `json:"initialize"`
		Includes    []string `json:"includes"`
		Paths       []string `json:"paths"`
	} `json:"templates"`
}

type LoggingRulesConfig struct {
	ModelPathTemplatesInitialized bool                `yaml:"model_path_templates_initialized" json:"model_path_templates_initialized"`
	ModelPathTemplates            []ModelPathTemplate `yaml:"model_path_templates" json:"model_path_templates"`
}

var (
	systemTemplatesOnce sync.Once
	systemTemplates     []SystemModelPathTemplate
	systemTemplatesErr  error
)

func SystemModelPathTemplates() ([]SystemModelPathTemplate, error) {
	systemTemplatesOnce.Do(func() {
		var catalog systemAIModelCatalog
		if err := json.Unmarshal(systemAIModelsJSON, &catalog); err != nil {
			systemTemplatesErr = fmt.Errorf("parse embedded ai_models.json: %w", err)
			return
		}
		systemTemplates, systemTemplatesErr = buildSystemModelPathTemplates(catalog)
	})
	return cloneSystemModelPathTemplates(systemTemplates), systemTemplatesErr
}

func buildSystemModelPathTemplates(catalog systemAIModelCatalog) ([]SystemModelPathTemplate, error) {
	if catalog.SchemaVersion != 1 {
		return nil, fmt.Errorf("validate embedded ai_models.json: unsupported schema_version %d", catalog.SchemaVersion)
	}
	if len(catalog.Templates) == 0 {
		return nil, fmt.Errorf("validate embedded ai_models.json: templates are required")
	}

	tagIndexes := make(map[string]int, len(catalog.Templates))
	for i := range catalog.Templates {
		template := &catalog.Templates[i]
		template.Tag = strings.TrimSpace(template.Tag)
		template.DisplayName = strings.TrimSpace(template.DisplayName)
		template.Description = strings.TrimSpace(template.Description)
		template.Provider = strings.ToLower(strings.TrimSpace(template.Provider))
		template.Category = strings.ToLower(strings.TrimSpace(template.Category))
		if template.Tag == "" || template.DisplayName == "" || template.Description == "" || template.Provider == "" || template.Category == "" {
			return nil, fmt.Errorf("validate embedded ai_models.json: template %d is missing required metadata", i)
		}
		key := strings.ToLower(template.Tag)
		if _, exists := tagIndexes[key]; exists {
			return nil, fmt.Errorf("validate embedded ai_models.json: duplicate template tag %q", template.Tag)
		}
		tagIndexes[key] = i
	}

	resolved := make([][]LoggingPathRule, len(catalog.Templates))
	resolving := make([]bool, len(catalog.Templates))
	var resolve func(int) ([]LoggingPathRule, error)
	resolve = func(index int) ([]LoggingPathRule, error) {
		if resolved[index] != nil {
			return resolved[index], nil
		}
		if resolving[index] {
			return nil, fmt.Errorf("template %q has a circular include", catalog.Templates[index].Tag)
		}
		resolving[index] = true
		template := &catalog.Templates[index]
		rules := make([]LoggingPathRule, 0, len(template.Paths))
		for includeIndex, include := range template.Includes {
			include = strings.TrimSpace(include)
			includedIndex, ok := tagIndexes[strings.ToLower(include)]
			if !ok {
				return nil, fmt.Errorf("template %q includes unknown template %q", template.Tag, include)
			}
			template.Includes[includeIndex] = catalog.Templates[includedIndex].Tag
			includedRules, err := resolve(includedIndex)
			if err != nil {
				return nil, err
			}
			rules = append(rules, includedRules...)
		}
		for _, pattern := range template.Paths {
			rules = append(rules, LoggingPathRule{Matcher: PathMatcherAnt, Pattern: pattern})
		}
		if len(rules) == 0 {
			return nil, fmt.Errorf("template %q has no paths or included paths", template.Tag)
		}
		normalized, err := normalizeLoggingPathRules(rules)
		if err != nil {
			return nil, fmt.Errorf("template %q: %w", template.Tag, err)
		}
		resolving[index] = false
		resolved[index] = normalized
		return normalized, nil
	}

	templates := make([]SystemModelPathTemplate, 0, len(catalog.Templates))
	for i, raw := range catalog.Templates {
		rules, err := resolve(i)
		if err != nil {
			return nil, fmt.Errorf("validate embedded ai_models.json: %w", err)
		}
		templates = append(templates, SystemModelPathTemplate{
			Tag:         raw.Tag,
			DisplayName: raw.DisplayName,
			Description: raw.Description,
			Provider:    raw.Provider,
			Category:    raw.Category,
			Initialize:  raw.Initialize,
			Includes:    append([]string(nil), raw.Includes...),
			Rules:       append([]LoggingPathRule(nil), rules...),
		})
	}
	return templates, nil
}

func NormalizeLoggingRules(in LoggingRulesConfig) (LoggingRulesConfig, error) {
	templates, err := normalizeModelPathTemplates(in.ModelPathTemplates)
	if err != nil {
		return LoggingRulesConfig{}, err
	}
	in.ModelPathTemplates = templates
	return in, nil
}

func NormalizeLoggingPathFilter(in *LoggingPathFilterConfig) (*LoggingPathFilterConfig, error) {
	if in == nil {
		return nil, nil
	}
	out := *in
	out.Mode = strings.ToLower(strings.TrimSpace(out.Mode))
	if out.Mode == "" {
		out.Mode = LoggingModeAll
	}
	switch out.Mode {
	case LoggingModeAll, LoggingModeAllowlist, LoggingModeDenylist:
	default:
		return nil, fmt.Errorf("invalid logging path filter mode %q", out.Mode)
	}

	rules, err := normalizeLoggingPathRules(out.Rules)
	if err != nil {
		return nil, err
	}
	out.Rules = rules
	if out.Mode != LoggingModeAll && len(out.Rules) == 0 {
		return nil, fmt.Errorf("logging path filter mode %q requires at least one rule", out.Mode)
	}
	return &out, nil
}

func normalizeModelPathTemplates(in []ModelPathTemplate) ([]ModelPathTemplate, error) {
	if in == nil {
		return nil, nil
	}
	out := make([]ModelPathTemplate, 0, len(in))
	seen := make(map[string]struct{}, len(in))
	for _, template := range in {
		template.Tag = strings.TrimSpace(template.Tag)
		if template.Tag == "" {
			return nil, fmt.Errorf("model path template tag is required")
		}
		key := strings.ToLower(template.Tag)
		if _, ok := seen[key]; ok {
			return nil, fmt.Errorf("duplicate model path template tag %q", template.Tag)
		}
		seen[key] = struct{}{}
		rules, err := normalizeLoggingPathRules(template.Rules)
		if err != nil {
			return nil, fmt.Errorf("model path template %q: %w", template.Tag, err)
		}
		template.Rules = rules
		out = append(out, template)
	}
	return out, nil
}

func normalizeLoggingPathRules(in []LoggingPathRule) ([]LoggingPathRule, error) {
	out := make([]LoggingPathRule, 0, len(in))
	seen := make(map[string]struct{}, len(in))
	for _, rule := range in {
		rule.Matcher = strings.ToLower(strings.TrimSpace(rule.Matcher))
		if rule.Matcher == "" {
			rule.Matcher = PathMatcherAnt
		}
		rule.Pattern = strings.TrimSpace(rule.Pattern)
		if rule.Pattern == "" {
			continue
		}

		switch rule.Matcher {
		case PathMatcherAnt:
			if !strings.HasPrefix(rule.Pattern, "/") {
				rule.Pattern = "/" + rule.Pattern
			}
			if err := validateAntPattern(rule.Pattern); err != nil {
				return nil, fmt.Errorf("invalid ant pattern %q: %w", rule.Pattern, err)
			}
		case PathMatcherRegex:
			compiled, err := regexp.Compile(`\A(?:` + rule.Pattern + `)\z`)
			if err != nil {
				return nil, fmt.Errorf("invalid regex pattern %q: %w", rule.Pattern, err)
			}
			rule.compiled = compiled
		default:
			return nil, fmt.Errorf("invalid path matcher %q", rule.Matcher)
		}

		key := rule.Matcher + "\x00" + rule.Pattern
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, rule)
	}
	return out, nil
}

func validateAntPattern(pattern string) error {
	for _, segment := range strings.Split(strings.TrimPrefix(pattern, "/"), "/") {
		if strings.Contains(segment, "**") {
			if segment != "**" {
				return fmt.Errorf("** must occupy a complete path segment")
			}
			continue
		}
		if _, err := path.Match(segment, ""); err != nil {
			return err
		}
	}
	return nil
}

func (f *LoggingPathFilterConfig) Allows(requestPath string) bool {
	if f == nil || f.Mode == LoggingModeAll || f.Mode == "" {
		return true
	}
	matched := false
	for _, rule := range f.Rules {
		if rule.matches(requestPath) {
			matched = true
			break
		}
	}
	if f.Mode == LoggingModeAllowlist {
		return matched
	}
	return !matched
}

func (r LoggingPathRule) matches(requestPath string) bool {
	switch r.Matcher {
	case PathMatcherAnt:
		return matchAntPath(r.Pattern, requestPath)
	case PathMatcherRegex:
		compiled := r.compiled
		if compiled == nil {
			compiled, _ = regexp.Compile(`\A(?:` + r.Pattern + `)\z`)
		}
		return compiled != nil && compiled.MatchString(requestPath)
	default:
		return false
	}
}

func matchAntPath(pattern, requestPath string) bool {
	patternParts := strings.Split(strings.TrimPrefix(pattern, "/"), "/")
	pathParts := strings.Split(strings.TrimPrefix(requestPath, "/"), "/")
	type state struct{ pattern, path int }
	memo := make(map[state]bool)
	seen := make(map[state]bool)
	var match func(int, int) bool
	match = func(pi, si int) bool {
		key := state{pi, si}
		if seen[key] {
			return memo[key]
		}
		seen[key] = true

		var result bool
		switch {
		case pi == len(patternParts):
			result = si == len(pathParts)
		case patternParts[pi] == "**":
			result = match(pi+1, si) || (si < len(pathParts) && match(pi, si+1))
		case si < len(pathParts):
			segmentMatch, _ := path.Match(patternParts[pi], pathParts[si])
			result = segmentMatch && match(pi+1, si+1)
		}
		memo[key] = result
		return result
	}
	return match(0, 0)
}

func cloneLoggingPathFilter(in *LoggingPathFilterConfig) *LoggingPathFilterConfig {
	if in == nil {
		return nil
	}
	out := *in
	out.Rules = append([]LoggingPathRule(nil), in.Rules...)
	return &out
}

func cloneModelPathTemplates(in []ModelPathTemplate) []ModelPathTemplate {
	if in == nil {
		return nil
	}
	out := make([]ModelPathTemplate, len(in))
	for i, template := range in {
		out[i] = template
		out[i].Rules = append([]LoggingPathRule(nil), template.Rules...)
	}
	return out
}

func cloneSystemModelPathTemplates(in []SystemModelPathTemplate) []SystemModelPathTemplate {
	if in == nil {
		return nil
	}
	out := make([]SystemModelPathTemplate, len(in))
	for i, template := range in {
		out[i] = template
		out[i].Includes = append([]string(nil), template.Includes...)
		out[i].Rules = append([]LoggingPathRule(nil), template.Rules...)
	}
	return out
}

func (c *Config) LoggingRulesSnapshot() LoggingRulesConfig {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := c.LogRules
	out.ModelPathTemplates = cloneModelPathTemplates(c.LogRules.ModelPathTemplates)
	return out
}

func (c *Config) ReplaceModelPathTemplates(templates []ModelPathTemplate) error {
	normalized, err := normalizeModelPathTemplates(templates)
	if err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	previous := c.LogRules.ModelPathTemplates
	c.LogRules.ModelPathTemplates = normalized
	if err := c.saveLocked(); err != nil {
		c.LogRules.ModelPathTemplates = previous
		return err
	}
	return nil
}

func (c *Config) EnsureModelPathTemplatesInitialized() error {
	catalog, err := SystemModelPathTemplates()
	if err != nil {
		return err
	}
	defaults := make([]ModelPathTemplate, 0, len(catalog))
	for _, template := range catalog {
		if template.Initialize {
			defaults = append(defaults, ModelPathTemplate{
				Tag:   template.Tag,
				Rules: append([]LoggingPathRule(nil), template.Rules...),
			})
		}
	}
	if len(defaults) == 0 {
		return fmt.Errorf("embedded ai_models.json has no templates marked for initialization")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.LogRules.ModelPathTemplatesInitialized {
		return nil
	}
	previous := c.LogRules
	if len(c.LogRules.ModelPathTemplates) == 0 {
		c.LogRules.ModelPathTemplates = defaults
	}
	c.LogRules.ModelPathTemplatesInitialized = true
	if err := c.saveLocked(); err != nil {
		c.LogRules = previous
		return fmt.Errorf("save initialized model path templates: %w", err)
	}
	return nil
}
