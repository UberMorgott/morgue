package config

import (
	"errors"
	"os"

	"gopkg.in/yaml.v3"
)

// Config holds application settings. JSON keys are PascalCase (Go defaults), matching frontend API contract.
type Config struct {
	SkipSystemLibs         bool            `json:"SkipSystemLibs" yaml:"skip_system_libs"`
	SkipCategories         map[string]bool `json:"SkipCategories" yaml:"skip_categories"`
	CustomSkip             []string        `json:"CustomSkip" yaml:"custom_skip"`
	CustomInclude          []string        `json:"CustomInclude" yaml:"custom_include"`
	DefaultOutputDir       string          `json:"DefaultOutputDir" yaml:"default_output_dir"`
	KeepIntermediates      bool            `json:"KeepIntermediates" yaml:"keep_intermediates"`
	GenerateCallgraph      bool            `json:"GenerateCallgraph" yaml:"generate_callgraph"`
	GenerateDotFiles       bool            `json:"GenerateDotFiles" yaml:"generate_dot_files"`
	StepTimeoutMinutes     int             `json:"StepTimeoutMinutes" yaml:"step_timeout_minutes"`
	MaxFileSizeMB          int             `json:"MaxFileSizeMB" yaml:"max_file_size_mb"`
	ConcurrentTargets      int             `json:"ConcurrentTargets" yaml:"concurrent_targets"`
	StopOnFirstError       bool            `json:"StopOnFirstError" yaml:"stop_on_first_error"`
	GitHubToken            string          `json:"-" yaml:"github_token"`
	DownloadRetries        int             `json:"DownloadRetries" yaml:"download_retries"`
	DownloadTimeoutMinutes int             `json:"DownloadTimeoutMinutes" yaml:"download_timeout_minutes"`
	PreferSystemTools      bool            `json:"PreferSystemTools" yaml:"prefer_system_tools"`
	AutoUpdateCheck        bool            `json:"AutoUpdateCheck" yaml:"auto_update_check"`
	AutoUpdateTools        bool            `json:"AutoUpdateTools" yaml:"auto_update_tools"`
	AutoUpdateApp          bool            `json:"AutoUpdateApp" yaml:"auto_update_app"`
	UpdateChannel          string          `json:"UpdateChannel" yaml:"update_channel"`
	CSharpLanguageVersion  string          `json:"CSharpLanguageVersion" yaml:"csharp_language_version"`
	GeneratePDB            bool            `json:"GeneratePDB" yaml:"generate_pdb"`
	DecompileProjectMode   bool            `json:"DecompileProjectMode" yaml:"decompile_project_mode"`
	LogLevel               string          `json:"LogLevel" yaml:"log_level"`
	LogToFile              bool            `json:"LogToFile" yaml:"log_to_file"`
	LogTimestamps          bool            `json:"LogTimestamps" yaml:"log_timestamps"`
	AllowDynamicExecution  bool            `json:"AllowDynamicExecution" yaml:"allow_dynamic_execution"`
	SandboxWarning         bool            `json:"SandboxWarning" yaml:"sandbox_warning"`

	// UE5 pipeline step toggles
	UE5ExtractPAK      bool `json:"UE5ExtractPAK" yaml:"ue5_extract_pak"`
	UE5SDKDump         bool `json:"UE5SDKDump" yaml:"ue5_sdk_dump"`
	UE5ExtractStrings  bool `json:"UE5ExtractStrings" yaml:"ue5_extract_strings"`
	UE5GhidraDecompile bool `json:"UE5GhidraDecompile" yaml:"ue5_ghidra_decompile"`
	UE5NameResolution  bool `json:"UE5NameResolution" yaml:"ue5_name_resolution"`
	UE5BuildIndexes    bool `json:"UE5BuildIndexes" yaml:"ue5_build_indexes"`
	UE5ExportHookable  bool `json:"UE5ExportHookable" yaml:"ue5_export_hookable"`
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

		UE5ExtractPAK:      true,
		UE5SDKDump:         true,
		UE5ExtractStrings:  true,
		UE5GhidraDecompile: false,
		UE5NameResolution:  true,
		UE5BuildIndexes:    true,
		UE5ExportHookable:  true,
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
	return os.WriteFile(path, data, 0600)
}
