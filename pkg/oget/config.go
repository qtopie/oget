package oget

import (
	"github.com/spf13/viper"
	"log"
)

var (
	Version = "dev"
	Commit  = "none"
)

// Config represents the global configuration of oget.
type Config struct {
	Concurrency    int    `mapstructure:"concurrency"`
	MaxConcurrency int    `mapstructure:"max_concurrency"`
	AutoTune       bool   `mapstructure:"autotune"`        // Enable dynamic bandwidth detection
	StorageType    string `mapstructure:"storage_type"`    // "file", "db", "uring"
	StateStoreType string `mapstructure:"state_store_type"` // "json", "bolt", "redis"
	ManifestPath   string `mapstructure:"manifest_path"`   // Path to save .oget state files
	ProxyURL       string `mapstructure:"proxy_url"`       // e.g., "http://localhost:8080"
	Verbose        bool   `mapstructure:"verbose"`         // Enable detailed logging
}

// DefaultConfig returns a configuration with default values.
func DefaultConfig() *Config {
	return &Config{
		Concurrency:    8, // Start small if autotune is on
		MaxConcurrency: 128,
		AutoTune:       true,
		StorageType:    "file",
		StateStoreType: "json",
		ManifestPath:   ".",
		ProxyURL:       "",
		Verbose:        false,
	}
}

// LoadConfig initializes Viper and loads configuration from file or env.
func LoadConfig(configPath string) (*Config, error) {
	v := viper.New()
	v.SetConfigName("oget") // name of config file (without extension)
	v.SetConfigType("json") // or "yaml", "toml"
	v.AddConfigPath(".")    // optionally look for config in the working directory
	if configPath != "" {
		v.SetConfigFile(configPath)
	}

	// Set defaults
	v.SetDefault("concurrency", 8)
	v.SetDefault("max_concurrency", 128)
	v.SetDefault("autotune", true)
	v.SetDefault("storage_type", "file")
	v.SetDefault("state_store_type", "json")
	v.SetDefault("manifest_path", ".")
	v.SetDefault("proxy_url", "")

	v.AutomaticEnv() // Read from environment variables

	if err := v.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return nil, err
		}
		log.Println("Config file not found, using defaults")
	}

	var config Config
	if err := v.Unmarshal(&config); err != nil {
		return nil, err
	}

	return &config, nil
}
