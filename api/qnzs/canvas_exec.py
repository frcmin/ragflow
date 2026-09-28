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
"""Internal entry that runs qnzs_completion for the Go agent-bot service.

The Go process authenticates the caller and suffixes session_id. This route
forwards the remaining JSON fields as kwargs. It does not call
canvas_service.completion.
"""
import hmac
import os

from quart import Blueprint, Response, jsonify, request

from api.db.services.qnzs_canvas_service import qnzs_completion

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

    tenant_id = payload.pop("tenant_id", "") or ""
    agent_id = payload.pop("agent_id", "") or ""
    session_id = payload.pop("session_id", None)
    if not tenant_id or not agent_id:
        return jsonify({"code": 102, "message": "tenant_id and agent_id are required"}), 400

    generator = qnzs_completion(tenant_id, agent_id, session_id=session_id, **payload)
    try:
        first = await anext(generator)
    except StopAsyncIteration:
        first = None
    except AssertionError as exc:
        await generator.aclose()
        return jsonify({"code": 102, "message": str(exc)}), 400
    except Exception as exc:
        await generator.aclose()
        return jsonify({"code": 102, "message": str(exc) or "Unknown error"}), 400

    async def stream():
        try:
            if first is not None:
                yield first
            async for chunk in generator:
                yield chunk
        finally:
            await generator.aclose()

    resp = Response(stream(), mimetype="text/event-stream")
    resp.headers["Cache-Control"] = "no-cache"
    resp.headers["X-Accel-Buffering"] = "no"
    return resp
