//go:build !darwin

package main

import "fmt"

func sandboxAICommand(command []string, r *aiReview, engine string) ([]string, []string, error) {
	return nil, nil, fmt.Errorf("当前平台尚不支持隔离审批模式")
}
