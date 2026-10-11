package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config holds runtime settings, all read from environment variables.
type Config struct {
	AppEnv      string
	Port        string
	DatabaseURL string
	JWTSecret   string
	AccessTTL   time.Duration
	RefreshTTL  time.Duration

	ShopName     string
	BahtPerPoint int      // one point per this many baht
	AdminEmails  []string // accounts registered with these emails become shop staff

	// The assistant calls Claude when an API key is set, otherwise it uses canned answers.
	AnthropicAPIKey string
	AnthropicModel  string
	AnthropicURL    string
}

func Load() (*Config, error) {
	cfg := &Config{
		AppEnv:          getenv("APP_ENV", "development"),
		Port:            getenv("PORT", "8080"),
		DatabaseURL:     os.Getenv("DATABASE_URL"),
		JWTSecret:       os.Getenv("JWT_SECRET"),
		AccessTTL:       getDuration("ACCESS_TOKEN_TTL", 15*time.Minute),
		RefreshTTL:      getDuration("REFRESH_TOKEN_TTL", 30*24*time.Hour),
		ShopName:        getenv("SHOP_NAME", "ร้านกาแฟตัวอย่าง"),
		BahtPerPoint:    getInt("BAHT_PER_POINT", 10),
		AdminEmails:     splitEmails(os.Getenv("ADMIN_EMAILS")),
		AnthropicAPIKey: os.Getenv("ANTHROPIC_API_KEY"),
		AnthropicModel:  os.Getenv("ANTHROPIC_MODEL"),
		AnthropicURL:    os.Getenv("ANTHROPIC_BASE_URL"),
	}

	if cfg.DatabaseURL == "" {
		return nil, fmt.Errorf("DATABASE_URL is required")
	}
	if len(cfg.JWTSecret) < 32 {
		return nil, fmt.Errorf("JWT_SECRET is required and must be at least 32 characters")
	}
	if cfg.BahtPerPoint < 1 {
		return nil, fmt.Errorf("BAHT_PER_POINT must be at least 1")
	}
	return cfg, nil
}

func splitEmails(v string) []string {
	var out []string
	for _, e := range strings.Split(v, ",") {
		if e = strings.ToLower(strings.TrimSpace(e)); e != "" {
			out = append(out, e)
		}
	}
	return out
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getInt(key string, fallback int) int {
	n, err := strconv.Atoi(os.Getenv(key))
	if err != nil {
		return fallback
	}
	return n
}

func getDuration(key string, fallback time.Duration) time.Duration {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return fallback
	}
	return d
}
