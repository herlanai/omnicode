package main

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"path/filepath"
	"time"
	"unicode/utf8"

	golet "github.com/HyperparameterSI/golet/backend"
	"github.com/HyperparameterSI/golet/backend/ui"
	"github.com/HyperparameterSI/golet/backend/ui/editor"
	"github.com/HyperparameterSI/golet/backend/ui/forma"
)

func backupDiff(path string, current, saved *aiFile) *editor.DiffState {
	buffer := func(f *aiFile) *editor.Buffer {
		if f != nil && (!utf8.Valid(f.Data) || bytes.IndexByte(f.Data, 0) >= 0) {
			return editor.NewBuffer(fmt.Sprintf("二进制文件 · %d 字节\nSHA-256  %x", len(f.Data), sha256.Sum256(f.Data)))
		}
		b := editor.NewBuffer(diffAIText(f))
		b.SetLanguage(editor.LanguageFor(path))
		return b
	}
	return &editor.DiffState{Old: buffer(current), New: buffer(saved), Layout: editor.DiffSplit, ExpandUnchanged: true}
}

func (a *app) presentBackupWindow() {
	if a.win == nil {
		return
	}
	if w := a.backupWindow; w != nil {
		w.Show()
		w.Focus()
		w.Invalidate()
		return
	}
	w := golet.NewWindow(golet.WindowOptions{Title: "AI 修改备份", TitleBarStyle: golet.TitleBarHiddenInset, TrafficLightPosition: &golet.Point{X: 12, Y: 11}, TitleBarHeight: 40, Width: 1180, Height: 760, MinWidth: 760, MinHeight: 450, Content: ui.View(a.backupView)})
	a.backupWindow = w
	w.OnClosed(func() {
		if a.backupWindow == w {
			a.backupWindow = nil
			a.backups = backupBrowser{}
		}
	})
}

func backupSideLabel(label string, f *aiFile) string {
	if f == nil {
		return label + " · 已删除"
	}
	if len(f.Data) == 0 {
		return label + " · 空文件"
	}
	return label
}
func (a *app) backupView(c *ui.Context) {
	b := &a.backups
	c.SetTheme(a.theme)
	ui.Column(c).Fill().Background(a.theme.Background).Children(func() {
		childTitlebar(c, "AI 修改备份", func() {
			if b.selected == nil {
				return
			}
			label := "恢复并替换当前文件"
			if b.current == nil {
				label = "恢复已删除的文件"
			}
			forma.Button(c.Key("restore-backup"), label, forma.ButtonOptions{Size: forma.Small}).OnClick(func() {
				err := a.restoreAIBackup()
				if b.open {
					if err != nil {
						b.err = err.Error()
					}
					return
				}
				a.report(err)
				if a.win != nil {
					a.win.Invalidate()
				}
				if w := a.backupWindow; w != nil {
					w.Close()
				}
			})
		})
		if b.err != "" {
			ui.Text(c, b.err).TextColor(c.Theme().Danger).Padding(8, 12).Wrap()
		}
		if len(b.versions) == 0 {
			muted(c, "暂无 AI 审批前的备份").Padding(16)
			return
		}
		ui.Row(c).FillWidth().Grow(1).MinHeight(0).Children(func() {
			ui.Column(c).Width(200).FillHeight().Padding(10).Gap(8).Background(c.Theme().Sidebar).Children(func() {
				ui.Text(c, "备份版本").Bold()
				ui.List(c.Key("backup-versions"), nil, len(b.versions), func(i int) {
					v := b.versions[i]
					rel, _ := filepath.Rel(v.root, v.path)
					stamp := time.Unix(0, v.created).Format("2006-01-02 15:04:05.000")
					label := stamp + " · " + v.action + "前 · " + rel
					bg := ui.Transparent
					if b.selected != nil && b.selected.id == v.id {
						bg = c.Theme().Sidebar.Mix(c.Theme().Accent, 0.14)
					}
					ui.Button(c.Key(fmt.Sprint(v.id)), "").FillWidth().Height(50).Padding(6, 8).Border(0, ui.Transparent).Radius(6).Background(bg).Label(label).Tooltip(label).OnClick(func() {
						if err := a.selectAIBackup(v); err != nil {
							b.err = err.Error()
						}
					}).Children(func() {
						ui.Column(c).FillWidth().Gap(3).Children(func() {
							ui.Text(c, filepath.Base(v.path)+" · "+v.action+"前").MaxLines(1)
							muted(c, stamp).MaxLines(1)
						})
					})
				}).Grow(1).FillWidth()
			})
			if b.selected == nil || b.diff == nil {
				return
			}
			ui.Column(c).Grow(1).MinWidth(0).FillHeight().Children(func() {
				muted(c, compactPath(b.selected.path)).MaxLines(1).Padding(8, 12).Tooltip(b.selected.path)
				ui.Row(c).FillWidth().Background(c.Theme().Surface).Children(func() {
					ui.Text(c, backupSideLabel("当前文件（磁盘）", b.current)).Basis(0).Grow(1).Padding(6, 12)
					ui.Text(c, backupSideLabel("备份版本（恢复后）", b.saved)).Basis(0).Grow(1).Padding(6, 12)
				})
				b.diff.Theme = a.codeTheme
				editor.DiffView(c.Key(fmt.Sprintf("backup-diff-%d", b.selected.id)), b.diff).Grow(1).FillWidth().MinHeight(0)
			})
		})
	})
}
