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
import unittest

from api.qnzs.dialog import normalize_dialog


class NormalizeDialogTest(unittest.TestCase):
    def test_none_and_empty(self):
        self.assertIsNone(normalize_dialog(None))
        self.assertEqual(normalize_dialog([]), [])

    def test_turns(self):
        self.assertEqual(
            normalize_dialog([{"role": "user", "content": "你好"}, {"role": "assistant", "content": "hello"}]),
            [("user", "你好"), ("assistant", "hello")],
        )

    def test_rejects_bad_shape(self):
        with self.assertRaises(ValueError):
            normalize_dialog({"role": "user"})
        with self.assertRaises(ValueError):
            normalize_dialog([{"role": "user"}])
        with self.assertRaises(ValueError):
            normalize_dialog([{"role": 1, "content": "x"}])


if __name__ == "__main__":
    unittest.main()
