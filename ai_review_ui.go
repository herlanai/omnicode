package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	golet "github.com/HyperparameterSI/golet/backend"
	"github.com/HyperparameterSI/golet/backend/ui"
	"github.com/HyperparameterSI/golet/backend/ui/editor"
	"github.com/HyperparameterSI/golet/backend/ui/forma"
)

func (a *app) hasPendingAI() bool {
	return a.ai.pending()
}
func (a *app) discardAIChanges() {
	a.ai.aiSession.discardChanges()
}
func (p *aiSession) discardChanges() {
	if p.running {
		return
	}
	if r := p.review; r != nil {
		os.RemoveAll(r.Temp)
	}
	p.review = nil
}
func (a *app) approveAIChanges(paths map[string]bool) {
	a.approveAISessionChanges(a.ai.aiSession, paths)
}
func (a *app) approveAISessionChanges(p *aiSession, paths map[string]bool) {
	r := p.review
	if r == nil || !r.Approval || p.running {
		return
	}
	for _, change := range r.Changes {
		if !paths[change.Path] {
			continue
		}
		for _, d := range a.docs {
			if d.path == filepath.Join(r.Root, change.Path) && d.editable() && d.state.Buffer.Modified() {
				p.err = "请先保存编辑器中的修改，再审批 AI 变更"
				return
			}
		}
		if err := checkAIChange(r.Root, change); err != nil {
			p.err = err.Error()
			return
		}
	}
	var applied []string
	var remaining []aiChange
	for _, change := range r.Changes {
		if !paths[change.Path] {
			remaining = append(remaining, change)
			continue
		}
		if err := a.applyApprovedChange(r.Root, change); err != nil {
			p.err = err.Error()
			remaining = append(remaining, change)
			continue
		}
		a.rememberEditedFile(filepath.Join(r.Root, change.Path))
		applied = append(applied, change.Path)
	}
	r.Changes = remaining
	if len(applied) > 0 {
		p.messages = append(p.messages, aiMessage{"assistant", fmt.Sprintf("已批准并应用 %d 个文件的修改：\n\n- %s", len(applied), strings.Join(applied, "\n- "))})
		p.dirty = true
		a.saveAISession(p, true)
		a.refresh()
		a.reloadAITextFor(p.root)
	}
	if len(remaining) == 0 {
		p.discardChanges()
	}
}
func (a *app) showAIDiff(change aiChange) {
	p := a.ai.aiSession
	r := p.review
	if r == nil {
		return
	}
	state := &editor.DiffState{Old: editor.NewBuffer(diffAIText(change.Before)), New: editor.NewBuffer(diffAIText(change.After)), ExpandUnchanged: true}
	var window *golet.Window
	window = golet.NewWindow(golet.WindowOptions{Title: change.label(), TitleBarStyle: golet.TitleBarHiddenInset, TrafficLightPosition: &golet.Point{X: 12, Y: 11}, TitleBarHeight: 40, Width: 1000, Height: 720, MinWidth: 650, MinHeight: 420, Content: ui.View(func(c *ui.Context) {
		c.SetTheme(a.theme)
		c.After(200 * time.Millisecond)
		ui.Column(c).Fill().Children(func() {
			childTitlebar(c, change.label(), func() {
				if r.Approval {
					forma.Button(c, "批准此文件").Disabled(p.running || p.review != r).OnClick(func() {
						// Approve only the exact revision shown in this window.
						for _, latest := range r.Changes {
							if latest.Path == change.Path && !sameAIChange(latest, change) {
								p.err = "差异已更新，请重新打开预览后批准"
								window.Close()
								return
							}
						}
						a.approveAISessionChanges(p, map[string]bool{change.Path: true})
						window.Close()
					})
				}
			})
			editor.DiffView(c, state).Fill()
		})
	})})
}
func (a *app) aiChangesView(c *ui.Context) {
	r := a.ai.review
	if r == nil {
		return
	}
	title := "正在观察文件修改…"
	if r.Approval {
		title = "修改在项目副本中生成，批准后写入项目"
	}
	if !a.ai.running && len(r.Changes) == 0 {
		title = "未检测到文件变更"
	}
	if len(r.Changes) > 0 {
		title = fmt.Sprintf("%d 个文件变更", len(r.Changes))
		if r.Approval {
			title += " · 待批准"
		} else {
			title += " · 已写入"
		}
	}
	ui.Column(c).FillWidth().Gap(6).Children(func() {
		muted(c, title).FontSize(12).Wrap()
		if len(r.Changes) > 0 {
			ui.Scroll(c.Key("ai-changes")).FillWidth().Height(float32(min(120, len(r.Changes)*36))).Children(func() {
				ui.Column(c).FillWidth().Children(func() {
					for _, change := range r.Changes {
						change := change
						forma.Button(c, change.label(), forma.ButtonOptions{Variant: forma.Ghost}).FillWidth().Justify(ui.Start).OnClick(func() { a.showAIDiff(change) })
					}
				})
			})
			if r.Approval {
				ui.Row(c).FillWidth().Gap(8).Children(func() {
					forma.Button(c, "全部批准").Disabled(a.ai.running).OnClick(func() {
						paths := map[string]bool{}
						for _, change := range r.Changes {
							paths[change.Path] = true
						}
						a.approveAIChanges(paths)
					})
					forma.Button(c, "拒绝", forma.ButtonOptions{Variant: forma.Ghost}).Disabled(a.ai.running).OnClick(func() {
						a.discardAIChanges()
						a.ai.messages = append(a.ai.messages, aiMessage{"assistant", "修改已拒绝，未写入项目。"})
						a.ai.dirty = true
						a.checkpointAI(true)
					})
				})
			}
		}
	})
}
