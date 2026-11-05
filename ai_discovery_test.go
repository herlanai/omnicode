package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDSHDesktopDiscovery(t *testing.T) {
	home, apps := t.TempDir(), t.TempDir()
	if _, err := findDSHDesktop(home, apps); err == nil {
		t.Fatal("missing launcher accepted")
	}
	launcher := filepath.Join(apps, "DeepSeek Harness.app/Contents/Resources/runtime/cli/bin/dsh")
	if err := os.MkdirAll(filepath.Dir(launcher), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(launcher, []byte("#!/bin/sh\nexit 0\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := findDSHDesktop(home, apps); err == nil {
		t.Fatal("non-executable accepted")
	}
	if err := os.Chmod(launcher, 0755); err != nil {
		t.Fatal(err)
	}
	got, err := findDSHDesktop(home, apps)
	if err != nil || got != launcher {
		t.Fatalf("got %q, %v", got, err)
	}
	userLauncher := filepath.Join(home, "Applications/DeepSeek Harness.app/Contents/Resources/runtime/cli/bin/dsh")
	if err := os.MkdirAll(filepath.Dir(userLauncher), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(userLauncher, []byte("#!/bin/sh\nexit 0\n"), 0755); err != nil {
		t.Fatal(err)
	}
	got, err = findDSHDesktop(home, apps)
	if err != nil || got != userLauncher {
		t.Fatalf("user install: got %q, %v", got, err)
	}
}

func TestDetectCLIsPreservesCustomPaths(t *testing.T) {
	p := defaultPreferences()
	p.PiPath, p.BuddyPath, p.DSHPath = "/custom/pi", "/custom/codebuddy", "/custom/dsh"
	if p.detectCLIs() {
		t.Fatal("custom paths changed")
	}
	if _, err := findCLI(p.DSHPath); err == nil {
		t.Fatal("missing explicit path silently replaced")
	}
}

func TestDetectCLIsFromPath(t *testing.T) {
	dir := t.TempDir()
	launcher := filepath.Join(dir, "dsh")
	if err := os.WriteFile(launcher, []byte("#!/bin/sh\nexit 0\n"), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	p := preferences{PiPath: "/custom/pi", BuddyPath: "/custom/buddy", DSHPath: "dsh"}
	if !p.detectCLIs() || p.DSHPath != launcher {
		t.Fatalf("not detected: %q", p.DSHPath)
	}
	if p.detectCLIs() {
		t.Fatal("resolved path changed again")
	}
}
