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
"""Tests for map_begin_inputs in qnzs_canvas_service.

The function is loaded from that module's source. Importing
api.db.services pulls the database models, which this mapping test does not
need.
"""
import ast
import unittest
from pathlib import Path


def _load_map_begin_inputs():
    path = Path(__file__).resolve().parents[1] / "db" / "services" / "qnzs_canvas_service.py"
    tree = ast.parse(path.read_text(encoding="utf-8"))
    func = next(node for node in tree.body if isinstance(node, ast.FunctionDef) and node.name == "map_begin_inputs")
    module = ast.Module(body=[func], type_ignores=[])
    ast.fix_missing_locations(module)
    namespace = {}
    exec(compile(module, str(path), "exec"), namespace)
    return namespace["map_begin_inputs"]


map_begin_inputs = _load_map_begin_inputs()


class MapBeginInputsTest(unittest.TestCase):
    def test_dot_dunder_and_dialog(self):
        params = {
            "question": {"type": "line", "optional": False},
            "customer.name": {"type": "line"},
            "order__id": {"type": "line", "optional": True},
            "a.b__c": {"type": "line"},
            "dialog": {"type": "text"},
        }
        kwargs = {
            "question": "你好",
            "customer": {"name": "张三"},
            "order": {"id": "88"},
            "a": {"b__c": "dot-wins"},
            "dialog": [{"role": "user", "content": "hi"}],
        }
        got = map_begin_inputs(params, kwargs)
        self.assertEqual(got["question"]["value"], "你好")
        self.assertFalse(got["question"]["optional"])
        self.assertEqual(got["question"]["type"], "line")
        self.assertEqual(got["question"]["options"], [])
        self.assertEqual(got["customer.name"]["value"], "张三")
        self.assertEqual(got["order__id"]["value"], "88")
        self.assertEqual(got["a.b__c"]["value"], "dot-wins")
        self.assertEqual(got["dialog"]["value"], "")

    def test_only_the_first_two_segments(self):
        # split(".") then index 0 and 1. "a.b.c" reads kwargs["a"]["b"], not kwargs["a"]["b.c"] and not kwargs["a"]["b"]["c"].
        got = map_begin_inputs({"a.b.c": {"type": "line"}}, {"a": {"b": {"c": "deep"}, "b.c": "two"}})
        self.assertEqual(got["a.b.c"]["value"], {"c": "deep"})


if __name__ == "__main__":
    unittest.main()
