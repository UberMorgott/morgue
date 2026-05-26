package config

import (
	"errors"
	"os"

	"gopkg.in/yaml.v3"
)

// Config holds all application configuration.
type Config struct {
	SkipSystemLibs         bool            `yaml:"skip_system_libs"`
	SkipCategories         map[string]bool `yaml:"skip_categories"`
	CustomSkip             []string        `yaml:"custom_skip"`
	CustomInclude          []string        `yaml:"custom_include"`
	DefaultOutputDir       string          `yaml:"default_output_dir"`
	KeepIntermediates      bool            `yaml:"keep_intermediates"`
	GenerateCallgraph      bool            `yaml:"generate_callgraph"`
	GenerateDotFiles       bool            `yaml:"generate_dot_files"`
	StepTimeoutMinutes     int             `yaml:"step_timeout_minutes"`
	MaxFileSizeMB          int             `yaml:"max_file_size_mb"`
	ConcurrentTargets      int             `yaml:"concurrent_targets"`
	StopOnFirstError       bool            `yaml:"stop_on_first_error"`
	GitHubToken            string          `yaml:"github_token"`
	DownloadRetries        int             `yaml:"download_retries"`
	DownloadTimeoutMinutes int             `yaml:"download_timeout_minutes"`
	PreferSystemTools      bool            `yaml:"prefer_system_tools"`
	AutoUpdateCheck        bool            `yaml:"auto_update_check"`
	AutoUpdateTools        bool            `yaml:"auto_update_tools"`
	AutoUpdateApp          bool            `yaml:"auto_update_app"`
	UpdateChannel          string          `yaml:"update_channel"`
	CSharpLanguageVersion  string          `yaml:"csharp_language_version"`
	GeneratePDB            bool            `yaml:"generate_pdb"`
	DecompileProjectMode   bool            `yaml:"decompile_project_mode"`
	LogLevel               string          `yaml:"log_level"`
	LogToFile              bool            `yaml:"log_to_file"`
	LogTimestamps          bool            `yaml:"log_timestamps"`
	AllowDynamicExecution  bool            `yaml:"allow_dynamic_execution"`
	SandboxWarning         bool            `yaml:"sandbox_warning"`
}

// Default returns a Config with sensible defaults.
func Default() Config {
	return Config{
		SkipSystemLibs:        true,
		StepTimeoutMinutes:    60,
		ConcurrentTargets:     1,
		DownloadRetries:       3,
		CSharpLanguageVersion: "Latest",
		LogLevel:              "info",
		UpdateChannel:         "stable",
		GenerateCallgraph:     true,
		GenerateDotFiles:      true,
		GeneratePDB:           true,
		DecompileProjectMode:  true,
		LogToFile:             true,
		LogTimestamps:         true,
		SandboxWarning:        true,
		AllowDynamicExecution: false,
	}
}

// Load reads config from a YAML file. If the file doesn't exist, returns defaults.
func Load(path string) (Config, error) {
	cfg := Default()

	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return cfg, nil
		}
		return cfg, err
	}

	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return cfg, err
	}

	return cfg, nil
}

// Save writes config to a YAML file.
func Save(path string, cfg Config) error {
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}
