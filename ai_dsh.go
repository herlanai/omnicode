package main

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Headless disables these services in its bundle. Re-enable DSH's own stores
// for this invocation, without copying any credentials or changing its profiles.
func dshArgs(prompt, configDir, home string) ([]string, error) {
	patch := []map[string]any{
		{"id": "credentials", "disabled": false},
		{"id": "authorization", "disabled": false},
		{"id": "deepseek-account", "disabled": false},
	}
	data, err := os.ReadFile(filepath.Join(home, "profiles", "desktop", "cordis.patch.yml"))
	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("读取 DSH 桌面模型配置失败：%w", err)
	}
	if err == nil {
		var entries []struct {
			ID     string               `yaml:"id"`
			Config map[string]yaml.Node `yaml:"config"`
		}
		if err := yaml.Unmarshal(data, &entries); err != nil {
			return nil, fmt.Errorf("DSH 桌面模型配置格式无效")
		}
		for _, entry := range entries {
			if entry.ID != "agent-default-model" {
				continue
			}
			provider, model := entry.Config["provider"], entry.Config["model"]
			if provider.Tag != "!!str" || model.Tag != "!!str" || model.Value == "" {
				continue
			}
			// Other providers need their own adapter configuration, not just a model ID.
			if provider.Value != "deepseek-account" && provider.Value != "deepseek-official" {
				continue
			}
			config := map[string]string{"provider": provider.Value, "model": model.Value}
			if effort := entry.Config["reasoningEffort"]; effort.Tag == "!!str" {
				config["reasoningEffort"] = effort.Value
			}
			patch = append(patch, map[string]any{"id": "agent-default-model", "config": config})
		}
	}
	data, err = yaml.Marshal(patch)
	if err != nil {
		return nil, err
	}
	if err = os.MkdirAll(configDir, 0700); err != nil {
		return nil, err
	}
	path := filepath.Join(configDir, "dsh-runtime.yml")
	if err = os.WriteFile(path, data, 0600); err != nil {
		return nil, err
	}
	// Two separators: the launcher consumes one and headless consumes the other.
	return []string{"--profile", "headless", "--patch", path, "--", "--", prompt}, nil
}

func dshHome() string {
	if home := os.Getenv("DSH_HOME"); home != "" {
		return home
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".dsh")
}
