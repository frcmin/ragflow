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
"""Internal canvas runner used by cmd/qnzsagentbot.

The public OpenAI-compatible endpoint lives in the Go service. This route
only executes an already-authorized canvas: it loads ``user_canvas`` (the
table the custom QNZSUserCanvasService reads), checks ``user_id == tenant_id``,
resets the canvas, applies ``dialog`` as history, and streams canvas events.
"""
import hmac
import json
import logging
import os

from quart import Blueprint, Response, jsonify, request

from api.qnzs.dialog import normalize_dialog

logger = logging.getLogger(__name__)

bp = Blueprint("qnzs_canvas_exec", __name__)


def _authorized() -> bool:
    expected = os.environ.get("QNZS_INTERNAL_TOKEN", "")
    if not expected:
        return False
    got = request.headers.get("X-QNZS-Internal-Token", "")
    return hmac.compare_digest(got, expected)


@bp.route("/canvas/run", methods=["POST"])
async def run_canvas():
    if not os.environ.get("QNZS_INTERNAL_TOKEN", ""):
        return jsonify({"code": 102, "message": "QNZS_INTERNAL_TOKEN is not configured"}), 503
    if not _authorized():
        return jsonify({"code": 102, "message": "internal token is not valid"}), 403

    payload = await request.get_json(force=False, silent=True)
    if not isinstance(payload, dict):
        return jsonify({"code": 102, "message": "JSON body is required"}), 400

    tenant_id = payload.get("tenant_id") or ""
    agent_id = payload.get("agent_id") or ""
    if not tenant_id or not agent_id:
        return jsonify({"code": 102, "message": "tenant_id and agent_id are required"}), 400

    try:
        dialog_turns = normalize_dialog(payload.get("dialog")) if "dialog" in payload else None
    except ValueError as exc:
        return jsonify({"code": 102, "message": str(exc)}), 400

    from agent.canvas import Canvas
    from api.db.services.canvas_service import UserCanvasService
    from common.misc_utils import get_uuid, thread_pool_exec

    exists, cvs = await thread_pool_exec(UserCanvasService.get_by_id, agent_id)
    if not exists or cvs is None:
        return jsonify({"code": 102, "message": "Agent not found."}), 400
    if getattr(cvs, "user_id", None) != tenant_id:
        return jsonify({"code": 102, "message": "You do not own the agent."}), 400

    dsl = cvs.dsl
    if not isinstance(dsl, str):
        dsl = json.dumps(dsl, ensure_ascii=False)
    session_id = payload.get("session_id") or get_uuid()
    # Match the reference call: Canvas(dsl, tenant_id, agent_id) binds agent_id
    # as task_id, not canvas_id.
    canvas = Canvas(dsl, tenant_id, agent_id)
    canvas.reset()
    if dialog_turns is not None:
        canvas.history = []
        for role, content in dialog_turns:
            canvas.history.append((role, content))

    query = payload.get("query") or payload.get("question") or ""
    user_id = payload.get("user_id") or ""
    inputs = payload.get("inputs") or {}

    async def stream():
        try:
            async for ans in canvas.run(query=query, files=[], user_id=user_id, inputs=inputs, session_id=session_id):
                if isinstance(ans, dict):
                    ans["session_id"] = session_id
                yield "data: " + json.dumps(ans, ensure_ascii=False) + "\n\n"
        except Exception as exc:
            logger.exception("qnzs canvas run failed session_id=%s", session_id)
            yield "data: " + json.dumps({"event": "error", "data": {"content": str(exc) or "Unknown error"}, "session_id": session_id}, ensure_ascii=False) + "\n\n"
        finally:
            close = getattr(canvas, "close", None)
            if callable(close):
                close()

    resp = Response(stream(), mimetype="text/event-stream")
    resp.headers["Cache-Control"] = "no-cache"
    resp.headers["X-Accel-Buffering"] = "no"
    return resp
