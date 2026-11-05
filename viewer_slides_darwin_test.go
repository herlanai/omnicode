//go:build darwin

package main

import (
	"fmt"
	"github.com/ebitengine/purego"
	"os"
	"path/filepath"
	"testing"
	"time"
	"unsafe"

	golet "github.com/HyperparameterSI/golet/backend"
	"github.com/HyperparameterSI/golet/backend/ui"
	"github.com/ebitengine/purego/objc"
)

func TestMain(m *testing.M) {
	if os.Getenv("OMNICODE_PPTX_NATIVE_TEST") == "" && os.Getenv("OMNICODE_WEB_NATIVE_TEST") == "" {
		os.Exit(m.Run())
	}
	done := make(chan int, 1)
	go func() { done <- m.Run(); golet.App.Quit() }()
	if err := golet.App.Run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	os.Exit(<-done)
}

// Opt-in because this opens native macOS windows and uses the window server.
func TestQuickLookNativePreview(t *testing.T) {
	path := os.Getenv("OMNICODE_PPTX_NATIVE_TEST")
	if path == "" {
		t.Skip("requires a macOS window session and a real PPTX fixture")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		t.Fatal(err)
	}
	a := newApp("")
	a.open(abs)
	if a.current() == nil {
		t.Fatal(a.notice)
	}
	result := make(chan error, 1)
	golet.App.WhenReady(func() {
		a.win = golet.NewWindow(golet.WindowOptions{Title: "Omnicode preview test", TitleBarStyle: golet.TitleBarHiddenInset, TrafficLightPosition: &golet.Point{X: 12, Y: 11}, Width: 900, Height: 600, Content: ui.View(a.view)})
		v := a.current().slides
		go func() {
			time.Sleep(2 * time.Second)
			var err error
			golet.RunOnMain(func() {
				if v.pane == nil || v.window != nil {
					err = fmt.Errorf("inline preview missing or unexpectedly opened a window: %s", v.previewError)
					return
				}
				host := objc.ID(a.win.NativeHandle()).Send(objc.RegisterName("contentView"))
				bounds := quickLook.bounds(host, objc.RegisterName("bounds"))
				h, _ := purego.Dlopen("/usr/lib/libobjc.A.dylib", purego.RTLD_NOW|purego.RTLD_GLOBAL)
				var repForRect func(objc.ID, objc.SEL, cgRect) objc.ID
				var cache func(objc.ID, objc.SEL, cgRect, objc.ID)
				purego.RegisterLibFunc(&repForRect, h, "objc_msgSend")
				purego.RegisterLibFunc(&cache, h, "objc_msgSend")
				rep := repForRect(host, objc.RegisterName("bitmapImageRepForCachingDisplayInRect:"), bounds)
				if rep == 0 {
					err = fmt.Errorf("no capture bitmap")
					return
				}
				cache(host, objc.RegisterName("cacheDisplayInRect:toBitmapImageRep:"), bounds, rep)
				dict := objc.ID(objc.GetClass("NSDictionary")).Send(objc.RegisterName("dictionary"))
				data := rep.Send(objc.RegisterName("representationUsingType:properties:"), uintptr(4), dict)
				n := int(data.Send(objc.RegisterName("length")))
				ptr := objc.Send[unsafe.Pointer](data, objc.RegisterName("bytes"))
				if ptr == nil || n == 0 {
					err = fmt.Errorf("empty capture")
					return
				}
				err = os.WriteFile("artifacts/pptx-inline-preview.png", unsafe.Slice((*byte)(ptr), n), 0600)
			})
			golet.RunOnMain(func() { a.newFile(); a.win.Invalidate() })
			time.Sleep(150 * time.Millisecond)
			golet.RunOnMain(func() {
				if pane, ok := v.pane.(*nativeSlidePane); ok && pane.view.Send(objc.RegisterName("isHidden")) == 0 {
					err = fmt.Errorf("preview covers another tab")
				}
				a.active = 0
				a.win.Invalidate()
			})
			time.Sleep(150 * time.Millisecond)

			golet.RunOnMain(func() {
				if pane, ok := v.pane.(*nativeSlidePane); ok {
					if pane.view.Send(objc.RegisterName("isHidden")) != 0 {
						err = fmt.Errorf("preview not restored after tab switch")
					}
					if pane.last.X < a.sidebarWidth+42 || pane.last.Y < 140 || pane.last.W <= 0 || pane.last.H <= 0 {
						err = fmt.Errorf("preview outside document region: %+v", pane.last)
					}
				}
				a.closeDoc(0)
				if v.pane != nil {
					err = fmt.Errorf("inline preview not released on tab close")
				}
				if v.window != nil {
					v.window.Destroy()
				}
				if v.window != nil && err == nil {
					err = fmt.Errorf("preview not released")
				}
				a.win.Destroy()
				result <- err
				golet.App.Quit()
			})
		}()
	})
	if err = <-result; err != nil {
		t.Fatal(err)
	}
}
