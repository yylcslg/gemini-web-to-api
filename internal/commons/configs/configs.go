package configs

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/joho/godotenv"
)

type Config struct {
	Gemini    GeminiConfig
	Claude    ClaudeConfig
	OpenAI    OpenAIConfig
	Server    ServerConfig
	RateLimit RateLimitConfig
	LogLevel  string
}

type RateLimitConfig struct {
	Enabled     bool
	WindowMs    int
	MaxRequests int
}

type GeminiConfig struct {
	Proxy           string
	Secure1PSID     string
	Secure1PSIDTS   string
	AuthUser        string
	RefreshInterval int
	MaxRetries      int
	Cookies         string
	Temporary       bool
	AccountDir      string
}

type ClaudeConfig struct {
	APIKey  string
	Model   string
	Cookies string
}

type OpenAIConfig struct {
	APIKey  string
	Model   string
	Cookies string
}

type ServerConfig struct {
	Port    string
	APIKey  string // SK: required on every API endpoint
	AddKey  string // ADD_KEY/add_key: required in the POST /v1/addAccount body
	BaseURL string // BASE_URL/base_url: public origin for generated asset links
}

const (
	defaultServerPort            = "4981"
	defaultGeminiRefreshInterval = 1440
	// Rotating cookies more often than this looks automated to Google and
	// tends to get the session flagged or blocked.
	minGeminiRefreshInterval = 10
	defaultGeminiMaxRetries  = 3
	defaultGeminiAccountDir  = "data/db"
	defaultLogLevel          = "info"
)

func New() (*Config, error) {
	// Load .env file if it exists
	_ = godotenv.Load()

	var cfg Config

	// Server
	cfg.Server.Port = getEnv("PORT", defaultServerPort)
	// SK is required on every API endpoint.
	cfg.Server.APIKey = strings.TrimSpace(getEnv("SK", ""))
	// ADD_KEY guards the addAccount request body; add_key is the legacy spelling.
	cfg.Server.AddKey = strings.TrimSpace(getEnv("ADD_KEY", ""))
	if cfg.Server.AddKey == "" {
		cfg.Server.AddKey = strings.TrimSpace(getEnv("add_key", ""))
	}
	// BASE_URL is the public origin used for generated image links; empty means
	// derive it from the server IP and PORT.
	cfg.Server.BaseURL = strings.TrimSpace(getEnv("BASE_URL", ""))
	if cfg.Server.BaseURL == "" {
		cfg.Server.BaseURL = strings.TrimSpace(getEnv("base_url", ""))
	}

	// General
	cfg.LogLevel = getEnv("LOG_LEVEL", defaultLogLevel)

	// Rate Limit
	cfg.RateLimit.Enabled = getEnvBool("RATE_LIMIT_ENABLED", false)
	cfg.RateLimit.WindowMs = getEnvInt("RATE_LIMIT_WINDOW_MS", 60000)
	cfg.RateLimit.MaxRequests = getEnvInt("RATE_LIMIT_MAX_REQUESTS", 10)

	// Gemini
	cfg.Gemini.Proxy = strings.TrimSpace(os.Getenv("GEMINI_PROXY"))
	cfg.Gemini.Cookies = os.Getenv("GEMINI_COOKIES")
	cfg.Gemini.AuthUser = strings.TrimSpace(os.Getenv("GEMINI_AUTH_USER"))
	cfg.Gemini.Secure1PSID = cookieValue(cfg.Gemini.Cookies, "__Secure-1PSID")
	cfg.Gemini.Secure1PSIDTS = cookieValue(cfg.Gemini.Cookies, "__Secure-1PSIDTS")
	cfg.Gemini.RefreshInterval = getEnvInt("GEMINI_REFRESH_INTERVAL", defaultGeminiRefreshInterval)
	if cfg.Gemini.RefreshInterval <= 0 {
		cfg.Gemini.RefreshInterval = defaultGeminiRefreshInterval
	} else if cfg.Gemini.RefreshInterval < minGeminiRefreshInterval {
		cfg.Gemini.RefreshInterval = minGeminiRefreshInterval
	}
	cfg.Gemini.MaxRetries = getEnvInt("GEMINI_MAX_RETRIES", defaultGeminiMaxRetries)
	cfg.Gemini.Temporary = getEnvBool("GEMINI_TEMPORARY", false)
	cfg.Gemini.AccountDir = getEnv("GEMINI_ACCOUNT_DIR", defaultGeminiAccountDir)
	if cfg.Gemini.AuthUser != "" {
		if n, err := strconv.Atoi(cfg.Gemini.AuthUser); err != nil || n < 0 {
			return nil, fmt.Errorf("invalid GEMINI_AUTH_USER value: %q (must be a non-negative account slot)", cfg.Gemini.AuthUser)
		}
	}

	// Validate configuration
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	return &cfg, nil
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

// Validate checks if the configuration has required values
func (c *Config) Validate() error {
	// GEMINI_COOKIES is a legacy single-account fallback. Accounts are normally
	// added through POST /v1/addAccount and stored in GEMINI_ACCOUNT_DIR, so the
	// server may start with zero accounts.
	if strings.TrimSpace(c.Gemini.Cookies) != "" && (c.Gemini.Secure1PSID == "" || c.Gemini.Secure1PSIDTS == "") {
		return fmt.Errorf("GEMINI_COOKIES must contain __Secure-1PSID and __Secure-1PSIDTS")
	}

	// Check Server port is valid
	if c.Server.Port == "" {
		c.Server.Port = defaultServerPort
	}

	if _, err := strconv.Atoi(c.Server.Port); err != nil {
		return fmt.Errorf("invalid PORT value: %q (must be a number)", c.Server.Port)
	}

	return nil
}

func getEnv(key, defaultValue string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	return defaultValue
}

func getEnvInt(key string, defaultValue int) int {
	valueStr := os.Getenv(key)
	if valueStr == "" {
		return defaultValue
	}
	value, err := strconv.Atoi(valueStr)
	if err != nil {
		return defaultValue
	}
	return value
}

func getEnvBool(key string, defaultValue bool) bool {
	valueStr := os.Getenv(key)
	if valueStr == "" {
		return defaultValue
	}
	value, err := strconv.ParseBool(valueStr)
	if err != nil {
		return defaultValue
	}
	return value
}
