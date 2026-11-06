package main

import (
	"fmt"

	"github.com/HyperparameterSI/golet/backend/ui"
)

var aiEngines = []string{"pi-coding-agent", "CodeBuddy CLI", "DSH"}

func aiEngineLabel(engine string) string {
	switch engine {
	case "pi-coding-agent":
		return "PICoding"
	case "CodeBuddy CLI":
		return "CodeBuddy"
	default:
		return engine
	}
}

func (a *app) newAITab() {
	a.ai.serial++
	tab := &aiSession{number: a.ai.serial}
	a.ai.tabs = append(a.ai.tabs, tab)
	a.ai.aiSession = tab
}

func (p *aiSession) pending() bool {
	return p.review != nil && p.review.Approval && len(p.review.Changes) > 0
}

func (a *app) busyAITab() *aiSession {
	for _, tab := range a.ai.tabs {
		if tab.running || tab.pending() {
			return tab
		}
	}
	return nil
}

func (a *app) closeAITab(tab *aiSession) {
	if tab.running || tab.pending() || !a.saveAISession(tab, true) {
		return
	}
	a.removeAITab(tab)
}

func (a *app) removeAITab(tab *aiSession) {
	for i, item := range a.ai.tabs {
		if item != tab {
			continue
		}
		a.ai.tabs = append(a.ai.tabs[:i:i], a.ai.tabs[i+1:]...)
		if len(a.ai.tabs) == 0 {
			a.ai.visible = false
			a.ai.aiSession = nil
			a.ai.shownTab = nil
			a.ai.serial = 0
		} else if a.ai.aiSession == tab {
			a.ai.aiSession = a.ai.tabs[min(i, len(a.ai.tabs)-1)]
		}
		return
	}
}

func (a *app) aiTabStrip(c *ui.Context) {
	ui.ScrollHorizontal(c.Key("ai-tabs")).Grow(1).MinWidth(0).FillHeight().Children(func() {
		ui.Row(c).FillHeight().AlignItems(ui.Center).Gap(3).Children(func() {
			for _, tab := range a.ai.tabs {
				label := fmt.Sprintf("agent %d", tab.number)
				if tab.running {
					label += " ···"
				} else if tab.pending() {
					label += " •"
				}
				bg := ui.Transparent
				if tab == a.ai.aiSession {
					bg = c.Theme().Surface
				}
				chip := ui.Row(c.Key(fmt.Sprintf("ai-tab-%d", tab.number))).Height(30).Shrink(0).Radius(7).Background(bg).AlignItems(ui.Center).Children(func() {
					action(c, "select", label, func() { a.ai.aiSession = tab }).Height(30).Padding(0, 21, 0, 10)
					closeLabel := fmt.Sprintf("关闭 agent %d", tab.number)
					ui.Button(c.Key("close"), "").Absolute().Top(0).Right(0).Size(18, 18).MinHeight(0).MinWidth(0).Padding(3).
						Background(ui.Transparent).Border(0, ui.Transparent).Radius(4).Label(closeLabel).Tooltip(closeLabel).
						Disabled(tab.running || tab.pending()).OnClick(func() { a.closeAITab(tab) }).
						Children(func() { icon(c, "close", 12) })
				})
				if tab == a.ai.aiSession && tab != a.ai.shownTab {
					chip.ScrollIntoView()
					a.ai.shownTab = tab
				}
			}
		})
	})
}
