package main

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestGitBranchActions(t *testing.T) {
	a, p := gitTestRepo(t)
	gitWrite(t, p.Path, "base", "base\n")
	gitMust(t, p.Path, "add", ".")
	gitMust(t, p.Path, "commit", "-m", "initial")
	gitMust(t, p.Path, "switch", "-c", "feature")
	gitWrite(t, p.Path, "feature", "feature\n")
	gitMust(t, p.Path, "add", ".")
	gitMust(t, p.Path, "commit", "-m", "feature")
	gitMust(t, p.Path, "switch", "main")
	out, e := a.GitBranchAction(p.ID, "refs/heads/feature", "compare", "", "main")
	if e != nil || !strings.Contains(out, "+feature") {
		t.Fatal(out, e)
	}
	if _, e = a.GitBranchAction(p.ID, "refs/heads/feature", "delete", "", "main"); e == nil {
		t.Fatal("deleted unmerged branch")
	}
	if _, e = a.GitBranchAction(p.ID, "refs/heads/feature", "rename", "new-name", "stale"); e == nil {
		t.Fatal("accepted stale branch")
	}
	if _, e = a.GitBranchAction(p.ID, "refs/heads/feature", "merge", "", "main"); e != nil {
		t.Fatal(e)
	}
	if _, e = a.GitBranchAction(p.ID, "refs/heads/feature", "rename", "new-name", "main"); e != nil {
		t.Fatal(e)
	}
	if _, e = a.GitBranchAction(p.ID, "refs/heads/new-name", "delete", "", "main"); e != nil {
		t.Fatal(e)
	}
	if _, e = a.GitBranchAction(p.ID, "refs/heads/main", "delete", "", "main"); e == nil {
		t.Fatal("deleted current branch")
	}
	if _, e = a.GitBranchAction(p.ID, "HEAD", "merge", "", "main"); e == nil {
		t.Fatal("accepted invalid ref")
	}
}
func TestGitBranchPushUsesSelectedBranchAndRemoteCheckout(t *testing.T) {
	a, p := gitTestRepo(t)
	gitWrite(t, p.Path, "base", "base")
	gitMust(t, p.Path, "add", ".")
	gitMust(t, p.Path, "commit", "-m", "initial")
	gitMust(t, p.Path, "branch", "feature")
	feature := strings.TrimSpace(gitMust(t, p.Path, "rev-parse", "feature"))
	gitWrite(t, p.Path, "base", "main changed")
	gitMust(t, p.Path, "commit", "-am", "main change")
	remote := filepath.Join(t.TempDir(), "remote.git")
	gitMust(t, p.Path, "init", "--bare", remote)
	gitMust(t, p.Path, "remote", "add", "origin", remote)
	if _, e := a.GitBranchAction(p.ID, "refs/heads/feature", "push", "origin:release", "main"); e != nil {
		t.Fatal(e)
	}
	if got := strings.TrimSpace(gitMust(t, remote, "rev-parse", "refs/heads/release")); got != feature {
		t.Fatal("pushed HEAD instead of selected branch")
	}
	gitMust(t, p.Path, "fetch", "origin")
	if _, e := a.GitBranchAction(p.ID, "refs/heads/feature", "track", "refs/remotes/origin/release", "main"); e != nil {
		t.Fatal(e)
	}
	if _, e := a.GitBranchAction(p.ID, "refs/remotes/origin/release", "checkout", "local-release", "main"); e != nil {
		t.Fatal(e)
	}
	if got := strings.TrimSpace(gitMust(t, p.Path, "symbolic-ref", "--short", "HEAD")); got != "local-release" {
		t.Fatal(got)
	}
}
func TestGitBranchRebasePreservesChanges(t *testing.T) {
	a, p := gitTestRepo(t)
	gitWrite(t, p.Path, "base", "base")
	gitMust(t, p.Path, "add", ".")
	gitMust(t, p.Path, "commit", "-m", "base")
	gitMust(t, p.Path, "switch", "-c", "feature")
	gitWrite(t, p.Path, "feature", "new")
	gitMust(t, p.Path, "add", ".")
	gitMust(t, p.Path, "commit", "-m", "feature")
	gitMust(t, p.Path, "switch", "main")
	gitWrite(t, p.Path, "main", "main")
	gitMust(t, p.Path, "add", ".")
	gitMust(t, p.Path, "commit", "-m", "main")
	gitMust(t, p.Path, "switch", "feature")
	if _, e := a.GitBranchAction(p.ID, "refs/heads/main", "rebase", "", "feature"); e != nil {
		t.Fatal(e)
	}
	gitMust(t, p.Path, "merge-base", "--is-ancestor", "main", "feature")
}
