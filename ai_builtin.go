package main

import (
	"context"
	"crypto/rand"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	dshgo "github.com/HyperparameterSI/golet/backend/plugins/dsh-go"
)

const builtinEngine = "Omnicode"

func builtinConversation(p preferences, workspace string, readOnly bool) (*dshgo.Conversation, func(), error) {
	if p.DefaultModel < 0 || p.DefaultModel >= len(p.Models) {
		return nil, nil, fmt.Errorf("请在设置中选择默认模型")
	}
	m := p.Models[p.DefaultModel]
	if m.API != "" && m.API != "openai-completions" {
		return nil, nil, fmt.Errorf("内置助手需要兼容 Chat Completions 的模型接口")
	}
	key := strings.TrimSpace(m.APIKey)
	if key == "" {
		key = configuredKey(m.KeyEnv)
	}
	if key == "" {
		return nil, nil, fmt.Errorf("请在设置 → AI 引擎中填写模型 API Key")
	}
	base := m.BaseURL
	if base == "" && m.Provider != "deepseek" {
		return nil, nil, fmt.Errorf("请填写模型 API 地址")
	}
	adapter, err := dshgo.NewDeepSeek(dshgo.DeepSeekOptions{APIKey: key, BaseURL: base})
	if err != nil {
		return nil, nil, err
	}
	root, err := os.OpenRoot(workspace)
	if err != nil {
		return nil, nil, err
	}
	conversation, err := dshgo.New(dshgo.Options{
		Adapter: adapter, Model: m.Model,
		System: "你是 Omnicode 内置代码助手。使用工具检查工作区，遵循用户指定的文件范围。用中文回答。修改文件前先读取并理解其内容。工具中的路径必须相对于工作区。工具没有提供的能力不可假称已经执行。",
		Tools:  builtinTools(root, readOnly), MaxSteps: 32, MaxToolCalls: 64, MaxTokens: 8192,
		MaxTurnBytes: 2 << 20, MaxHistoryBytes: 4 << 20, Timeout: 10 * time.Minute,
	})
	if err != nil {
		root.Close()
		return nil, nil, err
	}
	return conversation, func() { conversation.Close(); root.Close() }, nil
}

func builtinTools(root *os.Root, readOnly bool) []dshgo.Tool {
	schema := func(fields ...string) map[string]any {
		properties := map[string]any{}
		for _, f := range fields {
			properties[f] = map[string]any{"type": "string"}
		}
		return map[string]any{"type": "object", "properties": properties, "required": fields}
	}
	pathArg := func(input map[string]any) (string, error) {
		path, _ := input["path"].(string)
		if path == "" || !filepath.IsLocal(path) {
			return "", fmt.Errorf("请提供工作区内的相对路径")
		}
		return path, nil
	}
	tools := []dshgo.Tool{
		{Name: "read_file", Description: "读取 UTF-8 文本文件，最多 256 KiB", Parameters: schema("path"), Execute: func(ctx context.Context, in map[string]any) (any, error) {
			path, err := pathArg(in)
			if err != nil {
				return nil, err
			}
			if err = ctx.Err(); err != nil {
				return nil, err
			}
			f, err := root.Open(path)
			if err != nil {
				return nil, err
			}
			defer f.Close()
			info, err := f.Stat()
			if err != nil {
				return nil, err
			}
			if !info.Mode().IsRegular() {
				return nil, fmt.Errorf("请选择普通文件")
			}
			data, err := io.ReadAll(io.LimitReader(f, 256<<10+1))
			if len(data) > 256<<10 {
				return nil, fmt.Errorf("文件过大，请缩小检查范围")
			}
			return string(data), err
		}},
		{Name: "list_files", Description: "递归列出目录中的文件，最多 1000 项；path 可为 .", Parameters: schema("path"), Execute: func(ctx context.Context, in map[string]any) (any, error) {
			path, err := pathArg(in)
			if err != nil {
				return nil, err
			}
			var files []string
			err = fs.WalkDir(root.FS(), path, func(p string, d fs.DirEntry, e error) error {
				if e != nil {
					return e
				}
				if ctx.Err() != nil {
					return ctx.Err()
				}
				if d.IsDir() {
					switch d.Name() {
					case ".git", "node_modules", "vendor", "bin", ".venv":
						return fs.SkipDir
					}
					return nil
				}
				files = append(files, p)
				if len(files) >= 1000 {
					return fs.SkipAll
				}
				return nil
			})
			return files, err
		}},
	}
	if !readOnly {
		tools = append(tools, dshgo.Tool{Name: "write_file", Description: "创建或替换文本文件，content 必须包含完整内容。先读取已有文件。", Parameters: schema("path", "content"), Execute: func(ctx context.Context, in map[string]any) (any, error) {
			path, err := pathArg(in)
			if err != nil {
				return nil, err
			}
			if err = ctx.Err(); err != nil {
				return nil, err
			}
			if err = root.MkdirAll(filepath.Dir(path), 0755); err != nil {
				return nil, err
			}
			mode := os.FileMode(0644)
			if info, e := root.Stat(path); e == nil {
				if !info.Mode().IsRegular() {
					return nil, fmt.Errorf("目标不是普通文件")
				}
				mode = info.Mode().Perm()
			}
			content, _ := in["content"].(string)
			// Root confines paths and symlinks to the assigned review workspace.
			temp := filepath.Join(filepath.Dir(path), ".omnicode-ai-"+rand.Text())
			f, err := root.OpenFile(temp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
			if err != nil {
				return nil, err
			}
			defer root.Remove(temp)
			_, err = f.WriteString(content)
			if err == nil {
				err = f.Chmod(mode)
			}
			closeErr := f.Close()
			if err != nil {
				return nil, err
			}
			if closeErr != nil {
				return nil, closeErr
			}
			if err = ctx.Err(); err != nil {
				return nil, err
			}
			err = root.Rename(temp, path)
			return "文件已写入工作区", err
		}})
	}
	return tools
}

func runBuiltin(ctx context.Context, c *dshgo.Conversation, prompt string, events chan<- aiEvent) error {
	emit := func(ev aiEvent) {
		select {
		case events <- ev:
		case <-ctx.Done():
		}
	}
	emit(aiEvent{progress: "正在连接模型…"})
	_, err := c.Run(ctx, prompt, func(e dshgo.Event) {
		switch e.Kind {
		case "text":
			emit(aiEvent{text: e.Text})
		case "tool_start", "tool_end":
			emit(aiEvent{progress: e.Tool, toolID: e.CallID, toolDone: e.Kind == "tool_end", toolFailed: e.IsError})
		}
	})
	return err
}
