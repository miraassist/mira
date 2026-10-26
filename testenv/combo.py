"""Records a two-screen demo: a page of Mira in the browser, the Show in front,
with the voices.

    python testenv/combo.py --scene enroll [out dir]   # enrolling Anna's voice
    python testenv/combo.py --scene plan [out dir]     # cooking, the plans page behind
    python testenv/combo.py --scene show_only [out dir]  # the Show alone

Default out dir: docs/demo/<scene>. Needs what showdialog.py needs (the Show
over SSH, see there) and a login for anna in testenv/.env. Run it in a terminal
of your own: it prints what to say (lines starting with >>>).

Scenes:
- enroll: Anna's card; the browser starts enrolling, clicks "That was me" for
  each new sentence, stops after three and asks for a last question.
- plan: the plans page, which follows the plan by itself. It ends when a plan
  is finished and Mira has spoken, or after --quiet seconds without a turn.
- show_only: no browser, the Show alone (a quiz, a question); ends after --quiet
  seconds without a turn.

Esc ends any scene: the video is built from what was recorded so far. (Ctrl+C
reaches the browser too and stops everything.)
Meanwhile the Show's screen is fetched; afterwards the pipeline runs, the
Show's recordings and wake clips, and Mira's answers are fetched and laid
under the picture: the page as background, the Show top right. Out: combo.mp4,
and every part on its own (raw/, frames/, timeline.json, turns.json,
sounds.json, the WAVs) to put it together differently.
"""

import argparse
import atexit
import json
import pathlib
import subprocess
import sys
import select
import termios
import threading
import time
import tty
import wave

ROOT = pathlib.Path(__file__).resolve().parents[1]
ap = argparse.ArgumentParser(description="Two-screen demo: a page of Mira and the Show.")
ap.add_argument("out", nargs="?", type=pathlib.Path)
ap.add_argument("--scene", choices=["enroll", "plan", "show_only"], default="enroll")
ap.add_argument("--quiet", type=float, default=120, help="plan, show_only: end after this many seconds without a turn")
ARGS = ap.parse_args()
# Absolute: ffmpeg reads the paths in frames.txt relative to that file.
OUT = (ARGS.out or ROOT / "docs" / "demo" / ARGS.scene).resolve()
sys.argv = sys.argv[:1]  # showdialog reads its own arguments on import

import requests  # noqa: E402
from playwright.sync_api import Error as PlaywrightError, sync_playwright  # noqa: E402

sys.path.insert(0, str(pathlib.Path(__file__).parent))
import showdialog as D  # noqa: E402  (HA calls, SSH to the Show, pipeline runs)
import walkthrough as W  # noqa: E402  (the cursor)

SENTENCES = 3
VIEW = W.VIEW
CROP = W.CROP
SATELLITE = "assist_satellite.show_assist_satellit"


def say(text):
    print(f"\n>>> {text}", flush=True)


class Stop(Exception):
    """Esc was pressed: the scene ends where it is."""


ESCAPE = threading.Event()


def watch_escape():
    """Reads keys from the terminal (not Ctrl+C, which would reach the browser as well) and marks
    Esc. The terminal is set back when the script ends."""
    if not sys.stdin.isatty():
        return
    fd = sys.stdin.fileno()
    old = termios.tcgetattr(fd)
    atexit.register(termios.tcsetattr, fd, termios.TCSADRAIN, old)
    tty.setcbreak(fd)

    def run():
        while not ESCAPE.is_set():
            if select.select([fd], [], [], 0.5)[0] and sys.stdin.read(1) == "\x1b":
                ESCAPE.set()
    threading.Thread(target=run, daemon=True).start()


def check():
    if ESCAPE.is_set():
        raise Stop


def cue(what):
    """Tells what to say next, as the Show stands: after Mira is done, and with the wake word only
    when she is not listening anyway (she may end with a question and keep listening)."""
    wait_for_mira()
    time.sleep(0.8)  # between answering and listening again it may pass through idle
    if D.state(SATELLITE) == "listening":
        say(f"Mira hört noch zu – sag ohne Aktivierungswort: {what}")
    else:
        say(f"Aktivierungswort, dann: {what}")


def wait_for_mira(most=45):
    """Waits while Mira thinks or talks."""
    for _ in range(int(most / 0.3)):
        check()
        if D.state(SATELLITE) not in ("processing", "responding"):
            return
        time.sleep(0.3)


# What the Show is doing, as a line for whoever speaks and cannot see its screen.
STATES = {"idle": "bereit – Aktivierungswort nötig", "listening": "hört zu – einfach sprechen",
          "processing": "denkt nach …", "responding": "spricht …"}


class Satellite(threading.Thread):
    """Prints each change of the satellite's state until stopped."""

    def __init__(self):
        super().__init__(daemon=True)
        self.stop = threading.Event()

    def run(self):
        last = None
        while not self.stop.is_set():
            try:
                now = D.state(SATELLITE)
            except Exception:  # noqa: BLE001  a missed poll is no reason to stop
                now = last
            if now != last:
                print(f"    [{time.strftime('%H:%M:%S')}] Show: {STATES.get(now, now)}", flush=True)
                last = now
            time.sleep(0.3)


class Screen(threading.Thread):
    """Fetches the Show's screen until stopped, with the wall clock."""

    def __init__(self):
        super().__init__(daemon=True)
        self.frames, self.stop = [], threading.Event()
        (OUT / "frames").mkdir(parents=True, exist_ok=True)

    def run(self):
        s = requests.Session()
        while not self.stop.is_set():
            t = time.time()
            try:
                r = s.get(f"{D.SHOW}/screen.png", timeout=5)
            except requests.RequestException:
                continue
            if r.status_code == 200:
                f = OUT / "frames" / f"{len(self.frames):05d}.png"
                f.write_bytes(r.content)
                self.frames.append((t, f))


def ws(msgs):
    """HA's websocket in a thread of its own: Playwright's sync API runs an event loop in this one."""
    import asyncio
    from concurrent.futures import ThreadPoolExecutor
    with ThreadPoolExecutor(1) as ex:
        return ex.submit(lambda: asyncio.run(D.ws(msgs))).result()


def runs_since(t):
    """The pipeline runs that ended after t (wall clock), oldest first."""
    pipes = ws([{"type": "assist_pipeline/pipeline/list"}])[0]["pipelines"]
    out = []
    for p in pipes:
        lst = ws([{"type": "assist_pipeline/pipeline_debug/list", "pipeline_id": p["id"]}])[0]
        for run in lst.get("pipeline_runs", []):
            if D.ts(run["timestamp"]) >= t - 2:
                ev = ws([{"type": "assist_pipeline/pipeline_debug/get", "pipeline_id": p["id"],
                          "pipeline_run_id": run["pipeline_run_id"]}])[0]["events"]
                if any(e["type"] == "run-end" for e in ev):
                    out.append(ev)
    return sorted(out, key=lambda ev: ev[0]["timestamp"])


def plans():
    """The running plans, as Mira's page reads them."""
    r = requests.get("http://localhost:8788/v1/ui/plans", headers={"Authorization": f"Bearer {D.env['MIRA_API_TOKEN']}"}, timeout=10)
    return r.json().get("plans", [])


# ---- Scenes ------------------------------------------------------------------
# Each has prepare (the page before the clock starts) and run (after it; returns when the scene is
# over, or Esc is pressed).

def enroll_prepare(t, ui):
    t.glide(ui.locator(".home-person h2")); t.page.wait_for_timeout(300)
    ui.locator(".home-person h2").click()
    ui.locator("#voice button.primary").wait_for()
    t.page.wait_for_timeout(1500)
    t.glide(ui.locator("#voice h2")); t.page.wait_for_timeout(500)


def enroll_run(t, ui):
    pg = t.page
    t.glide(ui.locator("#voice button.primary")); pg.wait_for_timeout(600)
    ui.locator("#voice button.primary").click()
    ui.locator("#enroll").wait_for()
    keep_in_view = lambda: ui.locator("#enroll").evaluate("e => e.scrollIntoView({block: 'center', behavior: 'smooth'})")
    keep_in_view()
    cue("einen ganzen Satz, z. B. »Heute ist ein schöner Tag zum Spazierengehen.«")
    done = 0
    while done < SENTENCES:
        btn = ui.locator("#heard button.secondary")
        for _ in range(60):  # a minute, a second at a time, so Esc is heard
            check()
            if btn.count():
                break
            pg.wait_for_timeout(1000)
        else:
            say("Nichts gehört.")
            cue("noch einen ganzen Satz")
            continue
        keep_in_view()  # the list grows downwards: keep it in the middle, below the Show
        pg.wait_for_timeout(1200)  # let the row be seen first
        t.glide(btn.first); pg.wait_for_timeout(400)
        btn.first.click()
        done += 1
        pg.wait_for_timeout(1500)
        if done < SENTENCES:
            say(f"Gut ({done}/{SENTENCES}).")
            cue("den nächsten Satz, z. B. »Ich trinke morgens am liebsten Kaffee mit Milch.«")
    stop = ui.locator("#voice button.primary")
    t.glide(stop); pg.wait_for_timeout(500)
    stop.click()
    pg.wait_for_timeout(1500)
    say("Angelernt.")
    cue("»Wer spricht gerade mit dir?«")
    asked = time.time()
    for _ in range(60):  # wait for that turn to end
        check()
        time.sleep(2)
        if runs_since(asked):
            break
    wait_for_mira()
    pg.wait_for_timeout(2000)


def plan_prepare(t, ui):
    t.frame().evaluate("go('plans')")
    ui.locator("#planlist").wait_for()
    t.page.wait_for_timeout(1500)
    t.glide(ui.locator("#view section.card h2"))


def until_quiet(done=None):
    """Returns after --quiet seconds in which the Show did nothing (once something happened), or
    when done() says so."""
    last, turned = time.time(), False
    while True:
        check()
        time.sleep(2)
        if D.state(SATELLITE) != "idle":
            last, turned = time.time(), True
        if done and done():
            return
        if turned and time.time() - last > ARGS.quiet:
            say(f"{ARGS.quiet:.0f} s ohne Runde: Schluss.")
            return


def plan_run(t, ui):
    say("Leg los: Aktivierungswort, dann z. B. »Lass uns Pfannkuchen backen.« "
        f"Das Skript hört auf, wenn der Plan fertig ist, nach {ARGS.quiet:.0f} s Stille, oder mit Esc.")
    finished = {p["id"] for p in plans() if p.get("finished")}

    def plan_done():
        if not {p["id"] for p in plans() if p.get("finished")} - finished:
            return False
        say("Der Plan ist fertig.")
        wait_for_mira()
        time.sleep(1)
        wait_for_mira()  # she may say one more thing
        t.page.wait_for_timeout(3000)
        return True
    until_quiet(plan_done)


def show_run(t, ui):
    say(f"Leg los: Aktivierungswort, dann was du willst. Schluss nach {ARGS.quiet:.0f} s Stille oder mit Esc.")
    until_quiet()
    wait_for_mira()


# A scene without prepare needs no browser: the video is the Show alone.
SCENES = {"enroll": (enroll_prepare, enroll_run), "plan": (plan_prepare, plan_run), "show_only": (None, show_run)}


def play(run, t, screen, satellite):
    """Starts the clock, the Show's screen and its state, runs the scene until it ends or Esc, and
    stops them; returns start and end (wall clock)."""
    start = time.time()
    screen.start()
    satellite.start()
    try:
        run(t, t.ui if t else None)
    except Stop:
        say("Esc: Schluss, das Video wird aus dem Bisherigen gebaut.")
    say("Danke, fertig. Das Video wird gebaut, das dauert eine Minute.")
    screen.stop.set()
    satellite.stop.set()
    return start, time.time()


def record_with_browser(prepare, run, enabled, screen, satellite):
    """Records the scene with Anna's browser behind; returns start, end and the browser video."""
    with sync_playwright() as p:
        b = p.chromium.launch()
        ctx = b.new_context(viewport=VIEW)
        pg = ctx.new_page()
        pg.goto(f"{D.HA}/mira")
        pg.fill("input[name=username]", "anna"); pg.fill("input[name=password]", D.env["TESTHA_ANNA_PASSWORD"])
        pg.keyboard.press("Enter")
        pg.wait_for_url("**/mira")
        pg.frame_locator("iframe").locator("mira-home .home-grid").wait_for()
        state = ctx.storage_state()
        ctx.close()

        ctx = b.new_context(viewport=VIEW, storage_state=state, record_video_dir=str(OUT / "raw"), record_video_size=VIEW)
        ctx.add_init_script(W.CURSOR)
        pg = ctx.new_page()
        pg.goto(f"{D.HA}/mira")
        for attempt in range(5):  # HA may load the page once more right after opening it
            try:
                pg.wait_for_load_state("networkidle")
                pg.frame_locator("iframe").locator("mira-home .home-grid").wait_for()
                pg.wait_for_timeout(3000)
                pg.evaluate("""() => { const m = document.createElement('div'); m.id = 'wt-mark';
                  m.style.cssText = 'position:fixed;left:8px;top:8px;width:24px;height:24px;background:#808080;z-index:2147483647';
                  document.body.appendChild(m); }""")
                pg.wait_for_timeout(1000)
                if pg.evaluate("!!document.getElementById('wt-mark')"):
                    break
            except PlaywrightError:
                pass
        t = W.Tour(pg)
        prepare(t, t.ui)

        # The common clock: the mark turns white when it starts.
        wait_left = 10 - (time.time() - enabled)  # the wake clip needs the detector to have heard a while
        if wait_left > 0:
            pg.wait_for_timeout(wait_left * 1000)
        pg.evaluate("document.getElementById('wt-mark').style.background = '#fff'")
        start, end = play(run, t, screen, satellite)
        video = pg.video.path()
        ctx.close()
        b.close()
    return start, end, video


def mark_turned(video):
    """Where the browser video is at `start`: the frame where the mark turned white."""
    log = subprocess.run(["ffmpeg", "-v", "error", "-i", video, "-vf",
                          "crop=16:16:12:12,signalstats,metadata=print:key=lavfi.signalstats.YAVG:file=-", "-f", "null", "-"],
                         capture_output=True, text=True, check=True).stdout.split("\n")
    frames_y, when = [], None
    for line in log:
        if "pts_time:" in line:
            when = float(line.split("pts_time:")[1].split()[0])
        elif "YAVG=" in line and when is not None:
            frames_y.append((when, float(line.split("=")[1])))
    k = next(i for i, (_, y) in enumerate(frames_y) if 100 < y < 160)  # grey until the scene starts
    return next(w for w, y in frames_y[k:] if y > 190)


def main():
    OUT.mkdir(parents=True, exist_ok=True)
    prepare, run = SCENES[ARGS.scene]
    was = {e: D.state(e) for e in D.SWITCHES}
    kept = D.state(D.KEPT)
    watch_escape()
    say(f"Vorbereitung: {'Show und Browser werden' if prepare else 'die Show wird'} eingerichtet, noch nicht sprechen. (Esc beendet die Szene.)")
    for e in D.SWITCHES:
        D.call("switch", "turn_on", {"entity_id": e})
    D.call("number", "set_value", {"entity_id": D.KEPT, "value": 10})
    enabled = time.time()
    clips_before = set(D.sh(f"ls {D.ON_SHOW}/wake-clips/wake/ 2>/dev/null; true").split())
    screen, satellite = Screen(), Satellite()
    try:
        if prepare:
            start, end, video = record_with_browser(prepare, run, enabled, screen, satellite)
        else:
            wait_left = 10 - (time.time() - enabled)  # the wake clip needs the detector to have heard a while
            if wait_left > 0:
                time.sleep(wait_left)
            start, end, video = *play(run, None, screen, satellite), None
        screen.join(5)
        # The Show saves a wake clip only 30 s after the wake (until then the turn may still call it a
        # mistake), and switching clips off would drop it: wait for the last one before fetching.
        last = max((D.ts(ev[0]["timestamp"]) for ev in runs_since(start - 5)), default=time.time())
        if last + 32 > time.time():
            say(f"Warte noch {last + 32 - time.time():.0f} s auf den letzten Aktivierungswort-Clip der Show …")
            time.sleep(last + 32 - time.time())
        # Everything needed to put it together differently later, on the same clock (seconds from start).
        json.dump({"scene": ARGS.scene, "start": start, "enabled": enabled - start, "end": end - start, "browser": str(video) if video else None,
                   "show_frames": [(ft - start, str(path)) for ft, path in screen.frames]},
                  open(OUT / "timeline.json", "w"), indent=1)

        # What happened when, Mira's audio, your sentences and the wake word.
        runs = runs_since(start - 5)
        turns = []
        for ev in runs:
            at = {e["type"]: e for e in ev}
            def when(k):
                return D.ts(at[k]["timestamp"]) - start if k in at else None
            url = ((at.get("tts-end") or {}).get("data") or {}).get("tts_output", {}).get("url")
            turns.append({"stt": when("stt-start"), "tts": when("tts-end"), "url": url,
                          "heard": ((at.get("stt-end") or {}).get("data") or {}).get("stt_output", {}).get("text")})
        turns = [x for x in turns if x["stt"] is not None and x["stt"] < end - start + 5]
        json.dump(turns, open(OUT / "turns.json", "w"), indent=1, ensure_ascii=False)
        sounds = []
        for i, x in enumerate(turns):
            if x["url"]:
                f = OUT / f"mira{i}.wav"
                f.write_bytes(requests.get(D.HA + x["url"], headers=D.H).content)
                sounds.append((x["tts"], f))
        recs = []
        for name in D.sh(f"ls {D.ON_SHOW}/recordings/ | grep json$; true").split():
            recs.append((json.loads(D.sh(f"cat {D.ON_SHOW}/recordings/{name}"))["at"], name.replace(".json", ".wav")))
        recs.sort()
        since = [x for x in turns if x["stt"] >= enabled - start]
        for (_, name), x in zip(recs[-len(since):] if since else [], since[-len(recs):] if recs else []):
            f = OUT / name
            subprocess.run([*D.SCP, f"{D.SSH[-1]}:{D.ON_SHOW}/recordings/{name}", str(f)], check=True)
            sounds.append((x["stt"], f))
        for name in D.sh(f"ls {D.ON_SHOW}/wake-clips/wake/ 2>/dev/null; true").split():
            if name in clips_before or not name.endswith(".json"):
                continue
            at = D.ts(json.loads(D.sh(f"cat {D.ON_SHOW}/wake-clips/wake/{name}"))["at"]) - start
            f = OUT / ("wake-" + name.replace(".json", ".wav"))
            subprocess.run([*D.SCP, f"{D.SSH[-1]}:{D.ON_SHOW}/wake-clips/wake/{name.replace('.json', '.wav')}", str(f)], check=True)
            with wave.open(str(f)) as w:
                sounds.append((at - w.getnframes() / w.getframerate(), f))
        json.dump([(a, str(f)) for a, f in sounds], open(OUT / "sounds.json", "w"), indent=1)
    finally:
        for e, v in was.items():
            D.call("switch", "turn_on" if v == "on" else "turn_off", {"entity_id": e})
        D.call("number", "set_value", {"entity_id": D.KEPT, "value": float(kept)})

    # The Show: each frame as long as until the next, from `start`.
    frames = screen.frames
    with open(OUT / "frames.txt", "w") as f:
        for i, (ft, path) in enumerate(frames):
            f.write(f"file '{path}'\nduration {(frames[i + 1][0] - ft) if i + 1 < len(frames) else 0.2:.3f}\n")
        f.write(f"file '{frames[-1][1]}'\n")
    # The timeline starts at the earliest sound (somebody may start early), at most where the browser
    # video does.
    lead = max(0.0, -min((a for a, _ in sounds), default=0.0))
    if video:
        v_start = mark_turned(video)
        lead = min(lead, v_start)
    show_pad = frames[0][0] - start + lead
    first_sound = 2 if video else 1
    inputs, filt = [], []
    for i, (a, f) in enumerate(sounds):
        inputs += ["-i", str(f)]
        filt.append(f"[{i + first_sound}:a]aresample=48000,adelay={max(0, int((a + lead) * 1000))}:all=1[a{i}]")
    mix = "".join(f"[a{i}]" for i in range(len(sounds))) + f"amix=inputs={len(sounds)}:normalize=0,aformat=channel_layouts=stereo[a]"
    show = f"tpad=start_mode=clone:start_duration={show_pad:.3f},fps=25"
    if video:
        # The page as background, the Show top right.
        graph = (f"[0:v]{CROP},fps=25[bg];[1:v]{show},scale=480:-2,pad=iw+8:ih+8:4:4:color=0x1f2a2a[show];"
                 f"[bg][show]overlay=W-w-28:28:shortest=1,format=yuv420p[v];")
        pictures = ["-ss", f"{v_start - lead:.3f}", "-i", video, "-f", "concat", "-safe", "0", "-i", str(OUT / "frames.txt")]
    else:
        graph = f"[0:v]{show},scale=1280:-2,format=yuv420p[v];"
        pictures = ["-f", "concat", "-safe", "0", "-i", str(OUT / "frames.txt")]
    subprocess.run(["ffmpeg", "-y", "-v", "error", *pictures, *inputs,
                    "-filter_complex", graph + ";".join(filt) + ";" + mix, "-map", "[v]", "-map", "[a]",
                    "-c:v", "libx264", "-crf", "21", "-preset", "slow", "-c:a", "aac", "-b:a", "192k",
                    "-movflags", "+faststart", str(OUT / "combo.mp4")], check=True)
    print(OUT / "combo.mp4", flush=True)


if __name__ == "__main__":
    main()
