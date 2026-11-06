package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/HyperparameterSI/golet/backend/ui"
)

func TestApprovedFileVersions(t *testing.T) {
	a := newApp(t.TempDir())
	database := filepath.Join(t.TempDir(), "versions.sqlite")
	store, err := openProjectStore(database)
	if err != nil {
		t.Fatal(err)
	}
	a.projects = store
	path := filepath.Join(a.root, "source.go")
	if err := os.WriteFile(path, []byte("first\r\n"), 0750); err != nil {
		t.Fatal(err)
	}
	a.open(path)
	approve := func(value *aiFile) {
		t.Helper()
		file, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		p := a.ai.aiSession
		p.review = &aiReview{Approval: true, Root: a.root, Temp: t.TempDir(), Changes: []aiChange{{Path: "source.go", Before: &aiFile{file, info.Mode().Perm()}, After: value}}}
		a.approveAIChanges(map[string]bool{"source.go": true})
		if p.review != nil {
			t.Fatal(p.err)
		}
	}
	approve(&aiFile{[]byte("second\n"), 0644})
	approve(nil)
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("expected deletion")
	}
	if err = store.db.Close(); err != nil {
		t.Fatal(err)
	}
	a.projects, err = openProjectStore(database)
	if err != nil {
		t.Fatal(err)
	}
	defer a.projects.db.Close()
	a.showAIBackups(a.root, true)
	if len(a.backups.versions) != 2 || a.backups.versions[0].action != "删除" {
		t.Fatal(a.backups)
	}
	older := a.backups.versions[1]
	if err := a.selectAIBackup(older); err != nil {
		t.Fatal(err)
	}
	if err := a.restoreAIBackup(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(data, []byte("first\r\n")) {
		t.Fatal("wrong restored bytes", data, err)
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0750 {
		t.Fatal("mode not restored")
	}
	a.current().state.Buffer.Set("manual\n")
	if !a.writeAuto(a.current()) {
		t.Fatal(a.current().saveError)
	}
	v := ui.NewTester(a.view, 1100, 720)
	if err = v.Click("AI 修改前的版本"); err != nil {
		t.Fatal(err)
	}
	if !a.backups.open || len(a.backups.versions) != 2 {
		t.Fatal("manual save or restore created a version", a.backups)
	}
	if err = a.selectAIBackup(older); err != nil {
		t.Fatal(err)
	}
	put(t, path, "external edit")
	if err = a.restoreAIBackup(); err == nil {
		t.Fatal("restore overwrote concurrent edit")
	}
}

func TestApprovalFailsClosedOnBackupError(t *testing.T) {
	a := newApp(t.TempDir())
	s, err := openProjectStore(filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.db.Close()
	a.projects = s
	_, err = s.db.Execute(context.Background(), `CREATE TRIGGER fail_backup BEFORE INSERT ON ai_file_versions BEGIN SELECT RAISE(FAIL,'no space'); END`)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(a.root, "note")
	if err = os.WriteFile(path, []byte("before"), 0644); err != nil {
		t.Fatal(err)
	}
	a.ai.review = &aiReview{Approval: true, Root: a.root, Changes: []aiChange{{Path: "note", Before: &aiFile{[]byte("before"), 0644}, After: &aiFile{[]byte("after"), 0644}}}}
	a.approveAIChanges(map[string]bool{"note": true})
	b, _ := os.ReadFile(path)
	if string(b) != "before" || !a.ai.pending() || !strings.Contains(a.ai.err, "备份") {
		t.Fatal("lost original or pending approval", a.ai.err)
	}
}

func TestAIBackupRestoreThroughEditor(t *testing.T) {
	a := newApp(t.TempDir())
	store, err := openProjectStore(filepath.Join(t.TempDir(), "history.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	a.projects = store
	defer store.db.Close()
	path := filepath.Join(a.root, "empty.txt")
	if err = os.WriteFile(path, []byte{}, 0644); err != nil {
		t.Fatal(err)
	}
	if err = a.applyApprovedChange(a.root, aiChange{Path: "empty.txt", Before: &aiFile{[]byte{}, 0644}, After: &aiFile{[]byte("updated"), 0644}}); err != nil {
		t.Fatal(err)
	}
	a.open(path)
	v := ui.NewTester(a.view, 1100, 720)
	if err = v.Click("AI 修改前的版本"); err != nil {
		t.Fatal(err)
	}
	v = ui.NewTester(a.backupView, 1180, 760)
	record := a.backups.versions[0]
	label := time.Unix(0, record.created).Format("2006-01-02 15:04:05.000") + " · 修改前 · empty.txt"
	if err = v.Click(label); err != nil {
		t.Fatal(err)
	}
	if !v.HasText("备份版本（恢复后） · 空文件") || a.backups.diff.Old.String() != "updated" || a.backups.diff.New.String() != "" {
		t.Fatal("missing empty file preview", a.backups.err)
	}
	if err = v.Click("恢复并替换当前文件"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil || len(data) > 0 || a.current().state.Buffer.String() != "" {
		t.Fatal("restore failed", string(data), err)
	}
}

func TestBackupDiffFullFile(t *testing.T) {
	prefix := strings.Repeat("line\n", 1000)
	diff := backupDiff("notes.txt", &aiFile{Data: []byte(prefix + "now")}, &aiFile{Data: []byte(prefix + "then")})
	if diff.Old.String() != prefix+"now" || diff.New.String() != prefix+"then" || diff.Editable {
		t.Fatal("backup diff truncated, reversed, or editable")
	}
	binary := backupDiff("data.bin", &aiFile{Data: []byte{0, 1}}, &aiFile{Data: []byte{0, 2}})
	if binary.Old.String() == binary.New.String() {
		t.Fatal("same-sized binary files compared equal")
	}
}
