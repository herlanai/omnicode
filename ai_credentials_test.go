package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDSHCommandCredentialIsolation(t *testing.T) {
	t.Setenv("DSH_HOME", t.TempDir())
	t.Setenv("DEEPSEEK_API_KEY", "")
	t.Setenv("OMNICODE_TEST_KEY", "test-secret")
	p := defaultPreferences()
	p.Engine, p.DSHPath, p.Models[0].KeyEnv = "DSH", "/bin/echo", "OMNICODE_TEST_KEY"
	dir := t.TempDir()
	cmd, env, err := aiCommand(p, "hello", dir, dir)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.Join(cmd, " "), "test-secret") {
		t.Fatal("secret leaked into arguments")
	}
	if len(env) != 0 {
		t.Fatal("Omnicode credentials injected into DSH")
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 1 || entries[0].Name() != "dsh-runtime.yml" {
		t.Fatal("unexpected DSH config files")
	}
}

func TestConfiguredKey(t *testing.T) {
	t.Setenv("OMNICODE_TEST_KEY", "test-secret")
	if configuredKey("OMNICODE_TEST_KEY") != "test-secret" {
		t.Fatal("exported key unavailable")
	}
	if configuredKey("$(echo unsafe)") != "" {
		t.Fatal("invalid identifier accepted")
	}
	// A private startup file verifies Finder fallback without reading real credentials.
	home := t.TempDir()
	t.Setenv("ZDOTDIR", home)
	t.Setenv("SHELL", "/bin/zsh")
	t.Setenv("OMNICODE_SHELL_TEST_KEY", "")
	if err := os.WriteFile(filepath.Join(home, ".zshrc"), []byte("export OMNICODE_SHELL_TEST_KEY=shell-test-secret\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if configuredKey("OMNICODE_SHELL_TEST_KEY") != "shell-test-secret" {
		t.Fatal("shell credential unavailable")
	}
}

func TestDirectAPIKeyPersistenceAndCommands(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cache.sqlite")
	store, err := openProjectStore(path)
	if err != nil {
		t.Fatal(err)
	}
	p, err := store.settings()
	if err != nil {
		t.Fatal(err)
	}
	p.Models[0].APIKey = "direct-test-key"
	p.Models[0].KeyEnv = ""
	if err := store.saveSettings(p); err != nil {
		t.Fatal(err)
	}
	store.db.Close()
	store, err = openProjectStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.db.Close()
	p, err = store.settings()
	if err != nil || p.Models[0].APIKey != "direct-test-key" {
		t.Fatal("direct key not restored")
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("credential database must be private")
	}
	t.Setenv("DSH_HOME", t.TempDir())
	t.Setenv("DEEPSEEK_API_KEY", "")
	p.Engine, p.DSHPath = "DSH", "/bin/echo"
	cmd, env, err := aiCommand(p, "hello", t.TempDir(), t.TempDir())
	if err != nil || len(env) != 0 {
		t.Fatal("Omnicode direct key injected into DSH")
	}
	if strings.Contains(strings.Join(cmd, " "), "direct-test-key") {
		t.Fatal("secret in arguments")
	}
	p.Engine, p.PiPath = "pi-coding-agent", "/bin/echo"
	dir := t.TempDir()
	_, env, err = aiCommand(p, "hello", dir, dir)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(env, "\n"), "OMNICODE_MODEL_API_KEY=direct-test-key") {
		t.Fatal("Pi did not receive direct key")
	}
	data, err := os.ReadFile(filepath.Join(dir, "models.json"))
	if err != nil || strings.Contains(string(data), "direct-test-key") || !strings.Contains(string(data), "$OMNICODE_MODEL_API_KEY") {
		t.Fatal("unsafe model configuration")
	}
	exported, err := piModelJSON(p.Models[0])
	if err != nil || strings.Contains(string(exported), "direct-test-key") {
		t.Fatal("export contains secret")
	}
}
