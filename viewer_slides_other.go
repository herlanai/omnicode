//go:build !darwin

package main

import "github.com/HyperparameterSI/golet/backend/ui"

func (a *app) previewSlides(d *document) {
	if d != nil && d.slides != nil {
		d.slides.previewError = "原版预览目前支持 macOS；可使用系统默认应用打开。"
	}
}

func (a *app) embeddedSlides(c *ui.Context, d *document) {
	ui.Text(c, "此平台暂不支持原版视图，请使用默认应用打开。").Padding(24)
}
