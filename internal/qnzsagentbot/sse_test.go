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

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestFormatChunk(t *testing.T) {
	frame, err := FormatChunk("chatcmpl-abc", 1_700_000_000, "你好\n世界")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(frame, "data: ") || !strings.HasSuffix(frame, "\n\n") {
		t.Fatalf("frame framing: %q", frame)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSuffix(strings.TrimPrefix(frame, "data: "), "\n\n")), &payload); err != nil {
		t.Fatal(err)
	}
	if payload["object"] != "chat.completion.chunk" || payload["model"] != "agent-bot" || payload["id"] != "chatcmpl-abc" {
		t.Fatalf("payload identity: %#v", payload)
	}
	if strings.Contains(frame, `\u4f60`) {
		t.Fatalf("content was escaped: %s", frame)
	}
	choices := payload["choices"].([]any)
	choice := choices[0].(map[string]any)
	if choice["finish_reason"] != nil {
		t.Fatalf("finish_reason: %#v", choice["finish_reason"])
	}
	delta := choice["delta"].(map[string]any)
	if delta["role"] != "assistant" || delta["content"] != "你好\n世界" {
		t.Fatalf("delta: %#v", delta)
	}
}

func TestSSEHeaders(t *testing.T) {
	header := make(http.Header)
	SetSSEHeaders(header)
	if header.Get("Cache-Control") != "no-cache" {
		t.Fatalf("cache: %q", header.Get("Cache-Control"))
	}
	if header.Get("Connection") != "keep-alive" {
		t.Fatalf("connection: %q", header.Get("Connection"))
	}
	if header.Get("X-Accel-Buffering") != "no" {
		t.Fatalf("buffering: %q", header.Get("X-Accel-Buffering"))
	}
	if header.Get("Content-Type") != "text/event-stream; charset=utf-8" {
		t.Fatalf("content-type: %q", header.Get("Content-Type"))
	}
}

func TestFormatCompletion(t *testing.T) {
	payload, err := FormatCompletion("chatcmpl-1", 10, "全部内容")
	if err != nil {
		t.Fatal(err)
	}
	if payload["object"] != "chat.completion" || payload["model"] != modelName {
		t.Fatalf("%#v", payload)
	}
	choice := payload["choices"].([]any)[0].(map[string]any)
	if choice["finish_reason"] != "stop" {
		t.Fatalf("finish: %#v", choice["finish_reason"])
	}
	message := choice["message"].(map[string]any)
	if message["content"] != "全部内容" || message["role"] != "assistant" {
		t.Fatalf("message: %#v", message)
	}
}
