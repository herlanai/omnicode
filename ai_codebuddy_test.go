package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCodeBuddyWorkspacePermissions(t *testing.T) {
	root := filepath.Join(t.TempDir(), "项目 [draft], v1")
	if err := os.Mkdir(root, 0755); err != nil {
		t.Fatal(err)
	}
	args, err := codeBuddyWorkspaceArgs(root, "literal task")
	if err != nil {
		t.Fatal(err)
	}
	var rules struct {
		Permissions struct {
			Allow []string `json:"allow"`
		} `json:"permissions"`
	}
	if args[0] != "--permission-mode" || args[1] != "dontAsk" {
		t.Fatal("missing noninteractive policy")
	}
	if err = json.Unmarshal([]byte(args[3]), &rules); err != nil {
		t.Fatal(err)
	}
	for _, r := range rules.Permissions.Allow {
		if r == "Write" || r == "Edit" || r == "Bash" || r == "*" {
			t.Fatal("unscoped permission", r)
		}
	}
	if !strings.Contains(rules.Permissions.Allow[1], "\\[draft\\]") || !strings.HasPrefix(rules.Permissions.Allow[1], "Write(//") {
		t.Fatal("absolute path not escaped", rules)
	}
	for _, a := range args {
		if a == "--dangerously-skip-permissions" || a == "bypassPermissions" {
			t.Fatal("global bypass enabled")
		}
	}
	if args[len(args)-1] != "literal task" {
		t.Fatal("prompt changed")
	}
	if _, err = codeBuddyWorkspaceArgs("", "task"); err == nil {
		t.Fatal("empty workspace accepted")
	}
}
func TestCodeBuddyLiveWorkspaceWrite(t *testing.T) {
	if os.Getenv("OMNICODE_CODEBUDDY_WRITE_TEST") == "" {
		t.Skip("opt-in live CLI test")
	}
	root := t.TempDir()
	p := defaultPreferences()
	p.Engine = "CodeBuddy CLI"
	command, env, err := aiCommand(p, "Use the Write tool to create omnicode-permission-check.txt in the current directory with exactly OMNICODE_WRITE_OK. Then read the file with Read. Do not use Bash, do not modify any other file. Reply briefly.", t.TempDir(), root)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	events := make(chan aiEvent, 64)
	done := make(chan error, 1)
	go func() { done <- runAI(ctx, command, env, root, events) }()
	var out strings.Builder
	for {
		select {
		case e := <-events:
			out.WriteString(e.text)
		case err := <-done:
			if err != nil {
				t.Fatalf("CLI failed: %v\n%s", err, out.String())
			}
			b, e := os.ReadFile(filepath.Join(root, "omnicode-permission-check.txt"))
			if e != nil || strings.TrimSpace(string(b)) != "OMNICODE_WRITE_OK" {
				t.Fatalf("write not performed: %v\n%s", e, out.String())
			}
			return
		}
	}
}
