package services

import (
	"github.com/UberMorgott/morgue/internal/config"
	"github.com/UberMorgott/morgue/internal/util"
)

// ConfigService exposes configuration to the frontend.
type ConfigService struct{}

// Get loads and returns the current config.
func (s *ConfigService) Get() (*config.Config, error) {
	cfg, err := config.Load(util.ConfigPath())
	if err != nil {
		return nil, err
	}
	return &cfg, nil
}

// Save writes the given config to disk.
func (s *ConfigService) Save(cfg config.Config) error {
	return config.Save(util.ConfigPath(), cfg)
}

// GetSkipCategories returns the current skip category map.
func (s *ConfigService) GetSkipCategories() (map[string]bool, error) {
	cfg, err := config.Load(util.ConfigPath())
	if err != nil {
		return nil, err
	}
	return cfg.SkipCategories, nil
}
