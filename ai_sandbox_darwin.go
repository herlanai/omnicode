package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

func sandboxAICommand(command []string, r *aiReview, engine string) ([]string, []string, error) {
	// Fail closed: the agent may only write to its disposable copy and its CLI
	// state directories. Original workspace writes are explicitly denied.
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, nil, err
	}
	paths := []string{r.Temp}
	switch engine {
	case "DSH":
		paths = append(paths, dshHome())
	case "CodeBuddy CLI":
		paths = append(paths, filepath.Join(home, ".codebuddy"))
	}
	var b strings.Builder
	b.WriteString("(version 1)\n(allow default)\n(deny file-write*)\n")
	for _, p := range paths {
		p, err = filepath.Abs(p)
		if err != nil {
			return nil, nil, err
		}
		if resolved, e := filepath.EvalSymlinks(p); e == nil {
			p = resolved
		}
		// A project containing CLI state cannot safely use that state for writes.
		if inside(r.Root, p) || inside(p, r.Root) {
			return nil, nil, fmt.Errorf("项目与 AI 运行目录重叠，无法隔离审批")
		}
		b.WriteString("(allow file-write* (subpath " + strconv.Quote(p) + "))\n")
	}
	b.WriteString("(allow file-write* (literal \"/dev/null\") (literal \"/dev/tty\"))\n")
	b.WriteString("(deny file-write* (subpath " + strconv.Quote(r.Root) + "))\n")
	path := filepath.Join(r.Temp, "sandbox.sb")
	if err = os.WriteFile(path, []byte(b.String()), 0600); err != nil {
		return nil, nil, err
	}
	return append([]string{"/usr/bin/sandbox-exec", "-f", path}, command...), []string{"TMPDIR=" + r.Temp}, nil
}
