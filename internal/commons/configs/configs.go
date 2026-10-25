package configs

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Gemini    GeminiConfig    `yaml:"gemini"`
	Claude    ClaudeConfig    `yaml:"claude"`
	OpenAI    OpenAIConfig    `yaml:"openai"`
	Server    ServerConfig    `yaml:"server"`
	RateLimit RateLimitConfig `yaml:"rate_limit"`
	LogLevel  string          `yaml:"log_level"`
}

type RateLimitConfig struct {
	Enabled     bool `yaml:"enabled"`
	WindowMs    int  `yaml:"window_ms"`
	MaxRequests int  `yaml:"max_requests"`
}

type GeminiConfig struct {
	Proxy           string `yaml:"proxy"`
	Secure1PSID     string `yaml:"-"` // derived from Cookies
	Secure1PSIDTS   string `yaml:"-"` // derived from Cookies
	AuthUser        string `yaml:"auth_user"`
	RefreshInterval int    `yaml:"refresh_interval"`
	MaxRetries      int    `yaml:"max_retries"`
	Cookies         string `yaml:"cookies"` // optional legacy single-account header
	Temporary       bool   `yaml:"temporary"`
	AccountDir      string `yaml:"account_dir"`
}

type ClaudeConfig struct {
	APIKey  string `yaml:"api_key"`
	Model   string `yaml:"model"`
	Cookies string `yaml:"cookies"`
}

type OpenAIConfig struct {
	APIKey  string `yaml:"api_key"`
	Model   string `yaml:"model"`
	Cookies string `yaml:"cookies"`
}

type ServerConfig struct {
	Port    int    `yaml:"port"`
	APIKey  string `yaml:"sk"`      // required on every API endpoint
	AddKey  string `yaml:"add_key"` // required in the POST /v1/addAccount body
	BaseURL string `yaml:"base_url"`
}

const (
	defaultConfigFile            = "config.yml"
	defaultServerPort            = 4981
	defaultGeminiRefreshInterval = 1440
	// Rotating cookies more often than this looks automated to Google and
	// tends to get the session flagged or blocked.
	minGeminiRefreshInterval = 10
	defaultGeminiMaxRetries  = 3
	defaultGeminiAccountDir  = "data/db"
	defaultLogLevel          = "info"
	defaultRateLimitWindowMs = 60000
	defaultRateLimitMax      = 10
)

// New loads config.yml from the working directory. CONFIG_FILE overrides the
// path, which is useful for container mounts and tests.
func New() (*Config, error) {
	path := strings.TrimSpace(os.Getenv("CONFIG_FILE"))
	if path == "" {
		path = defaultConfigFile
	}
	return Load(path)
}

// Load reads and validates one YAML configuration file.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config %s: %w", path, err)
	}

	cfg := Config{
		LogLevel: defaultLogLevel,
		Server:   ServerConfig{Port: defaultServerPort},
		RateLimit: RateLimitConfig{
			WindowMs:    defaultRateLimitWindowMs,
			MaxRequests: defaultRateLimitMax,
		},
		Gemini: GeminiConfig{
			RefreshInterval: defaultGeminiRefreshInterval,
			MaxRetries:      defaultGeminiMaxRetries,
			AccountDir:      defaultGeminiAccountDir,
		},
	}
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config %s: %w", path, err)
	}

	cfg.normalize()

	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	return &cfg, nil
}

func (c *Config) normalize() {
	c.LogLevel = strings.TrimSpace(c.LogLevel)
	c.Server.APIKey = strings.TrimSpace(c.Server.APIKey)
	c.Server.AddKey = strings.TrimSpace(c.Server.AddKey)
	c.Server.BaseURL = strings.TrimRight(strings.TrimSpace(c.Server.BaseURL), "/")
	c.Gemini.Proxy = strings.TrimSpace(c.Gemini.Proxy)
	c.Gemini.AuthUser = strings.TrimSpace(c.Gemini.AuthUser)
	c.Gemini.AccountDir = strings.TrimSpace(c.Gemini.AccountDir)
	if c.Gemini.AccountDir == "" {
		c.Gemini.AccountDir = defaultGeminiAccountDir
	}

	c.Gemini.Secure1PSID = cookieValue(c.Gemini.Cookies, "__Secure-1PSID")
	c.Gemini.Secure1PSIDTS = cookieValue(c.Gemini.Cookies, "__Secure-1PSIDTS")

	if c.Gemini.RefreshInterval <= 0 {
		c.Gemini.RefreshInterval = defaultGeminiRefreshInterval
	} else if c.Gemini.RefreshInterval < minGeminiRefreshInterval {
		c.Gemini.RefreshInterval = minGeminiRefreshInterval
	}
}

func cookieValue(header, wanted string) string {
	for _, pair := range strings.Split(header, ";") {
		name, value, ok := strings.Cut(strings.TrimSpace(pair), "=")
		if ok && strings.TrimSpace(name) == wanted {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

// Validate checks if the configuration has required values.
func (c *Config) Validate() error {
	// gemini.cookies is a legacy single-account fallback. Accounts are normally
	// added through POST /v1/addAccount and stored in gemini.account_dir, so the
	// server may start with zero accounts.
	if strings.TrimSpace(c.Gemini.Cookies) != "" && (c.Gemini.Secure1PSID == "" || c.Gemini.Secure1PSIDTS == "") {
		return fmt.Errorf("gemini.cookies must contain __Secure-1PSID and __Secure-1PSIDTS")
	}

	if c.Gemini.AuthUser != "" {
		if n, err := strconv.Atoi(c.Gemini.AuthUser); err != nil || n < 0 {
			return fmt.Errorf("invalid gemini.auth_user value: %q (must be a non-negative account slot)", c.Gemini.AuthUser)
		}
	}

	if c.Server.Port == 0 {
		c.Server.Port = defaultServerPort
	}
	if c.Server.Port < 1 || c.Server.Port > 65535 {
		return fmt.Errorf("invalid server.port value: %d (must be between 1 and 65535)", c.Server.Port)
	}

	return nil
}
