package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/HyperparameterSI/golet/backend/ui/editor"
)

type backupVersion struct {
	id, created        int64
	root, path, action string
}

type backupBrowser struct {
	open           bool
	versions       []backupVersion
	selected       *backupVersion
	saved, current *aiFile
	err            string
	diff           *editor.DiffState
}

func (s *projectStore) initAIBackups() error {
	ctx := context.Background()
	for _, sql := range []string{
		`CREATE TABLE IF NOT EXISTS ai_file_versions(id INTEGER PRIMARY KEY, root TEXT NOT NULL, path TEXT NOT NULL, created INTEGER NOT NULL, action TEXT NOT NULL, mode INTEGER NOT NULL, content BLOB NOT NULL)`,
		`CREATE INDEX IF NOT EXISTS ai_versions_path ON ai_file_versions(path,id DESC)`,
		`CREATE INDEX IF NOT EXISTS ai_versions_root ON ai_file_versions(root,id DESC)`,
	} {
		if _, err := s.db.Execute(ctx, sql); err != nil {
			return err
		}
	}
	return nil
}

func (a *app) applyApprovedChange(root string, c aiChange) error {
	if c.Before == nil {
		return applyAIChange(root, c)
	}
	if a.projects == nil {
		return fmt.Errorf("本地备份数据库不可用，无法应用修改")
	}
	if err := checkAIChange(root, c); err != nil {
		return err
	}
	action := "修改"
	if c.After == nil {
		action = "删除"
	}
	ctx := context.Background()
	result, err := a.projects.db.Execute(ctx, `INSERT INTO ai_file_versions(root,path,created,action,mode,content) VALUES(?,?,?,?,?,?)`, root, filepath.Join(root, c.Path), time.Now().UnixNano(), action, int64(c.Before.Mode.Perm()), c.Before.Data)
	if err != nil {
		return fmt.Errorf("备份 %s 失败：%w", c.Path, err)
	}
	// The insert is durable before the filesystem mutation. Recheck for external edits.
	if err = applyAIChange(root, c); err != nil {
		if _, cleanup := a.projects.db.Execute(ctx, `DELETE FROM ai_file_versions WHERE id=?`, result.LastInsertID); cleanup != nil {
			return fmt.Errorf("%w（备份记录已保留：%v）", err, cleanup)
		}
		return err
	}
	return nil
}

func (a *app) showAIBackups(path string, project bool) {
	a.backups = backupBrowser{open: true}
	defer a.presentBackupWindow()
	if a.projects == nil {
		a.backups.err = "本地备份数据库不可用"
		return
	}
	query := `SELECT id,root,path,created,action FROM ai_file_versions WHERE path=? ORDER BY id DESC`
	if project {
		query = `SELECT id,root,path,created,action FROM ai_file_versions WHERE root=? ORDER BY id DESC`
	}
	rows, err := a.projects.db.Query(context.Background(), query, path)
	if err != nil {
		a.backups.err = err.Error()
		return
	}
	for _, r := range rows.Values {
		a.backups.versions = append(a.backups.versions, backupVersion{id: r[0].(int64), root: r[1].(string), path: r[2].(string), created: r[3].(int64), action: r[4].(string)})
	}
	if len(a.backups.versions) > 0 {
		if err := a.selectAIBackup(a.backups.versions[0]); err != nil {
			a.backups.err = err.Error()
		}
	}

}
func (a *app) selectAIBackup(v backupVersion) error {
	a.backups.selected = nil
	a.backups.diff = nil
	a.backups.saved = nil
	a.backups.current = nil
	rel, err := filepath.Rel(v.root, v.path)
	if err != nil || !filepath.IsLocal(rel) || rel == "." {
		return fmt.Errorf("备份路径无效")
	}
	root, err := filepath.EvalSymlinks(v.root)
	if err != nil || root != v.root {
		return fmt.Errorf("原项目目录已移动或重定向")
	}
	rows, err := a.projects.db.Query(context.Background(), `SELECT content,mode FROM ai_file_versions WHERE id=? AND path=? AND root=?`, v.id, v.path, v.root)
	if err != nil {
		return err
	}
	if len(rows.Values) != 1 {
		return fmt.Errorf("找不到此版本")
	}
	data, ok := rows.Values[0][0].([]byte)
	if !ok {
		return fmt.Errorf("无法读取备份内容")
	}
	var current *aiFile
	// Capture the on-disk version at preview time, then check it again at restore.
	fsroot, err := os.OpenRoot(root)
	if err != nil {
		return err
	}
	defer fsroot.Close()
	info, err := fsroot.Lstat(rel)
	if err == nil {
		if !info.Mode().IsRegular() {
			return fmt.Errorf("当前文件类型已改变")
		}
		currentData, err := fsroot.ReadFile(rel)
		if err != nil {
			return err
		}
		current = &aiFile{currentData, info.Mode().Perm()}
	} else if !os.IsNotExist(err) {
		return err
	}
	if err = checkAIChange(root, aiChange{Path: rel, Before: current}); err != nil {
		return err
	}

	a.backups.selected = &v
	a.backups.current = current
	a.backups.saved = &aiFile{data, os.FileMode(rows.Values[0][1].(int64))}
	a.backups.diff = backupDiff(v.path, current, a.backups.saved)
	a.backups.err = ""
	return nil
}
func (a *app) restoreAIBackup() error {
	b := &a.backups
	v := b.selected
	if v == nil || b.saved == nil {
		return fmt.Errorf("请先选择版本")
	}
	if a.git.mutating || a.busyAITab() != nil {
		return fmt.Errorf("请先完成当前 Git 或 AI 操作")
	}
	for _, d := range a.docs {
		if d.path == v.path && d.state.Buffer.Modified() {
			return fmt.Errorf("请先保存文件中的编辑，再恢复备份")
		}
	}
	root, err := filepath.EvalSymlinks(v.root)
	if err != nil || root != v.root {
		return fmt.Errorf("原项目目录已移动或重定向")
	}
	rel, err := filepath.Rel(root, v.path)
	if err != nil || !filepath.IsLocal(rel) || rel == "." {
		return fmt.Errorf("备份路径无效")
	}
	if err = applyAIChange(root, aiChange{Path: rel, Before: b.current, After: b.saved}); err != nil {
		return err
	}
	a.backups = backupBrowser{}
	a.refresh()
	for i, d := range a.docs {
		if d.path == v.path {
			fresh, err := loadDocument(v.path)
			if err != nil {
				return fmt.Errorf("文件已恢复，重新载入失败：%w", err)
			}
			a.releaseDocument(d)
			fresh.preview = d.preview
			a.docs[i] = fresh
			break
		}
	}
	a.notice = "已恢复 AI 修改前的版本 · " + filepath.Base(v.path)
	return nil
}
