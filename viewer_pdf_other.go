//go:build !darwin

package main

import (
	"errors"
	"image"
)

func renderPDF(path string, index int) (*image.RGBA, int, error) {
	return nil, 0, errors.New("PDF 原生阅读器目前支持 macOS")
}
