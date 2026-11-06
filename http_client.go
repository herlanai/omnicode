package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

const httpResponseLimit = 1 << 20

var httpVariable = regexp.MustCompile(`\{\{([a-zA-Z_][a-zA-Z0-9_.-]*)\}\}`)

type httpReply struct {
	label, status, headers, body string
	size                         int
	truncated                    bool
	elapsed                      time.Duration
	err                          error
}

// Leading variable definitions are shared by every request in this file.
// Delimiters start in column one so that indented body content is preserved.
func requestAt(ctx context.Context, source string, cursor int) (*http.Request, string, error) {
	return requestInRange(ctx, source, cursor, cursor)
}

// The selection end is exclusive, so selecting to the next heading still runs
// the preceding request. A selection spanning multiple requests is ambiguous.
func requestInRange(ctx context.Context, source string, cursor, selectionEnd int) (*http.Request, string, error) {
	preamble := 0
	vars := make(map[string]string)
	for _, line := range strings.Split(source, "\n") {
		length := len(line) + 1
		line = strings.TrimSpace(line)
		if line == "" || (strings.HasPrefix(line, "#") && !strings.HasPrefix(line, "###")) {
			preamble += length
			continue
		}
		if !strings.HasPrefix(line, "@") {
			break
		}
		key, value, ok := strings.Cut(line[1:], "=")
		if !ok {
			return nil, "", fmt.Errorf("变量格式应为 @name = value")
		}
		vars[strings.TrimSpace(key)] = strings.TrimSpace(value)
		preamble += length
	}
	cursor = max(cursor, preamble)
	start, end, offset := 0, len(source), 0
	label := ""
	for _, line := range strings.Split(source, "\n") {
		if line == "###" || strings.HasPrefix(line, "### ") {
			if offset > cursor {
				end = offset
				break
			}
			start = min(len(source), offset+len(line)+1)
			label = strings.TrimSpace(strings.TrimPrefix(line, "###"))
		}
		offset += len(line) + 1
	}
	if selectionEnd > end {
		return nil, label, fmt.Errorf("请只选中一个请求")
	}
	block := source[start:end]
	for len(block) > 0 {
		line, rest, _ := strings.Cut(block, "\n")
		trim := strings.TrimSpace(line)
		if trim != "" && !strings.HasPrefix(trim, "#") && !strings.HasPrefix(trim, "@") {
			break
		}
		block = rest
	}
	var missing string
	block = httpVariable.ReplaceAllStringFunc(block, func(token string) string {
		key := token[2 : len(token)-2]
		value, ok := vars[key]
		if !ok {
			missing = key
		}
		return value
	})
	if missing != "" {
		return nil, label, fmt.Errorf("请在文件开头定义 @%s", missing)
	}
	line, rest, _ := strings.Cut(block, "\n")
	fields := strings.Fields(line)
	if len(fields) != 2 {
		return nil, label, fmt.Errorf("请将光标放在请求中，首行格式为 GET http://localhost:8080/path")
	}
	headers := make(http.Header)
	for rest != "" {
		var h string
		h, rest, _ = strings.Cut(rest, "\n")
		if strings.TrimSpace(h) == "" {
			break
		}
		k, v, ok := strings.Cut(h, ":")
		if !ok || strings.TrimSpace(k) == "" {
			return nil, label, fmt.Errorf("请求头格式应为 Name: Value，请在正文前留一个空行")
		}
		headers.Add(strings.TrimSpace(k), strings.TrimSpace(v))
	}
	req, err := http.NewRequestWithContext(ctx, fields[0], fields[1], strings.NewReader(rest))
	if err != nil {
		return nil, label, err
	}
	if req.URL.Hostname() == "" || (req.URL.Scheme != "https" && req.URL.Scheme != "http") {
		return nil, label, fmt.Errorf("请输入完整的 HTTP 或 HTTPS 地址")
	}
	req.Header = headers
	if host := headers.Get("Host"); host != "" {
		req.Host = host
		req.Header.Del("Host")
	}
	if label == "" {
		label = line
	}
	return req, label, nil
}

func executeHTTP(req *http.Request, label string) httpReply {
	out := httpReply{label: label}
	start := time.Now()
	client := http.Client{Timeout: 30 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Do(req)
	if err != nil {
		out.err = err
		return out
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, httpResponseLimit+1))
	out.elapsed = time.Since(start)
	if err != nil {
		out.err = err
		return out
	}
	out.status = resp.Status
	var head bytes.Buffer
	_ = resp.Header.Write(&head)
	out.headers = head.String()
	if len(data) > httpResponseLimit {
		out.truncated = true
		data = data[:httpResponseLimit]
	}
	out.size = len(data)
	typ, _, _ := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if strings.HasPrefix(typ, "image/") || bytes.IndexByte(data, 0) >= 0 || (!utf8.Valid(data) && !out.truncated) {
		out.body = "二进制响应 · " + typ
	} else {
		out.body = strings.ToValidUTF8(string(data), "�")
		var pretty bytes.Buffer
		if !out.truncated && (typ == "application/json" || strings.HasSuffix(typ, "+json")) && json.Indent(&pretty, data, "", "  ") == nil {
			out.body = pretty.String()
		}
	}
	return out
}

// Quote each argument for copying into a POSIX shell. Copying does not execute it.
func curlCommand(req *http.Request) (string, error) {
	quote := func(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'" }
	args := []string{"curl", "-X", quote(req.Method)}
	headers := req.Header.Clone()
	if req.Host != req.URL.Host && req.Host != "" {
		headers.Set("Host", req.Host)
	}
	names := make([]string, 0, len(headers))
	for name := range headers {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		for _, value := range headers[name] {
			args = append(args, "-H", quote(name+": "+value))
		}
	}
	if req.ContentLength > 0 {
		body, err := req.GetBody()
		if err != nil {
			return "", err
		}
		defer body.Close()
		data, err := io.ReadAll(body)
		if err != nil {
			return "", err
		}
		args = append(args, "--data-raw", quote(string(data)))
	}
	args = append(args, "--", quote(req.URL.String()))
	return strings.Join(args, " "), nil
}
