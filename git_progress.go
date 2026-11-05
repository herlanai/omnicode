package main

import (
	"regexp"
	"strings"
	"time"
)

func gitCommandTimeout(args []string) time.Duration {
	if len(args) > 0 {
		switch args[0] {
		case "push", "pull", "fetch":
			return 10 * time.Minute
		case "ls-remote":
			return 2 * time.Minute
		}
	}
	return 30 * time.Second
}

var gitURLCredentials = regexp.MustCompile(`(https?://)[^\s/@]+(?::[^\s/@]*)?@`)

func gitProgressText(raw string) string {
	text := gitURLCredentials.ReplaceAllString(raw, "${1}[已隐藏]@")
	text = strings.Map(func(r rune) rune {
		if r == '\r' {
			return '\n'
		}
		if r < 32 && r != '\n' && r != '\t' {
			return -1
		}
		return r
	}, text)
	runes := []rune(strings.TrimSpace(text))
	if len(runes) > 4000 {
		runes = runes[len(runes)-4000:]
	}
	return string(runes)
}

type gitProgressWriter struct {
	buffer *gitLimitedBuffer
	emit   func(string)
	last   time.Time
	tail   string
}

func (w *gitProgressWriter) Write(p []byte) (int, error) {
	n, err := w.buffer.Write(p)
	w.tail += string(p)
	if len(w.tail) > 16000 {
		w.tail = w.tail[len(w.tail)-16000:]
	}
	if w.emit != nil && time.Since(w.last) >= 200*time.Millisecond {
		w.last = time.Now()
		w.emit(gitProgressText(w.tail))
	}
	return n, err
}
