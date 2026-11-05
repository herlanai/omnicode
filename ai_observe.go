package main

import (
	"bytes"
	"context"
	"time"
)

func sameAIFile(a, b *aiFile) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return a.Mode == b.Mode && bytes.Equal(a.Data, b.Data)
}
func sameAIChange(a, b aiChange) bool {
	return a.Path == b.Path && sameAIFile(a.Before, b.Before) && sameAIFile(a.After, b.After)
}
func runAIObserved(ctx context.Context, command, env []string, r *aiReview, events chan<- aiEvent) ([]aiChange, error) {
	result := make(chan error, 1)
	go func() { result <- runAI(ctx, command, env, r.Work, events) }()
	ticker := time.NewTicker(700 * time.Millisecond)
	defer ticker.Stop()
	var latest []aiChange
	for {
		select {
		case err := <-result:
			changes, scanErr := r.scan()
			if scanErr == nil {
				latest = changes
			} else if err == nil {
				err = scanErr
			}
			return latest, err
		case <-ticker.C:
			changes, err := r.scan()
			if err != nil {
				continue
			}
			equal := len(changes) == len(latest)
			if equal {
				for i := range changes {
					if !sameAIChange(changes[i], latest[i]) {
						equal = false
						break
					}
				}
			}
			if equal {
				continue
			}
			latest = changes
			select {
			case events <- aiEvent{changes: changes, scanned: true}:
			case <-ctx.Done():
			}
		}
	}
}
