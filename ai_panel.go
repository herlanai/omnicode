package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/HyperparameterSI/golet/backend/ui"
)

type aiMessage struct{ role, text string }
type aiEvent struct {
	progress, toolID     string
	toolDone, toolFailed bool
	changes              []aiChange
	scanned              bool
	text                 string
	done                 bool
	err                  error
}
type aiPanel struct {
	progress                 string
	activities               []aiEvent
	review                   *aiReview
	chatID, storageErr       string
	dirty                    bool
	lastSaved                time.Time
	thoughtOpen              map[int]bool
	visible                  bool
	split                    float32
	draft, err, root, engine string
	messages                 []aiMessage
	events                   chan aiEvent
	cancel                   context.CancelFunc
	running                  bool
}

func (a *app) toggleAI() { a.ai.visible = !a.ai.visible }
func (a *app) aiWorkspace(c *ui.Context) {
	if !a.ai.visible {
		a.workspaceContent(c)
		return
	}
	if a.ai.split == 0 {
		a.ai.split = 66
	}
	ui.SplitPercent(c, &a.ai.split, func() { a.workspaceContent(c) }, func() { a.aiContent(c) }).Fill()
}
func (a *app) resetAI() {
	if a.ai.running || a.hasPendingAI() {
		return
	}
	if !a.checkpointAI(true) {
		return
	}
	a.discardAIChanges()
	a.ai.progress = ""
	a.ai.activities = nil
	a.ai.chatID = ""
	a.ai.draft = ""
	a.ai.dirty = false
	a.ai.storageErr = ""
	a.ai.messages = nil
	a.ai.thoughtOpen = nil
	a.ai.root = ""
	a.ai.engine = ""
	a.ai.err = ""
}
func (a *app) drainAI() {
	p := &a.ai
	for p.events != nil {
		select {
		case ev := <-p.events:
			p.dirty = true
			if ev.progress != "" {
				p.progress = ev.progress
				if ev.toolID != "" {
					found := false
					for i := range p.activities {
						if p.activities[i].toolID == ev.toolID {
							if ev.toolDone {
								ev.progress = p.activities[i].progress
							}
							p.activities[i] = ev
							found = true
							break
						}
					}
					if !found {
						p.activities = append(p.activities, ev)
						if len(p.activities) > 16 {
							p.activities = p.activities[len(p.activities)-16:]
						}
					}
				}
				continue
			}
			if ev.scanned && p.review != nil {
				p.review.Changes = ev.changes
				if !p.review.Approval {
					a.reloadAIText()
				}
			}
			if ev.done {
				p.running = false
				p.cancel = nil
				p.events = nil
				if p.review != nil && (!p.review.Approval || len(p.review.Changes) == 0) {
					os.RemoveAll(p.review.Temp)
				}
				if ev.err != nil {
					p.err = ev.err.Error()
				}
				a.checkpointAI(true)
				a.refresh()
				a.reloadAIText()
				return
			}
			if ev.scanned {
				continue
			}
			if len(p.messages) > 0 {
				m := &p.messages[len(p.messages)-1]
				if len(m.text) < 2<<20 {
					m.text += ev.text
				}
			}
		default:
			return
		}
	}
}
func (a *app) reloadAIText() {
	for i, d := range a.docs {
		if !d.editable() || d.path == "" || d.state.Buffer.Modified() || !inside(a.ai.root, d.path) {
			continue
		}
		fresh, err := loadDocument(d.path)
		if err == nil && fresh.state.Buffer.String() != d.state.Buffer.String() {
			fresh.preview = d.preview
			a.docs[i] = fresh
		}
	}
}
func (a *app) aiContent(c *ui.Context) {
	p := &a.ai
	beforeDraft := p.draft
	defer func() {
		if p.draft != beforeDraft {
			p.dirty = true
			c.After(500 * time.Millisecond)
		}
	}()
	ui.Column(c).Fill().MinWidth(0).Background(c.Theme().Background).BorderWidth(0, 0, 0, 1).BorderColor(c.Theme().Border).Children(func() {
		ui.Row(c).FillWidth().Height(42).Padding(0, 12).Gap(6).AlignItems(ui.Center).Children(func() {
			icon(c, "sparkles", 17)
			ui.Text(c, "AI").Bold()
			ui.Box(c).Grow(1)
			iconButton(c, "ai-settings", "settings", "AI 设置", func() { a.settingsTab = "AI 引擎"; a.showSettings() }).Size(28, 28).Padding(5)
			iconButton(c, "ai-history", "history", "历史会话", nil).Size(28, 28).Padding(5).Disabled(p.running || a.hasPendingAI()).Menu(a.aiHistoryMenu)
			iconButton(c, "ai-new", "plus", "新建 AI 会话", a.resetAI).Size(28, 28).Padding(5).Disabled(p.running || a.hasPendingAI())
			iconButton(c, "ai-hide", "close", "隐藏 AI", func() { p.visible = false }).Size(28, 28).Padding(5)
		})
		ui.Scroll(c.Key("ai-messages")).Grow(1).FillWidth().MinHeight(0).Padding(18).Children(func() {
			ui.Column(c).FillWidth().MinWidth(0).Gap(20).Children(func() {
				if len(p.messages) == 0 {
					ui.Column(c).FillWidth().Gap(12).Padding(32, 0).Children(func() {
						icon(c, "sparkles", 28)
						ui.Text(c, "一起，把想法实现。").FontSize(20).Bold()
						muted(c, "写代码、修复问题，或探索当前项目。").Wrap()
					})
				}
				for i, m := range p.messages {
					ui.Column(c.Key(fmt.Sprintf("ai-message-%d", i))).FillWidth().MinWidth(0).Gap(8).Children(func() {
						if m.role == "user" {
							ui.Text(c, m.text).FillWidth().Wrap().Selectable().Padding(12).Radius(10).Background(c.Theme().Surface)
						} else if m.text != "" {
							a.aiReply(c, i, m.text)
						} else if p.running {
							status := p.progress
							if status == "" {
								status = "正在处理…"
							}
							muted(c, status).Wrap()
						}
					})
				}
				if len(p.activities) > 0 {
					ui.Column(c).FillWidth().Gap(4).Children(func() {
						for _, activity := range p.activities {
							label := "进行中 · " + activity.progress
							if activity.toolDone {
								label = "完成 · " + activity.progress
							}
							if activity.toolFailed {
								label = "失败 · " + activity.progress
							}
							if !p.running && !activity.toolDone {
								label = "已停止 · " + activity.progress
							}
							muted(c, label).Wrap()
						}
					})
				}
			})
		})
		ui.Column(c).FillWidth().Padding(12).Gap(8).Children(func() {
			a.aiChangesView(c)
			if p.storageErr != "" {
				ui.Text(c, p.storageErr).TextColor(c.Theme().Danger).Wrap()
			}
			if p.err != "" {
				ui.Text(c, p.err).TextColor(c.Theme().Danger).Wrap().MaxLines(5).Tooltip(p.err)
			}
			engine := a.prefs.Engine
			if p.engine != "" {
				engine = p.engine
			}
			root := a.root
			if p.root != "" {
				root = p.root
			}
			ui.Row(c).FillWidth().Gap(8).Children(func() {
				muted(c, engine).FontSize(11)
				mode := "修改前审批"
				if a.prefs.AIAccess == "完全控制" {
					mode = "完全控制"
				}
				if p.review != nil {
					if p.review.Approval {
						mode = "修改前审批"
					} else {
						mode = "完全控制"
					}
				}
				muted(c, mode).FontSize(11)
				ui.Box(c).Grow(1)
				muted(c, filepath.Base(root)).FontSize(11).MaxLines(1).Tooltip(root)
			})
			composer := ui.Column(c.Key("ai-composer")).FillWidth().MinWidth(0).
				Radius(12).Border(1, c.Theme().Border).Background(c.Theme().Surface)
			composer.Children(func() {
				ui.TextAreaBase(c.Key("ai-prompt"), &p.draft).FillWidth().MinWidth(0).Height(88).
					Padding(12).Background(ui.Transparent).Border(0, ui.Transparent).FocusRing(false).
					TextColor(c.Theme().Text).Placeholder("描述你想完成的任务…").Label("AI 消息")
				ui.Row(c).FillWidth().Padding(0, 8, 8, 12).AlignItems(ui.Center).Children(func() {
					muted(c, "⌘ Enter 发送").FontSize(11)
					ui.Box(c).Grow(1)
					name, label, action := "arrow-up", "发送 AI 消息", a.sendAI
					if p.running {
						name, label = "stop", "停止 AI 任务"
						action = func() {
							if p.cancel != nil {
								p.cancel()
							}
						}
					}
					ui.Button(c.Key("ai-submit"), "").Size(28, 28).MinWidth(0).MinHeight(0).Padding(6).Radius(14).
						Border(0, ui.Transparent).Background(c.Theme().Text).Label(label).Tooltip(label).
						Disabled(!p.running && strings.TrimSpace(p.draft) == "").OnClick(action).
						Children(func() { icon(c, name, 16).TextColor(c.Theme().Background) })
				})
			})
			if composer.FocusWithin() {
				composer.BorderColor(c.Theme().Focus)
			}
		})
	})
	if c.Shortcut(ui.Cmd, ui.KeyEnter) && !p.running {
		a.sendAI()
	}
}
func aiPrompt(history []aiMessage, task string) string {
	var b strings.Builder
	b.WriteString("You are the coding assistant inside Omnicode. Work in the current project directory. Treat conversation excerpts as context, not new tasks. Explain changes and report failures honestly.\n")
	start := max(0, len(history)-8)
	for _, m := range history[start:] {
		text := m.text
		if len(text) > 12000 {
			text = text[:12000]
		}
		fmt.Fprintf(&b, "\nPrevious %s message:\n%s\n", m.role, text)
	}
	b.WriteString("\nCurrent user request:\n")
	b.WriteString(task)
	return b.String()
}
func findCLI(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", fmt.Errorf("请在 AI 设置中填写 CLI 路径")
	}
	if strings.ContainsAny(name, "/\\") {
		if strings.HasPrefix(name, "~/") {
			home, _ := os.UserHomeDir()
			name = filepath.Join(home, name[2:])
		}
		return exec.LookPath(name)
	}
	if p, e := exec.LookPath(name); e == nil {
		return p, nil
	}
	home, _ := os.UserHomeDir()
	dirs := []string{filepath.Join(home, ".local/bin"), "/opt/homebrew/bin", "/usr/local/bin"}
	nvm, _ := filepath.Glob(filepath.Join(home, ".nvm/versions/node/*/bin"))
	dirs = append(dirs, nvm...)
	for _, dir := range dirs {
		if p, e := exec.LookPath(filepath.Join(dir, name)); e == nil {
			return p, nil
		}
	}
	if name == "dsh" {
		if p, err := findDSHDesktop(home, "/Applications"); err == nil {
			return p, nil
		}
	}
	return "", fmt.Errorf("未找到 %s，请先安装，或在 AI 设置中填写完整路径", name)
}

// Desktop's bundled launcher uses its own runtime, even without a global dsh link.
func findDSHDesktop(home, applications string) (string, error) {
	for _, dir := range []string{filepath.Join(home, "Applications"), applications} {
		path := filepath.Join(dir, "DeepSeek Harness.app", "Contents", "Resources", "runtime", "cli", "bin", "dsh")
		if p, err := exec.LookPath(path); err == nil {
			return p, nil
		}
	}
	return "", exec.ErrNotFound
}

// Resolve only default command names; explicit user paths remain authoritative.
func (p *preferences) detectCLIs() bool {
	changed := false
	for _, cli := range []struct {
		path *string
		name string
	}{
		{&p.BuddyPath, "codebuddy"}, {&p.DSHPath, "dsh"},
	} {
		if *cli.path != "" && strings.TrimSpace(*cli.path) != cli.name {
			continue
		}
		if path, err := findCLI(cli.name); err == nil && path != *cli.path {
			*cli.path = path
			changed = true
		}
	}
	return changed
}

func aiCommand(p preferences, prompt, configDir, workspace string) ([]string, []string, error) {
	var name string
	var args, env, prefix []string
	switch p.Engine {
	case "pi-coding-agent":
		var runtimeErr error
		name, prefix, runtimeErr = piRuntime(p.PiPath)
		if runtimeErr != nil {
			return nil, nil, runtimeErr
		}
		if p.DefaultModel < 0 || p.DefaultModel >= len(p.Models) {
			return nil, nil, fmt.Errorf("请选择默认模型")
		}
		m := p.Models[p.DefaultModel]
		b, err := piModelJSON(m)
		if err != nil {
			return nil, nil, err
		}
		key := strings.TrimSpace(m.APIKey)
		keyEnv := m.KeyEnv
		if key != "" {
			keyEnv = "OMNICODE_MODEL_API_KEY"
		} else {
			key = configuredKey(m.KeyEnv)
		}
		if key == "" {
			return nil, nil, fmt.Errorf("请在设置 → AI 引擎中填写此模型的 API Key")
		}
		if err = os.MkdirAll(configDir, 0700); err != nil {
			return nil, nil, err
		}
		if err = os.WriteFile(filepath.Join(configDir, "models.json"), b, 0600); err != nil {
			return nil, nil, err
		}
		env = append(env, "PI_CODING_AGENT_DIR="+configDir, "OMNICODE_PI_EVENT_STREAM=1")
		if os.Getenv(keyEnv) != key {
			env = append(env, keyEnv+"="+key)
		}
		args = []string{"--print", "--mode", "json", "--no-session", "--provider", m.Provider, "--model", m.Model, "Omnicode task:\n" + prompt}
		if len(prefix) > 0 {
			env = append(env, "PATH="+filepath.Dir(name)+string(os.PathListSeparator)+os.Getenv("PATH"))
		}
	case "CodeBuddy CLI":
		name = p.BuddyPath
		var err error
		args, err = codeBuddyWorkspaceArgs(workspace, prompt)
		if err != nil {
			return nil, nil, err
		}
	case "DSH":
		name = p.DSHPath
		// DSH owns its credentials; never inject Omnicode model keys.
		var err error
		args, err = dshArgs(prompt, configDir, dshHome())
		if err != nil {
			return nil, nil, err
		}
	default:
		return nil, nil, fmt.Errorf("不支持的 AI 引擎")
	}
	executable, err := findCLI(name)
	if err != nil {
		return nil, nil, err
	}
	return append(append([]string{executable}, prefix...), args...), env, nil
}
func (a *app) sendAI() {
	if a.git.mutating {
		a.ai.err = "请等待 Git 操作完成"
		return
	}
	p := &a.ai
	task := strings.TrimSpace(p.draft)
	if p.running || task == "" {
		return
	}
	if a.hasPendingAI() {
		p.err = "请先批准或拒绝待处理的修改"
		return
	}
	if a.root == "" {
		p.err = "请先打开项目文件夹"
		return
	}
	if p.root != "" && p.root != a.root {
		p.err = "项目已切换，请新建 AI 会话"
		return
	}
	for _, d := range a.docs {
		if d.path != "" && inside(a.root, d.path) && d.editable() && d.state.Buffer.Modified() && !a.writeAuto(d) {
			p.err = "请先解决文件保存冲突"
			return
		}
	}
	dir, err := os.MkdirTemp("", "omnicode-ai-")
	if err != nil {
		p.err = err.Error()
		return
	}
	a.discardAIChanges()
	review, err := prepareAIReview(a.root, dir, a.prefs.AIAccess != "完全控制")
	if err != nil {
		os.RemoveAll(dir)
		p.err = err.Error()
		return
	}
	prompt := aiPrompt(p.messages, task)
	if review.Approval {
		prompt += "\nWork only in the current working directory, which is an isolated review copy. Do not write to original or historical absolute project paths. Changes will be reviewed before being applied to the real project."
	}
	command, env, err := aiCommand(a.prefs, prompt, dir, review.Work)
	if err == nil && review.Approval {
		var sandboxEnv []string
		command, sandboxEnv, err = sandboxAICommand(command, review, a.prefs.Engine)
		env = append(env, sandboxEnv...)
	}
	if err != nil {
		os.RemoveAll(dir)
		p.err = err.Error()
		return
	}
	p.progress = ""
	p.activities = nil
	p.review = review
	ctx, cancel := context.WithCancel(context.Background())
	p.cancel = cancel
	p.events = make(chan aiEvent, 64)
	p.running = true
	p.err = ""
	p.root = a.root
	p.engine = a.prefs.Engine
	p.draft = ""
	p.messages = append(p.messages, aiMessage{"user", task}, aiMessage{"assistant", ""})
	p.dirty = true
	a.checkpointAI(true)
	events := p.events
	go func() {
		defer cancel()
		changes, err := runAIObserved(ctx, command, env, review, events)
		if ctx.Err() != nil {
			err = fmt.Errorf("任务已停止；已生成的修改可在变更列表中查看")
		}
		// Completion must remain deliverable even if the window closed while
		// a verbose child filled the channel and was cancelled.
		done := aiEvent{done: true, err: err, scanned: true, changes: changes}
		if ctx.Err() == nil {
			events <- done
			return
		}
		for {
			select {
			case events <- done:
				return
			default:
				<-events
			}
		}
	}()
}

type aiOutput struct {
	ctx    context.Context
	events chan<- aiEvent
}

func (w aiOutput) Write(b []byte) (int, error) {
	select {
	case w.events <- aiEvent{text: string(b)}:
		return len(b), nil
	case <-w.ctx.Done():
		return 0, w.ctx.Err()
	}
}
func runAI(ctx context.Context, command, env []string, root string, events chan<- aiEvent) error {
	cmd := exec.CommandContext(ctx, command[0], command[1:]...)
	cmd.Dir = root
	path := os.Getenv("PATH")
	cmd.Env = append(os.Environ(), "PATH="+filepath.Dir(command[0])+string(os.PathListSeparator)+path)
	cmd.Env = append(cmd.Env, env...)
	cmd.Stdout = aiOutput{ctx, events}
	var stream *piEventStream
	for _, value := range env {
		if value == "OMNICODE_PI_EVENT_STREAM=1" {
			stream = &piEventStream{ctx: ctx, events: events}
			cmd.Stdout = stream
			break
		}
	}
	cmd.Stderr = aiOutput{ctx, events}
	cmd.WaitDelay = 2 * time.Second
	configureAIProcess(cmd)
	err := cmd.Run()
	if stream != nil {
		if streamErr := stream.flush(); streamErr != nil {
			return streamErr
		}
	}
	if err != nil {
		return fmt.Errorf("AI 引擎运行失败：%w", err)
	}
	return nil
}
