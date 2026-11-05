package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"
)

func resolveOCR() (string, error) {
	exe, e := os.Executable()
	if e != nil {
		return "", e
	}
	dir := filepath.Dir(exe)
	if filepath.Base(dir) == "MacOS" {
		return bundledOCR(filepath.Join(dir, "../Resources"))
	}
	cwd, e := os.Getwd()
	if e != nil {
		return "", e
	}
	return bundledOCR(filepath.Join(cwd, "resources"))
}
func bundledOCR(root string) (string, error) {
	name := "opencodereview"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	p := filepath.Join(root, "open-code-review", name)
	info, err := os.Stat(p)
	if err != nil || !info.Mode().IsRegular() || (runtime.GOOS != "windows" && info.Mode()&0111 == 0) {
		return "", errors.New("内置 Open CodeReview 文件缺失或不可执行，请重新安装最新 Omnicode 安装包")
	}
	return p, nil
}

func runOCR(ctx context.Context, executable, root string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, executable, args...)
	cmd.Dir = root
	configureAIProcess(cmd)
	cmd.Env = os.Environ()
	var stdout, stderr gitLimitedBuffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		// Do not expose configuration or repository text from arbitrary CLI diagnostics.
		return nil, fmt.Errorf("Open CodeReview 命令失败（%v），请检查 ocr 版本及项目规则配置", err)
	}
	if stdout.overflow || stdout.Len() > 200000 {
		return nil, errors.New("Open CodeReview 规则输出超过 200 KB，请缩小检查范围")
	}
	return stdout.Bytes(), nil
}

func enrichOCRSnapshot(ctx context.Context, root string, snapshot *GitReviewSnapshot) error {
	executable, err := resolveOCR()
	if err != nil {
		return err
	}
	return enrichOCRSnapshotWith(ctx, executable, root, snapshot)
}
func enrichOCRSnapshotWith(ctx context.Context, executable, root string, snapshot *GitReviewSnapshot) error {
	// OCR workspace preview compares against HEAD. Do not apply it to index-only
	// or worktree-only snapshots: changes can cancel each other across those scopes.
	if snapshot.Scope == "all" {
		data, err := runOCR(ctx, executable, root, "delegate", "preview", "--format", "json")
		if err != nil {
			return err
		}
		var preview struct {
			Version string `json:"schema_version"`
			Files   []struct {
				Path string `json:"path"`
			} `json:"reviewable_files"`
		}
		if err = json.Unmarshal(data, &preview); err != nil || preview.Version != "1" {
			return errors.New("Open CodeReview preview 格式不受支持，请更新 CLI")
		}
		allowed := map[string]bool{}
		for _, f := range preview.Files {
			allowed[f.Path] = true
		}
		files := []GitReviewFile{}
		for _, f := range snapshot.Files {
			if allowed[f.Path] {
				files = append(files, f)
			} else {
				snapshot.Skipped = append(snapshot.Skipped, f.Path+"（Open CodeReview 筛选排除）")
			}
		}
		snapshot.Files = files
	}
	if len(snapshot.Files) == 0 {
		return errors.New("Open CodeReview 筛选后没有可检查文件")
	}
	args := []string{"delegate", "rule", "--format", "json", "--"}
	for _, f := range snapshot.Files {
		args = append(args, f.Path)
	}
	data, err := runOCR(ctx, executable, root, args...)
	if err != nil {
		return err
	}
	var rules struct {
		Version string            `json:"schema_version"`
		Groups  []json.RawMessage `json:"groups"`
	}
	if err = json.Unmarshal(data, &rules); err != nil || rules.Version != "1" || len(rules.Groups) == 0 {
		return errors.New("Open CodeReview 未返回有效审查规则，请检查 CLI 版本及规则配置")
	}
	snapshot.Rules = string(data)
	return nil
}
