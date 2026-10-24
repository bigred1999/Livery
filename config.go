//go:build windows

package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

type Config struct {
	Settings     Settings        `json:"settings"`
	Custom       []ThemeDef      `json:"customThemes"`
	AppliedPower string          `json:"appliedPowerButton,omitempty"`
	Tuya         TuyaConfig      `json:"smartBulbs,omitempty"`
	Screen       *ScreenSettings `json:"keyboardScreen,omitempty"`
}

func appliedPower() string { return loadConfig().AppliedPower }

func setAppliedPower(hex string) {
	c := loadConfig()
	c.AppliedPower = hex
	saveConfig(c)
}

var cfgMu sync.Mutex

func exeDir() string {
	exe, err := os.Executable()
	if err != nil {
		return "."
	}
	return filepath.Dir(exe)
}

func configPath() string { return filepath.Join(exeDir(), "config.json") }

func loadConfig() Config {
	cfgMu.Lock()
	defer cfgMu.Unlock()
	var c Config
	if b, err := os.ReadFile(configPath()); err == nil {
		json.Unmarshal(b, &c)
	}
	c.Settings.normalize()
	return c
}

func saveConfig(c Config) error {
	cfgMu.Lock()
	defer cfgMu.Unlock()
	c.Settings.normalize()
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	tmp := configPath() + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, configPath())
}
