package main

// Git semantics follow poprsi's status/staging workflow; the application owns
// one currently opened root, with no project registry or frontend bridge.
import (
	"bytes"
	"context"
	"errors"
	"fmt"

	"os"
	"os/exec"
	"path/filepath"

	"strings"
	"time"
	"unicode/utf8"
)

type repositoryFile struct {
	Path, OldPath, Index, Worktree string
	Conflict                       bool
}
type repositoryCommit struct{ Hash, Subject, Author, Date string }
type repositoryStash struct{ Hash, Label string }
type repositoryState struct {
	Advanced                                GitState
	Root, Branch, Head, Upstream, Operation string
	Files                                   []repositoryFile
	Branches, Remotes                       []string
	History                                 []repositoryCommit
	Stashes                                 []repositoryStash
	Ahead, Behind                           int
}
type gitLimitedBuffer struct {
	bytes.Buffer
	overflow bool
}

func (b *gitLimitedBuffer) Write(p []byte) (int, error) {
	n := len(p)
	room := (2 << 20) - b.Len()
	if len(p) > room {
		p = p[:room]
		b.overflow = true
	}
	_, _ = b.Buffer.Write(p)
	return n, nil
}
func repositoryGit(ctx context.Context, root string, args ...string) (string, error) {
	return repositoryGitInput(ctx, root, "", nil, args...)
}
func repositoryGitInput(ctx context.Context, root, input string, progress func(string), args ...string) (string, error) {
	timeout := gitCommandTimeout(args)
	if d, ok := ctx.Value(gitTimeoutKey{}).(time.Duration); ok {
		timeout = d
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	if progress == nil {
		progress, _ = ctx.Value(gitProgressKey{}).(func(string))
	}
	if len(args) > 0 && (args[0] == "push" || args[0] == "pull" || args[0] == "fetch") {
		args = append([]string{args[0], "--progress"}, args[1:]...)
	}
	defer cancel()
	all := append([]string{"--no-pager", "--literal-pathspecs", "-c", "color.ui=false", "-c", "core.quotepath=false", "-C", root}, args...)
	cmd := exec.CommandContext(ctx, "git", all...)
	configureAIProcess(cmd)
	cmd.WaitDelay = time.Second
	for _, v := range os.Environ() {
		if !strings.HasPrefix(v, "GIT_") {
			cmd.Env = append(cmd.Env, v)
		}
	}
	cmd.Env = append(cmd.Env, "GIT_TERMINAL_PROMPT=0", "GIT_OPTIONAL_LOCKS=0", "GIT_EDITOR=true", "GIT_MERGE_AUTOEDIT=no", "LC_ALL=C")
	var out, stderr gitLimitedBuffer
	cmd.Stdout = &out
	cmd.Stdin = strings.NewReader(input)
	cmd.Stderr = &gitProgressWriter{buffer: &stderr, emit: progress}
	e := cmd.Run()
	if ctx.Err() != nil {
		return "", fmt.Errorf("Git 操作已停止或超时，请刷新确认结果")
	}
	if e != nil {
		return "", fmt.Errorf("%s", gitProgressText(stderr.String()+" "+e.Error()))
	}
	if out.overflow {
		return "", errors.New("内容超过 2 MB，请使用终端查看")
	}
	return out.String(), nil
}
func repositoryRoot(ctx context.Context, root string) (string, error) {
	if root == "" {
		return "", errors.New("请先打开项目文件夹")
	}
	root, e := filepath.EvalSymlinks(root)
	if e != nil {
		return "", e
	}
	raw, e := repositoryGit(ctx, root, "rev-parse", "--show-toplevel")
	if e != nil {
		return "", errors.New("当前文件夹不是 Git 仓库")
	}
	actual, e := filepath.EvalSymlinks(strings.TrimSpace(raw))
	if e != nil {
		return "", e
	}
	if actual != root {
		return "", fmt.Errorf("请打开仓库根目录：%s", actual)
	}
	return root, nil
}
func parseRepositoryFiles(raw string) []repositoryFile {
	var files []repositoryFile
	parts := strings.Split(raw, "\x00")
	for i := 0; i < len(parts); i++ {
		v := parts[i]
		if len(v) < 4 {
			continue
		}
		f := repositoryFile{Path: v[3:], Index: v[:1], Worktree: v[1:2], Conflict: strings.Contains(v[:2], "U") || v[:2] == "AA" || v[:2] == "DD"}
		if strings.ContainsAny(v[:2], "RC") && i+1 < len(parts) {
			i++
			f.OldPath = parts[i]
		}
		files = append(files, f)
	}
	return files
}
func repositoryFiles(ctx context.Context, root string) ([]repositoryFile, error) {
	raw, e := repositoryGit(ctx, root, "status", "--porcelain=v1", "-z", "--untracked-files=all")
	return parseRepositoryFiles(raw), e
}
func readRepository(ctx context.Context, root string) (repositoryState, error) {
	s := repositoryState{}
	g := &gitService{ctx: ctx, root: root}
	v, e := g.GitSnapshot(root)
	if e != nil {
		return s, e
	}
	s.Advanced = v
	s.Root = root
	s.Branch = v.Branch
	s.Head = v.Head
	s.Upstream = v.Upstream
	s.Operation = v.Operation
	s.Ahead = v.Ahead
	s.Behind = v.Behind
	for _, f := range v.Files {
		s.Files = append(s.Files, repositoryFile{f.Path, f.OldPath, f.Index, f.Worktree, f.Conflict})
	}
	for _, b := range v.Branches {
		if !b.Remote {
			s.Branches = append(s.Branches, b.Name)
		}
	}
	for _, r := range v.Remotes {
		s.Remotes = append(s.Remotes, r.Name)
	}
	for _, h := range v.History {
		s.History = append(s.History, repositoryCommit{h.Hash, h.Subject, h.Author, h.Date})
	}
	for _, v := range v.Stashes {
		s.Stashes = append(s.Stashes, repositoryStash{v.Hash, v.Subject})
	}
	return s, nil
}
func nonemptyLines(raw string) []string {
	var out []string
	for _, s := range strings.Split(strings.TrimSuffix(raw, "\n"), "\n") {
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}
func validRepositoryPath(root, path string) error {
	if path == "" || filepath.IsAbs(path) || !inside(root, filepath.Join(root, path)) || path == "." {
		return errors.New("无效文件路径")
	}
	return nil
}
func repositoryStage(ctx context.Context, root, path string, stage bool) error {
	files, e := repositoryFiles(ctx, root)
	if e != nil {
		return e
	}
	var paths []string
	for _, f := range files {
		if path != "" && path != f.Path {
			continue
		}
		if stage && f.Worktree == " " {
			continue
		}
		if !stage && (f.Index == " " || f.Index == "?") {
			continue
		}
		if e = validRepositoryPath(root, f.Path); e != nil {
			return e
		}
		paths = append(paths, f.Path)
		if f.OldPath != "" && (!stage || strings.ContainsAny(f.Worktree, "RC")) {
			paths = append(paths, f.OldPath)
		}
	}
	if len(paths) == 0 {
		return errors.New("没有可操作的变更，请刷新")
	}
	args := []string{"add", "--"}
	if !stage {
		if _, e = repositoryGit(ctx, root, "rev-parse", "--verify", "HEAD"); e == nil {
			args = []string{"reset", "-q", "HEAD", "--"}
		} else {
			args = []string{"rm", "--cached", "-f", "--ignore-unmatch", "--"}
		}
	}
	_, e = repositoryGit(ctx, root, append(args, paths...)...)
	return e
}
func repositoryClean(ctx context.Context, root string) error {
	files, e := repositoryFiles(ctx, root)
	if e != nil {
		return e
	}
	if len(files) > 0 {
		return errors.New("请先提交或贮藏当前修改")
	}
	return nil
}
func repositoryAction(ctx context.Context, root, kind, value, extra string) error {
	if kind == "init" {
		if root == "" {
			return errors.New("请先打开项目")
		}
		if _, err := repositoryGit(ctx, root, "rev-parse", "--show-toplevel"); err == nil {
			return errors.New("当前目录已属于一个仓库，请打开仓库根目录")
		}
		_, err := repositoryGit(ctx, root, "init")
		return err
	}
	actual, e := repositoryRoot(ctx, root)
	if e != nil {
		return e
	}
	root = actual
	switch kind {
	case "stage", "unstage":
		return repositoryStage(ctx, root, value, kind == "stage")
	case "commit":
		if strings.TrimSpace(value) == "" || len(value) > 20000 {
			return errors.New("请填写提交说明（不超过 20,000 字节）")
		}
		files, err := repositoryFiles(ctx, root)
		if err != nil {
			return err
		}
		staged := false
		for _, f := range files {
			if f.Conflict {
				return errors.New("请先解决冲突并暂存")
			}
			staged = staged || f.Index != " " && f.Index != "?"
		}
		if !staged {
			return errors.New("没有已暂存的修改")
		}
		_, e = repositoryGit(ctx, root, "commit", "-m", value)
	case "switch", "branch", "merge":
		if strings.HasPrefix(value, "-") || value == "" {
			return errors.New("无效分支名称")
		}
		if _, e = repositoryGit(ctx, root, "check-ref-format", "--branch", value); e != nil {
			return e
		}
		if e = repositoryClean(ctx, root); e != nil {
			return e
		}
		if kind == "branch" {
			_, e = repositoryGit(ctx, root, "switch", "-c", value)
		} else {
			if _, e = repositoryGit(ctx, root, "show-ref", "--verify", "refs/heads/"+value); e != nil {
				return errors.New("本地分支不存在")
			}
			if kind == "switch" {
				_, e = repositoryGit(ctx, root, "switch", "--no-guess", value)
			} else {
				_, e = repositoryGit(ctx, root, "merge", "--no-edit", "refs/heads/"+value)
			}
		}
	case "fetch", "pull", "push":
		remotes, err := repositoryGit(ctx, root, "remote")
		if err != nil {
			return err
		}
		found := false
		for _, r := range nonemptyLines(remotes) {
			if r == value {
				found = true
			}
		}
		if !found || strings.HasPrefix(value, "-") {
			return errors.New("请选择已配置的远端")
		}
		if kind == "fetch" {
			_, e = repositoryGit(ctx, root, "fetch", "--", value)
			return e
		}
		if extra == "" || strings.HasPrefix(extra, "-") {
			return errors.New("请输入目标分支")
		}
		if _, e = repositoryGit(ctx, root, "check-ref-format", "refs/heads/"+extra); e != nil {
			return e
		}
		if kind == "pull" {
			if e = repositoryClean(ctx, root); e != nil {
				return e
			}
			_, e = repositoryGit(ctx, root, "pull", "--ff-only", "--", value, extra)
		} else {
			_, e = repositoryGit(ctx, root, "push", "--set-upstream", "--", value, "HEAD:refs/heads/"+extra)
		}
	case "stash":
		_, e = repositoryGit(ctx, root, "stash", "push", "--include-untracked", "-m", value)
	case "stash-apply":
		raw, err := repositoryGit(ctx, root, "stash", "list", "--format=%H")
		if err != nil {
			return err
		}
		found := false
		for _, hash := range nonemptyLines(raw) {
			if hash == value {
				found = true
			}
		}
		if !found {
			return errors.New("贮藏已变化，请刷新")
		}
		if e = repositoryClean(ctx, root); e != nil {
			return e
		}
		_, e = repositoryGit(ctx, root, "stash", "apply", value)
	case "discard":
		files, err := repositoryFiles(ctx, root)
		if err != nil {
			return err
		}
		found := false
		for _, f := range files {
			if f.Path != value {
				continue
			}
			if f.Conflict || f.Index == "?" || f.Worktree == " " {
				return errors.New("只支持丢弃已跟踪文件的未暂存修改；新文件请在文件列表移到废纸篓")
			}
			found = true
		}
		if !found {
			return errors.New("文件状态已变化")
		}
		if e = validRepositoryPath(root, value); e != nil {
			return e
		}
		_, e = repositoryGit(ctx, root, "restore", "--worktree", "--", value)
	case "remote":
		if value == "" || strings.ContainsAny(value, " \t\n\\") || strings.HasPrefix(value, "-") {
			return errors.New("无效远端名称")
		}
		if extra == "" || strings.ContainsAny(extra, "\r\n\x00") || strings.HasPrefix(extra, "-") {
			return errors.New("无效远端地址")
		}
		raw, err := repositoryGit(ctx, root, "remote")
		if err != nil {
			return err
		}
		action := "add"
		for _, r := range nonemptyLines(raw) {
			if r == value {
				action = "set-url"
			}
		}
		_, e = repositoryGit(ctx, root, "remote", action, value, extra)
	case "identity":
		if strings.TrimSpace(value) == "" || strings.TrimSpace(extra) == "" {
			return errors.New("请填写作者姓名和邮箱")
		}
		if _, e = repositoryGit(ctx, root, "config", "--local", "user.name", value); e == nil {
			_, e = repositoryGit(ctx, root, "config", "--local", "user.email", extra)
		}
	default:
		return errors.New("不支持的 Git 操作")
	}
	return e
}
func repositoryBlob(ctx context.Context, root, ref, path string) (string, error) {
	if e := validRepositoryPath(root, path); e != nil {
		return "", e
	}
	if _, e := repositoryGit(ctx, root, "cat-file", "-e", ref+":"+path); e != nil {
		return "", nil
	}
	return repositoryGit(ctx, root, "show", "--no-ext-diff", ref+":"+path)
}
func repositoryDiff(ctx context.Context, root string, f repositoryFile, staged bool) (string, string, error) {
	canonical, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", "", err
	}
	root = canonical
	if e := validRepositoryPath(root, f.Path); e != nil {
		return "", "", e
	}
	oldPath := f.Path
	if staged && f.OldPath != "" {
		oldPath = f.OldPath
	}
	var before, after string
	var e error
	if staged {
		before, e = repositoryBlob(ctx, root, "HEAD", oldPath)
		if e == nil {
			after, e = repositoryBlob(ctx, root, "", f.Path)
		}
	} else {
		before, e = repositoryBlob(ctx, root, "", oldPath)
		if e == nil {
			path := filepath.Join(root, f.Path)
			var info os.FileInfo
			info, e = os.Lstat(path)
			if os.IsNotExist(e) {
				e = nil
			} else if e == nil {
				resolved, err := filepath.EvalSymlinks(filepath.Dir(path))
				if err != nil || !inside(root, resolved) {
					return "", "", errors.New("文件位于仓库外")
				}
				if !info.Mode().IsRegular() || info.Size() > 2<<20 {
					return "", "", errors.New("二进制、大文件、符号链接或子模块请使用终端查看")
				}
				var b []byte
				b, e = os.ReadFile(path)
				after = string(b)
			}
		}
	}
	if e == nil && (!utf8.ValidString(before) || !utf8.ValidString(after) || strings.ContainsRune(before+after, 0)) {
		e = errors.New("二进制文件，无法显示文本差异")
	}
	return before, after, e
}
func repositoryHistoryPatch(ctx context.Context, root, hash string) (string, error) {
	if len(hash) != 40 && len(hash) != 64 {
		return "", errors.New("无效提交")
	}
	for _, r := range hash {
		if !strings.ContainsRune("0123456789abcdef", r) {
			return "", errors.New("无效提交")
		}
	}
	return repositoryGit(ctx, root, "show", "--no-ext-diff", "--no-textconv", "--format=fuller", "--stat", "--patch", hash, "--")
}
