// Package config loads and validates SentinelOps application configuration.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/gogf/gf/v2/frame/g"
	"github.com/gogf/gf/v2/os/gcfg"
	"gopkg.in/yaml.v3"
)

const (
	baseFileName  = "config.yaml"
	localFileName = "config.local.yaml"
)

type Config struct {
	Providers    map[string]Provider `yaml:"providers" json:"providers"`
	ModelCatalog map[string]Model    `yaml:"model_catalog" json:"model_catalog"`
	Routing      Routing             `yaml:"routing" json:"routing"`
}

type Provider struct {
	APIKey    string    `yaml:"api_key" json:"api_key"`
	Endpoints Endpoints `yaml:"endpoints" json:"endpoints"`
}

type Endpoints struct {
	OpenAICompatible string `yaml:"openai_compatible" json:"openai_compatible"`
	DashScope        string `yaml:"dashscope" json:"dashscope"`
	DashScopeRerank  string `yaml:"dashscope_rerank" json:"dashscope_rerank"`
}

type Model struct {
	ModelID      string   `yaml:"model_id" json:"model_id"`
	Driver       string   `yaml:"driver" json:"driver"`
	Capabilities []string `yaml:"capabilities" json:"capabilities"`
	Dimension    int      `yaml:"dimension,omitempty" json:"dimension,omitempty"`
	Pricing      Pricing  `yaml:"pricing" json:"pricing"`
}

type Pricing struct {
	Currency    string  `yaml:"currency" json:"currency"`
	Unit        string  `yaml:"unit" json:"unit"`
	Input       float64 `yaml:"input" json:"input"`
	CachedInput float64 `yaml:"cached_input,omitempty" json:"cached_input,omitempty"`
	Output      float64 `yaml:"output,omitempty" json:"output,omitempty"`
}

type Routing struct {
	Chat      map[string]Route `yaml:"chat" json:"chat"`
	Embedding map[string]Route `yaml:"embedding" json:"embedding"`
	Rerank    map[string]Route `yaml:"rerank" json:"rerank"`
}

type Route struct {
	Model   string       `yaml:"model" json:"model"`
	Options RouteOptions `yaml:"options,omitempty" json:"options,omitempty"`
}

type RouteOptions struct {
	EnableThinking bool   `yaml:"enable_thinking,omitempty" json:"enable_thinking,omitempty"`
	Instruct       string `yaml:"instruct,omitempty" json:"instruct,omitempty"`
}

var (
	currentMu sync.RWMutex
	current   *Config
)

func Parse(data []byte) (*Config, error) {
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse configuration: %w", err)
	}
	return &cfg, nil
}

// LoadDirectory treats config.local.yaml as a complete replacement when it
// exists and otherwise loads config.yaml. It intentionally performs no merge.
func LoadDirectory(dir string) (*Config, string, error) {
	selected := filepath.Join(dir, baseFileName)
	local := filepath.Join(dir, localFileName)
	if info, err := os.Stat(local); err == nil && !info.IsDir() {
		selected = local
	} else if err != nil && !os.IsNotExist(err) {
		return nil, "", fmt.Errorf("inspect local configuration: %w", err)
	}

	data, err := os.ReadFile(selected)
	if err != nil {
		return nil, "", fmt.Errorf("read configuration %s: %w", selected, err)
	}
	cfg, err := Parse(data)
	if err != nil {
		return nil, "", err
	}
	if err := cfg.Validate(); err != nil {
		return nil, "", fmt.Errorf("validate configuration %s: %w", selected, err)
	}

	adapter, err := gcfg.NewAdapterFile(filepath.Base(selected))
	if err != nil {
		return nil, "", fmt.Errorf("create GoFrame configuration adapter: %w", err)
	}
	if err := adapter.SetPath(filepath.Dir(selected)); err != nil {
		return nil, "", fmt.Errorf("set configuration directory: %w", err)
	}
	g.Cfg().SetAdapter(adapter)
	SetCurrent(cfg)
	return cfg, selected, nil
}

func SetCurrent(cfg *Config) {
	currentMu.Lock()
	defer currentMu.Unlock()
	current = cfg
}

func Current() (*Config, error) {
	currentMu.RLock()
	defer currentMu.RUnlock()
	if current == nil {
		return nil, fmt.Errorf("application configuration has not been loaded")
	}
	return current, nil
}

func (c *Config) Validate() error {
	if len(c.Providers) == 0 {
		return fmt.Errorf("at least one provider is required")
	}
	for name, provider := range c.Providers {
		if strings.TrimSpace(name) == "" {
			return fmt.Errorf("provider name is required")
		}
		if provider.Endpoints.OpenAICompatible == "" || provider.Endpoints.DashScope == "" || provider.Endpoints.DashScopeRerank == "" {
			return fmt.Errorf("provider %s requires openai_compatible, dashscope, and dashscope_rerank endpoints", name)
		}
	}
	if len(c.ModelCatalog) == 0 {
		return fmt.Errorf("model_catalog is required")
	}
	for ref, model := range c.ModelCatalog {
		providerName, err := providerFromRef(ref)
		if err != nil {
			return err
		}
		if _, ok := c.Providers[providerName]; !ok {
			return fmt.Errorf("model %s references missing provider %s", ref, providerName)
		}
		if model.ModelID == "" {
			return fmt.Errorf("model %s requires model_id", ref)
		}
		switch model.Driver {
		case "openai_compatible":
			if !hasCapability(model, "chat") || !hasCapability(model, "tool_calling") {
				return fmt.Errorf("model %s openai_compatible driver requires chat and tool_calling capabilities", ref)
			}
		case "dashscope":
			if !hasCapability(model, "embedding") {
				return fmt.Errorf("model %s dashscope driver requires embedding capability", ref)
			}
			if model.Dimension != 2048 {
				return fmt.Errorf("model %s embedding dimension must be 2048", ref)
			}
		case "dashscope_rerank":
			if !hasCapability(model, "rerank") {
				return fmt.Errorf("model %s dashscope_rerank driver requires rerank capability", ref)
			}
		default:
			return fmt.Errorf("model %s uses unsupported driver %q", ref, model.Driver)
		}
		if model.Pricing.Currency != "CNY" || model.Pricing.Unit != "per_million_tokens" {
			return fmt.Errorf("model %s pricing must use CNY per_million_tokens", ref)
		}
	}
	if err := c.validateRoute("routing.chat.default", c.Routing.Chat["default"], "chat", "tool_calling"); err != nil {
		return err
	}
	if err := c.validateRoute("routing.chat.reasoning", c.Routing.Chat["reasoning"], "chat", "tool_calling"); err != nil {
		return err
	}
	if err := c.validateRoute("routing.embedding.default", c.Routing.Embedding["default"], "embedding"); err != nil {
		return err
	}
	if err := c.validateRoute("routing.rerank.default", c.Routing.Rerank["default"], "rerank"); err != nil {
		return err
	}
	return nil
}

func (c *Config) validateRoute(name string, route Route, capabilities ...string) error {
	if route.Model == "" {
		return fmt.Errorf("%s model is required", name)
	}
	model, ok := c.ModelCatalog[route.Model]
	if !ok {
		return fmt.Errorf("%s references missing model %s", name, route.Model)
	}
	for _, capability := range capabilities {
		if !hasCapability(model, capability) {
			return fmt.Errorf("%s model %s lacks %s capability", name, route.Model, capability)
		}
	}
	return nil
}

func providerFromRef(ref string) (string, error) {
	provider, _, ok := strings.Cut(ref, "/")
	if !ok || provider == "" {
		return "", fmt.Errorf("model reference %q must use provider/model format", ref)
	}
	return provider, nil
}

func hasCapability(model Model, want string) bool {
	for _, capability := range model.Capabilities {
		if capability == want {
			return true
		}
	}
	return false
}

func (c *Config) Resolve(route Route) (Provider, Model, error) {
	model, ok := c.ModelCatalog[route.Model]
	if !ok {
		return Provider{}, Model{}, fmt.Errorf("routing references missing model %s", route.Model)
	}
	providerName, err := providerFromRef(route.Model)
	if err != nil {
		return Provider{}, Model{}, err
	}
	provider, ok := c.Providers[providerName]
	if !ok {
		return Provider{}, Model{}, fmt.Errorf("routing references missing provider %s", providerName)
	}
	return provider, model, nil
}
