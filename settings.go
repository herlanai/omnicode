package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"github.com/HyperparameterSI/golet/backend/plugins/terminal"
	"github.com/HyperparameterSI/golet/backend/ui"
	"github.com/HyperparameterSI/golet/backend/ui/editor"
	"github.com/HyperparameterSI/golet/backend/ui/forma"
)

type modelProfile struct {
	Name     string `json:"name"`
	Provider string `json:"provider"`
	Model    string `json:"model"`
	BaseURL  string `json:"baseUrl"`
	API      string `json:"api"`
	KeyEnv   string `json:"keyEnv"`
	APIKey   string `json:"apiKey,omitempty"`
}

var aiAccessModes = []string{"修改前审批", "完全控制"}

type preferences struct {
	AutoCodeReview bool
	VimMode        bool
	Theme          string
	Font           string
	FontSize       float64
	EditorFont     string
	EditorSize     float64
	TerminalSize   float64
	AIAccess       string
	Engine         string
	PiPath         string
	BuddyPath      string
	DSHPath        string
	DefaultModel   int
	Models         []modelProfile
}

func defaultPreferences() preferences {
	return preferences{AIAccess: "修改前审批", Theme: "跟随系统", FontSize: 13, EditorFont: "monospace", EditorSize: 13, TerminalSize: 13, Engine: "pi-coding-agent", PiPath: "pi", BuddyPath: "codebuddy", DSHPath: "dsh", Models: []modelProfile{
		{Name: "deepseek-flash", Provider: "deepseek", Model: "deepseek-flash", BaseURL: "https://api.deepseek.com", API: "openai-completions", KeyEnv: "DEEPSEEK_API_KEY"},
		{Name: "glm-5.3", Provider: "glm", Model: "glm-5.3", API: "openai-completions", KeyEnv: "GLM_API_KEY"},
	}}
}
func (p preferences) validate() error {
	if p.AIAccess != "" && p.AIAccess != "修改前审批" && p.AIAccess != "完全控制" {
		return fmt.Errorf("无效的 AI 文件修改权限")
	}
	if p.Theme != "跟随系统" && p.Theme != "浅色" && p.Theme != "深色" {
		return fmt.Errorf("无效主题")
	}
	for _, size := range []float64{p.FontSize, p.EditorSize, p.TerminalSize} {
		if size < 10 || size > 32 {
			return fmt.Errorf("字号范围为 10–32")
		}
	}
	if p.Engine != "pi-coding-agent" && p.Engine != "CodeBuddy CLI" && p.Engine != "DSH" {
		return fmt.Errorf("无效引擎")
	}
	if len(p.Models) == 0 || p.DefaultModel < 0 || p.DefaultModel >= len(p.Models) {
		return fmt.Errorf("请选择默认模型")
	}
	return nil
}
func (s *projectStore) settings() (preferences, error) {
	p := defaultPreferences()
	_, err := s.db.Execute(context.Background(), `CREATE TABLE IF NOT EXISTS settings (key TEXT PRIMARY KEY,value TEXT NOT NULL)`)
	if err != nil {
		return p, err
	}
	rows, err := s.db.Query(context.Background(), `SELECT value FROM settings WHERE key='preferences'`)
	if err != nil {
		return p, err
	}
	if len(rows.Values) > 0 {
		err = json.Unmarshal([]byte(rows.Values[0][0].(string)), &p)
	}
	if err == nil {
		err = p.validate()
	}
	if err == nil {
		changed := false
		for i := range p.Models {
			m := &p.Models[i]
			if strings.EqualFold(strings.TrimSpace(m.Provider), "deepseek") && strings.TrimSpace(m.BaseURL) == "" {
				m.BaseURL = "https://api.deepseek.com"
				changed = true
			}
		}
		if changed {
			err = s.saveSettings(p)
		}
	}
	return p, err
}
func (s *projectStore) saveSettings(p preferences) error {
	if err := p.validate(); err != nil {
		return err
	}
	b, err := json.Marshal(p)
	if err != nil {
		return err
	}
	_, err = s.db.Execute(context.Background(), `INSERT INTO settings(key,value) VALUES('preferences',?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, string(b))
	return err
}
func (a *app) applyAppearance() {
	p := a.prefs
	dark := p.Theme == "深色" || (p.Theme == "跟随系统" && a.systemDark)
	radius := float32(8)
	t := forma.NewTheme(forma.ThemeOptions{Preset: forma.Preset("Neutral"), Dark: dark, Radius: &radius, FontSize: float32(p.FontSize)})
	t.Font = p.Font
	if !dark {
		t.Background = ui.Hex("#ffffff")
		t.Sidebar = ui.Hex("#f8f8f7")
		t.Surface = ui.Hex("#efefed")
		t.Border = ui.Hex("#e4e4e1")
		t.Text = ui.Hex("#292b2c")
		t.TextMuted = ui.Hex("#6c6e6c")
		t.Selection = ui.Hex("#dfeafb")
		t.SurfaceHover = ui.Hex("#e8e9e7")
		t.SurfacePressed = ui.Hex("#dddfdc")
	} else {
		t.Background = ui.Hex("#202121")
		t.Sidebar = ui.Hex("#252626")
		t.Surface = ui.Hex("#2d2e2e")
		t.SurfaceHover = ui.Hex("#373939")
		t.SurfacePressed = ui.Hex("#414343")
		t.Border = ui.Hex("#383a39")
		t.Text = ui.Hex("#ececea")
		t.TextMuted = ui.Hex("#a5a8a5")
	}
	a.theme = t
	a.codeTheme = editor.DefaultTheme(t)
	if !dark {
		a.codeTheme.ActiveLine = ui.Hex("#f7f8f8")
		for k, v := range map[editor.TokenKind]string{editor.TokenKeyword: "#8b5776", editor.TokenOperator: "#7b6776", editor.TokenString: "#557966", editor.TokenFunction: "#576d91", editor.TokenType: "#8a7252", editor.TokenNumber: "#966a48", editor.TokenComment: "#9a9c98"} {
			a.codeTheme.Tokens[k].Color = ui.Hex(v)
		}
	}
	if dark {
		a.codeTheme.ActiveLine = ui.Hex("#292b2b")
		for k, v := range map[editor.TokenKind]string{editor.TokenKeyword: "#d9a6c5", editor.TokenOperator: "#c4b4c8", editor.TokenString: "#a5c7ad", editor.TokenFunction: "#a1bde6", editor.TokenType: "#d4bf99", editor.TokenNumber: "#dfb195", editor.TokenComment: "#939c99"} {
			a.codeTheme.Tokens[k].Color = ui.Hex(v)
		}
	}
	for _, tab := range a.terminal.tabs {
		for _, pane := range tab.panes {
			pane.session.SetFont(terminal.Font{Family: p.EditorFont, Size: float32(p.TerminalSize)})
		}
	}
	if a.win != nil {
		a.win.Invalidate()
	}
}
func (a *app) showSettings() {
	if a.prefs.detectCLIs() && a.projects != nil {
		if err := a.projects.saveSettings(a.prefs); err != nil {
			a.settingsError = err.Error()
		}
	}
	a.schedules.open = false
	a.sidebar = true
	a.settingsOpen = true
	if a.settingsTab == "" {
		a.settingsTab = "外观"
	}
	if a.win != nil {
		a.win.Show()
		a.win.Focus()
		a.win.Invalidate()
	}
}
func (a *app) settingsSidebar(c *ui.Context) {
	a.sidebarPanel(c, func() {
		ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
			ui.Text(c, "设置").Bold().Grow(1)
			iconButton(c, "back", "chevron-left", "返回工作区", a.leavePage)
		})
		for _, item := range []struct{ title, symbol string }{{"外观", "appearance"}, {"AI 引擎", "sparkles"}} {
			row := ui.ButtonBase(c.Key(item.title)).FillWidth().Height(30).Padding(0, 12).Gap(9).AlignItems(ui.Center).Justify(ui.Start).Radius(6).Label(item.title).OnClick(func() { a.settingsTab = item.title })
			selected := a.settingsTab == item.title || (a.settingsTab == "" && item.title == "外观")
			color := a.theme.TextMuted
			if selected {
				row.Background(a.theme.Sidebar.Mix(a.theme.Accent, 0.14))
				color = a.theme.Text
			} else if row.Hovered() {
				row.Background(a.theme.SurfaceHover)
			}
			row.Transition(chromeTransition).Children(func() {
				icon(c, item.symbol, 16).TextColor(color)
				ui.Text(c, item.title).FontWeight(500)
			})
		}
	})
}
func (a *app) settingsView(c *ui.Context) {
	before, _ := json.Marshal(a.prefs)
	ui.Scroll(c.Key("settings-body-" + a.settingsTab)).Grow(1).MinWidth(0).FillHeight().AlignItems(ui.Center).Padding(32).Children(func() {
		ui.Column(c).FillWidth().MaxWidth(720).Gap(16).Children(func() {
			if a.settingsTab == "AI 引擎" {
				a.engineSettings(c)
			} else {
				a.appearanceSettings(c)
			}
			if a.settingsError != "" {
				ui.Text(c, a.settingsError).TextColor(ui.Hex("#bd5555")).Wrap()
			}
		})
	})
	after, _ := json.Marshal(a.prefs)
	if string(before) != string(after) {
		a.savePreferences()
	}
}
func (a *app) savePreferences() {
	if !a.prefs.VimMode {
		for _, d := range a.docs {
			d.vim = vimEditor{}
		}
	}
	a.settingsError = ""
	if a.projects == nil {
		a.settingsError = "设置存储不可用，修改仅在本次运行生效"
	} else if err := a.projects.saveSettings(a.prefs); err != nil {
		a.settingsError = err.Error()
	}
	a.applyAppearance()
}
func settingsHeading(c *ui.Context, symbol, title string, size float32) ui.Element {
	return ui.Row(c).Gap(9).AlignItems(ui.Center).Children(func() {
		icon(c, symbol, size).TextColor(c.Theme().Text)
		family := c.Theme().Font
		if family == "" {
			family = "PingFang SC"
		}
		ui.Text(c, title).Font(family).FontSize(size).FontWeight(400)
	})
}

func settingsField(c *ui.Context, label string, build func()) {
	ui.Column(c.Key(label)).FillWidth().Gap(6).Children(func() { ui.Text(c, label); build() })
}
func (a *app) appearanceSettings(c *ui.Context) {
	p := &a.prefs
	settingsHeading(c, "appearance", "外观", 22)
	settingsField(c, "主题", func() { forma.Select(c, &p.Theme, []string{"跟随系统", "浅色", "深色"}).FillWidth() })
	settingsField(c, "界面字体", func() { forma.Input(c, &p.Font, forma.InputOptions{Placeholder: "系统默认"}).FillWidth() })
	settingsField(c, "界面字号", func() { ui.NumberInput(c, &p.FontSize, 10, 32, 1).Width(120) })
	settingsField(c, "编辑器字体", func() { forma.Input(c, &p.EditorFont, forma.InputOptions{Placeholder: "monospace"}).FillWidth() })
	settingsField(c, "编辑器字号", func() { ui.NumberInput(c, &p.EditorSize, 10, 32, 1).Width(120) })
	forma.Checkbox(c, &p.VimMode, "Vim 模式")
	settingsField(c, "终端字号", func() { ui.NumberInput(c, &p.TerminalSize, 10, 32, 1).Width(120) })
	muted(c, "字体填写本机已安装的字体名称。修改自动保存。")
}
func (a *app) engineSettings(c *ui.Context) {
	p := &a.prefs
	settingsHeading(c, "sparkles", "AI 引擎", 22)
	settingsField(c, "默认引擎", func() { forma.Select(c, &p.Engine, aiEngines).FillWidth() })
	path := &p.PiPath
	if p.Engine == "CodeBuddy CLI" {
		path = &p.BuddyPath
	}
	if p.Engine == "DSH" {
		path = &p.DSHPath
	}
	settingsField(c, "文件修改权限", func() {
		if p.AIAccess == "" {
			p.AIAccess = "修改前审批"
		}
		forma.Select(c, &p.AIAccess, aiAccessModes).FillWidth()
	})
	if p.Engine == "pi-coding-agent" {
		muted(c, "Pi 已内置，填写模型 API Key 即可使用。")
	} else {
		settingsField(c, "CLI 路径", func() { forma.Input(c, path).FillWidth() })
		muted(c, "引擎需单独安装并配置凭据；新发送的任务使用此处设置。")
	}
	ui.Text(c, "Pi 模型配置").Bold().Margin(12, 0, 0, 0)
	for i := range p.Models {
		i := i
		ui.Row(c.Key(fmt.Sprintf("model-%d", i))).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
			label := p.Models[i].Name
			if label == "" {
				label = "自定义模型"
			}
			if p.DefaultModel == i {
				label += " · 默认"
			}
			forma.Button(c, label, forma.ButtonOptions{Variant: forma.Ghost}).Grow(1).OnClick(func() { a.modelEdit = i })
			iconButton(c, "default", "check", "设为默认模型", func() { p.DefaultModel = i })
			if len(p.Models) > 1 {
				iconButton(c, "remove", "close", "删除模型", func() {
					p.Models = append(p.Models[:i], p.Models[i+1:]...)
					if p.DefaultModel == i {
						p.DefaultModel = 0
					} else if p.DefaultModel > i {
						p.DefaultModel--
					}
					a.modelEdit = 0
				})
			}
		})
	}
	iconButton(c, "add-model", "plus", "添加模型", func() {
		p.Models = append(p.Models, modelProfile{Name: "自定义模型", API: "openai-completions"})
		a.modelEdit = len(p.Models) - 1
	})
	if a.modelEdit >= len(p.Models) {
		a.modelEdit = 0
	}
	m := &p.Models[a.modelEdit]
	for _, f := range []struct {
		label string
		value *string
	}{{"名称", &m.Name}, {"Provider ID", &m.Provider}, {"Model ID", &m.Model}, {"API 地址", &m.BaseURL}} {
		f := f
		settingsField(c, f.label, func() { forma.Input(c, f.value).FillWidth() })
	}
	settingsField(c, "API Key", func() { forma.Input(c, &m.APIKey).Password().FillWidth() })
	settingsField(c, "API 协议", func() {
		forma.Select(c, &m.API, []string{"openai-completions", "openai-responses", "anthropic-messages"}).FillWidth()
	})
	muted(c, "请填写服务商实际的模型 ID 和 API 地址。直接填写 API Key，自动保存，仅供 Pi 使用。DSH 使用自身凭据配置。")
}

var envName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

func piModelJSON(m modelProfile) ([]byte, error) {
	u, err := url.Parse(m.BaseURL)
	if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") || u.User != nil {
		return nil, fmt.Errorf("请填写有效的 HTTP(S) API 地址")
	}
	if m.Provider == "" || m.Model == "" {
		return nil, fmt.Errorf("Provider ID 和 Model ID 不能为空")
	}
	if m.APIKey != "" {
		m.KeyEnv = "OMNICODE_MODEL_API_KEY"
	}
	if !envName.MatchString(m.KeyEnv) {
		return nil, fmt.Errorf("请填写 API Key 的环境变量名")
	}
	switch m.API {
	case "openai-completions", "openai-responses", "anthropic-messages":
	default:
		return nil, fmt.Errorf("不支持的 API 协议")
	}
	return json.MarshalIndent(map[string]any{"providers": map[string]any{m.Provider: map[string]any{"baseUrl": m.BaseURL, "api": m.API, "apiKey": "$" + m.KeyEnv, "models": []any{map[string]string{"id": m.Model, "name": m.Name}}}}}, "", "  ")
}
