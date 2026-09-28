//
//  Copyright 2026 The InfiniFlow Authors. All Rights Reserved.
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.
//

package service

import (
	"strings"
	"testing"

	"ragflow/internal/qnzsagentbot"
)

func TestPrepareQNZSDSLResetsThenAppliesDialog(t *testing.T) {
	dsl := map[string]any{
		"history": []any{[]any{"user", "old"}},
		"path":    []any{"begin_0"},
		"memory":  []any{map[string]any{"user": "old"}},
		"globals": map[string]any{"sys.query": "stale", "env.keep": "yes"},
		"components": map[string]any{
			"begin_0": map[string]any{
				"obj": map[string]any{"component_name": "Begin", "params": map[string]any{}},
			},
		},
	}
	got := prepareQNZSDSL(qnzsagentbot.RunRequest{
		HasDialog: true,
		Dialog: []qnzsagentbot.DialogTurn{
			{Role: "user", Content: "之前的问题"},
			{Role: "assistant", Content: "之前的回答"},
		},
		DSL: dsl,
	})
	history, _ := got["history"].([]any)
	if len(history) != 2 {
		t.Fatalf("history = %#v", got["history"])
	}
	first, _ := history[0].([]any)
	if len(first) != 2 || first[0] != "user" || first[1] != "之前的问题" {
		t.Fatalf("first turn = %#v", history[0])
	}
	if path, _ := got["path"].([]any); len(path) != 0 {
		t.Fatalf("path = %#v", got["path"])
	}
	if _, ok := dsl["history"].([]any); !ok || len(dsl["history"].([]any)) != 1 {
		t.Fatal("prepare mutated the caller's DSL")
	}
}

func TestRunQNZSCanvasUsesExistingRunner(t *testing.T) {
	svc := NewAgentService()
	events, err := svc.RunQNZSCanvas(t.Context(), qnzsagentbot.RunRequest{
		TenantID: "tenant-1",
		AgentID:  "canvas-qnzs",
		Query:    "world",
		DSL: map[string]any{
			"history": []any{[]any{"user", "should-reset"}},
			"components": map[string]any{
				"begin_0": map[string]any{
					"obj":        map[string]any{"component_name": "Begin", "params": map[string]any{}},
					"downstream": []any{"message_0"},
				},
				"message_0": map[string]any{
					"obj": map[string]any{
						"component_name": "Message",
						"params":         map[string]any{"text": "hello {{sys.query}}"},
					},
					"upstream": []any{"begin_0"},
				},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	var contents []string
	for event := range events {
		if text, ok := qnzsagentbot.ContentFromCanvasEvent(event.Type, event.Data); ok {
			contents = append(contents, text)
		}
	}
	joined := strings.Join(contents, "")
	if !strings.Contains(joined, "hello world") {
		t.Fatalf("contents = %#v", contents)
	}
}
