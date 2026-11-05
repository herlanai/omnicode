package main

import (
	"path/filepath"
	"strings"
	"testing"
)

func pullFixture(t *testing.T, conflict bool) (*gitService, GitProject, string) {
	a, p := gitTestRepo(t)
	gitWrite(t, p.Path, "base", "base\n")
	gitMust(t, p.Path, "add", ".")
	gitMust(t, p.Path, "commit", "-m", "base")
	bare := t.TempDir()
	gitMust(t, bare, "init", "--bare")
	gitMust(t, p.Path, "remote", "add", "origin", bare)
	gitMust(t, p.Path, "push", "origin", "main")
	other := t.TempDir()
	gitMust(t, other, "clone", "-b", "main", bare, ".")
	gitMust(t, other, "config", "user.name", "Test")
	gitMust(t, other, "config", "user.email", "test@example.invalid")
	gitMust(t, other, "config", "commit.gpgsign", "false")
	localFile, remoteFile := "local", "remote"
	if conflict {
		localFile = "base"
		remoteFile = "base"
	}
	gitWrite(t, p.Path, localFile, "local\n")
	gitMust(t, p.Path, "add", ".")
	gitMust(t, p.Path, "commit", "-m", "local commit")
	gitWrite(t, other, remoteFile, "remote\n")
	gitMust(t, other, "add", ".")
	gitMust(t, other, "commit", "-m", "remote commit")
	gitMust(t, other, "push", "origin", "main")
	return a, p, other
}
func TestGitPullStrategies(t *testing.T) {
	for _, mode := range []string{"ff-only", "merge", "rebase"} {
		t.Run(mode, func(t *testing.T) {
			a, p, _ := pullFixture(t, false)
			before := gitMust(t, p.Path, "rev-parse", "HEAD")
			gitMust(t, p.Path, "config", "pull.ff", "only")
			err := a.GitPull(p.ID, "origin", "main", GitPullOptions{Mode: mode, Commit: true, IncludeLog: true})
			if mode == "ff-only" {
				if err == nil || gitMust(t, p.Path, "rev-parse", "HEAD") != before {
					t.Fatal("ff changed divergence", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			parents := strings.Fields(gitMust(t, p.Path, "show", "-s", "--format=%P", "HEAD"))
			if mode == "merge" && len(parents) != 2 {
				t.Fatal(parents)
			}
			if mode == "rebase" && len(parents) != 1 {
				t.Fatal(parents)
			}
			if mode == "merge" && !strings.Contains(gitMust(t, p.Path, "show", "-s", "--format=%B", "HEAD"), "remote commit") {
				t.Fatal("missing merge log")
			}
			branches, e := a.GitRemoteBranches(p.ID, "origin")
			if e != nil || len(branches) != 1 || branches[0] != "main" {
				t.Fatal(branches, e)
			}
		})
	}
}
func TestGitPullNoCommitAndContinue(t *testing.T) {
	a, p, _ := pullFixture(t, false)
	if e := a.GitPull(p.ID, "origin", "main", GitPullOptions{Mode: "merge", Commit: false}); e != nil {
		t.Fatal(e)
	}
	state, e := a.GitSnapshot(p.ID)
	if e != nil || state.Operation != "merge" {
		t.Fatal(state.Operation, e)
	}
	if e = a.GitResolveOperation(p.ID, "continue"); e != nil {
		t.Fatal(e)
	}
	if gitPendingOperation(p.Path) != "" {
		t.Fatal("merge not finished")
	}
}
func TestGitPullRebaseConflictAbortAndContinue(t *testing.T) {
	for _, action := range []string{"abort", "continue"} {
		t.Run(action, func(t *testing.T) {
			a, p, _ := pullFixture(t, true)
			before := gitMust(t, p.Path, "rev-parse", "HEAD")
			if e := a.GitPull(p.ID, "origin", "main", GitPullOptions{Mode: "rebase"}); e == nil {
				t.Fatal("expected conflict")
			}
			state, e := a.GitSnapshot(p.ID)
			if e != nil || state.Operation != "rebase" {
				t.Fatal(state.Operation, e)
			}
			if e = a.GitResolveOperation(p.ID, "continue"); e == nil {
				t.Fatal("continued unresolved conflict")
			}
			if action == "continue" {
				gitWrite(t, p.Path, "base", "resolved\n")
				gitMust(t, p.Path, "add", "--", filepath.Base("base"))
			}
			if e = a.GitResolveOperation(p.ID, action); e != nil {
				t.Fatal(e)
			}
			if gitPendingOperation(p.Path) != "" {
				t.Fatal("rebase still active")
			}
			if action == "abort" && gitMust(t, p.Path, "rev-parse", "HEAD") != before {
				t.Fatal("did not restore branch")
			}
		})
	}
}

func TestGitPullNoFFCreatesMergeOnFastForward(t *testing.T) {
	a, p, _ := pullFixture(t, false)
	// This disposable fixture has one local commit; reset it to the shared base.
	gitMust(t, p.Path, "reset", "--hard", "HEAD~1")
	if e := a.GitPull(p.ID, "origin", "main", GitPullOptions{Mode: "merge", Commit: true, NoFF: true}); e != nil {
		t.Fatal(e)
	}
	if parents := strings.Fields(gitMust(t, p.Path, "show", "-s", "--format=%P", "HEAD")); len(parents) != 2 {
		t.Fatal(parents)
	}
}
