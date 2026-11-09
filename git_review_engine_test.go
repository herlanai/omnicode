package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestReviewUsesSelectedBuiltinEngine(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer review-key" {
			t.Error("review did not use configured key")
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"review-ok\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
	}))
	defer server.Close()
	for _, scope := range []string{"current-file", "all"} {
		t.Run(scope, func(t *testing.T) {
			a := newApp(t.TempDir())
			a.prefs.Engine = "CodeBuddy CLI"
			a.prefs.BuddyPath = "/nonexistent/codebuddy"
			a.prefs.Models[0].APIKey = "review-key"
			a.prefs.Models[0].BaseURL = server.URL
			a.ai.engine = builtinEngine
			r := &gitReviewUI{root: a.root, scope: scope, fileSnapshot: &GitReviewSnapshot{Scope: scope, Files: []GitReviewFile{{Path: "file.go", Diff: "package main"}}}}
			a.startGitReview(r)
			defer r.cancel()
			// Subsequent UI choices must not change the running review's engine.
			a.ai.engine = "DSH"
			if r.engine != builtinEngine {
				t.Fatalf("review captured %q", r.engine)
			}
			var output strings.Builder
			timer := time.NewTimer(10 * time.Second)
			defer timer.Stop()
			for {
				select {
				case ev := <-r.events:
					output.WriteString(ev.text)
					if ev.done {
						if ev.err != nil || output.String() != "review-ok" {
							t.Fatalf("result %q: %v", output.String(), ev.err)
						}
						return
					}
				case <-timer.C:
					t.Fatal("review did not complete")
				}
			}
		})
	}
}
