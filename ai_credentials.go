package main

import (
	"context"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

// Finder launches do not inherit the interactive shell's exported credentials.
// Fetch just the requested variable, never the complete environment.
func configuredKey(name string) string {
	if !envName.MatchString(name) {
		return ""
	}
	if value := os.Getenv(name); strings.TrimSpace(value) != "" {
		return value
	}
	shell := os.Getenv("SHELL")
	if shell == "" && runtime.GOOS == "darwin" {
		shell = "/bin/zsh"
	}
	if shell != "/bin/zsh" && shell != "/bin/bash" {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	const start, end = "\x1eOMNICODE_KEY\x1f", "\x1eOMNICODE_END\x1f"
	// name is validated as an environment identifier above. Secrets are never arguments.
	cmd := exec.CommandContext(ctx, shell, "-ilc", `printf '\036OMNICODE_KEY\037%s\036OMNICODE_END\037' "${`+name+`}"`)
	cmd.WaitDelay = 100 * time.Millisecond
	output, err := cmd.Output()
	if err != nil {
		return ""
	}
	text := string(output)
	i := strings.LastIndex(text, start)
	if i < 0 {
		return ""
	}
	text = text[i+len(start):]
	j := strings.Index(text, end)
	if j < 0 {
		return ""
	}
	return text[:j]
}
