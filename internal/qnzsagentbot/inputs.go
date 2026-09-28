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

import "strings"

// BeginInput is one begin-component field after request mapping.
type BeginInput struct {
	Name     string `json:"name"`
	Optional bool   `json:"optional"`
	Options  []any  `json:"options"`
	Type     string `json:"type"`
	Value    any    `json:"value"`
}

// DialogTurn is one history item from the request dialog array.
type DialogTurn struct {
	Role    string
	Content string
}

// BeginInputSchema returns the begin component's input elements.
// Component id "begin" wins. Otherwise the component named Begin is used.
// A canvas without those inputs yields an empty schema.
func BeginInputSchema(dsl map[string]any) map[string]any {
	comp := beginComponent(dsl)
	if comp == nil {
		return map[string]any{}
	}
	obj, _ := comp["obj"].(map[string]any)
	params, _ := obj["params"].(map[string]any)
	inputs, _ := params["inputs"].(map[string]any)
	if inputs == nil {
		return map[string]any{}
	}
	return inputs
}

func beginComponent(dsl map[string]any) map[string]any {
	if dsl == nil {
		return nil
	}
	components, _ := dsl["components"].(map[string]any)
	if components == nil {
		return nil
	}
	if raw, ok := components["begin"]; ok {
		if comp, ok := raw.(map[string]any); ok {
			return comp
		}
	}
	for _, raw := range components {
		comp, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		obj, _ := comp["obj"].(map[string]any)
		name, _ := obj["component_name"].(string)
		if name == "Begin" {
			return comp
		}
	}
	return nil
}

// MapBeginInputs builds the inputs dict passed to canvas.run.
// "dialog" is kept empty. Keys containing "." are read as body[a][b];
// else keys containing "__" use the same two-level lookup. "." wins when
// a key contains both separators. Only the first two segments are used.
func MapBeginInputs(schema map[string]any, body map[string]any) map[string]BeginInput {
	out := make(map[string]BeginInput, len(schema))
	for key, raw := range schema {
		meta, _ := raw.(map[string]any)
		field := BeginInput{
			Name:     key,
			Optional: optionalOf(meta),
			Options:  []any{},
			Type:     typeOf(meta),
		}
		switch {
		case key == "dialog":
			field.Value = ""
		case strings.Contains(key, "."):
			field.Value = nested(body, key, ".")
		case strings.Contains(key, "__"):
			field.Value = nested(body, key, "__")
		default:
			if body != nil {
				field.Value = body[key]
			}
		}
		out[key] = field
	}
	return out
}

func optionalOf(meta map[string]any) bool {
	if meta == nil {
		return true
	}
	value, ok := meta["optional"]
	if !ok || value == nil {
		return true
	}
	flag, ok := value.(bool)
	if !ok {
		return true
	}
	return flag
}

func typeOf(meta map[string]any) string {
	if meta == nil {
		return ""
	}
	text, _ := meta["type"].(string)
	return text
}

func nested(body map[string]any, key, sep string) any {
	parts := strings.Split(key, sep)
	if len(parts) < 2 || body == nil {
		return nil
	}
	parent, ok := body[parts[0]].(map[string]any)
	if !ok {
		return nil
	}
	value, ok := parent[parts[1]]
	if !ok {
		return nil
	}
	return value
}

// DialogFromBody reads the dialog history array. present is false when the
// key is absent, which leaves canvas history at the post-reset empty list.
func DialogFromBody(body map[string]any) (turns []DialogTurn, present bool, err *APIError) {
	if body == nil {
		return nil, false, nil
	}
	raw, ok := body["dialog"]
	if !ok {
		return nil, false, nil
	}
	if raw == nil {
		return []DialogTurn{}, true, nil
	}
	items, ok := raw.([]any)
	if !ok {
		return nil, true, dataError("dialog must be a list.")
	}
	turns = make([]DialogTurn, 0, len(items))
	for _, item := range items {
		obj, ok := item.(map[string]any)
		if !ok {
			return nil, true, dataError("dialog items require role and content.")
		}
		role, roleOK := obj["role"].(string)
		content, contentOK := obj["content"].(string)
		if !roleOK || !contentOK {
			return nil, true, dataError("dialog items require role and content.")
		}
		turns = append(turns, DialogTurn{Role: role, Content: content})
	}
	return turns, true, nil
}

// RequestQuery prefers query, then question.
func RequestQuery(body map[string]any) string {
	if body == nil {
		return ""
	}
	if text, ok := body["query"].(string); ok && text != "" {
		return text
	}
	text, _ := body["question"].(string)
	return text
}

// RequestUserID returns the user_id string, or empty.
func RequestUserID(body map[string]any) string {
	if body == nil {
		return ""
	}
	text, _ := body["user_id"].(string)
	return text
}

// StreamRequested defaults to true. Missing stream is streaming.
// Python truthiness applies to a present value: false, 0, and "" are off.
func StreamRequested(body map[string]any) bool {
	if body == nil {
		return true
	}
	value, ok := body["stream"]
	if !ok {
		return true
	}
	return truthy(value)
}

func truthy(value any) bool {
	switch typed := value.(type) {
	case nil:
		return false
	case bool:
		return typed
	case float64:
		return typed != 0
	case string:
		return typed != ""
	case []any:
		return len(typed) > 0
	case map[string]any:
		return len(typed) > 0
	default:
		return true
	}
}
