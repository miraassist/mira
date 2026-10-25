"""The enrollment page and a small JSON API.

GET  /             the page: recent utterances, assign each to a speaker
GET  /api/state    speakers, threshold and recent utterances
GET  /api/last     the latest utterance
GET  /api/match    ?text=...&within=10: who said this text just now (docs/spec/sprecher.md)
POST /api/assign   {"id": "...", "name": "..."}
POST /api/delete   {"name": "..."}
"""

from aiohttp import web

from speaker_store import SpeakerStore

PAGE = """<!doctype html>
<html lang="de"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>Speaker ID</title>
<style>
:root{--bg:#fff;--fg:#1d1d1f;--mut:#6e6e73;--line:#e5e5ea;--ok:#1a7f37;--warn:#9a6700;--bad:#b42318;--acc:#0a66c2}
@media (prefers-color-scheme:dark){:root{--bg:#161618;--fg:#f2f2f7;--mut:#a1a1a6;--line:#2c2c2e;--ok:#3fb950;--warn:#d29922;--bad:#f85149;--acc:#58a6ff}}
body{background:var(--bg);color:var(--fg);font:15px/1.4 system-ui,sans-serif;margin:0 auto;max-width:960px;padding:16px}
h1{font-size:20px;margin:0 0 4px} h2{font-size:16px;margin:24px 0 8px}
.mut{color:var(--mut)} table{border-collapse:collapse;width:100%} td,th{border-bottom:1px solid var(--line);padding:6px 4px;text-align:left;vertical-align:top}
.accepted{color:var(--ok)} .uncertain{color:var(--warn)} .unknown,.unavailable{color:var(--bad)}
input{font:inherit;width:8em;padding:2px 4px;background:var(--bg);color:var(--fg);border:1px solid var(--line);border-radius:4px}
button{font:inherit;padding:2px 8px;border:1px solid var(--line);border-radius:4px;background:var(--bg);color:var(--acc);cursor:pointer}
.chip{display:inline-block;border:1px solid var(--line);border-radius:12px;padding:2px 10px;margin:0 6px 6px 0}
</style></head><body>
<h1>Speaker ID</h1>
<div class="mut" id="meta"></div>
<h2>Enrolled</h2><div id="speakers"></div>
<h2>Recent utterances</h2>
<p class="mut">Only voice prints are kept, no audio. Assign an utterance to a person to enroll it, by the id of the person in Home Assistant (person.<b>anna</b> → anna); several per person work best.</p>
<table><thead><tr><th>Time</th><th>Said</th><th>Result</th><th>Enroll as</th></tr></thead><tbody id="recent"></tbody></table>
<script>
const esc=s=>String(s??"").replace(/[&<>"]/g,c=>({"&":"&amp;","<":"&lt;",">":"&gt;",'"':"&quot;"}[c]));
let editing=false;
async function post(url,body){await fetch(url,{method:"POST",headers:{"Content-Type":"application/json"},body:JSON.stringify(body)});load(true)}
async function load(force){
  if(editing&&!force)return;
  const s=await (await fetch("api/state")).json();
  document.getElementById("meta").textContent=`threshold ${s.threshold} · ${s.recent.length} recent`;
  const names=Object.keys(s.speakers);
  document.getElementById("speakers").innerHTML=names.length?names.map(n=>`<span class="chip">${esc(n)} · ${s.speakers[n]} <button onclick="if(confirm('Delete '+this.dataset.n+'?'))post('api/delete',{name:this.dataset.n})" data-n="${esc(n)}">×</button></span>`).join(""):'<span class="mut">nobody yet</span>';
  document.getElementById("recent").innerHTML=s.recent.map(u=>{
    const t=new Date(u.at*1000).toLocaleTimeString();
    const res=u.decision==="unavailable"?`<span class="unavailable">too short (${u.seconds}s)</span>`:
      `<span class="${u.decision}">${esc(u.decision)}</span> ${esc(u.speaker??"")} ${u.speaker?u.score:""}${u.second?` <span class="mut">(2nd ${esc(u.second)} ${u.second_score})</span>`:""}<br><span class="mut">${u.seconds}s · ${u.elapsed_ms} ms</span>`;
    const enroll=u.decision==="unavailable"?"":(u.assigned?`✓ ${esc(u.assigned)}`:
      `<input list="names" id="n-${u.id}" onfocus="editing=true" onblur="editing=false" placeholder="person id"> <button onclick="post('api/assign',{id:'${u.id}',name:document.getElementById('n-${u.id}').value})">Save</button>`);
    return `<tr><td>${t}</td><td>${esc(u.transcript)}</td><td>${res}</td><td>${enroll}</td></tr>`}).join("")
    +`<datalist id="names">${names.map(n=>`<option value="${esc(n)}">`).join("")}</datalist>`;
}
load(true);setInterval(load,2000);
</script></body></html>
"""


def make_app(store: SpeakerStore) -> web.Application:
    async def page(_):
        return web.Response(text=PAGE, content_type="text/html")

    async def state(_):
        return web.json_response(store.snapshot())

    async def last(_):
        recent = store.snapshot()["recent"]
        return web.json_response(recent[0] if recent else None)

    async def match(request):
        text = request.query.get("text", "")
        try:
            within = min(float(request.query.get("within", "10")), 60.0)
        except ValueError:
            within = 10.0
        return web.json_response(store.match(text, within))

    async def assign(request):
        body = await request.json()
        ok = store.assign(str(body.get("id", "")), str(body.get("name", "")))
        return web.json_response({"ok": ok}, status=200 if ok else 404)

    async def delete(request):
        body = await request.json()
        ok = store.delete_speaker(str(body.get("name", "")))
        return web.json_response({"ok": ok}, status=200 if ok else 404)

    app = web.Application()
    app.router.add_get("/", page)
    app.router.add_get("/api/state", state)
    app.router.add_get("/api/last", last)
    app.router.add_get("/api/match", match)
    app.router.add_post("/api/assign", assign)
    app.router.add_post("/api/delete", delete)
    return app
