package main

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"path"
	"path/filepath"
	"strings"

	golet "github.com/HyperparameterSI/golet/backend"
	"github.com/HyperparameterSI/golet/backend/ui"
)

type slidePane interface {
	place(ui.Rect)
	hide()
	close()
}

type slideContent struct{ Text, Notes string }
type slidesView struct {
	slides       []slideContent
	page         int
	notes        bool
	textMode     bool
	window       *golet.Window
	previewError string
	pane         slidePane
}
type relationship struct {
	ID     string `xml:"Id,attr"`
	Target string `xml:"Target,attr"`
	Type   string `xml:"Type,attr"`
	Mode   string `xml:"TargetMode,attr"`
}
type relationships struct {
	Items []relationship `xml:"Relationship"`
}

func zipPart(files map[string]*zip.File, name string) ([]byte, error) {
	f := files[name]
	if f == nil {
		return nil, fmt.Errorf("缺少文档部件：%s", name)
	}
	if f.UncompressedSize64 > 16<<20 {
		return nil, errors.New("文档部件超过 16 MiB")
	}
	r, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer r.Close()
	b, err := io.ReadAll(io.LimitReader(r, (16<<20)+1))
	if len(b) > 16<<20 {
		return nil, errors.New("文档部件过大")
	}
	return b, err
}
func partTarget(base, target string) string {
	if strings.HasPrefix(target, "/") {
		return strings.TrimPrefix(path.Clean(target), "/")
	}
	return path.Clean(path.Join(path.Dir(base), target))
}
func relationsFor(files map[string]*zip.File, part string) (relationships, error) {
	data, err := zipPart(files, path.Join(path.Dir(part), "_rels", path.Base(part)+".rels"))
	if err != nil {
		return relationships{}, err
	}
	var rels relationships
	err = xml.Unmarshal(data, &rels)
	return rels, err
}
func slideText(data []byte) (string, error) {
	dec := xml.NewDecoder(bytes.NewReader(data))
	var b strings.Builder
	for {
		token, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", err
		}
		switch t := token.(type) {
		case xml.StartElement:
			if t.Name.Local == "t" {
				var text string
				if err = dec.DecodeElement(&text, &t); err != nil {
					return "", err
				}
				b.WriteString(text)
			}
			if t.Name.Local == "br" {
				b.WriteByte('\n')
			}
		case xml.EndElement:
			if t.Name.Local == "p" {
				b.WriteByte('\n')
			}
		}
	}
	return strings.TrimSpace(b.String()), nil
}
func loadSlides(file string) (*document, error) {
	data, err := readOfficeFile(file)
	if err != nil {
		return nil, err
	}
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, err
	}
	files := map[string]*zip.File{}
	var total uint64
	for _, f := range reader.File {
		total += f.UncompressedSize64
		if total > 256<<20 || len(files) >= 20000 {
			return nil, errors.New("演示文稿解压体积过大")
		}
		files[f.Name] = f
	}
	main := "ppt/presentation.xml"
	b, err := zipPart(files, main)
	if err != nil {
		return nil, err
	}
	var presentation struct {
		Slides []struct {
			ID string `xml:"http://schemas.openxmlformats.org/officeDocument/2006/relationships id,attr"`
		} `xml:"sldIdLst>sldId"`
	}
	if err = xml.Unmarshal(b, &presentation); err != nil {
		return nil, err
	}
	rels, err := relationsFor(files, main)
	if err != nil {
		return nil, err
	}
	ids := map[string]string{}
	for _, r := range rels.Items {
		if r.Mode != "External" && strings.HasSuffix(r.Type, "/slide") {
			ids[r.ID] = partTarget(main, r.Target)
		}
	}
	view := &slidesView{}
	if len(presentation.Slides) > 2000 {
		return nil, errors.New("演示文稿超过 2000 页")
	}
	for _, item := range presentation.Slides {
		part := ids[item.ID]
		if part == "" {
			return nil, errors.New("无法解析幻灯片顺序")
		}
		b, err = zipPart(files, part)
		if err != nil {
			return nil, err
		}
		text, e := slideText(b)
		if e != nil {
			return nil, e
		}
		slide := slideContent{Text: text}
		relPath := path.Join(path.Dir(part), "_rels", path.Base(part)+".rels")
		if files[relPath] != nil {
			r, e := relationsFor(files, part)
			if e != nil {
				return nil, e
			}
			for _, rel := range r.Items {
				if rel.Mode != "External" && strings.HasSuffix(rel.Type, "/notesSlide") {
					nb, e := zipPart(files, partTarget(part, rel.Target))
					if e != nil {
						return nil, e
					}
					slide.Notes, e = slideText(nb)
					if e != nil {
						return nil, e
					}
				}
			}
		}
		view.slides = append(view.slides, slide)
	}
	if len(view.slides) == 0 {
		return nil, errors.New("演示文稿中没有幻灯片")
	}
	d := newDocument(filepath.Base(file), "")
	d.path = file
	d.kind = presentationFile
	d.slides = view
	return d, nil
}
func (a *app) slidesContent(c *ui.Context, d *document) {
	v := d.slides
	if !v.textMode {
		ui.Row(c).FillWidth().Height(36).Padding(0, 12).Gap(6).AlignItems(ui.Center).Children(func() {
			muted(c, "原始版式")
			ui.Box(c).Grow(1)
			iconButton(c, "preview-slides", "slides", "播放预览", func() { a.previewSlides(d) })
			iconButton(c, "slide-text", "file", "文字与备注", func() {
				v.textMode = true
				if v.pane != nil {
					v.pane.hide()
				}
			})
			iconButton(c, "external-slides", "external", "使用默认应用打开", func() { a.report(golet.Shell.OpenPath(d.path)) })
		})
		a.embeddedSlides(c, d)
		return
	}

	ui.Row(c).FillWidth().Height(42).Padding(0, 12).Gap(6).AlignItems(ui.Center).Children(func() {
		iconButton(c, "previous-slide", "chevron-left", "上一页", func() { v.page-- }).Disabled(v.page <= 0)
		muted(c, fmt.Sprintf("%d / %d", v.page+1, len(v.slides)))
		iconButton(c, "next-slide", "chevron-right", "下一页", func() { v.page++ }).Disabled(v.page >= len(v.slides)-1)
		ui.Box(c).Grow(1)
		muted(c, "文字视图")
		iconButton(c, "original-preview", "slides", "原版预览", func() { v.textMode = false })
		iconButton(c, "slide-notes", "file", "显示或隐藏备注", func() { v.notes = !v.notes })
		iconButton(c, "copy-slide", "files", "复制本页文字", func() { c.WriteClipboard(v.slides[v.page].Text) })
	})
	ui.Row(c).Grow(1).FillWidth().MinHeight(0).Children(func() {
		ui.Scroll(c.Key("slide-list")).Width(140).FillHeight().Background(c.Theme().Sidebar).Padding(8).Children(func() {
			ui.Column(c).FillWidth().Gap(4).Children(func() {
				for i, slide := range v.slides {
					index := i
					title := strings.SplitN(slide.Text, "\n", 2)[0]
					label := fmt.Sprintf("%d  %s", i+1, title)
					action(c, fmt.Sprint(i), label, func() { v.page = index }).FillWidth().Justify(ui.Start).MaxLines(1).Tooltip(title)
				}
			})
		})
		ui.Scroll(c.Key(fmt.Sprintf("slide-content-%d", v.page))).Grow(1).FillHeight().Padding(32).Children(func() {
			ui.Column(c).FillWidth().Gap(20).Children(func() {
				text := v.slides[v.page].Text
				if text == "" {
					muted(c, "此页没有可提取的文字（图片、图表等内容暂不显示）")
				} else {
					lines := strings.SplitN(text, "\n", 2)
					ui.Text(c, lines[0]).FontSize(26).Bold().Selectable().FillWidth()
					if len(lines) > 1 {
						ui.Text(c, lines[1]).FontSize(16).Selectable().FillWidth()
					}
				}
				if v.notes {
					muted(c, "备注")
					notes := v.slides[v.page].Notes
					if notes == "" {
						notes = "无备注"
					}
					ui.Text(c, notes).Selectable().FillWidth()
				}
			})
		})
	})
}
