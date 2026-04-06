package config

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/viper"
)

// Config holds all experiment configuration.
type Config struct {
	Target    TargetConfig    `mapstructure:"target"`
	GitHub    GitHubConfig    `mapstructure:"github"`
	Policy    PolicyConfig    `mapstructure:"policy"`
	Execution ExecutionConfig `mapstructure:"execution"`
	Reporting ReportingConfig `mapstructure:"reporting"`
}

type GitHubConfig struct {
	Owner      string `mapstructure:"owner"`
	Repo       string `mapstructure:"repo"`
	ForkOwner  string `mapstructure:"fork_owner"`
	BaseBranch string `mapstructure:"base_branch"`
}

type TargetConfig struct {
	RepoPath string `mapstructure:"repo_path"`
	Ref      string `mapstructure:"ref"`
}

type PolicyConfig struct {
	MaxDifficulty    string `mapstructure:"max_difficulty"`
	MaxBlastRadius   string `mapstructure:"max_blast_radius"`
	AllowPush        bool   `mapstructure:"allow_push"`
	AllowMerge       bool   `mapstructure:"allow_merge"`
	RequireRationale bool   `mapstructure:"require_rationale"`
	MaxFilesChanged  int    `mapstructure:"max_files_changed"`
	MaxRevisions     int    `mapstructure:"max_revisions"`
}

type ExecutionConfig struct {
	UseWorktree    bool `mapstructure:"use_worktree"`
	TimeoutSeconds int  `mapstructure:"timeout_seconds"`
}

type ReportingConfig struct {
	OutputDir string   `mapstructure:"output_dir"`
	Formats   []string `mapstructure:"formats"`
}

// Load reads the config file at path and applies env var overrides.
func Load(path string) (Config, error) {
	v := viper.New()
	v.SetConfigFile(path)

	if err := v.ReadInConfig(); err != nil {
		return Config{}, fmt.Errorf("reading config %s: %w", path, err)
	}

	// Apply env override for repo path.
	if envPath := os.Getenv("CONDUIT_REPO_PATH"); envPath != "" {
		v.Set("target.repo_path", envPath)
	}

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return Config{}, fmt.Errorf("unmarshalling config: %w", err)
	}

	return cfg, nil
}

type ModelsConfig struct {
	Provider ProviderConfig        `mapstructure:"provider"`
	Roles    map[string]RoleConfig `mapstructure:"roles"`
	APIKey   string
}

type ProviderConfig struct {
	BaseURL string `mapstructure:"base_url"`
}

type RoleConfig struct {
	Model string `mapstructure:"model"`
}

// ModelForRole returns the configured model for a role, or fallback if not set.
func (m ModelsConfig) ModelForRole(role, fallback string) string {
	if rc, ok := m.Roles[role]; ok && rc.Model != "" {
		return rc.Model
	}
	return fallback
}

func LoadModels(path string) (ModelsConfig, error) {
	v := viper.New()
	v.SetConfigFile(path)

	if err := v.ReadInConfig(); err != nil {
		return ModelsConfig{}, fmt.Errorf("reading models config %s: %w", path, err)
	}

	var mcfg ModelsConfig
	if err := v.Unmarshal(&mcfg); err != nil {
		return ModelsConfig{}, fmt.Errorf("unmarshalling models config: %w", err)
	}

	mcfg.APIKey = os.Getenv("GEMINI_API_KEY")

	if strings.TrimSpace(mcfg.Provider.BaseURL) == "" {
		return ModelsConfig{}, fmt.Errorf("models config: provider.base_url is required")
	}

	return mcfg, nil
}
