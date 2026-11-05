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
		}
		a.systemDark = golet.Theme.IsDark()
		a.applyAppearance()
		golet.Theme.OnUpdated(func() { a.systemDark = golet.Theme.IsDark(); a.applyAppearance() })
		golet.App.OnQuit(func() {
			a.closeIntelligence()
			a.closeGit()
			a.ai.dirty = true
			a.checkpointAI(true)
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
		a.win.OnClosed(func() {
			a.closeIntelligence()
			a.closeGit()
			for _, preview := range a.webPreviews {
				preview.window.Destroy()
			}
			a.drainAI()
			a.ai.dirty = true
			a.checkpointAI(true)
			if a.ai.cancel != nil {
				a.ai.cancel()
			}
			for _, d := range a.docs {
				if d.slides != nil && d.slides.pane != nil {
					d.slides.pane.close()
					d.slides.pane = nil
				}
				if v := d.slides; v != nil && v.window != nil {
					v.window.Destroy()
				}
			}
			if a.settingsWindow != nil {
				a.settingsWindow.Destroy()
			}
			if a.terminal.session != nil {
				_ = a.terminal.session.Close()
			}
		})
		a.win.OnClose(func(e *golet.CloseEvent) {
			if !a.canQuit() {
				e.PreventDefault()
			}
		})
		run := func(fn func()) func(*golet.MenuItem, *golet.Window) {
			return func(*golet.MenuItem, *golet.Window) { fn(); a.win.Invalidate() }
		}
		golet.App.SetMenu(golet.NewMenu([]*golet.MenuItem{
			{Role: golet.RoleAppMenu},
			{Label: "File", Submenu: []*golet.MenuItem{
				{Label: "New File", Accelerator: "CmdOrCtrl+N", Click: run(a.newFile)},
				{Label: "Open File…", Accelerator: "CmdOrCtrl+O", Click: run(func() { a.pick(false) })},
				{Label: "Open Folder…", Accelerator: "CmdOrCtrl+Shift+O", Click: run(func() { a.pick(true) })},
				golet.Separator(),
				{Label: "Save", Accelerator: "CmdOrCtrl+S", Click: run(func() { a.save(a.current(), false) })},
				{Label: "Save As…", Accelerator: "CmdOrCtrl+Shift+S", Click: run(func() { a.save(a.current(), true) })},
				{Label: "Close Tab", Accelerator: "CmdOrCtrl+W", Click: run(func() { a.closeDoc(a.active) })},
			}},
			{Role: golet.RoleEditMenu},
			{Label: "Settings", Submenu: []*golet.MenuItem{{Label: "设置…", Accelerator: "CmdOrCtrl+,", Click: run(a.showSettings)}}},
			{Label: "View", Submenu: []*golet.MenuItem{
				{Label: "Quick Open", Accelerator: "CmdOrCtrl+P", Click: run(a.toggleQuick)},
				{Label: "Toggle Sidebar", Accelerator: "CmdOrCtrl+B", Click: run(func() { a.sidebar = !a.sidebar })},
				{Label: "Toggle Terminal", Accelerator: "Ctrl+`", Click: run(a.toggleTerminal)},
				{Label: "Toggle Word Wrap", Click: run(func() { a.wrap = !a.wrap })},
				{Label: "Format Go", Accelerator: "Alt+Shift+F", Click: run(a.format)},
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
