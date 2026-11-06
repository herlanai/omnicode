package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/HyperparameterSI/golet/backend/ui"
)

const scheduleTable = `CREATE TABLE IF NOT EXISTS scheduled_tasks (
 id INTEGER PRIMARY KEY AUTOINCREMENT, workspace TEXT NOT NULL, name TEXT NOT NULL,
 prompt TEXT NOT NULL, engine TEXT NOT NULL, cadence TEXT NOT NULL, clock TEXT NOT NULL,
 due INTEGER NOT NULL, last_run TEXT NOT NULL DEFAULT '', result TEXT NOT NULL DEFAULT '')`

type scheduledTask struct {
	workspace                                             string
	id, due                                               int64
	name, prompt, engine, cadence, clock, lastRun, result string
}
type schedulePanel struct {
	search        bool
	query, filter string
	runs          []aiHistoryEntry
	open          bool
	poll          time.Time
	tasks         []scheduledTask
	edit          *scheduledTask
	selected      int64
	err           string
}

var cadences = []string{"每小时", "每天", "工作日", "每周一", "每周二", "每周三", "每周四", "每周五", "每周六", "每周日"}

func (t scheduledTask) next(now time.Time) (time.Time, error) {
	at, err := time.Parse("15:04", t.clock)
	if err != nil {
		return time.Time{}, fmt.Errorf("请输入 24 小时制时间，例如 09:30")
	}
	y, m, d := now.Date()
	hour := at.Hour()
	if t.cadence == "每小时" {
		hour = now.Hour()
	}
	next := time.Date(y, m, d, hour, at.Minute(), 0, 0, now.Location())
	if t.cadence == "每小时" {
		if !next.After(now) {
			next = next.Add(time.Hour)
		}
		return next, nil
	}
	if !next.After(now) {
		next = next.AddDate(0, 0, 1)
	}
	switch t.cadence {
	case "每天":
	case "工作日":
		for next.Weekday() == time.Saturday || next.Weekday() == time.Sunday {
			next = next.AddDate(0, 0, 1)
		}
	default:
		weekday := slices.Index(cadences[3:], t.cadence)
		if weekday < 0 {
			return time.Time{}, fmt.Errorf("请选择执行频率")
		}
		for next.Weekday() != time.Weekday((weekday+1)%7) {
			next = next.AddDate(0, 0, 1)
		}
	}
	return next, nil
}

func (s *projectStore) initSchedules() error {
	_, err := s.db.Execute(context.Background(), scheduleTable)
	return err
}
func (a *app) loadSchedules() error {
	rows, err := a.projects.db.Query(context.Background(), `SELECT id,name,prompt,engine,cadence,clock,due,last_run,result,workspace FROM scheduled_tasks ORDER BY due=0,due,id`)
	if err != nil {
		return err
	}
	var tasks []scheduledTask
	for _, r := range rows.Values {
		tasks = append(tasks, scheduledTask{id: r[0].(int64), name: r[1].(string), prompt: r[2].(string), engine: r[3].(string), cadence: r[4].(string), clock: r[5].(string), due: r[6].(int64), lastRun: r[7].(string), result: r[8].(string), workspace: r[9].(string)})
	}
	a.schedules.tasks = tasks
	a.schedules.runs = nil
	if id := a.schedules.selected; id != 0 {
		rows, err := a.projects.db.Query(context.Background(), `SELECT id,updated_at,CASE WHEN COALESCE(json_extract(payload,'$.error'),'') != '' THEN '失败' WHEN json_extract(payload,'$.running') THEN '执行中或已中断' ELSE '已完成' END FROM ai_conversations WHERE id GLOB ? ORDER BY id DESC LIMIT 100`, fmt.Sprintf("schedule-%d-*", id))
		if err != nil {
			return err
		}
		for _, r := range rows.Values {
			a.schedules.runs = append(a.schedules.runs, aiHistoryEntry{ID: r[0].(string), Updated: r[1].(int64), Title: r[2].(string)})
		}
	}
	return nil
}
func (a *app) scheduleSQL(query string, args ...any) bool {
	_, err := a.projects.db.Execute(context.Background(), query, args...)
	if err != nil {
		a.schedules.err = err.Error()
		return false
	}
	a.schedules.err = ""
	a.schedules.poll = time.Time{}
	return true
}
func (a *app) saveSchedule(t scheduledTask) {
	t.name = strings.TrimSpace(t.name)
	t.prompt = strings.TrimSpace(t.prompt)
	if t.name == "" || t.prompt == "" {
		a.schedules.err = "请填写任务名称和执行指令"
		return
	}
	root, err := filepath.EvalSymlinks(t.workspace)
	if err != nil || !filepath.IsAbs(root) {
		a.schedules.err = "请选择可访问的项目文件夹"
		return
	}
	info, err := os.Stat(root)
	if err != nil || !info.IsDir() {
		a.schedules.err = "请选择项目文件夹"
		return
	}
	t.workspace = root
	next, err := t.next(time.Now())
	if err != nil {
		a.schedules.err = err.Error()
		return
	}
	due := next.Unix()
	if t.id == 0 {
		res, err := a.projects.db.Execute(context.Background(), `INSERT INTO scheduled_tasks(workspace,name,prompt,engine,cadence,clock,due) VALUES(?,?,?,?,?,?,?)`, t.workspace, t.name, t.prompt, t.engine, t.cadence, t.clock, due)
		if err != nil {
			a.schedules.err = err.Error()
			return
		}
		a.schedules.selected = res.LastInsertID
	} else {
		if t.due == 0 {
			due = 0
		}
		if !a.scheduleSQL(`UPDATE scheduled_tasks SET name=?,prompt=?,engine=?,cadence=?,clock=?,due=? WHERE id=? AND workspace=?`, t.name, t.prompt, t.engine, t.cadence, t.clock, due, t.id, t.workspace) {
			return
		}
	}
	a.schedules.edit = nil
	a.schedules.err = ""
	a.schedules.poll = time.Time{}
}

func (a *app) tickSchedules(c *ui.Context) {
	if a.projects == nil {
		return
	}
	now := time.Now()
	if now.Before(a.schedules.poll) {
		c.After(time.Until(a.schedules.poll))
		return
	}
	a.schedules.poll = now.Add(10 * time.Second)
	c.After(10 * time.Second)
	if err := a.loadSchedules(); err != nil {
		a.schedules.err = err.Error()
		return
	}
	for _, t := range a.schedules.tasks {
		if t.due == 0 || t.due > now.Unix() || a.scheduleBlocked(t) != "" {
			continue
		}
		next, err := t.next(now)
		if err != nil {
			a.schedules.err = err.Error()
			return
		}
		a.dispatchSchedule(t, now, next.Unix())
		return
	}
}
func (a *app) scheduleBlocked(t scheduledTask) string {
	if a.git.mutating {
		return "请等待当前操作完成，并处理待审批修改"
	}
	for _, p := range a.ai.tabs {
		if p.running || (p.pending() && p.root == t.workspace) {
			return "请等待 AI 完成，并处理项目中的待审批修改"
		}
	}
	for _, d := range a.docs {
		if d.editable() && d.path != "" && inside(t.workspace, d.path) && d.state.Buffer.Modified() {
			return "请先保存项目中的编辑"
		}
	}
	return ""
}
func (a *app) dispatchSchedule(t scheduledTask, now time.Time, due int64) {
	if reason := a.scheduleBlocked(t); reason != "" {
		a.schedules.err = reason
		return
	}

	// Claim the occurrence before launching, including when two windows share a store.
	run := fmt.Sprintf("schedule-%d-%d", t.id, now.UnixNano())
	res, err := a.projects.db.Execute(context.Background(), `UPDATE scheduled_tasks SET due=?,last_run=?,result=? WHERE id=? AND due=? AND last_run=?`, due, run, "已触发 · "+now.Format("01-02 15:04"), t.id, t.due, t.lastRun)
	if err != nil {
		a.schedules.err = err.Error()
		return
	}
	if res.Changes == 0 {
		a.schedules.poll = time.Time{}
		return
	}
	a.schedules.err = ""
	prev := a.ai.aiSession
	a.newAITab()
	p := a.ai.aiSession
	p.scheduleID = t.id
	p.chatID = run
	p.engine = t.engine
	p.draft = t.prompt
	p.root = t.workspace
	info, statErr := os.Stat(t.workspace)
	if statErr != nil || !info.IsDir() {
		p.err = "项目目录不可用：" + t.workspace
	} else {
		a.sendAIFor(p, t.workspace, nil, false)
	}
	if prev != nil {
		a.ai.aiSession = prev
	}
	if !p.running {
		p.dirty = true
		a.saveAISession(p, true)
		a.finishSchedule(p)
		a.removeAITab(p)
	}
	a.schedules.poll = time.Time{}
}
func (a *app) finishSchedule(p *aiSession) {
	status := "已完成"
	if p.err != "" {
		status = p.err
	} else if p.pending() {
		status = "已生成修改，请在会话中审批"
	}
	a.scheduleSQL(`UPDATE scheduled_tasks SET result=? WHERE id=? AND last_run=?`, time.Now().Format("01-02 15:04")+" · "+status, p.scheduleID, p.chatID)
	p.scheduleID = 0
}
