package main

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
)

// A transparent code mark, tinted by the native menu bar.
func trayIcon() []byte {
	img := image.NewNRGBA(image.Rect(0, 0, 32, 32))
	line := func(x0, y0, x1, y1 int) {
		dx, dy := x1-x0, y1-y0
		n := max(absInt(dx), absInt(dy))
		for i := 0; i <= n; i++ {
			x, y := x0+dx*i/n, y0+dy*i/n
			for xx := x; xx < x+2; xx++ {
				for yy := y; yy < y+2; yy++ {
					img.Set(xx, yy, color.Black)
				}
			}
		}
	}
	line(10, 8, 3, 15)
	line(3, 15, 10, 22)
	line(21, 8, 28, 15)
	line(28, 15, 21, 22)
	line(18, 5, 13, 25)
	var b bytes.Buffer
	_ = png.Encode(&b, img)
	return b.Bytes()
}
func absInt(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
