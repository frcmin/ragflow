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

package qnzsagentbot

import "testing"

func TestMapBeginInputs(t *testing.T) {
	dsl := map[string]any{
		"components": map[string]any{
			"begin": map[string]any{
				"obj": map[string]any{
					"component_name": "Begin",
					"params": map[string]any{
						"inputs": map[string]any{
							"question":      map[string]any{"type": "line", "optional": false},
							"customer.name": map[string]any{"type": "line"},
							"order__id":     map[string]any{"type": "line", "optional": true},
							"a.b__c":        map[string]any{"type": "line"},
							"dialog":        map[string]any{"type": "text"},
						},
					},
				},
			},
		},
	}
	body := map[string]any{
		"question": "你好",
		"customer": map[string]any{"name": "张三"},
		"order":    map[string]any{"id": "88"},
		"a":        map[string]any{"b__c": "dot-wins"},
		"dialog": []any{
			map[string]any{"role": "user", "content": "hi"},
		},
	}
	got := MapBeginInputs(BeginInputSchema(dsl), body)
	if got["question"].Value != "你好" || got["question"].Optional || got["question"].Type != "line" {
		t.Fatalf("question: %+v", got["question"])
	}
	if got["customer.name"].Value != "张三" {
		t.Fatalf("dot key: %+v", got["customer.name"])
	}
	if got["order__id"].Value != "88" {
		t.Fatalf("dunder key: %+v", got["order__id"])
	}
	if got["a.b__c"].Value != "dot-wins" {
		t.Fatalf("dot wins over dunder: %+v", got["a.b__c"])
	}
	if got["dialog"].Value != "" {
		t.Fatalf("dialog input must stay empty: %+v", got["dialog"])
	}
	if len(got["question"].Options) != 0 {
		t.Fatalf("options: %+v", got["question"].Options)
	}

	turns, present, err := DialogFromBody(body)
	if err != nil || !present || len(turns) != 1 || turns[0].Role != "user" || turns[0].Content != "hi" {
		t.Fatalf("dialog history: %+v present=%v err=%v", turns, present, err)
	}
	_, present, err = DialogFromBody(map[string]any{"query": "x"})
	if err != nil || present {
		t.Fatalf("absent dialog: present=%v err=%v", present, err)
	}
}

func TestBeginSchemaFallsBackToComponentName(t *testing.T) {
	dsl := map[string]any{
		"components": map[string]any{
			"node-1": map[string]any{
				"obj": map[string]any{
					"component_name": "Begin",
					"params": map[string]any{
						"inputs": map[string]any{
							"city": map[string]any{"type": "line"},
						},
					},
				},
			},
		},
	}
	got := MapBeginInputs(BeginInputSchema(dsl), map[string]any{"city": "上海"})
	if got["city"].Value != "上海" {
		t.Fatalf("named Begin: %+v", got["city"])
	}
}

func TestStreamRequested(t *testing.T) {
	if !StreamRequested(nil) || !StreamRequested(map[string]any{}) {
		t.Fatal("default stream")
	}
	if StreamRequested(map[string]any{"stream": false}) {
		t.Fatal("explicit false")
	}
	if StreamRequested(map[string]any{"stream": float64(0)}) {
		t.Fatal("numeric 0")
	}
	if !StreamRequested(map[string]any{"stream": true}) {
		t.Fatal("explicit true")
	}
}
