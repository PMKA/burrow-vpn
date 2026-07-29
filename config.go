package main

import (
	"encoding/json"
	"os"
	"path/filepath"
)

type Config struct {
	TrustedSSIDs []string `json:"trusted_ssids"`
	WGConnection string   `json:"wg_connection"`
	AutoConnect  bool     `json:"auto_connect"`
}

func configPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "burrow", "config.json")
}

func loadConfig() Config {
	cfg := Config{AutoConnect: true}
	data, err := os.ReadFile(configPath())
	if err != nil {
		return cfg
	}
	json.Unmarshal(data, &cfg)
	return cfg
}

func saveConfig(cfg Config) error {
	path := configPath()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

func (cfg *Config) isTrusted(ssid string) bool {
	for _, s := range cfg.TrustedSSIDs {
		if s == ssid {
			return true
		}
	}
	return false
}

func (cfg *Config) addTrusted(ssid string) {
	if !cfg.isTrusted(ssid) {
		cfg.TrustedSSIDs = append(cfg.TrustedSSIDs, ssid)
	}
}

func (cfg *Config) removeTrusted(ssid string) {
	result := cfg.TrustedSSIDs[:0]
	for _, s := range cfg.TrustedSSIDs {
		if s != ssid {
			result = append(result, s)
		}
	}
	cfg.TrustedSSIDs = result
}
