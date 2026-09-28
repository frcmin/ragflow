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

// ResolveSessionID reads session_id from the JSON body, then the Session-Id
// header, then a body field named Session-Id. The result is always suffixed
// with "-" and the agent id.
//
// A missing or blank session_id falls through to the next source. The Python
// reference crashes when every source is missing; this returns an error instead.
func ResolveSessionID(body map[string]any, headerSessionID, agentID string) (string, *APIError) {
	raw, ok, err := takeSession(body, "session_id")
	if err != nil {
		return "", err
	}
	if !ok {
		raw = strings.TrimSpace(headerSessionID)
	}
	if raw == "" {
		raw, _, err = takeSession(body, "Session-Id")
		if err != nil {
			return "", err
		}
	}
	if raw == "" {
		return "", dataError("session_id is required.")
	}
	return raw + "-" + agentID, nil
}

func takeSession(body map[string]any, key string) (string, bool, *APIError) {
	if body == nil {
		return "", false, nil
	}
	value, exists := body[key]
	if !exists || value == nil {
		return "", false, nil
	}
	text, ok := value.(string)
	if !ok {
		return "", false, dataError(key + " must be a string.")
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return "", false, nil
	}
	return text, true, nil
}
