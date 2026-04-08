package hitl

import (
	"testing"
	"time"
)

func TestLoadConfig_Defaults(t *testing.T) {
	t.Setenv("HITL_MODE", "")
	t.Setenv("HITL_GATE1_ENABLED", "")
	t.Setenv("HITL_GATE3_ENABLED", "")
	t.Setenv("HITL_BOT_REVIEWERS", "")
	cfg := LoadConfig()

	if cfg.Mode != "full" {
		t.Errorf("Mode = %q, want %q", cfg.Mode, "full")
	}
	if !cfg.Gate1Enabled {
		t.Error("Gate1Enabled should be true by default")
	}
	if !cfg.Gate3Enabled {
		t.Error("Gate3Enabled should be true by default")
	}
	if cfg.Gate1PollInterval != 5*time.Minute {
		t.Errorf("Gate1PollInterval = %v, want %v", cfg.Gate1PollInterval, 5*time.Minute)
	}
	if cfg.Gate3PollInterval != 5*time.Minute {
		t.Errorf("Gate3PollInterval = %v, want %v", cfg.Gate3PollInterval, 5*time.Minute)
	}
	if !cfg.ResolveBotComments {
		t.Error("ResolveBotComments should be true by default")
	}
	if cfg.BotReviewWait != 120*time.Second {
		t.Errorf("BotReviewWait = %v, want %v", cfg.BotReviewWait, 120*time.Second)
	}
	if cfg.BotMaxIterations != 3 {
		t.Errorf("BotMaxIterations = %d, want 3", cfg.BotMaxIterations)
	}
}

func TestLoadConfig_YoloMode(t *testing.T) {
	t.Setenv("HITL_MODE", "yolo")
	cfg := LoadConfig()

	if cfg.Mode != "yolo" {
		t.Errorf("Mode = %q, want %q", cfg.Mode, "yolo")
	}
	if cfg.Gate1Enabled {
		t.Error("Gate1Enabled should be false in yolo mode")
	}
	if cfg.Gate3Enabled {
		t.Error("Gate3Enabled should be false in yolo mode")
	}
}

func TestLoadConfig_CustomMode(t *testing.T) {
	t.Setenv("HITL_MODE", "custom")
	t.Setenv("HITL_GATE1_ENABLED", "false")
	t.Setenv("HITL_GATE3_ENABLED", "true")
	t.Setenv("HITL_GATE1_POLL_INTERVAL", "10m")
	t.Setenv("HITL_BOT_MAX_ITERATIONS", "5")

	cfg := LoadConfig()

	if cfg.Mode != "custom" {
		t.Errorf("Mode = %q, want %q", cfg.Mode, "custom")
	}
	if cfg.Gate1Enabled {
		t.Error("Gate1Enabled should be false")
	}
	if !cfg.Gate3Enabled {
		t.Error("Gate3Enabled should be true")
	}
	if cfg.Gate1PollInterval != 10*time.Minute {
		t.Errorf("Gate1PollInterval = %v, want %v", cfg.Gate1PollInterval, 10*time.Minute)
	}
	if cfg.BotMaxIterations != 5 {
		t.Errorf("BotMaxIterations = %d, want 5", cfg.BotMaxIterations)
	}
}

func TestLoadConfig_BotReviewers(t *testing.T) {
	t.Setenv("HITL_BOT_REVIEWERS", "@coderabbitai review,@greptile review")
	cfg := LoadConfig()

	if len(cfg.BotReviewers) != 2 {
		t.Fatalf("BotReviewers len = %d, want 2", len(cfg.BotReviewers))
	}
	if cfg.BotReviewers[0] != "@coderabbitai review" {
		t.Errorf("BotReviewers[0] = %q, want %q", cfg.BotReviewers[0], "@coderabbitai review")
	}
	if cfg.BotReviewers[1] != "@greptile review" {
		t.Errorf("BotReviewers[1] = %q, want %q", cfg.BotReviewers[1], "@greptile review")
	}
}
