package main

import (
	"errors"
	"strings"
)

// Single-statement scanner adapted from the local poprsi database tool.
func dbSQL(statement, engine string) (string, []string, error) {
	s := strings.TrimSpace(statement)
	if len(s) == 0 || len(s) > 100000 || strings.ContainsRune(s, 0) {
		return "", nil, errors.New("SQL 不能为空，且最多 100,000 字节")
	}
	words := []string{}
	ended := false
	endSQL := len(s)
	for i := 0; i < len(s); {
		c := s[i]
		if c == ' ' || c == '\t' || c == '\r' || c == '\n' {
			i++
			continue
		}
		if c == '#' || (c == '-' && i+2 < len(s) && s[i+1] == '-' && s[i+2] <= ' ') {
			for i < len(s) && s[i] != '\n' {
				i++
			}
			continue
		}
		if c == '/' && i+1 < len(s) && s[i+1] == '*' {
			if i+2 < len(s) && (s[i+2] == '!' || s[i+2] == '+' || (i+3 < len(s) && s[i+2] == 'M' && s[i+3] == '!')) {
				return "", nil, errors.New("暂不支持可执行注释或优化器提示")
			}
			end := strings.Index(s[i+2:], "*/")
			if end < 0 {
				return "", nil, errors.New("SQL 注释未闭合")
			}
			i += end + 4
			continue
		}
		if ended {
			return "", nil, errors.New("每次仅支持执行一条 SQL，请拆分多条语句")
		}
		if c == ';' {
			endSQL = i
			ended = true
			i++
			continue
		}
		if c == '\'' || c == '"' || c == '`' {
			q := c
			i++
			closed := false
			for i < len(s) {
				if s[i] == '\\' && engine == "MySQL" {
					i += 2
					continue
				}
				if s[i] == q {
					if i+1 < len(s) && s[i+1] == q {
						i += 2
						continue
					}
					i++
					closed = true
					break
				}
				i++
			}
			if !closed {
				return "", nil, errors.New("SQL 引号未闭合")
			}
			continue
		}
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || c == '_' {
			j := i + 1
			for j < len(s) && ((s[j] >= 'a' && s[j] <= 'z') || (s[j] >= 'A' && s[j] <= 'Z') || (s[j] >= '0' && s[j] <= '9') || s[j] == '_') {
				j++
			}
			words = append(words, strings.ToUpper(s[i:j]))
			i = j
			continue
		}
		i++
	}
	if len(words) == 0 {
		return "", nil, errors.New("请输入有效 SQL")
	}
	return strings.TrimSpace(s[:endSQL]), words, nil
}
