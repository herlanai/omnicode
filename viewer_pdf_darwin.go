//go:build darwin

package main

import (
	"errors"
	"image"
	"math"
	"runtime"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
)

type cgRect struct{ X, Y, W, H float64 }
type cgTransform struct{ A, B, C, D, TX, TY float64 }

var pdfAPI struct {
	once            sync.Once
	err             error
	provider        func(string) uintptr
	releaseProvider func(uintptr)
	document        func(uintptr) uintptr
	releaseDocument func(uintptr)
	count           func(uintptr) int
	unlocked        func(uintptr) bool
	page            func(uintptr, int) uintptr
	box             func(uintptr, int32) cgRect
	rotation        func(uintptr) int32
	transform       func(uintptr, int32, cgRect, int32, bool) cgTransform
	colorSpace      func() uintptr
	releaseSpace    func(uintptr)
	bitmap          func(unsafe.Pointer, uintptr, uintptr, uintptr, uintptr, uintptr, uint32) uintptr
	releaseContext  func(uintptr)
	concat          func(uintptr, cgTransform)
	draw            func(uintptr, uintptr)
}

func initPDF() {
	pdfAPI.once.Do(func() {
		h, err := purego.Dlopen("/System/Library/Frameworks/CoreGraphics.framework/CoreGraphics", purego.RTLD_NOW|purego.RTLD_LOCAL)
		if err != nil {
			pdfAPI.err = err
			return
		}
		for _, item := range []struct {
			fn   any
			name string
		}{
			{&pdfAPI.provider, "CGDataProviderCreateWithFilename"}, {&pdfAPI.releaseProvider, "CGDataProviderRelease"},
			{&pdfAPI.document, "CGPDFDocumentCreateWithProvider"}, {&pdfAPI.releaseDocument, "CGPDFDocumentRelease"},
			{&pdfAPI.count, "CGPDFDocumentGetNumberOfPages"}, {&pdfAPI.unlocked, "CGPDFDocumentIsUnlocked"},
			{&pdfAPI.page, "CGPDFDocumentGetPage"}, {&pdfAPI.box, "CGPDFPageGetBoxRect"}, {&pdfAPI.rotation, "CGPDFPageGetRotationAngle"},
			{&pdfAPI.transform, "CGPDFPageGetDrawingTransform"}, {&pdfAPI.colorSpace, "CGColorSpaceCreateDeviceRGB"}, {&pdfAPI.releaseSpace, "CGColorSpaceRelease"},
			{&pdfAPI.bitmap, "CGBitmapContextCreate"}, {&pdfAPI.releaseContext, "CGContextRelease"}, {&pdfAPI.concat, "CGContextConcatCTM"}, {&pdfAPI.draw, "CGContextDrawPDFPage"},
		} {
			purego.RegisterLibFunc(item.fn, h, item.name)
		}
	})
}

// Render only the requested page; bound memory regardless of a PDF's page dimensions.
func renderPDF(path string, index int) (*image.RGBA, int, error) {
	initPDF()
	if pdfAPI.err != nil {
		return nil, 0, pdfAPI.err
	}
	provider := pdfAPI.provider(path)
	if provider == 0 {
		return nil, 0, errors.New("无法读取 PDF")
	}
	defer pdfAPI.releaseProvider(provider)
	doc := pdfAPI.document(provider)
	if doc == 0 {
		return nil, 0, errors.New("PDF 文件损坏或格式不支持")
	}
	defer pdfAPI.releaseDocument(doc)
	if !pdfAPI.unlocked(doc) {
		return nil, 0, errors.New("暂不支持需要密码的 PDF，请先解锁文件")
	}
	count := pdfAPI.count(doc)
	if index < 1 || index > count {
		return nil, count, errors.New("PDF 页码超出范围")
	}
	page := pdfAPI.page(doc, index)
	if page == 0 {
		return nil, count, errors.New("无法读取 PDF 页面")
	}
	rect := pdfAPI.box(page, 1)
	w, h := rect.W, rect.H
	if pdfAPI.rotation(page)%180 != 0 {
		w, h = h, w
	}
	if w <= 0 || h <= 0 || math.IsNaN(w) || math.IsNaN(h) || math.IsInf(w, 0) || math.IsInf(h, 0) {
		return nil, count, errors.New("PDF 页面尺寸无效")
	}
	scale := math.Min(2, 1800/math.Max(w, h))
	width, height := max(1, int(w*scale)), max(1, int(h*scale))
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for i := range img.Pix {
		img.Pix[i] = 255
	}
	space := pdfAPI.colorSpace()
	if space == 0 {
		return nil, count, errors.New("无法创建 PDF 色彩空间")
	}
	defer pdfAPI.releaseSpace(space)
	ctx := pdfAPI.bitmap(unsafe.Pointer(&img.Pix[0]), uintptr(width), uintptr(height), 8, uintptr(img.Stride), space, 1)
	if ctx == 0 {
		return nil, count, errors.New("无法创建 PDF 渲染上下文")
	}
	defer pdfAPI.releaseContext(ctx)
	pdfAPI.concat(ctx, cgTransform{A: scale, D: scale})
	pdfAPI.concat(ctx, pdfAPI.transform(page, 1, cgRect{W: w, H: h}, 0, true))
	pdfAPI.draw(ctx, page)
	runtime.KeepAlive(img)
	return img, count, nil
}
