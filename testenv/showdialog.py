"""Records a voice dialog at the Show: its screen, what was said, Mira's answers.

    python testenv/showdialog.py [out dir] [seconds]   # default docs/demo/show-dialog, 90 s

Needs the test HA with the test Mira and a TECHO5 Show reachable over SSH as
root: TESTHA_SHOW_HOST and TESTHA_SHOW_SSH_KEY in testenv/.env.

During the time the Show's screen is fetched (/screen.png) with timestamps.
Afterwards the script reads Home Assistant's pipeline runs of that time (when
the wake word came, what was heard, Mira's answer and its audio), fetches
from the Show over SSH what it sent to be transcribed and the wake word
clips, and builds show.mp4. Switches "Screen web access", the kept
recordings and "Keep wake clips" on for the time, and sets them back after
fetching (setting them back deletes the recordings).
"""

import asyncio
import base64
import datetime
import json
import os
import pathlib
import subprocess
import sys
import time
import wave

import requests
import websockets

ROOT = pathlib.Path(__file__).resolve().parents[1]
HA = os.environ.get("TESTHA_URL", "http://localhost:8123")
ON_SHOW = "/data/misc/techo5"  # recordings/ and wake-clips/wake/
SWITCHES = {"switch.show_screen_web_access": "on", "switch.show_microphone_keep_wake_clips": "on"}
KEPT = "number.show_assistant_1_recordings_kept"
OUT = pathlib.Path(sys.argv[1]) if len(sys.argv) > 1 else ROOT / "docs" / "demo" / "show-dialog"
SECONDS = float(sys.argv[2]) if len(sys.argv) > 2 else 90
env = dict(l.split("=", 1) for l in (ROOT / "testenv/.env").read_text().splitlines() if "=" in l and not l.startswith("#"))
SHOW = f"http://{env['TESTHA_SHOW_HOST']}:8181"
KEY = os.path.expanduser(env.get("TESTHA_SHOW_SSH_KEY", "~/.ssh/id_ed25519"))
SSH = ["ssh", "-i", KEY, "-o", "IdentitiesOnly=yes", f"root@{env['TESTHA_SHOW_HOST']}"]
SCP = ["scp", "-O", "-i", KEY, "-o", "IdentitiesOnly=yes"]
TOKEN = env["TESTHA_ADMIN_TOKEN"]
H = {"Authorization": f"Bearer {TOKEN}", "Content-Type": "application/json"}


def state(entity):
    return requests.get(f"{HA}/api/states/{entity}", headers=H).json()["state"]


def call(domain, service, data, response=False):
    url = f"{HA}/api/services/{domain}/{service}" + ("?return_response" if response else "")
    r = requests.post(url, headers=H, json=data, timeout=60)
    r.raise_for_status()
    return r.json()


async def ws(msgs):
    """Sends messages over HA's websocket and returns their results."""
    async with websockets.connect(f"{HA.replace('http', 'ws')}/api/websocket", max_size=None) as c:
        await c.recv()
        await c.send(json.dumps({"type": "auth", "access_token": TOKEN}))
        await c.recv()
        out = []
        for i, m in enumerate(msgs, 1):
            await c.send(json.dumps({"id": i, **m}))
            while True:
                r = json.loads(await c.recv())
                if r.get("id") == i:
                    out.append(r.get("result"))
                    break
        return out


def sh(cmd):
    """Runs a command on the Show."""
    return subprocess.run([*SSH, cmd], capture_output=True, text=True, check=True).stdout


def ts(iso):
    return datetime.datetime.fromisoformat(iso.replace("Z", "+00:00")).timestamp()


def main():
    OUT.mkdir(parents=True, exist_ok=True)
    (OUT / "frames").mkdir(exist_ok=True)
    was = {e: state(e) for e in SWITCHES}
    kept = state(KEPT)
    for e in SWITCHES:
        call("switch", "turn_on", {"entity_id": e})
    call("number", "set_value", {"entity_id": KEPT, "value": 10})
    enabled = time.time()
    # The wake word clip is cut from what the detector heard while it was on: give it a moment.
    time.sleep(10)
    clips_before = set(sh(f"ls {ON_SHOW}/wake-clips/wake/ 2>/dev/null; true").split())
    try:
        # The screen, frame by frame, with the wall clock (HA runs on the same machine).
        frames, s = [], requests.Session()
        start = time.time()
        print(f"recording {SECONDS:.0f}s, speak now", flush=True)
        while time.time() - start < SECONDS:
            t = time.time()
            r = s.get(f"{SHOW}/screen.png", timeout=5)
            if r.status_code == 200:
                f = OUT / "frames" / f"{len(frames):05d}.png"
                f.write_bytes(r.content)
                frames.append((t, f))
        end = time.time()
        print(f"{len(frames)} frames, {len(frames) / (end - start):.1f} fps", flush=True)
        time.sleep(3)  # let the last turn finish

        # The pipeline runs of that time: when what happened, and Mira's audio.
        pipes = asyncio.run(ws([{"type": "assist_pipeline/pipeline/list"}]))[0]["pipelines"]
        runs = []
        for p in pipes:
            lst = asyncio.run(ws([{"type": "assist_pipeline/pipeline_debug/list", "pipeline_id": p["id"]}]))[0]
            for run in lst.get("pipeline_runs", []):
                if start - 5 <= ts(run["timestamp"]) <= end + 5:
                    ev = asyncio.run(ws([{"type": "assist_pipeline/pipeline_debug/get", "pipeline_id": p["id"],
                                          "pipeline_run_id": run["pipeline_run_id"]}]))[0]["events"]
                    runs.append(ev)
        turns = []
        for ev in runs:
            at = {e["type"]: e for e in ev}
            def when(k):
                return ts(at[k]["timestamp"]) - start if k in at else None
            heard = ((at.get("stt-end") or {}).get("data") or {}).get("stt_output", {}).get("text")
            tts = ((at.get("tts-end") or {}).get("data") or {}).get("tts_output", {})
            turns.append({"stt": when("stt-start"), "stt_end": when("stt-end"), "tts": when("tts-end"),
                          "heard": heard, "url": tts.get("url"), "answer":
                          ((at.get("intent-end") or {}).get("data") or {}).get("intent_output", {})
                          .get("response", {}).get("speech", {}).get("plain", {}).get("speech")})
        turns.sort(key=lambda x: x["stt"] if x["stt"] is not None else 1e9)
        json.dump(turns, open(OUT / "turns.json", "w"), indent=1, ensure_ascii=False)
        for x in turns:
            print(f"turn at {x['stt']}: heard {x['heard']!r} -> {x['answer']!r}", flush=True)

        # What the Show sent to be transcribed. Its sidecar says when it was saved, which is when the
        # whole turn closed (after the answer), so it does not tell which turn by itself; but the order
        # does. Oldest first, one per turn that began after recording was switched on.
        recs = []
        for name in sh(f"ls {ON_SHOW}/recordings/ | grep json$; true").split():
            recs.append((json.loads(sh(f"cat {ON_SHOW}/recordings/{name}"))["at"], name.replace(".json", ".wav")))
        recs.sort()
        since = [x for x in turns if x["stt"] is not None and x["stt"] >= enabled - start]
        voices = []
        for (at, name), x in zip(recs[-len(since):] if since else [], since[-len(recs):] if recs else []):
            wav = OUT / name
            subprocess.run([*SCP, f"{SSH[-1]}:{ON_SHOW}/recordings/{name}", str(wav)], check=True)
            voices.append((x["stt"], wav))
        # The wake word as the room said it: a clip of what the detector heard, ending when it fired.
        for name in sh(f"ls {ON_SHOW}/wake-clips/wake/ 2>/dev/null; true").split():
            if name in clips_before or not name.endswith(".json"):
                continue
            at = ts(json.loads(sh(f"cat {ON_SHOW}/wake-clips/wake/{name}"))["at"]) - start
            wav = OUT / ("wake-" + name.replace(".json", ".wav"))
            subprocess.run([*SCP, f"{SSH[-1]}:{ON_SHOW}/wake-clips/wake/{name.replace('.json', '.wav')}", str(wav)], check=True)
            with wave.open(str(wav)) as w:
                voices.append((at - w.getnframes() / w.getframerate(), wav))
        answers = []
        for i, x in enumerate(turns):
            if x["url"]:
                f = OUT / f"mira{i}.mp3"
                f.write_bytes(requests.get(HA + x["url"] if x["url"].startswith("/") else x["url"], headers=H).content)
                answers.append((x["tts"], f))
    finally:
        for e, v in was.items():
            call("switch", "turn_on" if v == "on" else "turn_off", {"entity_id": e})
        call("number", "set_value", {"entity_id": KEPT, "value": float(kept)})

    # The video: each frame as long as until the next.
    with open(OUT / "frames.txt", "w") as f:
        for i, (t, p) in enumerate(frames):
            f.write(f"file '{p}'\nduration {(frames[i + 1][0] - t) if i + 1 < len(frames) else 0.2:.3f}\n")
        f.write(f"file '{frames[-1][1]}'\n")
    t0 = frames[0][0] - start
    sounds = [(at, v) for at, v in voices if at is not None] + answers
    json.dump([(at, str(f)) for at, f in sounds], open(OUT / "sounds.json", "w"), indent=1)
    # Somebody who starts before the first frame (the wake word comes earlier still) is not cut: the
    # first frame stays that much longer at the front.
    lead = max(0.0, t0 - min((at for at, _ in sounds), default=t0))
    inputs, filt = [], []
    for i, (at, f) in enumerate(sounds):
        inputs += ["-i", str(f)]
        filt.append(f"[{i + 1}:a]aresample=48000,adelay={max(0, int((at - t0 + lead) * 1000))}:all=1[a{i}]")
    args = ["ffmpeg", "-y", "-v", "error", "-f", "concat", "-safe", "0", "-i", str(OUT / "frames.txt"), *inputs]
    if sounds:
        mix = "".join(f"[a{i}]" for i in range(len(sounds))) + f"amix=inputs={len(sounds)}:normalize=0,aformat=channel_layouts=stereo[a]"
        args += ["-filter_complex", ";".join(filt) + ";" + mix, "-map", "0:v", "-map", "[a]", "-c:a", "aac", "-b:a", "192k"]
    args += ["-vf", f"tpad=start_mode=clone:start_duration={lead:.3f},fps=25,format=yuv420p", "-c:v", "libx264", "-crf", "20", "-movflags", "+faststart", str(OUT / "show.mp4")]
    subprocess.run(args, check=True)
    print(OUT / "show.mp4")


if __name__ == "__main__":
    main()
