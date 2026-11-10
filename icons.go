package main

import (
	"time"

	"github.com/HyperparameterSI/golet/backend/ui"
)

var icons = map[string]*ui.SVG{}

func init() {
	paths := map[string]string{
		"database":      `<ellipse cx="12" cy="5" rx="8" ry="3"/><path d="M4 5v14c0 4 16 4 16 0V5M4 12c0 4 16 4 16 0"/>`,
		"clock":         `<circle cx="12" cy="12" r="9"/><path d="M12 6v6l4 2"/>`,
		"play":          `<path d="m7 4 13 8-13 8z"/>`,
		"maximize":      `<path d="M4 9V4h5m6 0h5v5m0 6v5h-5M9 20H4v-5"/>`,
		"more":          `<circle cx="5" cy="12" r="1"/><circle cx="12" cy="12" r="1"/><circle cx="19" cy="12" r="1"/>`,
		"tag":           `<path d="M3 3h8l10 10-8 8L3 11z"/><circle cx="7.5" cy="7.5" r="1"/>`,
		"remote":        `<circle cx="12" cy="12" r="9"/><path d="M3 12h18M12 3c5 5 5 13 0 18-5-5-5-13 0-18z"/>`,
		"trash":         `<path d="M3 6h18M9 6V3h6v3M5 6l1 15h12l1-15M10 10v7m4-7v7"/>`,
		"git":           `<circle cx="6" cy="5" r="3"/><circle cx="6" cy="19" r="3"/><circle cx="18" cy="6" r="3"/><path d="M6 8v8m12-7a10 10 0 0 1-9 10"/>`,
		"minus":         `<path d="M5 12h14"/>`,
		"arrow-down":    `<path d="M12 4v16m-6-6 6 6 6-6"/>`,
		"archive":       `<rect x="3" y="3" width="18" height="4" rx="1"/><path d="M5 7v13h14V7M10 11h4"/>`,
		"merge":         `<circle cx="6" cy="5" r="3"/><circle cx="6" cy="19" r="3"/><path d="M6 8v8m12-12v6c0 5-12 1-12 6"/>`,
		"history":       `<path d="M3 11a9 9 0 1 1 2.6 7M3 4v7h7m2-5v6l4 2"/>`,
		"chevron-down":  `<path d="m5 9 7 7 7-7"/>`,
		"sparkles":      `<path d="m12 3 2.4 6.6L21 12l-6.6 2.4L12 21l-2.4-6.6L3 12l6.6-2.4zM20 2v4m-2-2h4"/>`,
		"arrow-up":      `<path d="M12 20V4m-6 6 6-6 6 6"/>`,
		"stop":          `<rect x="6" y="6" width="12" height="12" rx="2"/>`,
		"slides":        `<rect x="3" y="3" width="18" height="13" rx="2"/><path d="M12 16v5m-4 0 4-3 4 3M7 7h10M7 11h6"/>`,
		"external":      `<path d="M14 3h7v7m0-7L10 14M10 3H5a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h14a2 2 0 0 0 2-2v-5"/>`,
		"appearance":    `<circle cx="12" cy="12" r="9"/><path d="M12 3a9 9 0 0 1 0 18z" fill="currentColor" stroke="none"/>`,
		"settings":      `<path d="M9 3h6l1 3 3 1 2 5-2 5-3 1-1 3H9l-1-3-3-1-2-5 2-5 3-1z"/><circle cx="12" cy="12" r="3"/>`,
		"check":         `<path d="m5 12 4 4L19 6"/>`,
		"chevron-left":  `<path d="m15 5-7 7 7 7"/>`,
		"chevron-right": `<path d="m9 5 7 7-7 7"/>`,
		"panel-right":   `<rect x="3" y="4" width="18" height="16" rx="3"/><path d="M15 4v16"/>`,
		"panel-bottom":  `<rect x="3" y="4" width="18" height="16" rx="3"/><path d="M3 14h18"/>`,
		"close":         `<path d="m6 6 12 12M18 6 6 18"/>`,
		"panel":         `<rect x="3" y="4" width="18" height="16" rx="3"/><path d="M9 4v16"/>`,
		"folder":        `<path d="M3 7V5a2 2 0 0 1 2-2h5l3 3h6a2 2 0 0 1 2 2v10a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V7z"/><path d="M3 9h18"/>`,
		"file":          `<path d="M14 3H6a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V9z"/><path d="M14 3v6h6"/>`,
		"files":         `<rect x="8" y="7" width="12" height="14" rx="2"/><path d="M16 7V5a2 2 0 0 0-2-2H5a2 2 0 0 0-2 2v11a2 2 0 0 0 2 2h3"/>`,
		"search":        `<circle cx="10.5" cy="10.5" r="7"/><path d="m16 16 5 5"/>`,
		"plus":          `<path d="M12 5v14M5 12h14"/>`,
		"refresh":       `<path d="M20 7v5h-5M4 17v-5h5"/><path d="M6 6a8 8 0 0 1 14 6M18 18a8 8 0 0 1-14-6"/>`,
		"wrap":          `<path d="M3 6h18M3 11h14a4 4 0 0 1 0 8h-4m3-3-3 3 3 3M3 16h5"/>`,
		"terminal":      `<path d="m5 6 6 6-6 6m8 0h6"/>`,
		"code":          `<path d="m8 7-5 5 5 5m8-10 5 5-5 5m-3-13-2 22"/>`,
	}
	for name, path := range paths {
		icons[name] = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round">` + path + `</svg>`))
	}
}
func icon(c *ui.Context, name string, size float32) ui.Element {
	return ui.Icon(c, icons[name]).Size(size, size).TextColor(c.Theme().TextMuted)
}
func iconButton(c *ui.Context, key, name, label string, fn func()) ui.Element {
	return toolButton(c, key, name, label, false, fn)
}

var chromeTransition = ui.ElementTransition{Colors: true, Duration: 110 * time.Millisecond}

func toolButton(c *ui.Context, key, name, label string, active bool, fn func()) ui.Element {
	t := c.Theme()
	b := ui.Button(c.Key(key), "").Size(32, 32).MinWidth(0).MinHeight(0).Padding(7).Border(0, ui.Transparent).Radius(6).Label(label).Tooltip(label).OnClick(fn)
	bg, fg := ui.Transparent, t.TextMuted
	if active {
		bg, fg = t.Surface.Mix(t.Accent, 0.14), t.Accent
	}
	if b.Pressed() {
		bg, fg = t.SurfacePressed, t.Text
	} else if b.Hovered() {
		bg, fg = t.SurfaceHover, t.Text
	}
	return b.Background(bg).Transition(chromeTransition).Children(func() { icon(c, name, 18).TextColor(fg) })
}
