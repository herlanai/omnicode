package main

import (
	"bytes"
	"errors"
	"fmt"
	_ "golang.org/x/image/tiff"
	"image"
	"os"
	"path/filepath"
	"strings"

	"github.com/HyperparameterSI/golet/backend/ui"
)

type fileKind uint8

const (
	textFile fileKind = iota
	markdownFile
	imageFile
	pdfFile
	spreadsheetFile
	presentationFile
)

type mediaView struct {
	source        ui.ImageSource
	width, height int
	zoom          float32
	page, pages   int
	err           string
}

func kindFor(path string) fileKind {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".md", ".markdown", ".mdown":
		return markdownFile
	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".bmp", ".tif", ".tiff", ".svg":
		return imageFile
	case ".xlsx", ".xlsm", ".xltx", ".csv", ".tsv":
		return spreadsheetFile
	case ".pptx":
		return presentationFile
	case ".pdf":
		return pdfFile
	}
	return textFile
}
func (d *document) editable() bool { return d.kind == textFile || d.kind == markdownFile }
func loadMedia(path string, kind fileKind) (*document, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > 128<<20 {
		return nil, errors.New("仅支持 128 MiB 以内的普通图片/PDF 文件")
	}
	d := newDocument(filepath.Base(path), "")
	d.path = path
	d.kind = kind
	d.media = &mediaView{page: 1}
	if kind == pdfFile {
		img, pages, e := renderPDF(path, 1)
		if e != nil {
			return nil, e
		}
		d.media.pages = pages
		d.media.source = ui.NewBitmap(img)
		d.media.width = img.Bounds().Dx()
		d.media.height = img.Bounds().Dy()
		return d, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if strings.EqualFold(filepath.Ext(path), ".svg") {
		svg, e := ui.ParseSVG(data)
		if e != nil {
			return nil, e
		}
		w, h := svg.Size()
		if w <= 0 || h <= 0 || w > 32768 || h > 32768 {
			return nil, errors.New("SVG 尺寸无效")
		}
		d.media.source = svg
		d.media.width = int(w)
		d.media.height = int(h)
	} else {
		cfg, _, e := image.DecodeConfig(bytes.NewReader(data))
		if e != nil {
			return nil, e
		}
		if cfg.Width <= 0 || cfg.Height <= 0 || int64(cfg.Width)*int64(cfg.Height) > 40_000_000 {
			return nil, errors.New("图片超过 4000 万像素，无法预览")
		}
		bitmap, e := ui.DecodeBitmap(data)
		if e != nil {
			return nil, e
		}
		d.media.source = bitmap
		d.media.width, d.media.height = bitmap.Size()
	}
	return d, nil
}
func (a *app) mediaContent(c *ui.Context, d *document) {
	v := d.media
	ui.Row(c).FillWidth().Height(42).Padding(0, 18).Gap(8).AlignItems(ui.Center).Children(func() {
		if d.kind == pdfFile {
			action(c, "previous-page", "上一页", func() { a.pdfPage(d, v.page-1) }).Disabled(v.page <= 1)
			muted(c, fmt.Sprintf("%d / %d 页", v.page, v.pages))
			action(c, "next-page", "下一页", func() { a.pdfPage(d, v.page+1) }).Disabled(v.page >= v.pages)
		} else {
			muted(c, fmt.Sprintf("%d × %d", v.width, v.height))
			if strings.EqualFold(filepath.Ext(d.path), ".gif") {
				muted(c, "GIF · 首帧")
			}
		}
		ui.Box(c).Grow(1)
		action(c, "zoom-out", "−", func() {
			if v.zoom == 0 {
				v.zoom = 1
			}
			v.zoom = max(.1, v.zoom/1.25)
		})
		label := "适应窗口"
		if v.zoom != 0 {
			label = fmt.Sprintf("%.0f%%", v.zoom*100)
		}
		muted(c, label)
		action(c, "zoom-in", "+", func() {
			if v.zoom == 0 {
				v.zoom = 1
			}
			v.zoom = min(4, v.zoom*1.25)
		})
		action(c, "actual-size", "100%", func() { v.zoom = 1 })
		action(c, "fit-image", "适应窗口", func() { v.zoom = 0 })
	})
	if v.err != "" {
		ui.Text(c, v.err).TextColor(c.Theme().Danger)
	}
	if v.zoom == 0 {
		ui.Box(c).Grow(1).FillWidth().Padding(24).Background(a.theme.Surface).Children(func() { ui.Image(c.Key(d.path), v.source).Fill().Fit(ui.Contain).Label(d.name) })
	} else {
		ui.ScrollBoth(c.Key(fmt.Sprintf("media-%s-%d", d.path, v.page))).Grow(1).FillWidth().Background(a.theme.Surface).Children(func() {
			ui.Image(c, v.source).Size(float32(v.width)*v.zoom, float32(v.height)*v.zoom).Fit(ui.Contain).Label(d.name)
		})
	}
}
func (a *app) pdfPage(d *document, page int) {
	if page < 1 || page > d.media.pages {
		return
	}
	img, pages, err := renderPDF(d.path, page)
	if err != nil {
		d.media.err = err.Error()
		return
	}
	d.media.err = ""
	d.media.page = page
	d.media.pages = pages
	d.media.source = ui.NewBitmap(img)
	d.media.width = img.Bounds().Dx()
	d.media.height = img.Bounds().Dy()
}
