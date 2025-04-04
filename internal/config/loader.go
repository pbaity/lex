package config

import (
	"fmt"
	"os"

	"github.com/pbaity/lex/pkg/models" // Corrected import path
	"gopkg.in/yaml.v3"
)

// LoadConfig reads a YAML configuration file from the given path,
// unmarshals it into a models.Config struct, and returns it.
func LoadConfig(configPath string) (*models.Config, error) {
	// Read the configuration file content
	yamlFile, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file '%s': %w", configPath, err)
	}

	// Initialize the config struct
	var config models.Config

	// Unmarshal the YAML content into the struct
	err = yaml.Unmarshal(yamlFile, &config)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal config file '%s': %w", configPath, err)
	}

	// Validate the loaded configuration
	if err := ValidateConfig(&config); err != nil {
		return nil, fmt.Errorf("configuration validation failed: %w", err)
	}

	// Note: Merging of default settings (like retry policies) will be handled
	// dynamically when the settings are accessed, rather than modifying the
	// loaded config struct directly.

	return &config, nil
}
