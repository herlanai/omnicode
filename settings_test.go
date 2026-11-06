package main

import (
	"encoding/json"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/HyperparameterSI/golet/backend/ui"
)

func TestPreferencesRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cache.sqlite")
	s, err := openProjectStore(path)
	if err != nil {
		t.Fatal(err)
	}
	p, err := s.settings()
	if err != nil {
		t.Fatal(err)
	}
	p.Theme = "深色"
	p.EditorFont = "Menlo"
	p.EditorSize = 17
	p.VimMode = true
	p.Engine = "DSH"
	p.DSHPath = "/opt/bin/dsh"
	p.Models = append(p.Models, modelProfile{Name: "Custom", Model: "custom", Provider: "local", API: "openai-completions", BaseURL: "http://localhost:8000/v1", KeyEnv: "LOCAL_KEY"})
	p.DefaultModel = 2
	if err = s.saveSettings(p); err != nil {
		t.Fatal(err)
	}
	s.db.Close()
	s, err = openProjectStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.db.Close()
	got, err := s.settings()
	if err != nil {
		t.Fatal(err)
	}
	a, _ := json.Marshal(p)
	b, _ := json.Marshal(got)
	if string(a) != string(b) {
		t.Fatalf("settings changed on reopen: %s", b)
	}
}
func TestPiModelExport(t *testing.T) {
	m := defaultPreferences().Models[0]
	m.BaseURL = ""
	if _, err := piModelJSON(m); err == nil {
		t.Fatal("missing endpoint accepted")
	}
	m.BaseURL = "https://api.example.com/v1"
	b, err := piModelJSON(m)
	if err != nil {
		t.Fatal(err)
	}
	var v struct {
		Providers map[string]struct {
			APIKey string `json:"apiKey"`
		}
	}
	if err = json.Unmarshal(b, &v); err != nil {
		t.Fatal(err)
	}
	if v.Providers["deepseek"].APIKey != "$DEEPSEEK_API_KEY" {
		t.Fatalf("wrong environment reference: %s", b)
	}
	m.KeyEnv = "!cat secret"
	if _, err = piModelJSON(m); err == nil {
		t.Fatal("command key accepted")
	}
}
func TestSettingsNativeLayout(t *testing.T) {
	a := newApp("")
	for _, tab := range []string{"外观", "AI 引擎"} {
		a.settingsTab = tab
		a.settingsOpen = true
		view := ui.NewTester(a.view, 760, 680)
		if view.Image().Bounds().Dx() != 760 {
			t.Fatal("invalid layout")
		}
		if os.Getenv("OMNICODE_SETTINGS_SCREENSHOT") != "" {
			name := "appearance"
			if tab == "AI 引擎" {
				name = "engines"
			}
			os.MkdirAll("artifacts", 0755)
			f, err := os.Create("artifacts/settings-" + name + ".png")
			if err != nil {
				t.Fatal(err)
			}
			png.Encode(f, view.Image())
			f.Close()
		}
	}
	a.prefs.Theme = "深色"
	a.applyAppearance()
	if a.theme.Text == a.theme.Background {
		t.Fatal("dark theme lacks contrast")
	}
}

func TestDeepSeekEndpointMigration(t *testing.T) {
	s, err := openProjectStore(filepath.Join(t.TempDir(), "cache.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.db.Close()
	p, err := s.settings()
	if err != nil {
		t.Fatal(err)
	}
	if p.Models[0].BaseURL != "https://api.deepseek.com" {
		t.Fatal("missing default endpoint")
	}
	p.Models[0].BaseURL = ""
	custom := p.Models[0]
	custom.BaseURL = "https://my-proxy.example/v1"
	p.Models = append(p.Models, custom)
	if err = s.saveSettings(p); err != nil {
		t.Fatal(err)
	}
	got, err := s.settings()
	if err != nil {
		t.Fatal(err)
	}
	if got.Models[0].BaseURL != "https://api.deepseek.com" || got.Models[1].BaseURL != "" || got.Models[2].BaseURL != custom.BaseURL {
		t.Fatalf("incorrect endpoint migration: %+v", got.Models)
	}
}

func TestWorkspaceSettingsNavigation(t *testing.T) {
	a := newApp("")
	a.newFile()
	doc := a.current()
	v := ui.NewTester(a.view, 1100, 720)
	for _, label := range []string{"设置  ⌘ ,", "AI 引擎", "返回工作区"} {
		if err := v.Click(label); err != nil {
			t.Fatal(err)
		}
	}
	if a.settingsOpen || a.current() != doc || a.settingsTab != "AI 引擎" {
		t.Fatal("settings navigation lost workspace state")
	}
	a.showSettings()
	v.Frame()
	v.Key(ui.Cmd, ui.KeyW)
	if a.settingsOpen || a.current() != doc {
		t.Fatal("closing settings closed the editor instead")
	}
}

func TestSettingsAndScheduleSharedNavigation(t *testing.T) {
	a := newApp("")
	v := ui.NewTester(a.view, 800, 520)
	for _, label := range []string{"设置  ⌘ ,", "AI 引擎", "定时任务", "设置  ⌘ ,", "切换侧栏", "切换侧栏", "文件浏览器"} {
		if err := v.Click(label); err != nil {
			t.Fatal(err)
		}
	}
	if a.settingsOpen || a.schedules.open || !a.sidebar || a.settingsTab != "AI 引擎" {
		t.Fatal("shared navigation lost page state")
	}
}
