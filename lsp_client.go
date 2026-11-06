package main

// Native LSP transport. UI objects never cross the reader goroutine boundary.
import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf16"
)

type lspPosition struct {
	Line      int `json:"line"`
	Character int `json:"character"`
}
type lspRange struct {
	Start lspPosition `json:"start"`
	End   lspPosition `json:"end"`
}

func lspURI(path string) string {
	return (&url.URL{Scheme: "file", Path: filepath.ToSlash(path)}).String()
}
func lspPath(uri string) string {
	u, e := url.Parse(uri)
	if e != nil || u.Scheme != "file" || (u.Host != "" && u.Host != "localhost") {
		return ""
	}
	return filepath.FromSlash(u.Path)
}
func lspPos(text string, offset int) lspPosition {
	p := lspPosition{}
	if offset > len(text) {
		offset = len(text)
	}
	if offset < 0 {
		offset = 0
	}
	for _, r := range text[:offset] {
		if r == '\n' {
			p.Line++
			p.Character = 0
		} else {
			p.Character += utf16.RuneLen(r)
		}
	}
	return p
}
func lspOffset(text string, p lspPosition) int {
	line, col := 0, 0
	for i, r := range text {
		if line == p.Line && col >= p.Character {
			return i
		}
		if r == '\n' {
			if line == p.Line {
				return i
			}
			line++
			col = 0
		} else {
			col += utf16.RuneLen(r)
		}
	}
	return len(text)
}

type lspPacket struct {
	ID     json.RawMessage `json:"id,omitempty"`
	Method string          `json:"method,omitempty"`
	Params json.RawMessage `json:"params,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}
type lspRPC struct {
	in      io.WriteCloser
	mu      sync.Mutex
	next    int
	pending map[int]chan lspPacket
	done    chan struct{}
	notify  func(string, json.RawMessage)
}

func readLSP(r *bufio.Reader) (lspPacket, error) {
	n := -1
	for {
		line, e := r.ReadString('\n')
		if e != nil {
			return lspPacket{}, e
		}
		line = strings.TrimSpace(line)
		if line == "" {
			break
		}
		key, value, ok := strings.Cut(line, ":")
		if ok && strings.EqualFold(key, "Content-Length") {
			n, _ = strconv.Atoi(strings.TrimSpace(value))
		}
	}
	if n < 0 || n > 32<<20 {
		return lspPacket{}, fmt.Errorf("invalid LSP frame size")
	}
	b := make([]byte, n)
	if _, e := io.ReadFull(r, b); e != nil {
		return lspPacket{}, e
	}
	var p lspPacket
	e := json.Unmarshal(b, &p)
	return p, e
}
func (r *lspRPC) send(v any) error {
	b, e := json.Marshal(v)
	if e != nil {
		return e
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	_, e = fmt.Fprintf(r.in, "Content-Length: %d\r\n\r\n%s", len(b), b)
	return e
}
func (r *lspRPC) notification(method string, params any) error {
	return r.send(map[string]any{"jsonrpc": "2.0", "method": method, "params": params})
}
func (r *lspRPC) call(ctx context.Context, method string, params any, out any) error {
	r.mu.Lock()
	r.next++
	id := r.next
	ch := make(chan lspPacket, 1)
	r.pending[id] = ch
	r.mu.Unlock()
	defer func() { r.mu.Lock(); delete(r.pending, id); r.mu.Unlock() }()
	if e := r.send(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params}); e != nil {
		return e
	}
	select {
	case p := <-ch:
		if p.Error != nil {
			return fmt.Errorf("%s", p.Error.Message)
		}
		if out != nil {
			return json.Unmarshal(p.Result, out)
		}
		return nil
	case <-ctx.Done():
		_ = r.notification("$/cancelRequest", map[string]any{"id": id})
		return ctx.Err()
	case <-r.done:
		return fmt.Errorf("language server disconnected")
	}
}
func (r *lspRPC) read(out io.Reader) {
	defer close(r.done)
	reader := bufio.NewReader(out)
	for {
		p, e := readLSP(reader)
		if e != nil {
			return
		}
		if p.Method != "" {
			if len(p.ID) > 0 {
				// Servers cannot apply workspace edits or run commands implicitly.
				var result any
				if p.Method == "workspace/configuration" {
					var v struct{ Items []json.RawMessage }
					_ = json.Unmarshal(p.Params, &v)
					result = make([]any, len(v.Items))
				}
				if p.Method == "workspace/applyEdit" {
					result = map[string]any{"applied": false, "failureReason": "Use the editor to apply changes"}
				}
				_ = r.send(map[string]any{"jsonrpc": "2.0", "id": p.ID, "result": result})
			} else if r.notify != nil {
				r.notify(p.Method, p.Params)
			}
			continue
		}
		var id int
		if json.Unmarshal(p.ID, &id) == nil {
			r.mu.Lock()
			ch := r.pending[id]
			r.mu.Unlock()
			if ch != nil {
				select {
				case ch <- p:
				default:
				}
			}
		}
	}
}
func lspRuntime() (string, string) {
	exe, _ := os.Executable()
	dir := filepath.Dir(exe)
	if filepath.Base(dir) == "MacOS" {
		res := filepath.Join(dir, "../Resources")
		return filepath.Join(res, "lsp"), filepath.Join(res, "pi/bin/node")
	}
	cwd, _ := os.Getwd()
	return filepath.Join(cwd, "bin/lsp-runtime"), filepath.Join(cwd, "bin/pi-runtime/bin/node")
}
func lspLanguage(path string) (server, language string) {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".go":
		return "go", "go"
	case ".js", ".mjs", ".cjs":
		return "typescript", "javascript"
	case ".jsx":
		return "typescript", "javascriptreact"
	case ".ts", ".mts", ".cts":
		return "typescript", "typescript"
	case ".tsx":
		return "typescript", "typescriptreact"
	case ".html", ".htm":
		return "html", "html"
	case ".css":
		return "css", "css"
	case ".scss":
		return "css", "scss"
	case ".less":
		return "css", "less"
	case ".json":
		return "json", "json"
	case ".jsonc":
		return "json", "jsonc"
	}
	return "", ""
}
func startLSP(ctx context.Context, kind, root string, notify func(string, json.RawMessage)) (*lspRPC, error) {
	runtime, node := lspRuntime()
	var cmd *exec.Cmd
	if kind == "go" {
		cmd = exec.CommandContext(ctx, filepath.Join(runtime, "bin/gopls"), "serve")
	} else {
		entry := filepath.Join(runtime, "node_modules/vscode-langservers-extracted/bin/vscode-"+kind+"-language-server")
		if kind == "typescript" {
			entry = filepath.Join(runtime, "node_modules/typescript-language-server/lib/cli.mjs")
		}
		cmd = exec.CommandContext(ctx, node, entry, "--stdio")
	}
	configureAIProcess(cmd)
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "PATH="+filepath.Dir(node)+":/usr/local/go/bin:/opt/homebrew/bin:/usr/local/bin:"+os.Getenv("PATH"))
	cmd.WaitDelay = time.Second
	in, e := cmd.StdinPipe()
	if e != nil {
		return nil, e
	}
	out, e := cmd.StdoutPipe()
	if e != nil {
		return nil, e
	}
	if e = cmd.Start(); e != nil {
		return nil, e
	}
	rpc := &lspRPC{in: in, pending: map[int]chan lspPacket{}, done: make(chan struct{}), notify: notify}
	go func() { rpc.read(out); _ = cmd.Wait() }()
	initCtx, cancel := context.WithTimeout(ctx, 40*time.Second)
	defer cancel()
	options := map[string]any{}
	if kind == "typescript" {
		options["tsserver"] = map[string]any{"path": filepath.Join(runtime, "node_modules/typescript/lib/tsserver.js")}
	}
	if kind == "go" {
		options = map[string]any{"completeUnimported": false, "usePlaceholders": false, "staticcheck": true}
	}
	params := map[string]any{"processId": os.Getpid(), "rootUri": lspURI(root), "workspaceFolders": []any{map[string]any{"uri": lspURI(root), "name": filepath.Base(root)}}, "clientInfo": map[string]any{"name": "Omnicode"}, "initializationOptions": options, "capabilities": map[string]any{"general": map[string]any{"positionEncodings": []string{"utf-16"}}, "textDocument": map[string]any{"definition": map[string]any{"linkSupport": true}, "completion": map[string]any{"completionItem": map[string]any{"snippetSupport": false, "documentationFormat": []string{"plaintext"}}}, "hover": map[string]any{"contentFormat": []string{"plaintext"}}, "publishDiagnostics": map[string]any{"versionSupport": true}}}}
	var result struct {
		Capabilities struct {
			PositionEncoding string `json:"positionEncoding"`
		} `json:"capabilities"`
	}
	if e = rpc.call(initCtx, "initialize", params, &result); e != nil {
		return nil, e
	}
	if result.Capabilities.PositionEncoding != "" && result.Capabilities.PositionEncoding != "utf-16" {
		return nil, fmt.Errorf("unsupported LSP encoding")
	}
	e = rpc.notification("initialized", map[string]any{})
	return rpc, e
}
