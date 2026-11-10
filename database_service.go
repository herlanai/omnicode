package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/HyperparameterSI/golet/backend/plugins/sqlite"
	mysql "github.com/go-sql-driver/mysql"
)

type dbConnection struct {
	ID, Name, Engine, Host, Port, User, Database, Path, AccountID, DatabaseID string
	Password, Token                                                           string
	TLS                                                                       bool
}
type dbResult struct {
	Columns   []string
	Rows      [][]any
	Affected  int64
	Elapsed   time.Duration
	Truncated bool
}

const dbRowLimit = 1000

var dbCloudID = regexp.MustCompile(`^[a-zA-Z0-9-]+$`)

func validateDB(c dbConnection) error {
	if strings.TrimSpace(c.Name) == "" {
		return errors.New("请输入连接名称")
	}
	switch c.Engine {
	case "MySQL":
		port, e := strconv.Atoi(c.Port)
		if e != nil || port < 1 || port > 65535 || c.Host == "" || strings.ContainsAny(c.Host, "/\\@?# \r\n") || c.User == "" || c.Database == "" {
			return errors.New("请填写有效的主机、端口、用户名和数据库名称")
		}
	case "SQLite":
		info, e := os.Stat(c.Path)
		if e != nil {
			return fmt.Errorf("SQLite 文件：%w", e)
		}
		if !info.Mode().IsRegular() {
			return errors.New("请选择 SQLite 数据库文件")
		}
	case "Cloudflare D1":
		if !dbCloudID.MatchString(c.AccountID) || !dbCloudID.MatchString(c.DatabaseID) || strings.TrimSpace(c.Token) == "" {
			return errors.New("请填写 Account ID、Database ID 和 API Token")
		}
	default:
		return errors.New("不支持的数据库类型")
	}
	return nil
}
func dbMySQLConfig(c dbConnection) *mysql.Config {
	cfg := mysql.NewConfig()
	cfg.User = c.User
	cfg.Passwd = c.Password
	cfg.Net = "tcp"
	cfg.Addr = net.JoinHostPort(c.Host, c.Port)
	cfg.DBName = c.Database
	cfg.Timeout = 8 * time.Second
	cfg.ReadTimeout = 30 * time.Second
	cfg.WriteTimeout = 30 * time.Second
	cfg.Params = map[string]string{"charset": "utf8mb4"}
	if c.TLS {
		cfg.TLSConfig = "true"
	}
	return cfg
}
func dbReadSQL(words []string) bool {
	if len(words) == 0 {
		return false
	}
	switch words[0] {
	case "SELECT", "WITH", "SHOW", "DESC", "DESCRIBE", "EXPLAIN":
	default:
		return false
	}
	for i, w := range words {
		if i == 1 && words[0] == "SHOW" && w == "CREATE" {
			continue
		}
		switch w {
		case "INSERT", "UPDATE", "DELETE", "REPLACE", "ALTER", "DROP", "CREATE", "TRUNCATE", "RENAME", "GRANT", "REVOKE", "CALL", "SET", "INTO", "LOCK", "GET_LOCK", "RELEASE_LOCK", "LOAD_FILE", "ATTACH", "DETACH", "VACUUM", "PRAGMA":
			return false
		}
	}
	return true
}
func dbRun(ctx context.Context, c dbConnection, statement string, args []any, write bool) (out dbResult, err error) {
	start := time.Now()
	defer func() {
		out.Elapsed = time.Since(start)
		if err != nil {
			msg := err.Error()
			for _, secret := range []string{c.Password, c.Token} {
				if secret != "" {
					msg = strings.ReplaceAll(msg, secret, "[redacted]")
				}
			}
			err = errors.New(msg)
		}
	}()
	if err = validateDB(c); err != nil {
		return
	}
	statement, words, e := dbSQL(statement, c.Engine)
	if e != nil {
		return out, e
	}
	switch words[0] {
	case "BEGIN", "COMMIT", "ROLLBACK", "SAVEPOINT", "RELEASE", "START":
		return out, errors.New("每次独立执行一条语句，暂不支持跨次查询事务")
	}
	if !write && !dbReadSQL(words) {
		return out, errors.New("该语句可能修改数据，需要确认后执行")
	}
	if !write && c.Engine != "MySQL" && (words[0] == "SELECT" || words[0] == "WITH") {
		statement = "SELECT * FROM (" + strings.TrimSuffix(strings.TrimSpace(statement), ";") + "\n) AS omnicode_result LIMIT 1001"
	}
	switch c.Engine {
	case "SQLite":
		db, e := sqlite.Open(ctx, c.Path, sqlite.OpenOptions{ReadOnly: !write, BusyTimeout: 3 * time.Second})
		if e != nil {
			return out, e
		}
		defer db.Close()
		if write {
			r, e := db.Execute(ctx, statement, args...)
			out.Affected = r.Changes
			return out, e
		}
		rows, e := db.Query(ctx, statement, args...)
		if e != nil {
			return out, e
		}
		out.Columns = rows.Columns
		out.Rows = rows.Values
	case "Cloudflare D1":
		out, err = dbD1(ctx, &http.Client{Timeout: 30 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, c, statement, args)
	}
	if c.Engine == "MySQL" {
		connector, e := mysql.NewConnector(dbMySQLConfig(c))
		if e != nil {
			return out, e
		}
		db := sql.OpenDB(connector)
		defer db.Close()
		db.SetMaxOpenConns(1)
		if write {
			r, e := db.ExecContext(ctx, statement, args...)
			if e != nil {
				return out, e
			}
			out.Affected, _ = r.RowsAffected()
			return out, nil
		}
		tx, e := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
		if e != nil {
			return out, e
		}
		defer tx.Rollback()
		rows, e := tx.QueryContext(ctx, statement, args...)
		if e != nil {
			return out, e
		}
		defer rows.Close()
		out.Columns, e = rows.Columns()
		if e != nil {
			return out, e
		}
		for rows.Next() {
			values := make([]any, len(out.Columns))
			ptr := make([]any, len(values))
			for i := range ptr {
				ptr[i] = &values[i]
			}
			if e = rows.Scan(ptr...); e != nil {
				return out, e
			}
			for i, v := range values {
				if b, ok := v.([]byte); ok {
					values[i] = string(b)
				}
			}
			out.Rows = append(out.Rows, values)
			if len(out.Rows) > dbRowLimit {
				break
			}
		}
		if e = rows.Err(); e != nil {
			return out, e
		}
	}
	if len(out.Rows) > dbRowLimit {
		out.Rows = out.Rows[:dbRowLimit]
		out.Truncated = true
	}
	return
}
func dbD1(ctx context.Context, client *http.Client, c dbConnection, statement string, args []any) (dbResult, error) {
	if args == nil {
		args = []any{}
	}
	payload, e := json.Marshal(map[string]any{"sql": statement, "params": args})
	if e != nil {
		return dbResult{}, e
	}
	url := "https://api.cloudflare.com/client/v4/accounts/" + c.AccountID + "/d1/database/" + c.DatabaseID + "/raw"
	req, e := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if e != nil {
		return dbResult{}, e
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("Content-Type", "application/json")
	resp, e := client.Do(req)
	if e != nil {
		return dbResult{}, e
	}
	defer resp.Body.Close()
	data, e := io.ReadAll(io.LimitReader(resp.Body, 16<<20+1))
	if e != nil {
		return dbResult{}, e
	}
	if len(data) > 16<<20 {
		return dbResult{}, errors.New("D1 返回超过 16 MB，请缩小查询范围")
	}
	var wire struct {
		Success bool
		Errors  []struct{ Message string }
		Result  []struct {
			Success bool
			Error   string
			Results struct {
				Columns []string
				Rows    [][]any
			}
			Meta struct{ Changes int64 }
		}
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	if e = dec.Decode(&wire); e != nil {
		return dbResult{}, fmt.Errorf("D1 返回无效响应（HTTP %d）", resp.StatusCode)
	}
	if resp.StatusCode != 200 || !wire.Success {
		msg := fmt.Sprintf("D1 请求失败（HTTP %d）", resp.StatusCode)
		for _, e := range wire.Errors {
			msg += " · " + e.Message
		}
		return dbResult{}, errors.New(msg)
	}
	if len(wire.Result) != 1 {
		return dbResult{}, errors.New("D1 返回的结果数量异常")
	}
	if !wire.Result[0].Success {
		return dbResult{}, fmt.Errorf("D1 语句执行失败：%s", wire.Result[0].Error)
	}
	r := wire.Result[0]
	return dbResult{Columns: r.Results.Columns, Rows: r.Results.Rows, Affected: r.Meta.Changes}, nil
}
func dbQuote(name string) string { return "`" + strings.ReplaceAll(name, "`", "``") + "`" }
func dbTablesSQL(c dbConnection) string {
	if c.Engine == "MySQL" {
		return "SELECT TABLE_NAME, TABLE_TYPE FROM information_schema.TABLES WHERE TABLE_SCHEMA = DATABASE() ORDER BY TABLE_NAME"
	}
	return "SELECT name, type FROM sqlite_master WHERE type IN ('table','view') AND name NOT LIKE 'sqlite_%' ORDER BY name"
}
func dbStructureSQL(c dbConnection, table string) (string, []any) {
	if c.Engine == "MySQL" {
		return "SELECT COLUMN_NAME, COLUMN_TYPE, IS_NULLABLE, COLUMN_DEFAULT, COLUMN_KEY, EXTRA FROM information_schema.COLUMNS WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME=? ORDER BY ORDINAL_POSITION", []any{table}
	}
	return "SELECT name, type, \"notnull\", dflt_value, pk FROM pragma_table_info(?)", []any{table}
}

// Read the database's original DDL instead of reconstructing it from columns.
func dbCreateSQL(ctx context.Context, c dbConnection, table string) (string, error) {
	query := "SELECT sql FROM sqlite_master WHERE name=? AND type IN ('table','view')"
	args := []any{table}
	column := 0
	if c.Engine == "MySQL" {
		query = "SHOW CREATE TABLE " + dbQuote(table)
		args = nil
		column = 1
	}
	result, err := dbRun(ctx, c, query, args, false)
	if err != nil {
		return "", err
	}
	if len(result.Rows) == 0 || len(result.Rows[0]) <= column || result.Rows[0][column] == nil {
		return "", fmt.Errorf("未找到 %s 的创建 SQL", table)
	}
	if value, ok := result.Rows[0][column].([]byte); ok {
		return string(value), nil
	}
	return fmt.Sprint(result.Rows[0][column]), nil
}
func dbCell(v any) string {
	if v == nil {
		return "NULL"
	}
	if b, ok := v.([]byte); ok {
		return "0x" + hex.EncodeToString(b)
	}
	return fmt.Sprint(v)
}
