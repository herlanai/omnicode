package main

import (
	"context"
	"encoding/json"
	"os"
	"slices"
)

type workspaceSession struct {
	Root, Active   string
	Folders, Files []string
}

func (a *app) checkpointWorkspace() {
	if !a.trackWorkspace || a.projects == nil {
		return
	}
	s := workspaceSession{Root: a.root}
	if d := a.current(); d != nil {
		s.Active = d.path
	}
	for _, f := range a.folders {
		s.Folders = append(s.Folders, f.path)
	}
	for _, d := range a.docs {
		if d.path != "" {
			s.Files = append(s.Files, d.path)
		}
	}
	last := a.lastWorkspace
	if last.Root == s.Root && last.Active == s.Active && slices.Equal(last.Files, s.Files) && slices.Equal(last.Folders, s.Folders) {
		return
	}
	a.updateRecentDock()
	data, err := json.Marshal(s)
	if err == nil {
		_, err = a.projects.db.Execute(context.Background(), "INSERT INTO settings(key,value) VALUES('workspace',?) ON CONFLICT(key) DO UPDATE SET value=excluded.value", string(data))
	}
	if err != nil {
		a.report(err)
		return
	}
	a.lastWorkspace = s
}

func (a *app) restoreWorkspace() error {
	rows, err := a.projects.db.Query(context.Background(), "SELECT value FROM settings WHERE key='workspace'")
	if err != nil {
		return err
	}
	if len(rows.Values) == 0 {
		// Earlier versions recorded recent projects but not editor tabs.
		for _, recent := range a.recent {
			if f, err := readWorkspaceFolder(recent.Path); err == nil {
				a.replaceFolder(f)
				break
			}
		}
		return nil
	}
	var s workspaceSession
	if err := json.Unmarshal([]byte(rows.Values[0][0].(string)), &s); err != nil {
		return err
	}
	var activeFolder *workspaceFolder
	for _, path := range s.Folders {
		f, err := readWorkspaceFolder(path)
		if err != nil {
			continue
		}
		a.folders = append(a.folders, f)
		if path == s.Root {
			activeFolder = f
		}
	}
	if activeFolder == nil && len(a.folders) > 0 {
		activeFolder = a.folders[0]
	}
	if activeFolder != nil {
		a.activateFolder(activeFolder)
	}
	for _, path := range s.Files {
		info, err := os.Stat(path)
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		d, err := loadDocument(path)
		if err != nil {
			continue
		}
		a.docs = append(a.docs, d)
		if a.active < 0 || path == s.Active {
			a.active = len(a.docs) - 1
		}
	}
	a.focusEditor = a.current() != nil
	return nil
}
