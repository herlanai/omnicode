package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func piRuntimeAt(resources string) (string, []string, error) {
	root := filepath.Join(resources, "pi")
	node := filepath.Join(root, "bin", "node")
	entry := filepath.Join(root, "node_modules", "@earendil-works", "pi-coding-agent", "dist", "cli.js")
	for _, path := range []string{node, entry} {
		info, err := os.Stat(path)
		if err != nil || !info.Mode().IsRegular() {
			return "", nil, fmt.Errorf("内置 Pi 运行时不完整，请重新安装 Omnicode")
		}
		if path == node && info.Mode().Perm()&0111 == 0 {
			return "", nil, fmt.Errorf("内置 Pi 运行时不可执行，请重新安装 Omnicode")
		}
	}
	return node, []string{entry}, nil
}
func piRuntime(developmentPath string) (string, []string, error) {
	executable, err := os.Executable()
	if err != nil {
		return "", nil, err
	}
	if resolved, e := filepath.EvalSymlinks(executable); e == nil {
		executable = resolved
	}
	dir := filepath.Dir(executable)
	if filepath.Base(dir) == "MacOS" && filepath.Base(filepath.Dir(dir)) == "Contents" {
		// Installed apps always use their own runtime, including after migration
		// from settings containing an old user-installed Pi path.
		return piRuntimeAt(filepath.Join(filepath.Dir(dir), "Resources"))
	}
	// Explicit paths are retained only for development and local test engines.
	if developmentPath != "" && developmentPath != "pi" && strings.ContainsAny(developmentPath, "/\\") {
		return developmentPath, nil, nil
	}
	root, err := os.Getwd()
	if err != nil {
		return "", nil, err
	}
	runtime := filepath.Join(root, "bin", "pi-runtime")
	node := filepath.Join(runtime, "bin", "node")
	entry := filepath.Join(runtime, "node_modules", "@earendil-works", "pi-coding-agent", "dist", "cli.js")
	if _, err = os.Stat(entry); err != nil {
		return "", nil, fmt.Errorf("请先运行 make pi-runtime 准备开发环境的内置引擎")
	}
	return node, []string{entry}, nil
}
