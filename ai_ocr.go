package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/pmezard/go-difflib/difflib"
)

func reviewLines(content string) []string {
	if content == "" {
		return nil
	}
	lines := strings.SplitAfter(content, "\n")
	last := len(lines) - 1
	if lines[last] == "" {
		return lines[:last]
	}
	lines[last] += "\n"
	return lines
}

// Compare the task's captured before/after images, including unapproved changes.
func assistantReviewSnapshot(changes []aiChange) GitReviewSnapshot {
	s := GitReviewSnapshot{Scope: "ai-task", Created: time.Now().Format(time.RFC3339)}
	total := 0
	for _, change := range changes {
		reason := ""
		var old, next string
		for i, f := range []*aiFile{change.Before, change.After} {
			if f == nil {
				continue
			}
			if sensitiveReviewPath(change.Path) || len(f.Data) > 100000 || !utf8.Valid(f.Data) || bytes.IndexByte(f.Data, 0) >= 0 {
				reason = "敏感、二进制或过大文件"
				break
			}
			if i == 0 {
				old = string(f.Data)
			} else {
				next = string(f.Data)
			}
		}
		if reason == "" && (len(s.Files) >= 100 || total >= 450000) {
			reason = "超过审查大小限制"
		}
		if reason == "" {
			diff, _ := difflib.GetUnifiedDiffString(difflib.UnifiedDiff{
				A: reviewLines(old), B: reviewLines(next),
				FromFile: change.Path, ToFile: change.Path, Context: 3,
			})
			switch {
			case diff == "":
				reason = "没有文本差异"
			case len(diff) > 100000 || total+len(diff) > 450000:
				reason = "超过审查大小限制"
			default:
				s.Files = append(s.Files, GitReviewFile{Path: change.Path, Diff: diff})
				total += len(diff)
			}
		}
		if reason != "" {
			s.Skipped = append(s.Skipped, change.Path+"（"+reason+"）")
		}
	}
	return s
}

func reviewAssistantChanges(ctx context.Context, prefs preferences, review *aiReview, changes []aiChange, events chan<- aiEvent) error {
	emit := func(ev aiEvent) {
		select {
		case events <- ev:
		case <-ctx.Done():
		}
	}
	emit(aiEvent{text: "\n\n### Open Code Review\n\n"})
	s := assistantReviewSnapshot(changes)
	if len(s.Files) == 0 {
		emit(aiEvent{text: "本次变更没有可审查的文本差异。"})
		return nil
	}
	if err := matchEditorReviewRules(ctx, review.Root, &s); err != nil {
		return err
	}
	result, err := runGitSnapshotAI(ctx, prefs, review.Root, s, false, "Apply the matched reviewRules to the supplied AI-generated changes.", emit)
	if err != nil {
		return fmt.Errorf("Open Code Review 审查失败：%w", err)
	}
	if result == "" {
		return fmt.Errorf("Open Code Review 未返回审查结果")
	}
	return nil
}

func matchEditorReviewRules(ctx context.Context, root string, s *GitReviewSnapshot) error {
	// The approval copy omits .git. Match rules in the original project,
	// using only the captured paths. OCR also requires a repo for built-in
	// rules, so give plain folders a disposable one.
	ruleRoot := root
	if _, err := repositoryRoot(ctx, ruleRoot); err != nil {
		ruleRoot, err = os.MkdirTemp("", "ocr-rules-")
		if err != nil {
			return err
		}
		defer os.RemoveAll(ruleRoot)
		if _, err = repositoryGit(ctx, ruleRoot, "init", "--quiet"); err != nil {
			return fmt.Errorf("Open Code Review 初始化失败：%w", err)
		}
	}
	if err := enrichOCRSnapshot(ctx, ruleRoot, s); err != nil {
		return fmt.Errorf("Open Code Review 规则匹配失败：%w", err)
	}
	return nil
}
