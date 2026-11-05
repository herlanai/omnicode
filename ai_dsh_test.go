package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

func TestDSHDesktopModelAndStores(t *testing.T) {
	home, dir := t.TempDir(), t.TempDir()
	path := filepath.Join(home, "profiles/desktop/cordis.patch.yml")
	os.MkdirAll(filepath.Dir(path), 0700)
	original := "- id: agent-default-model\n  config:\n    provider: deepseek-account\n    model: deepseek-flash\n    reasoningEffort: high\n- id: unrelated\n  config:\n    apiKey: never-copy-this\n"
	os.WriteFile(path, []byte(original), 0600)
	args, err := dshArgs("--literal-task", dir, home)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(args[len(args)-3:], " ") != "-- -- --literal-task" {
		t.Fatal("task must be literal")
	}
	data, err := os.ReadFile(args[3])
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "never-copy-this") {
		t.Fatal("unrelated config copied")
	}
	var patch []struct {
		ID       string            `yaml:"id"`
		Disabled bool              `yaml:"disabled"`
		Config   map[string]string `yaml:"config"`
	}
	if err := yaml.Unmarshal(data, &patch); err != nil {
		t.Fatal(err)
	}
	if len(patch) != 4 || patch[3].Config["provider"] != "deepseek-account" {
		t.Fatal("desktop account route not selected")
	}
	for i, id := range []string{"credentials", "authorization", "deepseek-account"} {
		if patch[i].ID != id || patch[i].Disabled {
			t.Fatal("DSH store not enabled")
		}
	}
	after, _ := os.ReadFile(path)
	if string(after) != original {
		t.Fatal("desktop profile mutated")
	}
	info, _ := os.Stat(args[3])
	if info.Mode().Perm() != 0600 {
		t.Fatal("patch permissions")
	}
}

func TestDSHWithoutDesktopProfile(t *testing.T) {
	args, err := dshArgs("hello", t.TempDir(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(args[3])
	if strings.Contains(string(data), "agent-default-model") {
		t.Fatal("default route overridden without desktop settings")
	}
}

func TestDSHLiveDesktopCredentials(t *testing.T) {
	if os.Getenv("OMNICODE_DSH_LIVE_TEST") != "1" {
		t.Skip("opt-in real DSH authentication smoke test")
	}
	p := defaultPreferences()
	p.Engine = "DSH"
	command, env, err := aiCommand(p, "只回复 OK，不调用任何工具，不修改任何文件。", t.TempDir(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Second)
	defer cancel()
	events := make(chan aiEvent, 1024)
	err = runAI(ctx, command, env, t.TempDir(), events)
	var output strings.Builder
	for len(events) > 0 {
		output.WriteString((<-events).text)
	}
	if err != nil {
		t.Fatalf("DSH smoke test failed: %v", err)
	}
	if !strings.Contains(output.String(), "OK") {
		t.Fatal("DSH did not return expected answer")
	}
	t.Log("DSH replied OK using its own credentials")
}
