package main

import (
	_ "embed"
	"flag"
	"image/png"
	"log"
	"os"
	"path/filepath"
	"runtime"

	golet "github.com/HyperparameterSI/golet/backend"
	"github.com/HyperparameterSI/golet/backend/ui"
)

//go:embed assets/omnicode-icon.png
var applicationIcon []byte

func main() {
	screenshot := flag.String("screenshot", "", "render the native UI to a PNG without opening a window")
	preview := flag.Bool("preview", false, "open Markdown with a split preview")
	flag.Parse()
	root := ""
	var files []string
	for _, path := range flag.Args() {
		info, err := os.Stat(path)
		if err != nil {
			log.Fatal(err)
		}
		if info.IsDir() {
			root = path
		} else {
			files = append(files, path)
		}
	}
	if root == "" && len(files) > 0 {
		root = filepath.Dir(files[0])
	}
	a := newApp(root)
	for _, p := range files {
		a.open(p)
	}
	if d := a.current(); d != nil && d.markdown() {
		d.preview = *preview
	}
	if *screenshot != "" {
		t := ui.NewTester(a.view, 1440, 900)
		f, err := os.Create(*screenshot)
		if err != nil {
			log.Fatal(err)
		}
		defer f.Close()
		if err = png.Encode(f, t.Image()); err != nil {
			log.Fatal(err)
		}
		return
	}
	golet.App.WhenReady(func() {
		a.initProjectStore()
		if a.projects != nil {
			if p, err := a.projects.settings(); err != nil {
				a.report(err)
			} else {
				a.prefs = p
			}
			if flag.NArg() == 0 {
				a.report(a.restoreWorkspace())
			}
			a.trackWorkspace = true
			a.checkpointWorkspace()
		}
		a.systemDark = golet.Theme.IsDark()
		a.applyAppearance()
		golet.Theme.OnUpdated(func() { a.systemDark = golet.Theme.IsDark(); a.applyAppearance() })
		golet.App.OnQuit(func() {
			a.checkpointWorkspace()
			a.trackWorkspace = false
			a.closeIntelligence()
			a.closeGit()
			for _, tab := range a.ai.tabs {
				a.saveAISession(tab, true)
				if tab.cancel != nil {
					tab.cancel()
				}
			}
			if a.tray != nil {
				a.tray.Destroy()
			}
			if a.projects != nil {
				_ = a.projects.db.Close()
			}
		})
		if runtime.GOOS == "darwin" {
			if err := golet.App.Dock.SetIcon(applicationIcon); err != nil {
				log.Printf("Dock icon: %v", err)
			}
		}
		a.win = golet.NewWindow(golet.WindowOptions{Title: "Omnicode", TitleBarStyle: golet.TitleBarHiddenInset, TrafficLightPosition: &golet.Point{X: 12, Y: 11}, TitleBarHeight: 40, Width: 1440, Height: 900, MinWidth: 800, MinHeight: 520, BackgroundColor: "#f7f7f6", Content: ui.View(a.view)})
		a.updateRecentDock()
		a.win.OnClosed(func() {
			a.checkpointWorkspace()
			if a.backupWindow != nil {
				a.backupWindow.Destroy()
			}
			a.closeIntelligence()
			a.closeGit()
			for _, preview := range a.webPreviews {
				preview.window.Destroy()
			}
			a.drainAI()
			for _, tab := range a.ai.tabs {
				a.saveAISession(tab, true)
				if tab.cancel != nil {
					tab.cancel()
				}
			}
			for _, d := range a.docs {
				if d.http != nil && d.http.cancel != nil {
					d.http.cancel()
				}
				if d.slides != nil && d.slides.pane != nil {
					d.slides.pane.close()
					d.slides.pane = nil
				}
				if v := d.slides; v != nil && v.window != nil {
					v.window.Destroy()
				}
			}
			a.terminal.closeAll()
		})
		a.win.OnClose(func(e *golet.CloseEvent) {
			if !a.canQuit() {
				e.PreventDefault()
			}
		})
		run := func(fn func()) func(*golet.MenuItem, *golet.Window) {
			return func(*golet.MenuItem, *golet.Window) { fn(); a.win.Invalidate() }
		}
		workspaceAction := func(fn func()) func(*golet.MenuItem, *golet.Window) {
			return run(func() { a.settingsOpen = false; a.schedules.open = false; fn() })
		}
		golet.App.SetMenu(golet.NewMenu([]*golet.MenuItem{
			{Role: golet.RoleAppMenu},
			{Label: "File", Submenu: []*golet.MenuItem{
				{Label: "New File", Accelerator: "CmdOrCtrl+N", Click: workspaceAction(a.newFile)},
				{Label: "Open File…", Accelerator: "CmdOrCtrl+O", Click: workspaceAction(func() { a.pick(false) })},
				{Label: "Open Folder…", Accelerator: "CmdOrCtrl+Shift+O", Click: run(func() { a.pick(true) })},
				golet.Separator(),
				{Label: "Save", Accelerator: "CmdOrCtrl+S", Click: workspaceAction(func() { a.save(a.current(), false) })},
				{Label: "Save As…", Accelerator: "CmdOrCtrl+Shift+S", Click: workspaceAction(func() { a.save(a.current(), true) })},
				{Label: "Close Tab", Accelerator: "CmdOrCtrl+W", Click: func(_ *golet.MenuItem, w *golet.Window) {
					if w != nil && w == a.backupWindow {
						w.Close()
						return
					}
					defer a.win.Invalidate()
					if a.schedules.open {
						a.schedules.open = false
					} else if a.settingsOpen {
						a.settingsOpen = false
					} else {
						a.closeDoc(a.active)
					}
				}},
			}},
			{Role: golet.RoleEditMenu},
			{Label: "Settings", Submenu: []*golet.MenuItem{{Label: "设置…", Accelerator: "CmdOrCtrl+,", Click: run(a.showSettings)}}},
			{Label: "View", Submenu: []*golet.MenuItem{
				{Label: "跳转到定义", Click: run(func() {
					if d := a.current(); !a.settingsOpen && !a.schedules.open && d != nil && d.state != nil && d.state.OnDefinition != nil {
						d.state.OnDefinition(d.state.Cursor())
					} else {
						a.notice = "请在支持语言服务的代码文件中选择方法名"
					}
				})},
				{Label: "Quick Open", Accelerator: "CmdOrCtrl+P", Click: run(a.toggleQuick)},
				{Label: "Toggle Sidebar", Accelerator: "CmdOrCtrl+B", Click: run(func() { a.sidebar = !a.sidebar })},
				{Label: "Toggle Terminal", Accelerator: "Ctrl+`", Click: run(a.toggleTerminal)},
				{Label: "Toggle Word Wrap", Click: workspaceAction(func() { a.wrap = !a.wrap })},
				{Label: "Format Go", Accelerator: "Alt+Shift+F", Click: workspaceAction(a.format)},
			}},
			{Role: golet.RoleWindowMenu},
		}))
		var err error
		a.tray, err = golet.NewTray(golet.TrayOptions{ID: "dev.omnicode.editor", Icon: trayIcon(), IconIsTemplate: true, ToolTip: "Omnicode", Menu: golet.NewMenu([]*golet.MenuItem{
			{Label: "打开 Omnicode", Click: run(func() { a.win.Show(); a.win.Focus() })},
			{Label: "设置…", Click: run(a.showSettings)}, golet.Separator(),
			{Label: "退出", Click: run(func() {
				if a.canQuit() {
					golet.App.Quit()
				}
			})},
		})})
		if err != nil {
			log.Printf("系统状态栏: %v", err)
		}
	})
	if err := golet.App.Run(); err != nil {
		log.Fatal(err)
	}
}
