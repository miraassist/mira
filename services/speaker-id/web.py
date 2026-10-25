"""The service's API, for Mira (docs/spec/sprecher.md). With a token (--token, SPEAKER_TOKEN) every call
needs it as "Authorization: Bearer <token>"; enrolling happens in Mira's page, not here.

GET  /api/match      ?text=...&within=10: every utterance of this text just now, with its best and
                     second-best person and their scores; Mira decides who that is
GET  /api/heard      ?since=<unix seconds>: the utterances since then, for enrolling (at most 5 minutes)
GET  /api/profiles   the enrolled persons and their number of samples
POST /api/assign     {"id": "...", "name": "<person id>"}: enroll an utterance for a person
POST /api/delete     {"name": "<person id>"}: delete a person's voice
"""

import hmac
import time

from aiohttp import web

from speaker_store import SpeakerStore

# How far back enrolling may look: what was said before somebody started to enroll is not theirs to see.
HEARD_MAX = 300.0


def make_app(store: SpeakerStore, token: str = "") -> web.Application:
    @web.middleware
    async def auth(request, handler):
        if token:
            got = request.headers.get("Authorization", "").removeprefix("Bearer ")
            if not hmac.compare_digest(got.encode(), token.encode()):
                return web.json_response({"error": "missing or wrong token"}, status=401)
        return await handler(request)

    async def match(request):
        text = request.query.get("text", "")
        try:
            within = min(float(request.query.get("within", "10")), 60.0)
        except ValueError:
            within = 10.0
        return web.json_response(store.match(text, within))

    async def heard(request):
        try:
            since = float(request.query.get("since", "0"))
        except ValueError:
            return web.json_response({"error": "since must be unix seconds"}, status=400)
        since = max(since, time.time() - HEARD_MAX)
        return web.json_response({"now": time.time(), "heard": store.heard_since(since)})

    async def profiles(_):
        return web.json_response(store.profiles())

    async def assign(request):
        body = await request.json()
        ok = store.assign(str(body.get("id", "")), str(body.get("name", "")))
        return web.json_response({"ok": ok}, status=200 if ok else 404)

    async def delete(request):
        body = await request.json()
        ok = store.delete_speaker(str(body.get("name", "")))
        return web.json_response({"ok": ok}, status=200 if ok else 404)

    async def now(_):
        return web.json_response({"now": time.time()})

    app = web.Application(middlewares=[auth])
    app.router.add_get("/api/match", match)
    app.router.add_get("/api/heard", heard)
    app.router.add_get("/api/now", now)
    app.router.add_get("/api/profiles", profiles)
    app.router.add_post("/api/assign", assign)
    app.router.add_post("/api/delete", delete)
    return app
