//go:build darwin

package main

import (
	"fmt"
	"path/filepath"
	"sync"
	"time"

	golet "github.com/HyperparameterSI/golet/backend"
	"github.com/HyperparameterSI/golet/backend/ui"
	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"
)

var quickLook struct {
	once     sync.Once
	err      error
	initView func(objc.ID, objc.SEL, cgRect, int) objc.ID
	bounds   func(objc.ID, objc.SEL) cgRect
	setFrame func(objc.ID, objc.SEL, cgRect)
}

func initQuickLook() error {
	quickLook.once.Do(func() {
		_, quickLook.err = purego.Dlopen("/System/Library/Frameworks/Quartz.framework/Frameworks/QuickLookUI.framework/QuickLookUI", purego.RTLD_NOW|purego.RTLD_GLOBAL)
		if quickLook.err != nil {
			return
		}
		h, err := purego.Dlopen("/usr/lib/libobjc.A.dylib", purego.RTLD_NOW|purego.RTLD_GLOBAL)
		if err != nil {
			quickLook.err = err
			return
		}
		purego.RegisterLibFunc(&quickLook.initView, h, "objc_msgSend")
		purego.RegisterLibFunc(&quickLook.bounds, h, "objc_msgSend")
		purego.RegisterLibFunc(&quickLook.setFrame, h, "objc_msgSend")
		if objc.GetClass("QLPreviewView") == 0 {
			quickLook.err = fmt.Errorf("系统 Quick Look 预览不可用")
		}
	})
	return quickLook.err
}

// The original Office document is handed to the OS preview renderer. No text
// reflow, conversion, network service or external Office installation is used.
func (a *app) previewSlides(d *document) {
	if a.win == nil || d == nil || d.slides == nil {
		return
	}
	v := d.slides
	if v.window != nil {
		v.window.Show()
		v.window.Focus()
		return
	}
	golet.RunOnMain(func() {
		if err := initQuickLook(); err != nil {
			v.previewError = err.Error()
			a.win.Invalidate()
			return
		}
		abs, err := filepath.Abs(d.path)
		if err != nil {
			v.previewError = err.Error()
			return
		}
		w := golet.NewWindow(golet.WindowOptions{Title: d.name, TitleBarStyle: golet.TitleBarHiddenInset, TrafficLightPosition: &golet.Point{X: 12, Y: 11}, TitleBarHeight: 40, Width: 1120, Height: 760, MinWidth: 640, MinHeight: 420, Hidden: true, Content: ui.View(func(c *ui.Context) {
			c.SetTheme(a.theme)
			ui.Column(c).Fill().Children(func() { childTitlebar(c, d.name, nil); ui.Box(c).Fill().Background(ui.Hex("#202020")) })
		})})
		host := objc.ID(w.NativeHandle()).Send(objc.RegisterName("contentView"))
		allocated := objc.ID(objc.GetClass("QLPreviewView")).Send(objc.RegisterName("alloc"))
		frame := quickLook.bounds(host, objc.RegisterName("bounds"))
		frame.H = max(0, frame.H-40)
		if host.Send(objc.RegisterName("isFlipped")) != 0 {
			frame.Y += 40
		}
		view := quickLook.initView(allocated, objc.RegisterName("initWithFrame:style:"), frame, 0)
		if view == 0 {
			w.Destroy()
			v.previewError = "无法创建系统预览，请使用文字视图或系统默认应用打开"
			a.win.Invalidate()
			return
		}
		view.Send(objc.RegisterName("setAutoresizingMask:"), uintptr(2|16))
		view.Send(objc.RegisterName("setShouldCloseWithWindow:"), true)
		view.Send(objc.RegisterName("setAutostarts:"), false)
		str := objc.ID(objc.GetClass("NSString")).Send(objc.RegisterName("stringWithUTF8String:"), abs)
		url := objc.ID(objc.GetClass("NSURL")).Send(objc.RegisterName("fileURLWithPath:"), str)
		host.Send(objc.RegisterName("addSubview:"), view)
		view.Send(objc.RegisterName("setPreviewItem:"), url)
		v.window = w
		v.previewError = ""
		w.OnClosed(func() {
			view.Send(objc.RegisterName("close"))
			view.Send(objc.RegisterName("removeFromSuperview"))
			view.Send(objc.RegisterName("release"))
			v.window = nil
		})
		w.Show()
		w.Focus()
	})
}

// QLPreviewView is a native NSView placed inside the document region. It is
// hidden when another tab or an in-app modal takes over, and released with its tab.
type nativeSlidePane struct {
	view, host objc.ID
	last       ui.Rect
	visible    bool
}

func (p *nativeSlidePane) place(r ui.Rect) {
	golet.RunOnMain(func() {
		if p.view == 0 {
			return
		}
		y := float64(r.Y)
		if p.host.Send(objc.RegisterName("isFlipped")) == 0 {
			y = quickLook.bounds(p.host, objc.RegisterName("bounds")).H - y - float64(r.H)
		}
		if p.last != r {
			quickLook.setFrame(p.view, objc.RegisterName("setFrame:"), cgRect{float64(r.X), y, float64(r.W), float64(r.H)})
			p.last = r
		}
		if !p.visible {
			p.view.Send(objc.RegisterName("setHidden:"), false)
			p.visible = true
		}
	})
}
func (p *nativeSlidePane) hide() {
	golet.RunOnMain(func() {
		if p.view != 0 {
			p.view.Send(objc.RegisterName("setHidden:"), true)
			p.visible = false
		}
	})
}
func (p *nativeSlidePane) close() {
	golet.RunOnMain(func() {
		if p.view != 0 {
			p.view.Send(objc.RegisterName("close"))
			p.view.Send(objc.RegisterName("removeFromSuperview"))
			p.view.Send(objc.RegisterName("release"))
			p.view = 0
		}
	})
}
func (a *app) embeddedSlides(c *ui.Context, d *document) {
	v := d.slides
	box := ui.Box(c.Key("native-slide-canvas")).Grow(1).FillWidth().MinHeight(0).Background(c.Theme().Surface)
	if a.win == nil {
		return
	} // Headless UI rendering does not initialize native frameworks.
	if v.pane == nil && v.previewError == "" {
		golet.RunOnMain(func() {
			if err := initQuickLook(); err != nil {
				v.previewError = err.Error()
				return
			}
			host := objc.ID(a.win.NativeHandle()).Send(objc.RegisterName("contentView"))
			allocated := objc.ID(objc.GetClass("QLPreviewView")).Send(objc.RegisterName("alloc"))
			view := quickLook.initView(allocated, objc.RegisterName("initWithFrame:style:"), cgRect{0, 0, 1, 1}, 0)
			if view == 0 {
				v.previewError = "系统无法创建 PPTX 原版视图"
				return
			}
			view.Send(objc.RegisterName("setHidden:"), true)
			view.Send(objc.RegisterName("setAutostarts:"), false)
			host.Send(objc.RegisterName("addSubview:"), view)
			abs, _ := filepath.Abs(d.path)
			str := objc.ID(objc.GetClass("NSString")).Send(objc.RegisterName("stringWithUTF8String:"), abs)
			url := objc.ID(objc.GetClass("NSURL")).Send(objc.RegisterName("fileURLWithPath:"), str)
			view.Send(objc.RegisterName("setPreviewItem:"), url)
			v.pane = &nativeSlidePane{view: view, host: host}
		})
	}
	if v.pane != nil {
		r := box.Bounds()
		if r.W > 0 && r.H > 0 && !a.quick && !a.filePrompt.open {
			v.pane.place(r)
		} else {
			v.pane.hide()
		}
		// Bounds is the last committed layout. Follow resize/splitter changes on
		// the next frame without recreating the document or losing its scroll position.
		c.After(50 * time.Millisecond)
	}
	if v.previewError != "" {
		box.Children(func() { ui.Text(c, v.previewError).Padding(24).Wrap() })
	}
}
