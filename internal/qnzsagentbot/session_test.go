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

func TestResolveSessionID(t *testing.T) {
	got, err := ResolveSessionID(map[string]any{"session_id": "body-sid"}, "header-sid", "agent-1")
	if err != nil || got != "body-sid-agent-1" {
		t.Fatalf("body: %q %v", got, err)
	}
	got, err = ResolveSessionID(map[string]any{}, "header-sid", "agent-1")
	if err != nil || got != "header-sid-agent-1" {
		t.Fatalf("header: %q %v", got, err)
	}
	got, err = ResolveSessionID(map[string]any{"Session-Id": "field-sid"}, "", "agent-1")
	if err != nil || got != "field-sid-agent-1" {
		t.Fatalf("body Session-Id: %q %v", got, err)
	}
	got, err = ResolveSessionID(map[string]any{"session_id": ""}, "header-sid", "ag")
	if err != nil || got != "header-sid-ag" {
		t.Fatalf("blank body falls through: %q %v", got, err)
	}
	if _, err := ResolveSessionID(map[string]any{}, "", "ag"); err == nil {
		t.Fatal("expected missing session_id error")
	}
	if _, err := ResolveSessionID(map[string]any{"session_id": 12}, "header", "ag"); err == nil {
		t.Fatal("expected type error")
	}
}
