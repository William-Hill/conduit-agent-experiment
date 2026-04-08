package hitl

import (
	"os"
	"strconv"
	"strings"
	"time"
)

// HITL operating modes.
const (
	ModeFull   = "full"
	ModeYolo   = "yolo"
	ModeCustom = "custom"
)

// Config holds all HITL gate settings.
type Config struct {
	Mode               string        // full, yolo, custom
	Gate1Enabled       bool          // Gate 1: issue selection approval
	Gate1PollInterval  time.Duration // how often to poll for label changes
	Gate3Enabled       bool          // Gate 3: PR review with bot loop
	Gate3PollInterval  time.Duration // how often to poll for human PR action
	ResolveBotComments bool          // resolve threads after fixing bot comments
	BotReviewWait      time.Duration // time to wait for bot reviews after triggering
	BotMaxIterations   int           // max bot review → fix cycles
	BotReviewers       []string      // trigger comments to post (e.g., "@coderabbitai review")
}

// LoadConfig loads HITL configuration from environment variables.
func LoadConfig() Config {
	mode := envOrDefault("HITL_MODE", ModeFull)

	cfg := Config{
		Mode:               mode,
		Gate1PollInterval:  envDurationOrDefault("HITL_GATE1_POLL_INTERVAL", 5*time.Minute),
		Gate3PollInterval:  envDurationOrDefault("HITL_GATE3_POLL_INTERVAL", 5*time.Minute),
		ResolveBotComments: envBoolOrDefault("HITL_RESOLVE_BOT_COMMENTS", true),
		BotReviewWait:      envDurationOrDefault("HITL_BOT_REVIEW_WAIT", 120*time.Second),
		BotMaxIterations:   envIntOrDefault("HITL_BOT_MAX_ITERATIONS", 3),
		BotReviewers:       envListOrDefault("HITL_BOT_REVIEWERS", []string{"@coderabbitai review", "@greptile review"}),
	}

	switch mode {
	case ModeYolo:
		cfg.Gate1Enabled = false
		cfg.Gate3Enabled = false
	case ModeCustom:
		cfg.Gate1Enabled = envBoolOrDefault("HITL_GATE1_ENABLED", true)
		cfg.Gate3Enabled = envBoolOrDefault("HITL_GATE3_ENABLED", true)
	default:
		cfg.Gate1Enabled = true
		cfg.Gate3Enabled = true
	}

	return cfg
}

func envOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envBoolOrDefault(key string, fallback bool) bool {
	s := os.Getenv(key)
	if s == "" {
		return fallback
	}
	v, err := strconv.ParseBool(s)
	if err != nil {
		return fallback
	}
	return v
}

func envIntOrDefault(key string, fallback int) int {
	s := os.Getenv(key)
	if s == "" {
		return fallback
	}
	v, err := strconv.Atoi(s)
	if err != nil {
		return fallback
	}
	return v
}

func envDurationOrDefault(key string, fallback time.Duration) time.Duration {
	s := os.Getenv(key)
	if s == "" {
		return fallback
	}
	v, err := time.ParseDuration(s)
	if err != nil {
		return fallback
	}
	return v
}

func envListOrDefault(key string, fallback []string) []string {
	s := os.Getenv(key)
	if s == "" {
		return fallback
	}
	parts := strings.Split(s, ",")
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			result = append(result, p)
		}
	}
	return result
}
