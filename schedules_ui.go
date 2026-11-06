package main

import (
	"fmt"
	golet "github.com/HyperparameterSI/golet/backend"
	"strings"
	"time"

	"github.com/HyperparameterSI/golet/backend/ui"
	"github.com/HyperparameterSI/golet/backend/ui/forma"
)

func (a *app) newSchedule(name, prompt, cadence string) {
	a.schedules.edit = &scheduledTask{workspace: a.root, name: name, prompt: prompt, cadence: cadence, clock: "09:00", engine: a.prefs.Engine}
	a.schedules.err = ""
}
func (a *app) scheduleView(c *ui.Context) {
	s := &a.schedules
	if a.projects == nil {
		muted(c, "任务数据库不可用")
		return
	}
	ui.Scroll(c.Key("schedule-content")).Grow(1).MinWidth(0).FillHeight().Children(func() {
		ui.Column(c).FillWidth().AlignItems(ui.Center).Children(func() {
			ui.Column(c).FillWidth().MaxWidth(760).Padding(24).Gap(14).Children(func() {
				if s.err != "" {
					ui.Text(c, s.err).TextColor(c.Theme().Danger)
				}
				if s.edit != nil {
					a.scheduleForm(c, s.edit)
					return
				}
				for _, t := range s.tasks {
					if t.id == s.selected {
						a.scheduleDetail(c, t)
						return
					}
				}
				a.scheduleTemplates(c)
			})
		})
	})
}
func timingLabel(t scheduledTask) string {
	if t.due == 0 {
		return "已暂停"
	}
	return t.cadence + " · " + time.Unix(t.due, 0).Format("01-02 15:04")
}
func (a *app) scheduleForm(c *ui.Context, t *scheduledTask) {
	title := "新建任务"
	if t.id != 0 {
		title = "编辑任务"
	}
	settingsHeading(c, "clock", title, 22)
	settingsField(c, "项目目录", func() {
		ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
			label := compactPath(t.workspace)
			if label == "" {
				label = "选择项目文件夹"
			}
			muted(c, label).Grow(1).Wrap()
			if t.id == 0 {
				iconButton(c, "choose-project", "folder", "选择项目目录", func() {
					paths, err := golet.Dialog.Open(golet.OpenDialogOptions{Parent: a.win, Directory: true, Title: "选择任务的项目目录"})
					if err != nil {
						a.schedules.err = err.Error()
					} else if len(paths) > 0 {
						t.workspace = paths[0]
					}
				})
			}
		})
	})
	settingsField(c, "任务名称", func() {
		forma.Input(c, &t.name, forma.InputOptions{Placeholder: "例如：每日测试检查"}).FillWidth()
	})
	settingsField(c, "执行指令", func() {
		ui.TextAreaBase(c, &t.prompt).FillWidth().Height(96).Placeholder("希望 AI 在这个项目里完成什么？")
	})
	settingsField(c, "AI 助手", func() { forma.Select(c, &t.engine, aiEngines).FillWidth() })
	settingsField(c, "执行频率", func() { forma.Select(c, &t.cadence, cadences).FillWidth() })
	label := "本地时间"
	if t.cadence == "每小时" {
		label = "本地时间（每小时取分钟数）"
	}
	settingsField(c, label, func() { forma.Input(c, &t.clock, forma.InputOptions{Placeholder: "09:30"}).Width(120) })
	muted(c, "本机运行 · 保持 Omnicode 开启，错过的触发将合并补跑一次。").Wrap()
	muted(c, "文件修改权限 · "+a.prefs.AIAccess)
	ui.Row(c).Gap(8).Children(func() {
		forma.Button(c.Key("save"), "保存任务", forma.ButtonOptions{}).OnClick(func() { a.saveSchedule(*t) })
		action(c, "cancel", "取消", func() { a.schedules.edit = nil; a.schedules.err = "" })
	})
}
func (a *app) scheduleDetail(c *ui.Context, t scheduledTask) {
	s := &a.schedules
	settingsHeading(c, "clock", t.name, 22)
	muted(c, t.engine+" · "+timingLabel(t))
	muted(c, compactPath(t.workspace)).Wrap()
	ui.Text(c, t.prompt).Wrap()
	ui.Row(c).Gap(6).Children(func() {
		action(c, "run-now", "立即运行", func() { a.dispatchSchedule(t, time.Now(), t.due) })
		action(c, "edit", "编辑", func() { s.edit = &t; s.err = "" })
		label := "暂停"
		if t.due == 0 {
			label = "恢复"
		}
		action(c, "toggle", label, func() {
			var due int64
			if t.due == 0 {
				next, err := t.next(time.Now())
				if err != nil {
					s.err = err.Error()
					return
				}
				due = next.Unix()
			}
			a.scheduleSQL(`UPDATE scheduled_tasks SET due=? WHERE id=?`, due, t.id)
		})
		iconButton(c, "delete", "trash", "删除任务", func() {
			if a.scheduleSQL(`DELETE FROM scheduled_tasks WHERE id=?`, t.id) {
				s.selected = 0
			}
		})
	})
	if t.lastRun != "" {
		ui.Text(c, "最近执行").Bold()
		muted(c, t.result)
	}
	if len(s.runs) > 0 {
		ui.Text(c, "执行记录").Bold()
	}
	for _, run := range s.runs {
		action(c, run.ID, time.Unix(0, run.Updated).Format("2006-01-02 15:04:05")+"  ·  "+run.Title+"  ›", func() { a.openScheduledRun(t.workspace, run.ID) })
	}
}
func (a *app) openScheduledRun(root, id string) {
	s := &a.schedules
	for _, p := range a.ai.tabs {
		if p.chatID == id {
			a.ai.aiSession = p
			a.ai.visible = true
			s.open = false
			return
		}
	}
	chat, err := a.projects.loadAIChat(root, id)
	if err != nil {
		s.err = err.Error()
		return
	}
	a.newAITab()
	a.ai.applyStoredChat(root, id, chat)
	a.ai.visible = true
	s.open = false
}

func (a *app) scheduleList(c *ui.Context) {
	s := &a.schedules
	a.sidebarPanel(c, func() {
		ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
			ui.Text(c, "定时任务").Bold().Grow(1)
			iconButton(c, "create", "plus", "新建任务", func() { a.newSchedule("", "", "每天") }).Disabled(a.projects == nil)
			iconButton(c, "filter", "chevron-down", "筛选任务", nil).Menu(func(m *ui.Menu) {
				for _, label := range []string{"全部", "活跃", "已暂停"} {
					if m.Item(label).Chosen() {
						s.filter = label
					}
				}
			})
			toolButton(c, "search-tasks", "search", "搜索任务", s.search, func() {
				s.search = !s.search
				if !s.search {
					s.query = ""
				}
			})
		})
		if s.search {
			forma.Input(c, &s.query, forma.InputOptions{Placeholder: "搜索名称或项目"}).FillWidth()
		}
		ui.Scroll(c).FillWidth().Grow(1).Children(func() {
			ui.Column(c).FillWidth().Gap(4).Children(func() {
				count := 0
				for _, group := range []string{"活跃", "已暂停"} {
					if s.filter != "" && s.filter != "全部" && s.filter != group {
						continue
					}
					heading := false
					for _, t := range s.tasks {
						if (t.due == 0) != (group == "已暂停") || !strings.Contains(strings.ToLower(t.name+" "+t.workspace), strings.ToLower(strings.TrimSpace(s.query))) {
							continue
						}
						if !heading {
							muted(c, group)
							heading = true
						}
						count++
						bg := ui.Transparent
						if t.id == s.selected && s.edit == nil {
							bg = c.Theme().Sidebar.Mix(c.Theme().Accent, 0.14)
						}
						ui.Button(c.Key(fmt.Sprint(t.id)), "").FillWidth().Height(56).Padding(6, 10).Radius(7).Background(bg).Border(0, ui.Transparent).Label(t.name).OnClick(func() { s.selected = t.id; s.edit = nil; s.err = ""; s.poll = time.Time{} }).Children(func() {
							ui.Column(c).FillWidth().Gap(4).Children(func() { ui.Text(c, t.name).MaxLines(1); muted(c, timingLabel(t)) })
						})
					}
				}
				if count == 0 {
					muted(c, "暂无匹配任务")
				}
			})
		})
	})
}
func (a *app) scheduleTemplates(c *ui.Context) {
	width, _ := c.Size()
	ui.Column(c).FillWidth().Padding(32, 0, 20, 0).Gap(8).AlignItems(ui.Center).Children(func() {
		icon(c, "clock", 40)
		ui.Text(c, "安排任务").FontSize(22).Bold()
		muted(c, "选择一个模板，或新建任务让 AI 定期处理项目工作。").Wrap()
	})
	templates := []struct{ name, desc, prompt, cadence, symbol string }{
		{"检查测试并修复", "每天检查失败的测试，验证最小修复。", "运行项目已有测试，定位可复现的失败，做最小修复并重跑相关测试，汇报改动与结果。", "每天", "code"},
		{"审查近期提交", "每周汇总潜在回归和验证方法。", "审查过去一周的提交，列出可能的回归、涉及文件和验证方法。", "每周一", "git"},
		{"同步项目文档", "工作日检查 README 与代码的差异。", "对照近期代码改动检查 README 的使用说明，更新过时的示例并总结差异。", "工作日", "files"},
		{"整理发布说明", "每周从提交记录中提取用户可见的变化。", "基于本周的提交整理发布说明草稿，按新增功能、修复和兼容性变化分组，标出对应提交。", "每周五", "tag"},
	}
	cols := 2
	if width < 900 {
		cols = 1
	}
	for start := 0; start < len(templates); start += cols {
		ui.Row(c).FillWidth().Gap(12).Children(func() {
			for _, v := range templates[start:min(start+cols, len(templates))] {
				ui.Button(c.Key(v.name), "").Grow(1).Basis(0).MinWidth(0).Height(92).Padding(12).Border(1, c.Theme().Border).Radius(12).Background(c.Theme().Background).Label(v.name).OnClick(func() { a.newSchedule(v.name, v.prompt, v.cadence) }).Children(func() {
					ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(10).Children(func() {
						icon(c, v.symbol, 20)
						ui.Column(c).MinWidth(0).Grow(1).Gap(5).Children(func() { ui.Text(c, v.name).Bold(); muted(c, v.desc).Wrap() })
					})
				})
			}
		})
	}
}
