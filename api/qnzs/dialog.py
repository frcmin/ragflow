#
#  Copyright 2026 The InfiniFlow Authors. All Rights Reserved.
#
#  Licensed under the Apache License, Version 2.0 (the "License");
#  you may not use this file except in compliance with the License.
#  You may obtain a copy of the License at
#
#      http://www.apache.org/licenses/LICENSE-2.0
#
#  Unless required by applicable law or agreed to in writing, software
#  distributed under the License is distributed on an "AS IS" BASIS,
#  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
#  See the License for the specific language governing permissions and
#  limitations under the License.
#
"""Normalize the QNZS ``dialog`` history payload."""


def normalize_dialog(dialog):
    """Return ``[(role, content), ...]`` or raise ValueError.

    ``None`` means the caller did not send ``dialog``. An empty list is a
    valid history that replaces whatever the canvas loaded.
    """
    if dialog is None:
        return None
    if not isinstance(dialog, list):
        raise ValueError("dialog must be a list")
    turns = []
    for index, item in enumerate(dialog):
        if not isinstance(item, dict) or "role" not in item or "content" not in item:
            raise ValueError(f"dialog[{index}] requires role and content")
        role = item["role"]
        content = item["content"]
        if not isinstance(role, str) or not isinstance(content, str):
            raise ValueError(f"dialog[{index}] role and content must be strings")
        turns.append((role, content))
    return turns
