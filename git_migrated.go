// Git operations migrated from the local poprsi project. UI and project scoping
// are native Omnicode implementations; no frontend or project registry is used.
package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	golet "github.com/HyperparameterSI/golet/backend"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

var gitMu sync.Mutex

const gitOutputLimit = 2 * 1024 * 1024

type gitTimeoutKey struct{}
type gitService struct {
	ctx      context.Context
	root     string
	progress func(string)
}

func (g *gitService) runGit(root string, args ...string) (string, error) {
	return g.runGitInput(root, "", args...)
}
func (g *gitService) runGitInput(root, input string, args ...string) (string, error) {
	return repositoryGitInput(g.ctx, root, input, g.progress, args...)
}
func (g *gitService) runGitTimed(root, input string, timeout time.Duration, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(g.ctx, timeout)
	defer cancel()
	return repositoryGitInput(context.WithValue(ctx, gitTimeoutKey{}, timeout), root, input, g.progress, args...)
}
func (g *gitService) gitRoot(id string) (string, error) {
	if id != g.root {
		return "", errors.New("只能操作当前打开的项目")
	}
	return repositoryRoot(g.ctx, id)
}
func (g *gitService) gitAddProject(path string) (GitProject, error) {
	root, e := repositoryRoot(g.ctx, path)
	return GitProject{ID: root, Name: filepath.Base(root), Path: root}, e
}

type GitProject struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Path string `json:"path"`
}
type GitFile struct {
	Path     string `json:"path"`
	OldPath  string `json:"oldPath"`
	Index    string `json:"index"`
	Worktree string `json:"worktree"`
	Conflict bool   `json:"conflict"`
}
type GitBranch struct {
	Name    string `json:"name"`
	Current bool   `json:"current"`
	Remote  bool   `json:"remote"`
}
type GitHistory struct {
	Hash    string `json:"hash"`
	Parents string `json:"parents"`
	Subject string `json:"subject"`
	Author  string `json:"author"`
	Date    string `json:"date"`
	Refs    string `json:"refs"`
}
type GitReference struct {
	Name    string `json:"name"`
	Hash    string `json:"hash"`
	Subject string `json:"subject"`
}
type GitRemote struct {
	Name string `json:"name"`
	URL  string `json:"url"`
}
type GitState struct {
	Behind    int            `json:"behind"`
	Ahead     int            `json:"ahead"`
	Upstream  string         `json:"upstream"`
	Operation string         `json:"operation"`
	Branch    string         `json:"branch"`
	Head      string         `json:"head"`
	Files     []GitFile      `json:"files"`
	Branches  []GitBranch    `json:"branches"`
	History   []GitHistory   `json:"history"`
	Tags      []GitReference `json:"tags"`
	Stashes   []GitReference `json:"stashes"`
	Remotes   []GitRemote    `json:"remotes"`
}

func (g *gitService) gitFiles(root string) ([]GitFile, error) {
	raw, e := g.runGit(root, "status", "--porcelain=v1", "-z", "--untracked-files=all")
	if e != nil {
		return nil, e
	}
	parts := strings.Split(raw, "\x00")
	files := []GitFile{}
	for i := 0; i < len(parts); i++ {
		v := parts[i]
		if len(v) < 4 {
			continue
		}
		f := GitFile{Path: v[3:], Index: v[:1], Worktree: v[1:2]}
		f.Conflict = strings.Contains(v[:2], "U") || v[:2] == "AA" || v[:2] == "DD"
		if strings.ContainsAny(v[:2], "RC") && i+1 < len(parts) {
			i++
			f.OldPath = parts[i]
		}
		files = append(files, f)
	}
	return files, nil
}
func (g *gitService) GitSnapshot(id string) (GitState, error) {
	gitMu.Lock()
	defer gitMu.Unlock()
	s := GitState{Files: []GitFile{}, Branches: []GitBranch{}, History: []GitHistory{}, Tags: []GitReference{}, Stashes: []GitReference{}, Remotes: []GitRemote{}}
	root, e := g.gitRoot(id)
	if e != nil {
		return s, e
	}
	s.Operation = g.gitPendingOperation(root)
	s.Files, e = g.gitFiles(root)
	if e != nil {
		return s, e
	}
	branch, _ := g.runGit(root, "symbolic-ref", "--quiet", "--short", "HEAD")
	s.Branch = strings.TrimSpace(branch)
	head, _ := g.runGit(root, "rev-parse", "--verify", "HEAD")
	s.Head = strings.TrimSpace(head)
	s.Ahead, s.Upstream = g.gitUnpushed(root)
	s.Behind = g.gitUnpulled(root)
	if s.Branch == "" {
		s.Branch = "游离 HEAD"
	}
	branches, e := g.runGit(root, "for-each-ref", "--format=%(refname)%00%(HEAD)", "refs/heads", "refs/remotes")
	if e != nil {
		return s, e
	}
	for _, line := range strings.Split(strings.TrimSpace(branches), "\n") {
		parts := strings.Split(line, "\x00")
		if len(parts) != 2 || strings.HasSuffix(parts[0], "/HEAD") {
			continue
		}
		remote := strings.HasPrefix(parts[0], "refs/remotes/")
		name := strings.TrimPrefix(strings.TrimPrefix(parts[0], "refs/heads/"), "refs/remotes/")
		s.Branches = append(s.Branches, GitBranch{Name: name, Current: parts[1] == "*", Remote: remote})
	}
	{
		args := []string{"log", "-100", "--all", "--topo-order", "--date=iso-strict", "--format=%H%x00%P%x00%s%x00%an%x00%aI%x00%D"}
		if s.Head != "" {
			args = append(args, "HEAD")
		}
		log, e := g.runGit(root, args...)
		if e != nil {
			return s, e
		}
		for _, line := range strings.Split(strings.TrimSuffix(log, "\n"), "\n") {
			p := strings.Split(line, "\x00")
			if len(p) == 6 {
				s.History = append(s.History, GitHistory{p[0], p[1], p[2], p[3], p[4], p[5]})
			}
		}
	}
	tags, e := g.runGit(root, "for-each-ref", "--sort=-creatordate", "--format=%(refname:short)%00%(*objectname)%00%(objectname)%00%(subject)", "refs/tags")
	if e != nil {
		return s, e
	}
	for _, line := range strings.Split(strings.TrimSuffix(tags, "\n"), "\n") {
		p := strings.Split(line, "\x00")
		if len(p) == 4 {
			hash := p[1]
			if hash == "" {
				hash = p[2]
			}
			s.Tags = append(s.Tags, GitReference{Name: p[0], Hash: hash, Subject: p[3]})
		}
	}
	stash, e := g.runGit(root, "stash", "list", "--format=%gd%x00%H%x00%gs")
	if e != nil {
		return s, e
	}
	for _, line := range strings.Split(strings.TrimSuffix(stash, "\n"), "\n") {
		p := strings.Split(line, "\x00")
		if len(p) == 3 {
			s.Stashes = append(s.Stashes, GitReference{Name: p[0], Hash: p[1], Subject: p[2]})
		}
	}
	remotes, e := g.runGit(root, "remote")
	if e != nil {
		return s, e
	}
	for _, name := range strings.Split(strings.TrimSuffix(remotes, "\n"), "\n") {
		if name == "" {
			continue
		}
		raw, e := g.runGit(root, "remote", "get-url", "--", name)
		if e != nil {
			return s, e
		}
		s.Remotes = append(s.Remotes, GitRemote{Name: name, URL: g.gitRemoteDisplay(strings.TrimSpace(raw))})
	}
	return s, nil
}

// Display remote locations without embedded HTTP credentials or query tokens.
func (g *gitService) gitRemoteDisplay(raw string) string {
	u, e := url.Parse(raw)
	if e == nil && u.Scheme != "" && u.Host != "" {
		u.User = nil
		u.RawQuery = ""
		u.Fragment = ""
		return u.String()
	}
	return raw
}

func (g *gitService) GitRefHistory(id, ref string) ([]GitHistory, error) {
	gitMu.Lock()
	defer gitMu.Unlock()
	root, e := g.gitRoot(id)
	if e != nil {
		return nil, e
	}
	if !strings.HasPrefix(ref, "refs/heads/") && !strings.HasPrefix(ref, "refs/remotes/") && !strings.HasPrefix(ref, "refs/tags/") {
		return nil, errors.New("请选择项目内的分支或标签")
	}
	hash, e := g.runGit(root, "rev-parse", "--verify", ref+"^{commit}")
	if e != nil {
		return nil, e
	}
	hash = strings.TrimSpace(hash)
	raw, e := g.runGit(root, "log", "-100", "--topo-order", "--format=%H%x00%P%x00%s%x00%an%x00%aI%x00%D", hash, "--")
	if e != nil {
		return nil, e
	}
	items := []GitHistory{}
	for _, line := range strings.Split(strings.TrimSuffix(raw, "\n"), "\n") {
		p := strings.Split(line, "\x00")
		if len(p) == 6 {
			items = append(items, GitHistory{p[0], p[1], p[2], p[3], p[4], p[5]})
		}
	}
	return items, nil
}
func (g *gitService) GitStashDiff(id, hash string) (string, error) {
	gitMu.Lock()
	defer gitMu.Unlock()
	root, e := g.gitRoot(id)
	if e != nil {
		return "", e
	}
	if !gitHash.MatchString(hash) {
		return "", errors.New("无效贮藏记录")
	}
	hashes, e := g.runGit(root, "stash", "list", "--format=%H")
	if e != nil {
		return "", e
	}
	found := false
	for _, h := range strings.Fields(hashes) {
		if h == hash {
			found = true
		}
	}
	if !found {
		return "", errors.New("贮藏记录已改变，请刷新")
	}
	return g.runGit(root, "stash", "show", "--patch", "--include-untracked", "--no-ext-diff", "--no-textconv", hash)
}

var gitHash = regexp.MustCompile(`^[0-9a-f]{40,64}$`)

func (g *gitService) GitDiff(id, path string, staged bool, commit string) (string, error) {
	gitMu.Lock()
	defer gitMu.Unlock()
	root, e := g.gitRoot(id)
	if e != nil {
		return "", e
	}
	if commit != "" {
		if !gitHash.MatchString(commit) {
			return "", errors.New("无效提交")
		}
		return g.runGit(root, "show", "--format=fuller", "--stat", "--patch", "--no-ext-diff", "--no-textconv", commit, "--")
	}
	files, e := g.gitFiles(root)
	if e != nil {
		return "", e
	}
	var file *GitFile
	for i := range files {
		if files[i].Path == path {
			file = &files[i]
			break
		}
	}
	if file == nil {
		return "", errors.New("文件状态已改变，请刷新")
	}
	if file.Index == "?" {
		full := filepath.Join(root, path)
		rel, e := filepath.Rel(root, full)
		if e != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
			return "", errors.New("无效路径")
		}
		info, e := os.Lstat(full)
		if e != nil {
			return "", e
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return os.Readlink(full)
		}
		if !info.Mode().IsRegular() {
			return "此目录或子模块请在对应仓库中查看。", nil
		}
		f, e := os.Open(full)
		if e != nil {
			return "", e
		}
		defer f.Close()
		data, e := io.ReadAll(io.LimitReader(f, gitOutputLimit+1))
		if e != nil {
			return "", e
		}
		if len(data) > gitOutputLimit {
			return "文件超过 2 MB，无法预览。", nil
		}
		if bytes.IndexByte(data, 0) >= 0 {
			return "二进制文件，无法显示文本差异。", nil
		}
		return "未跟踪文件 · " + path + "\n\n" + string(data), nil
	}
	args := g.gitPatchArgs()
	if staged {
		args = append(args, "--cached")
	}
	args = append(args, "--", path)
	if file.OldPath != "" {
		args = append(args, file.OldPath)
	}
	return g.runGit(root, args...)
}
func (g *gitService) GitStage(id, path string, stage bool) error {
	gitMu.Lock()
	defer gitMu.Unlock()
	root, e := g.gitRoot(id)
	if e != nil {
		return e
	}
	files, e := g.gitFiles(root)
	if e != nil {
		return e
	}
	var file *GitFile
	for i := range files {
		if files[i].Path == path {
			file = &files[i]
			break
		}
	}
	if file == nil {
		return errors.New("文件状态已改变，请刷新")
	}
	paths := []string{path}
	if file.OldPath != "" && (!stage || file.Worktree == "R" || file.Worktree == "C") {
		paths = append(paths, file.OldPath)
	}
	args := []string{"add", "--"}
	if !stage {
		if _, e := g.runGit(root, "rev-parse", "--verify", "HEAD"); e == nil {
			args = []string{"reset", "-q", "HEAD", "--"}
		} else {
			args = []string{"rm", "--cached", "-f", "--ignore-unmatch", "--"}
		}
	}
	_, e = g.runGit(root, append(args, paths...)...)
	return e
}

// GitStageAll changes the index in one Git command and preserves working files.
func (g *gitService) GitStageAll(id string, stage bool) error {
	gitMu.Lock()
	defer gitMu.Unlock()
	root, err := g.gitRoot(id)
	if err != nil {
		return err
	}
	files, err := g.gitFiles(root)
	if err != nil {
		return err
	}
	var paths []string
	seen := map[string]bool{}
	for _, file := range files {
		matches := file.Index != " " && file.Index != "?"
		if stage {
			matches = file.Worktree != " " || file.Index == "?"
		}
		if !matches {
			continue
		}
		filePaths := []string{file.Path}
		if !stage || file.Worktree == "R" || file.Worktree == "C" {
			filePaths = append(filePaths, file.OldPath)
		}
		for _, path := range filePaths {
			if path != "" && !seen[path] {
				paths = append(paths, path)
				seen[path] = true
			}
		}
	}
	if len(paths) == 0 {
		return nil
	}
	args := []string{"add", "--"}
	if !stage {
		if _, err := g.runGit(root, "rev-parse", "--verify", "HEAD"); err == nil {
			args = []string{"reset", "-q", "HEAD", "--"}
		} else {
			args = []string{"rm", "--cached", "-f", "--ignore-unmatch", "--"}
		}
	}
	_, err = g.runGit(root, append(args, paths...)...)
	return err
}

func (g *gitService) GitCommit(id, message string) (string, error) {
	gitMu.Lock()
	defer gitMu.Unlock()
	root, e := g.gitRoot(id)
	if e != nil {
		return "", e
	}
	if strings.TrimSpace(message) == "" || len(message) > 20000 {
		return "", errors.New("请填写提交说明（最多 20,000 字节）")
	}
	files, e := g.gitFiles(root)
	if e != nil {
		return "", e
	}
	hasStaged := false
	for _, f := range files {
		if f.Conflict {
			return "", errors.New("请先解决所有冲突并暂存")
		}
		if f.Index != " " && f.Index != "?" {
			hasStaged = true
		}
	}
	if !hasStaged {
		return "", errors.New("没有已暂存的改动")
	}
	return g.runGit(root, "commit", "-m", message)
}
func (g *gitService) GitCheckout(id, branch string, create bool) error {
	gitMu.Lock()
	defer gitMu.Unlock()
	root, e := g.gitRoot(id)
	if e != nil {
		return e
	}
	if strings.HasPrefix(branch, "-") || branch == "" {
		return errors.New("无效分支名称")
	}
	if _, e = g.runGit(root, "check-ref-format", "--branch", branch); e != nil {
		return e
	}
	if !create {
		if _, e = g.runGit(root, "show-ref", "--verify", "refs/heads/"+branch); e != nil {
			return errors.New("本地分支不存在，请刷新分支列表")
		}
	}
	files, e := g.gitFiles(root)
	if e != nil {
		return e
	}
	if len(files) > 0 {
		return errors.New("请先提交或在终端处理工作区改动，再切换分支")
	}
	args := []string{"checkout", "--no-guess"}
	if create {
		args = append(args, "-b")
	}
	args = append(args, branch)
	_, e = g.runGit(root, args...)
	return e
}

// Compare local refs only; opening the workspace must not fetch from the network.
func (g *gitService) gitUnpushed(root string) (int, string) {
	upstream, _ := g.runGit(root, "rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{upstream}")
	upstream = strings.TrimSpace(upstream)
	args := []string{"rev-list", "--count", "HEAD", "--not", "--remotes"}
	if upstream != "" {
		args = []string{"rev-list", "--count", "@{upstream}..HEAD"}
	}
	out, err := g.runGit(root, args...)
	if err != nil {
		return -1, upstream
	}
	count, err := strconv.Atoi(strings.TrimSpace(out))
	if err != nil {
		return -1, upstream
	}
	return count, upstream
}

// Count only the configured upstream; unrelated remote branches are not pull targets.
// Like the ahead count, this uses the latest locally fetched remote refs.
func (g *gitService) gitUnpulled(root string) int {
	out, err := g.runGit(root, "rev-list", "--count", "HEAD..@{upstream}")
	if err != nil {
		return -1
	}
	count, err := strconv.Atoi(strings.TrimSpace(out))
	if err != nil {
		return -1
	}
	return count
}

func (g *gitService) gitKnownRemote(root, remote string) error {
	out, err := g.runGit(root, "remote")
	if err != nil {
		return err
	}
	for _, name := range strings.Fields(out) {
		if name == remote && !strings.HasPrefix(name, "-") {
			return nil
		}
	}
	return errors.New("请选择仓库中已有的远端")
}
func (g *gitService) gitClean(root string) error {
	files, err := g.gitFiles(root)
	if err != nil {
		return err
	}
	if len(files) > 0 {
		return errors.New("请先提交或贮藏工作区修改，再执行此操作")
	}
	return nil
}

// GitNetwork never force-pushes and only permits fast-forward pulls.
func (g *gitService) GitNetwork(id, operation, remote, branch string) error {
	return g.gitNetwork(id, operation, remote, branch, g.runGit)
}
func (g *gitService) gitNetwork(id, operation, remote, branch string, run func(string, ...string) (string, error)) error {
	gitMu.Lock()
	defer gitMu.Unlock()
	root, err := g.gitRoot(id)
	if err != nil {
		return err
	}
	if err = g.gitKnownRemote(root, remote); err != nil {
		return err
	}
	if operation == "fetch" {
		_, err = run(root, "fetch", "--", remote)
		return err
	}
	if operation != "pull" && operation != "push" {
		return errors.New("不支持的远端操作")
	}
	if strings.HasPrefix(branch, "-") || strings.TrimSpace(branch) == "" {
		return errors.New("请输入有效的目标分支")
	}
	if _, err = g.runGit(root, "check-ref-format", "refs/heads/"+branch); err != nil {
		return err
	}
	if operation == "pull" {
		if err = g.gitClean(root); err != nil {
			return err
		}
		_, err = run(root, "pull", "--ff-only", "--no-rebase", "--", remote, branch)
	} else {
		if _, err = g.runGit(root, "symbolic-ref", "--quiet", "HEAD"); err != nil {
			return errors.New("游离 HEAD 不能推送，请先切换或创建分支")
		}
		_, err = run(root, "push", "--", remote, "HEAD:refs/heads/"+branch)
	}
	return err
}
func (g *gitService) GitMerge(id, ref string) error {
	gitMu.Lock()
	defer gitMu.Unlock()
	root, err := g.gitRoot(id)
	if err != nil {
		return err
	}
	if err = g.gitClean(root); err != nil {
		return err
	}
	if !strings.HasPrefix(ref, "refs/heads/") && !strings.HasPrefix(ref, "refs/remotes/") {
		return errors.New("请选择有效的分支")
	}
	hash, err := g.runGit(root, "rev-parse", "--verify", ref+"^{commit}")
	if err != nil {
		return err
	}
	_, err = g.runGit(root, "merge", "--no-edit", "--", strings.TrimSpace(hash))
	return err
}
func (g *gitService) GitStashSave(id, message string) error {
	gitMu.Lock()
	defer gitMu.Unlock()
	root, err := g.gitRoot(id)
	if err != nil {
		return err
	}
	files, err := g.gitFiles(root)
	if err != nil {
		return err
	}
	if len(files) == 0 {
		return errors.New("工作区没有可贮藏的修改")
	}
	if len(message) > 2000 {
		return errors.New("贮藏说明过长")
	}
	_, err = g.runGit(root, "stash", "push", "--include-untracked", "-m", message)
	return err
}

// Delete only after a successful apply; conflicts always retain the stash.
func (g *gitService) GitStashApply(id, hash string, deleteAfter bool) error {
	gitMu.Lock()
	defer gitMu.Unlock()
	root, err := g.gitRoot(id)
	if err != nil {
		return err
	}
	if err = g.gitClean(root); err != nil {
		return err
	}
	out, err := g.runGit(root, "stash", "list", "--format=%H")
	if err != nil {
		return err
	}
	for _, h := range strings.Fields(out) {
		if h == hash {
			_, err = g.runGit(root, "stash", "apply", "--index", h)
			if err != nil || !deleteAfter {
				return err
			}
			if err = g.dropGitStash(root, hash); err != nil {
				return fmt.Errorf("贮藏已应用，但删除失败，请刷新后检查：%w", err)
			}
			return nil
		}
	}
	return errors.New("贮藏记录已不存在，请刷新")
}

// Resolve the selected object's current reflog position; stash indices can move.
func (g *gitService) GitStashDrop(id, hash string) error {
	gitMu.Lock()
	defer gitMu.Unlock()
	root, err := g.gitRoot(id)
	if err != nil {
		return err
	}
	return g.dropGitStash(root, hash)
}

func (g *gitService) dropGitStash(root, hash string) error {
	out, err := g.runGit(root, "stash", "list", "--format=%gd%x00%H")
	if err != nil {
		return err
	}
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		ref, h, ok := strings.Cut(line, "\x00")
		if ok && h == hash {
			_, err = g.runGit(root, "stash", "drop", ref)
			return err
		}
	}
	return errors.New("贮藏记录已不存在，请刷新")
}

type GitSettings struct {
	Name  string `json:"name"`
	Email string `json:"email"`
}

func (g *gitService) GitSettings(id string) (GitSettings, error) {
	gitMu.Lock()
	defer gitMu.Unlock()
	root, err := g.gitRoot(id)
	if err != nil {
		return GitSettings{}, err
	}
	name, _ := g.runGit(root, "config", "--get", "user.name")
	email, _ := g.runGit(root, "config", "--get", "user.email")
	return GitSettings{strings.TrimSpace(name), strings.TrimSpace(email)}, nil
}
func (g *gitService) GitSaveSettings(id, name, email string) error {
	gitMu.Lock()
	defer gitMu.Unlock()
	root, err := g.gitRoot(id)
	if err != nil {
		return err
	}
	name = strings.TrimSpace(name)
	email = strings.TrimSpace(email)
	if name == "" || email == "" || len(name) > 200 || len(email) > 320 || strings.ContainsAny(name+email, "\r\n\x00") {
		return errors.New("请填写有效的提交者姓名与邮箱")
	}
	if _, err = g.runGit(root, "config", "--local", "user.name", name); err != nil {
		return err
	}
	_, err = g.runGit(root, "config", "--local", "user.email", email)
	return err
}
func (g *gitService) gitWebURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if !strings.Contains(raw, "://") {
		if at := strings.Index(raw, "@"); at >= 0 {
			raw = raw[at+1:]
		} else {
			return "", errors.New("此远端没有可打开的网页地址")
		}
		host, path, ok := strings.Cut(raw, ":")
		if !ok || strings.ContainsAny(host, "/\\ ") {
			return "", errors.New("此远端没有可打开的网页地址")
		}
		raw = "https://" + host + "/" + path
	}
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" {
		return "", errors.New("远端地址无效")
	}
	if u.Scheme == "ssh" || u.Scheme == "git" {
		u.Scheme = "https"
		u.Host = u.Hostname()
	}
	if u.Scheme != "https" && u.Scheme != "http" {
		return "", errors.New("此远端不支持网页浏览")
	}
	u.User = nil
	u.RawQuery = ""
	u.Fragment = ""
	u.Path = strings.TrimSuffix(u.Path, ".git")
	return u.String(), nil
}

// GitSetRemoteURL updates the selected repository's default remote URL only.
// Explicit push URLs, other remotes and branch tracking configuration stay intact.
func (g *gitService) GitSetRemoteURL(id, remote, address string) error {
	gitMu.Lock()
	defer gitMu.Unlock()
	root, err := g.gitRoot(id)
	if err != nil {
		return err
	}
	if err = g.gitKnownRemote(root, remote); err != nil {
		return err
	}
	address = strings.TrimSpace(address)
	if err = g.validateGitCloneSource(address); err != nil {
		return err
	}
	_, err = g.runGit(root, "remote", "set-url", "--", remote, address)
	return err
}

func (g *gitService) gitBranchRef(root, ref string) (string, error) {
	if !strings.HasPrefix(ref, "refs/heads/") && !strings.HasPrefix(ref, "refs/remotes/") {
		return "", errors.New("请选择本地或远端分支")
	}
	if _, e := g.runGit(root, "check-ref-format", ref); e != nil {
		return "", e
	}
	hash, e := g.runGit(root, "rev-parse", "--verify", ref+"^{commit}")
	return strings.TrimSpace(hash), e
}

// All operands are validated refs. Deletion and push deliberately never force.
func (g *gitService) GitBranchAction(id, ref, operation, value, expectedBranch string) (string, error) {
	gitMu.Lock()
	defer gitMu.Unlock()
	root, e := g.gitRoot(id)
	if e != nil {
		return "", e
	}
	hash, e := g.gitBranchRef(root, ref)
	if e != nil {
		return "", e
	}
	current, _ := g.runGit(root, "symbolic-ref", "--quiet", "--short", "HEAD")
	current = strings.TrimSpace(current)
	if current != expectedBranch {
		return "", errors.New("当前分支已变化，请刷新后重试")
	}
	if operation == "compare" {
		return g.runGit(root, "diff", "--no-ext-diff", "--no-textconv", "--stat", "--patch", "HEAD", hash, "--")
	}
	if g.gitPendingOperation(root) != "" {
		return "", errors.New("请先完成或中止当前合并/变基")
	}
	local := strings.HasPrefix(ref, "refs/heads/")
	name := strings.TrimPrefix(ref, "refs/heads/")
	validName := func(n string) error {
		if n == "" || strings.HasPrefix(n, "-") {
			return errors.New("分支名称无效")
		}
		_, err := g.runGit(root, "check-ref-format", "refs/heads/"+n)
		return err
	}
	switch operation {
	case "checkout":
		if e = g.gitClean(root); e != nil {
			return "", e
		}
		if local {
			return g.runGit(root, "switch", "--no-guess", name)
		}
		if e = validName(value); e != nil {
			return "", e
		}
		return g.runGit(root, "switch", "--track", "-c", value, ref)
	case "merge", "rebase":
		if current == "" {
			return "", errors.New("请先检出本地分支")
		}
		if ref == "refs/heads/"+current {
			return "", errors.New("不能对当前分支执行此操作")
		}
		if e = g.gitClean(root); e != nil {
			return "", e
		}
		if operation == "merge" {
			return g.runGit(root, "merge", "--no-edit", "--", hash)
		}
		return g.runGit(root, "rebase", hash)
	case "rename":
		if !local {
			return "", errors.New("只能重命名本地分支")
		}
		if e = validName(value); e != nil {
			return "", e
		}
		return g.runGit(root, "branch", "-m", name, value)
	case "delete":
		if !local || name == current {
			return "", errors.New("不能删除当前分支或远端分支")
		}
		return g.runGit(root, "branch", "-d", name)
	case "track":
		if !local {
			return "", errors.New("请选择本地分支")
		}
		if !strings.HasPrefix(value, "refs/remotes/") {
			return "", errors.New("请选择远端跟踪分支")
		}
		if _, e = g.gitBranchRef(root, value); e != nil {
			return "", e
		}
		return g.runGit(root, "branch", "--set-upstream-to="+strings.TrimPrefix(value, "refs/remotes/"), name)
	case "push":
		if !local {
			return "", errors.New("请选择本地分支")
		}
		remote, target, ok := strings.Cut(value, ":")
		if !ok {
			return "", errors.New("请选择远端和目标分支")
		}
		if e = g.gitKnownRemote(root, remote); e != nil {
			return "", e
		}
		if e = validName(target); e != nil {
			return "", e
		}
		return g.runGit(root, "push", "--", remote, ref+":refs/heads/"+target)
	}
	return "", errors.New("不支持的分支操作")
}

func (g *gitService) gitPatchArgs() []string {
	return []string{"diff", "--no-ext-diff", "--no-textconv", "--no-color", "--no-renames", "--src-prefix=a/", "--dst-prefix=b/", "--unified=3"}
}

var gitHunkHeader = regexp.MustCompile(`^@@ -(\d+)(?:,(\d+))? \+(\d+)(?:,(\d+))? @@`)

// Build a source-to-target patch containing only the selected changed lines.
// Reverse operations use the new side as their source, preserving all unselected
// changes as context. Never accept a patch supplied by the frontend for applying.
func (g *gitService) selectedGitPatch(diff string, selected []int, reverse bool) (string, error) {
	lines := strings.Split(strings.TrimSuffix(diff, "\n"), "\n")
	chosen := map[int]bool{}
	for _, i := range selected {
		if i < 0 || i >= len(lines) {
			return "", errors.New("无效修改行")
		}
		chosen[i] = true
	}
	if len(chosen) == 0 {
		return "", errors.New("请选择修改行")
	}
	var out strings.Builder
	header := false
	delta := 0
	used := 0
	for i := 0; i < len(lines); {
		match := gitHunkHeader.FindStringSubmatch(lines[i])
		if match == nil {
			if strings.HasPrefix(lines[i], "--- ") || strings.HasPrefix(lines[i], "+++ ") {
				out.WriteString(lines[i] + "\n")
				header = true
			}
			i++
			continue
		}
		source, _ := strconv.Atoi(match[1])
		if reverse {
			source, _ = strconv.Atoi(match[3])
		}
		i++
		var body strings.Builder
		oldCount, newCount := 0, 0
		changed := false
		lastKept := false
		for i < len(lines) && gitHunkHeader.FindStringSubmatch(lines[i]) == nil {
			line := lines[i]
			if line == "" {
				i++
				continue
			}
			kind := line[0]
			keep := true
			if kind == '\\' {
				if lastKept {
					body.WriteString(line + "\n")
				}
				i++
				continue
			}
			switch kind {
			case ' ':
			case '+', '-':
				if chosen[i] {
					used++
					changed = true
					if reverse {
						if kind == '+' {
							kind = '-'
						} else {
							kind = '+'
						}
					}
				} else {
					if (!reverse && kind == '+') || (reverse && kind == '-') {
						keep = false
					} else {
						kind = ' '
					}
				}
			default:
				return "", errors.New("不支持的差异内容")
			}
			if keep {
				body.WriteByte(kind)
				body.WriteString(line[1:] + "\n")
				if kind != '+' {
					oldCount++
				}
				if kind != '-' {
					newCount++
				}
			}
			lastKept = keep
			i++
		}
		if changed {
			target := source + delta
			if oldCount == 0 {
				target++
			}
			if newCount == 0 {
				target--
			}
			fmt.Fprintf(&out, "@@ -%d,%d +%d,%d @@\n", source, oldCount, target, newCount)
			out.WriteString(body.String())
			delta += newCount - oldCount
		}
	}
	if !header || used != len(chosen) {
		return "", errors.New("只能选择差异中的新增或删除行")
	}
	return out.String(), nil
}
func (g *gitService) GitApplyLines(id, path, expected string, selected []int, operation string) error {
	gitMu.Lock()
	defer gitMu.Unlock()
	if operation != "stage" && operation != "unstage" && operation != "discard" {
		return errors.New("无效区块操作")
	}
	if len(expected) > gitOutputLimit || len(selected) > 5000 {
		return errors.New("差异过大，请使用整文件操作")
	}
	root, err := g.gitRoot(id)
	if err != nil {
		return err
	}
	file, full, err := g.gitChangedFile(root, path)
	if err != nil {
		return err
	}
	info, err := os.Lstat(full)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || file.Conflict || file.Index == "?" || file.OldPath != "" || file.Index == "A" || file.Index == "D" || file.Worktree == "D" {
		return errors.New("此文件请使用整文件操作；部分操作支持普通文本修改")
	}
	args := g.gitPatchArgs()
	if operation == "unstage" {
		args = append(args, "--cached")
	}
	args = append(args, "--", path)
	current, err := g.runGit(root, args...)
	if err != nil {
		return err
	}
	if current != expected {
		return errors.New("文件差异已变化，请刷新后重新选择修改行")
	}
	patch, err := g.selectedGitPatch(current, selected, operation != "stage")
	if err != nil {
		return err
	}
	args = []string{"apply", "--whitespace=nowarn"}
	if operation != "discard" {
		args = append(args, "--cached")
	}
	if _, err = g.runGitInput(root, patch, append(args, "--check", "-")...); err != nil {
		return err
	}
	_, err = g.runGitInput(root, patch, append(args, "-")...)
	return err
}

type GitPullOptions struct {
	Mode       string `json:"mode"`
	Commit     bool   `json:"commit"`
	IncludeLog bool   `json:"includeLog"`
	NoFF       bool   `json:"noFF"`
}

func (g *gitService) gitPendingOperation(root string) string {
	for _, item := range []struct{ path, kind string }{{"rebase-merge", "rebase"}, {"rebase-apply", "rebase"}, {"MERGE_HEAD", "merge"}} {
		p, e := g.runGit(root, "rev-parse", "--git-path", item.path)
		if e != nil {
			continue
		}
		p = strings.TrimSpace(p)
		if !filepath.IsAbs(p) {
			p = filepath.Join(root, p)
		}
		if _, e = os.Stat(p); e == nil {
			return item.kind
		}
	}
	return ""
}
func (g *gitService) GitPull(id, remote, branch string, options GitPullOptions) error {
	gitMu.Lock()
	defer gitMu.Unlock()
	root, err := g.gitRoot(id)
	if err != nil {
		return err
	}
	if g.gitPendingOperation(root) != "" {
		return errors.New("请先完成或中止当前合并/变基")
	}
	if err = g.gitKnownRemote(root, remote); err != nil {
		return err
	}
	if branch == "" || strings.HasPrefix(branch, "-") {
		return errors.New("请选择有效的远端分支")
	}
	if _, err = g.runGit(root, "check-ref-format", "refs/heads/"+branch); err != nil {
		return err
	}
	if _, err = g.runGit(root, "symbolic-ref", "--quiet", "HEAD"); err != nil {
		return errors.New("请先切换到本地分支")
	}
	if err = g.gitClean(root); err != nil {
		return err
	}
	args := []string{"pull", "--no-autostash"}
	switch options.Mode {
	case "ff-only":
		args = append(args, "--ff-only", "--no-rebase")
	case "merge":
		args = append(args, "--no-rebase", "--no-edit")
		if options.NoFF || !options.Commit {
			args = append(args, "--no-ff")
		} else {
			args = append(args, "--ff")
		}
		if options.Commit {
			args = append(args, "--commit")
		} else {
			args = append(args, "--no-commit")
		}
		if options.IncludeLog {
			args = append(args, "--log=20")
		} else {
			args = append(args, "--no-log")
		}
	case "rebase":
		args = append(args, "--rebase", "--ff")
	default:
		return errors.New("请选择拉取方式")
	}
	_, err = g.runGit(root, append(args, "--", remote, branch)...)
	return err
}
func (g *gitService) GitResolveOperation(id, action string) error {
	gitMu.Lock()
	defer gitMu.Unlock()
	root, err := g.gitRoot(id)
	if err != nil {
		return err
	}
	kind := g.gitPendingOperation(root)
	if kind == "" {
		return errors.New("没有待完成的合并或变基")
	}
	if action == "abort" {
		_, err = g.runGit(root, kind, "--abort")
		return err
	}
	if action != "continue" {
		return errors.New("无效操作")
	}
	files, err := g.gitFiles(root)
	if err != nil {
		return err
	}
	for _, file := range files {
		if file.Conflict {
			return errors.New("请解决冲突并暂存文件，再继续")
		}
	}
	if kind == "merge" {
		_, err = g.runGit(root, "-c", "core.editor=true", "commit", "--no-edit")
	} else {
		_, err = g.runGit(root, "-c", "core.editor=true", "rebase", "--continue")
	}
	return err
}
func (g *gitService) GitRemoteBranches(id, remote string) ([]string, error) {
	gitMu.Lock()
	defer gitMu.Unlock()
	root, err := g.gitRoot(id)
	if err != nil {
		return nil, err
	}
	if err = g.gitKnownRemote(root, remote); err != nil {
		return nil, err
	}
	out, err := g.runGit(root, "ls-remote", "--heads", "--", remote)
	if err != nil {
		return nil, err
	}
	branches := []string{}
	for _, line := range strings.Split(out, "\n") {
		parts := strings.SplitN(line, "\t", 2)
		if len(parts) == 2 && strings.HasPrefix(parts[1], "refs/heads/") {
			branches = append(branches, strings.TrimPrefix(parts[1], "refs/heads/"))
		}
	}
	return branches, nil
}

var gitTrashFile = golet.Shell.TrashItem

func (g *gitService) gitChangedFile(root, path string) (GitFile, string, error) {
	files, err := g.gitFiles(root)
	if err != nil {
		return GitFile{}, "", err
	}
	var found *GitFile
	for _, f := range files {
		if f.Path == path {
			copy := f
			found = &copy
			break
		}
	}
	if found == nil {
		return GitFile{}, "", errors.New("文件状态已改变，请重新选择文件")
	}
	full := filepath.Join(root, path)
	rel, err := filepath.Rel(root, full)
	if err != nil || rel == ".." || strings.HasPrefix(rel, "../") || filepath.IsAbs(path) {
		return GitFile{}, "", errors.New("无效文件路径")
	}
	// Resolve only the parent: acting on a symlink must not act on its target.
	parent, err := filepath.EvalSymlinks(filepath.Dir(full))
	if err != nil {
		return GitFile{}, "", err
	}
	relative, err := filepath.Rel(root, parent)
	if err != nil || relative == ".." || strings.HasPrefix(relative, "../") {
		return GitFile{}, "", errors.New("文件位于仓库外")
	}
	if info, err := os.Lstat(full); err == nil && info.IsDir() {
		return GitFile{}, "", errors.New("目录或子模块请在终端中处理")
	}
	return *found, full, nil
}

func (g *gitService) GitFileAction(id, path, action string) error {
	gitMu.Lock()
	defer gitMu.Unlock()
	root, err := g.gitRoot(id)
	if err != nil {
		return err
	}
	file, full, err := g.gitChangedFile(root, path)
	if err != nil {
		return err
	}
	switch action {
	case "finder":
		golet.Shell.ShowItemInFolder(full)
		return nil
	case "remove":
		return gitTrashFile(full)
	case "discard":
		if file.Conflict {
			return errors.New("冲突文件请先手动解决，不能直接丢弃")
		}
		if file.Index == "?" {
			return gitTrashFile(full)
		}
		if file.Worktree == " " {
			return errors.New("没有未暂存修改；请先取消暂存")
		}
		_, err = g.runGit(root, "restore", "--worktree", "--", path)
		return err
	case "ignore":
		rule, err := g.gitIgnoreRule(root, path, "exact", "", "project")
		if err != nil {
			return err
		}
		return g.appendGitIgnore(rule)
	default:
		return errors.New("不支持的文件操作")
	}
}

type GitIgnoreRule struct {
	Pattern     string `json:"pattern"`
	Location    string `json:"location"`
	Description string `json:"description"`
}

func (g *gitService) gitIgnoreEscape(value string) string {
	return strings.NewReplacer("\\", "\\\\", "*", "\\*", "?", "\\?", "[", "\\[", "]", "\\]", " ", "\\ ", "!", "\\!", "#", "\\#").Replace(value)
}
func (g *gitService) gitIgnoreRule(root, path, mode, value, scope string) (GitIgnoreRule, error) {
	var rule GitIgnoreRule
	file, _, err := g.gitChangedFile(root, path)
	if err != nil {
		return rule, err
	}
	if file.Index != "?" {
		return rule, errors.New("仅未跟踪文件可以忽略；已跟踪文件不受忽略规则影响")
	}
	if strings.ContainsAny(path+value, "\x00\r\n") {
		return rule, errors.New("忽略规则不能包含换行或空字符")
	}
	switch mode {
	case "exact":
		rule.Pattern = "/" + g.gitIgnoreEscape(filepath.ToSlash(path))
	case "extension":
		ext := filepath.Ext(path)
		if ext == "" || ext == filepath.Base(path) || ext == "." {
			return rule, errors.New("此文件没有可用的扩展名")
		}
		rule.Pattern = "*" + g.gitIgnoreEscape(ext)
	case "directory":
		dir := filepath.ToSlash(filepath.Dir(path))
		valid := false
		for dir != "." && dir != "/" {
			if value == dir {
				valid = true
				break
			}
			dir = filepath.ToSlash(filepath.Dir(dir))
		}
		if !valid {
			return rule, errors.New("请选择该文件的上级目录")
		}
		rule.Pattern = "/" + g.gitIgnoreEscape(value) + "/"
	case "custom":
		if strings.TrimSpace(value) == "" || len(value) > 2000 {
			return rule, errors.New("请输入单行忽略规则（最多 2000 字节）")
		}
		if strings.HasPrefix(value, "#") || strings.HasPrefix(value, "!") {
			return rule, errors.New("请输入忽略规则，而非注释或取消忽略规则")
		}
		rule.Pattern = value
	default:
		return rule, errors.New("请选择忽略方式")
	}
	switch scope {
	case "project":
		rule.Location = filepath.Join(root, ".gitignore")
		rule.Description = "仅当前项目；.gitignore 可提交并与团队共享。"
	case "local":
		location, e := g.runGit(root, "rev-parse", "--git-path", "info/exclude")
		if e != nil {
			return rule, e
		}
		rule.Location = strings.TrimSpace(location)
		if !filepath.IsAbs(rule.Location) {
			rule.Location = filepath.Join(root, rule.Location)
		}
		rule.Description = "仅本机当前仓库；不会提交到 Git，共用仓库的 worktree 也会使用此规则。"
	case "global":
		location, e := g.runGit(root, "config", "--global", "--path", "--default=", "--get", "core.excludesFile")
		if e != nil {
			return rule, e
		}
		rule.Location = strings.TrimSpace(location)
		if rule.Location == "" {
			base := os.Getenv("XDG_CONFIG_HOME")
			if base == "" {
				home, e := os.UserHomeDir()
				if e != nil {
					return rule, e
				}
				base = filepath.Join(home, ".config")
			}
			rule.Location = filepath.Join(base, "git", "ignore")
		}
		if !filepath.IsAbs(rule.Location) {
			return rule, errors.New("全局忽略文件配置必须使用绝对路径")
		}
		rule.Description = "影响当前用户的所有仓库（除自行覆盖忽略文件配置的仓库）；精确路径相对每个仓库根目录匹配。"
	default:
		return rule, errors.New("请选择忽略列表")
	}
	return rule, nil
}
func (g *gitService) appendGitIgnore(rule GitIgnoreRule) error {
	info, err := os.Lstat(rule.Location)
	if err == nil && !info.Mode().IsRegular() {
		return errors.New("忽略列表必须是普通文件，不能是符号链接或目录")
	}
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	data, err := os.ReadFile(rule.Location)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.TrimSuffix(line, "\r") == rule.Pattern {
			return nil
		}
	}
	if err = os.MkdirAll(filepath.Dir(rule.Location), 0700); err != nil {
		return err
	}
	f, err := os.OpenFile(rule.Location, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()
	prefix := ""
	if len(data) > 0 && data[len(data)-1] != '\n' {
		prefix = "\n"
	}
	_, err = f.WriteString(prefix + rule.Pattern + "\n")
	return err
}
func (g *gitService) GitIgnorePreview(id, path, mode, value, scope string) (GitIgnoreRule, error) {
	gitMu.Lock()
	defer gitMu.Unlock()
	root, err := g.gitRoot(id)
	if err != nil {
		return GitIgnoreRule{}, err
	}
	return g.gitIgnoreRule(root, path, mode, value, scope)
}
func (g *gitService) GitIgnoreSave(id, path, mode, value, scope string) error {
	gitMu.Lock()
	defer gitMu.Unlock()
	root, err := g.gitRoot(id)
	if err != nil {
		return err
	}
	rule, err := g.gitIgnoreRule(root, path, mode, value, scope)
	if err != nil {
		return err
	}
	return g.appendGitIgnore(rule)
}

type GitLargeFile struct {
	Path string `json:"path"`
	Size int64  `json:"size"`
}

// Read index blobs, not working files: these are the bytes a commit contains.
func (g *gitService) gitLargeFiles(root string) ([]GitLargeFile, error) {
	out, err := g.runGit(root, "diff", "--cached", "--name-only", "--diff-filter=ACMRT", "-z")
	if err != nil {
		return nil, err
	}
	changed := map[string]bool{}
	for _, path := range strings.Split(out, "\x00") {
		changed[path] = true
	}
	// Index entries give stable object IDs and modes. A submodule's commit
	// belongs to another repository and need not exist in this object database.
	index, err := g.runGit(root, "ls-files", "--stage", "-z")
	if err != nil {
		return nil, err
	}
	result := []GitLargeFile{}
	for _, entry := range strings.Split(index, "\x00") {
		metadata, path, ok := strings.Cut(entry, "\t")
		if !ok || !changed[path] {
			continue
		}
		fields := strings.Fields(metadata)
		if len(fields) != 3 || fields[2] != "0" || fields[0] == "160000" {
			continue
		}
		raw, err := g.runGit(root, "cat-file", "-s", fields[1])
		if err != nil {
			return nil, fmt.Errorf("检查暂存文件 %q 失败：%w", path, err)
		}
		size, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
		if err != nil {
			return nil, err
		}
		if size > 10*1024*1024 {
			result = append(result, GitLargeFile{path, size})
		}
	}
	return result, nil
}
func (g *gitService) GitLargeFiles(id string) ([]GitLargeFile, error) {
	gitMu.Lock()
	defer gitMu.Unlock()
	root, err := g.gitRoot(id)
	if err != nil {
		return nil, err
	}
	return g.gitLargeFiles(root)
}

func (g *gitService) validateGitCloneSource(source string) error {
	if source == "" || strings.HasPrefix(source, "-") || strings.ContainsAny(source, "\r\n\x00") {
		return errors.New("请输入有效的仓库地址")
	}
	if filepath.IsAbs(source) {
		return nil
	}
	if strings.Contains(source, "://") {
		u, err := url.Parse(source)
		if err != nil {
			return errors.New("仓库地址格式错误")
		}
		switch u.Scheme {
		case "https", "http", "ssh", "git":
			if u.Hostname() != "" && u.Path != "" {
				return nil
			}
		case "file":
			if u.Path != "" && (u.Host == "" || u.Host == "localhost") {
				return nil
			}
		}
	} else {
		host, path, ok := strings.Cut(source, ":")
		if ok && strings.Contains(host, "@") && !strings.ContainsAny(host, "/\\ ") && path != "" && !strings.HasPrefix(path, ":") {
			return nil
		}
	}
	return errors.New("支持 HTTPS、SSH（git@host:path）、Git URL 或本地绝对路径")
}
func (g *gitService) GitCloneProject(source, parent, name string) (GitProject, error) {
	gitMu.Lock()
	defer gitMu.Unlock()
	source = strings.TrimSpace(source)
	parent = strings.TrimSpace(parent)
	name = strings.TrimSpace(name)
	if err := g.validateGitCloneSource(source); err != nil {
		return GitProject{}, err
	}
	if name == "" || name == "." || name == ".." || strings.ContainsAny(name, "/\\\x00\r\n") || strings.HasPrefix(name, "-") {
		return GitProject{}, errors.New("请输入有效的项目目录名称")
	}
	if !filepath.IsAbs(parent) {
		return GitProject{}, errors.New("请选择保存仓库的绝对目录")
	}
	parent, err := filepath.EvalSymlinks(parent)
	if err != nil {
		return GitProject{}, err
	}
	info, err := os.Stat(parent)
	if err != nil {
		return GitProject{}, err
	}
	if !info.IsDir() {
		return GitProject{}, errors.New("保存位置必须是目录")
	}
	destination := filepath.Join(parent, name)
	// Reserve a new directory; never clone over an existing file, symlink or folder.
	if err = os.Mkdir(destination, 0700); err != nil {
		return GitProject{}, fmt.Errorf("无法创建目标目录（已有目录不会覆盖）：%w", err)
	}
	_, err = g.runGitTimed(parent, "", 5*time.Minute, "-c", "core.sshCommand=ssh -oBatchMode=yes", "clone", "--", source, destination)
	if err != nil {
		return GitProject{}, fmt.Errorf("克隆失败：%w。请检查本机 Git 认证和网络；若目标目录 %s 有残留，可检查后更换目录重试", err, destination)
	}
	project, err := g.gitAddProject(destination)
	if err != nil {
		return project, fmt.Errorf("仓库已克隆到 %s，但添加项目记录失败：%w；可通过本地项目入口重新添加", destination, err)
	}
	return project, nil
}

var gitBinaryPatch = regexp.MustCompile(`(?m)^Binary files .+ differ$`)

type GitReviewFile struct {
	Path string `json:"path"`
	Diff string `json:"diff"`
}
type GitReviewSnapshot struct {
	Scope   string          `json:"scope"`
	Files   []GitReviewFile `json:"files"`
	Skipped []string        `json:"skipped"`
	Created string          `json:"created"`
	Rules   string          `json:"reviewRules,omitempty"`
}

func sensitiveReviewPath(path string) bool {
	base := strings.ToLower(filepath.Base(path))
	return base == ".env" || strings.HasPrefix(base, ".env.") || strings.HasSuffix(base, ".pem") || strings.HasSuffix(base, ".key") || base == "id_rsa" || base == "id_ed25519"
}

func (g *gitService) gitReviewSnapshot(id, scope string) (GitReviewSnapshot, error) {
	gitMu.Lock()
	defer gitMu.Unlock()
	s := GitReviewSnapshot{Scope: scope, Files: []GitReviewFile{}, Skipped: []string{}, Created: time.Now().Format(time.RFC3339)}
	if scope != "all" && scope != "staged" && scope != "worktree" {
		return s, errors.New("请选择检查范围")
	}
	root, err := g.gitRoot(id)
	if err != nil {
		return s, err
	}
	files, err := g.gitFiles(root)
	if err != nil {
		return s, err
	}
	_, headErr := g.runGit(root, "rev-parse", "--verify", "HEAD")
	hasHEAD := headErr == nil
	total := 0
	for _, file := range files {
		if scope == "staged" && (file.Index == " " || file.Index == "?") {
			continue
		}
		if scope == "worktree" && file.Worktree == " " && file.Index != "?" {
			continue
		}
		if file.Conflict || sensitiveReviewPath(file.Path) {
			s.Skipped = append(s.Skipped, file.Path+"（冲突或敏感文件）")
			continue
		}
		var content string
		if file.Index == "?" {
			_, full, e := g.gitChangedFile(root, file.Path)
			if e != nil {
				s.Skipped = append(s.Skipped, file.Path+"（无法读取）")
				continue
			}
			info, e := os.Lstat(full)
			if e != nil || !info.Mode().IsRegular() || info.Size() > 100000 {
				s.Skipped = append(s.Skipped, file.Path+"（非普通文件或超过 100 KB）")
				continue
			}
			f, e := os.Open(full)
			if e != nil {
				return s, e
			}
			data, e := io.ReadAll(io.LimitReader(f, 100001))
			f.Close()
			if e != nil {
				return s, e
			}
			content = "未跟踪的新文件：\n" + string(data)
		} else {
			args := g.gitPatchArgs()
			args = append(args, "--unified=20")
			if scope == "staged" {
				args = append(args, "--cached")
			} else if scope == "all" {
				if hasHEAD {
					args = append(args, "HEAD")
				} else {
					args = append(args, "--cached")
				}
			}
			args = append(args, "--", file.Path)
			if file.OldPath != "" {
				args = append(args, file.OldPath)
			}
			content, err = g.runGit(root, args...)
			if err != nil && strings.Contains(err.Error(), "内容超过 2 MB") {
				s.Skipped = append(s.Skipped, file.Path+"（超过审查大小限制）")
				continue
			}
			if err != nil {
				return s, err
			}
			// In an unborn repository, index-to-worktree differences supplement new staged files.
			if scope == "all" {
				if !hasHEAD {
					extra, e := g.runGit(root, "diff", "--no-ext-diff", "--no-textconv", "--", file.Path)
					if e != nil {
						return s, e
					}
					content += "\n工作区相对暂存区：\n" + extra
				}
			}
		}
		if strings.TrimSpace(content) == "" {
			continue
		}
		if !utf8.ValidString(content) || strings.ContainsRune(content, 0) || gitBinaryPatch.MatchString(content) {
			s.Skipped = append(s.Skipped, file.Path+"（二进制文件）")
			continue
		}
		if len(content) > 100000 || total+len(content) > 450000 || len(s.Files) >= 100 {
			s.Skipped = append(s.Skipped, file.Path+"（超过审查大小限制）")
			continue
		}
		s.Files = append(s.Files, GitReviewFile{file.Path, content})
		total += len(content)
	}
	if len(s.Files) == 0 {
		return s, errors.New("所选范围没有可检查的文本变更（敏感、冲突、二进制或过大文件会跳过）")
	}
	return s, nil
}
