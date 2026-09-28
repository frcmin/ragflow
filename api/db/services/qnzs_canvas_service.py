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
"""QNZS agent completion.

The HTTP handler imports this as
``from api.db.services.qnzs_canvas_service import qnzs_completion as agent_completion``.
It is not ``canvas_service.completion``: there is no API4Conversation resume,
``agent_id`` is the canvas task id, ``dialog`` replaces history, and
``canvas.run`` is not given ``session_id``. The yielded strings are already
OpenAI ``chat.completion.chunk`` SSE frames. The reference canvas pool is
not kept; each run builds a canvas and releases it after the generator ends.
"""
import json
import logging
import time
import uuid

from agent.canvas import Canvas
from api.db.db_models import UserCanvas
from api.db.services.canvas_service import UserCanvasService
from common.misc_utils import get_uuid, thread_pool_exec

logger = logging.getLogger(__name__)


class QNZSUserCanvasService(UserCanvasService):
    """Canvas rows for qnzs_completion.

    This fork has no separate QNZS canvas table. The service reads ``user_canvas``
    and qnzs_completion checks ``user_id == tenant_id``.
    """

    model = UserCanvas


def map_begin_inputs(input_params, kwargs):
    """Build the begin-component ``inputs`` dict passed to ``canvas.run``."""
    inputs = {}
    for key, value in input_params.items():
        # dialog is history, not a begin value.
        if key == "dialog":
            inputs[key] = {
                "name": key,
                "optional": value.get("optional", True),
                "options": [],
                "type": value.get("type", ""),
                "value": "",
            }
            continue
        if "." in key:
            new_keys = key.split(".")
            mapped = kwargs.get(new_keys[0], {}).get(new_keys[1], None)
        elif "__" in key:
            new_keys = key.split("__")
            mapped = kwargs.get(new_keys[0], {}).get(new_keys[1], None)
        else:
            mapped = kwargs.get(key, None)
        inputs[key] = {
            "name": key,
            "optional": value.get("optional", True),
            "options": [],
            "type": value.get("type", ""),
            "value": mapped,
        }
    return inputs


async def qnzs_completion(tenant_id, agent_id, session_id=None, **kwargs):
    query = kwargs.get("query", "") or kwargs.get("question", "")
    user_id = kwargs.get("user_id", "")
    inputs = {}
    exists, cvs = await thread_pool_exec(QNZSUserCanvasService.get_by_id, agent_id)
    assert exists, "Agent not found."
    assert cvs.user_id == tenant_id, "You do not own the agent."
    dsl = cvs.dsl
    if not isinstance(dsl, str):
        dsl = json.dumps(dsl, ensure_ascii=False)
        logger.warning("cvs.dsl joson dumps from str,%s", session_id)
    if not session_id:
        session_id = get_uuid()
    # agent_id is the task id. canvas_id stays unset.
    canvas = Canvas(dsl, tenant_id, agent_id)
    try:
        canvas.reset()
        if "dialog" in kwargs:
            canvas.history = []
            for item in kwargs.get("dialog", []):
                canvas.history.append([item["role"], item["content"]])
        try:
            input_params = canvas.dsl["components"]["begin"]["obj"].get_input_elements()
            logger.warning("after load canvas.dsl:%s", input_params)
            inputs = map_begin_inputs(input_params, kwargs)
        except json.JSONDecodeError:
            logger.exception("get inputs  error,session_id:%s", session_id)
        txt = ""
        logger.warning("query:%s,inputs:%s", query, inputs)
        async for ans in canvas.run(query=query, files=[], user_id=user_id, inputs=inputs):
            ans["session_id"] = session_id
            if ans["event"] == "message" and isinstance(ans["data"]["content"], str):
                content = ans["data"]["content"]
                if len(content) == 0:
                    continue
                txt += content.replace("\r", "").replace("\n", "")
                chunk = {
                    "id": f"chatcmpl-{uuid.uuid4().hex}",
                    "object": "chat.completion.chunk",
                    "created": int(time.time()),
                    "model": "agent-bot",
                    "choices": [
                        {
                            "index": 0,
                            "delta": {"role": "assistant", "content": content},
                            "finish_reason": None,
                        }
                    ],
                }
                yield f"data: {json.dumps(chunk, ensure_ascii=False)}\n\n"
        logger.warning("session_id:%s; LLM ret:%s", session_id, txt)
    finally:
        _release_canvas(canvas)


def _release_canvas(canvas):
    """Close MCP sessions and stop the per-canvas thread pool.

    The reference keeps the canvas in a pool that is never read (the reuse
    branch is ``if False``). Leaving that pool in place would retain a
    ThreadPoolExecutor on every request. Releasing the canvas does not change
    the SSE frames already yielded.
    """
    try:
        canvas.close()
    finally:
        pool = getattr(canvas, "_thread_pool", None)
        if pool is not None:
            pool.shutdown(wait=False, cancel_futures=True)
