package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Use absolute path rules: CodeBuddy interprets a single slash as project
// relative, and a double slash as filesystem absolute. Never grant bare Write,
// Edit or Bash, and never change the user's settings on disk.
func codeBuddyWorkspaceArgs(workspace, prompt string, readOnly bool) ([]string, error) {
	if workspace == "" {
		return nil, fmt.Errorf("请先打开项目文件夹")
	}
	root, err := filepath.Abs(workspace)
	if err != nil {
		return nil, err
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(root)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("项目路径不是文件夹")
	}
	escaped := strings.NewReplacer("\\", "\\\\", "*", "\\*", "?", "\\?", "[", "\\[", "]", "\\]", "{", "\\{", "}", "\\}", "(", "\\(", ")", "\\)", "!", "\\!", "+", "\\+", "@", "\\@").Replace(filepath.ToSlash(root))
	scope := "/" + strings.TrimRight(escaped, "/") + "/**"
	allow := []string{"Read(" + scope + ")"}
	if !readOnly {
		allow = append(allow, "Write("+scope+")", "Edit("+scope+")")
	}
	allow = append(allow, "Glob", "Grep", "Bash(pwd)", "Bash(ls)", "Bash(ls -la)", "Bash(git status)", "Bash(git status --short)", "Bash(git diff)", "Bash(git diff --stat)")
	settings, err := json.Marshal(map[string]any{"permissions": map[string]any{"allow": allow}})
	if err != nil {
		return nil, err
	}
	instruction := "Omnicode authorizes reading, creating and editing files within the opened workspace: " + root + ". Use Read/Glob/Grep to inspect and Write/Edit to change files. Shell commands not pre-authorized may be denied in this non-interactive session; do not request bypassPermissions or repeat denied commands. Report any actual denial accurately and use the authorized file tools where possible."
	if readOnly {
		instruction = "Answer using the selected code. You may inspect related files in " + root + " with Read, Glob or Grep. Do not modify the project."
	}
	return []string{"--permission-mode", "dontAsk", "--settings", string(settings), "--add-dir", root, "--append-system-prompt", instruction, "--print", "--verbose", "--output-format", "stream-json", "--include-partial-messages", prompt}, nil
}
