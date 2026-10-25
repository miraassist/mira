"""Records a walk through Mira's page in the test HA, with Mira's voice.

    pip install playwright requests && playwright install chromium
    python testenv/walkthrough.py               # into docs/demo/
    python testenv/walkthrough.py /tmp/take2    # elsewhere

Needs the test HA with the test Mira (see README.md) and GEMINI_API_TOKEN in
testenv/.env; logs in as admin. Each scene in scenes() has a line Mira says
and what happens on screen meanwhile. All lines are synthesized first with
Gemini TTS (kept in <out>/tts-cache by their text, so a changed line is the
only one made anew); a line's length sets how long its scene stays, a click
comes after the line. Actions that would change something (forget, new
model, switch on) are only shown and then cancelled.

The browser shows HA as usual; the video is cropped to Mira's page. Playwright
does not write its frames evenly in time, so a mark in the cropped-away
corner turns white or black at each line, and the voice is laid where the
video shows the mark turn.

Out: walkthrough.mp4 (1280x720, with the voice), walkthrough.srt (subtitles)
and walkthrough.txt (the lines).
"""

import base64
import datetime
import hashlib
import os
import pathlib
import re
import subprocess
import sys
import shutil
import time
import wave

import requests

from playwright.sync_api import Error as PlaywrightError, sync_playwright

ROOT = pathlib.Path(__file__).resolve().parents[1]
HA = os.environ.get("TESTHA_URL", "http://localhost:8123")
MODEL = "gemini-3.8-flash-tts"
VOICE = "Gacrux"  # female, mature
STYLE = "[warm, calm, caring]"  # a tag, not read aloud
OUT = pathlib.Path(sys.argv[1]) if len(sys.argv) > 1 else ROOT / "docs" / "demo"
CACHE = OUT / "tts-cache"
# The page is recorded larger and cropped: HA's sidebar (256) and toolbar (56) go.
VIEW = {"width": 1536, "height": 776}
CROP = "crop=1280:720:256:56"
GAP = 0.6  # seconds of quiet between two lines

CURSOR = """
(() => {
  if (window.top === window) return;  // only Mira's frame, the part that stays
  const add = () => {
    const c = document.createElement("div");
    c.id = "wt-cursor";
    c.innerHTML = '<svg width="22" height="22" viewBox="0 0 24 24"><path d="M3 2l7 19 2.5-8L21 10z" fill="#222" stroke="#fff" stroke-width="1.5" stroke-linejoin="round"/></svg>';
    c.style.cssText = "position:fixed;left:-50px;top:-50px;z-index:2147483647;pointer-events:none;transition:none";
    document.documentElement.appendChild(c);
    addEventListener("mousemove", e => { c.style.left = e.clientX - 3 + "px"; c.style.top = e.clientY - 2 + "px"; }, true);
    addEventListener("mousedown", e => {
      const r = document.createElement("div");
      r.style.cssText = `position:fixed;left:${e.clientX - 18}px;top:${e.clientY - 18}px;width:36px;height:36px;border-radius:50%;border:3px solid #d35fa5;z-index:2147483646;pointer-events:none;transition:transform .5s,opacity .5s`;
      document.documentElement.appendChild(r);
      requestAnimationFrame(() => { r.style.transform = "scale(1.8)"; r.style.opacity = "0"; });
      setTimeout(() => r.remove(), 600);
    }, true);
  };
  if (document.readyState === "loading") addEventListener("DOMContentLoaded", add); else add();
  // Zoom: the page moves so the focus area sits in the middle, larger; the
  // browser draws it anew, so it stays sharp. The cursor (outside body) stays.
  // Not used yet: on the wide card "Ich" it slides and jumps back.
  // wtZoom(selector, most): measures what the selector shows with the page
  // unzoomed, then moves from the current zoom to the new one.
  window.wtZoom = (sel, most) => {
    const b = document.body, vw = innerWidth, vh = innerHeight, was = b.style.transform;
    b.style.transition = "none"; b.style.transform = "none";
    const els = [...document.querySelectorAll(sel)];
    els[0].scrollIntoView({ block: "center" });
    const rs = els.map(e => { const g = document.createRange(); g.selectNodeContents(e); return g.getBoundingClientRect(); }).filter(r => r.width && r.height);
    const x = Math.min(...rs.map(r => r.left)), y = Math.min(...rs.map(r => r.top));
    const w = Math.max(...rs.map(r => r.right)) - x, h = Math.max(...rs.map(r => r.bottom)) - y;
    const r = b.getBoundingClientRect();
    const s = Math.max(1, Math.min(most, .8 * vw / w, .8 * vh / h));
    const tx = vw / 2 - r.left - s * (x + w / 2 - r.left), ty = vh / 2 - r.top - s * (y + h / 2 - r.top);
    document.documentElement.style.background = getComputedStyle(b).backgroundColor;
    b.style.transformOrigin = "0 0";
    b.style.transform = was; b.getBoundingClientRect();  // back where it was, then glide
    b.style.transition = "transform .9s cubic-bezier(.4,0,.2,1)";
    b.style.transform = `translate(${tx}px, ${ty}px) scale(${s})`;
  };
  window.wtUnzoom = () => { document.body.style.transform = "none"; };
})();
"""


def env() -> dict[str, str]:
    return dict(l.split("=", 1) for l in (ROOT / "testenv/.env").read_text().splitlines() if "=" in l and not l.startswith("#"))


def tts(text: str, out: pathlib.Path) -> float:
    """Synthesizes a line with Gemini TTS; kept in CACHE by its text."""
    key = hashlib.sha1(f"{MODEL}|{VOICE}|{STYLE}|{text}".encode()).hexdigest()[:16]
    cached = CACHE / f"{key}.wav"
    if not cached.exists():
        for attempt in range(8):
            r = requests.post(f"https://generativelanguage.googleapis.com/v1beta/models/{MODEL}:generateContent",
                              headers={"x-goog-api-key": env()["GEMINI_API_TOKEN"]}, timeout=180,
                              json={"contents": [{"parts": [{"text": f"{STYLE} {text}"}]}],
                                    "generationConfig": {"responseModalities": ["AUDIO"],
                                                         "speechConfig": {"voiceConfig": {"prebuiltVoiceConfig": {"voiceName": VOICE}}}}})
            if r.status_code != 429:
                break
            # The free tier limits requests per minute; wait as long as Google says.
            wait = 30
            for d in r.json().get("error", {}).get("details", []):
                if "retryDelay" in d:
                    wait = float(d["retryDelay"].rstrip("s")) + 2
            print(f"TTS limit, waiting {wait:.0f}s", file=sys.stderr)
            time.sleep(wait)
        r.raise_for_status()
        part = r.json()["candidates"][0]["content"]["parts"][0]["inlineData"]
        data = base64.b64decode(part["data"])
        CACHE.mkdir(parents=True, exist_ok=True)
        if "wav" in part["mimeType"]:
            cached.write_bytes(data)
        else:  # raw 16-bit PCM
            with wave.open(str(cached), "wb") as w:
                w.setnchannels(1); w.setsampwidth(2); w.setframerate(24000); w.writeframes(data)
    shutil.copy(cached, out)
    with wave.open(str(out)) as w:
        return w.getnframes() / w.getframerate()


class Tour:
    def __init__(self, page, dry=False):
        self.page = page
        self.dry = dry  # only synthesize the lines, nothing on screen
        self.lines = {}  # text -> (wav, length)
        self.ui = page.frame_locator("iframe") if page else None
        self.x, self.y = VIEW["width"] / 2, VIEW["height"] / 2
        self.cues = []  # (start, end, text, wav)
        self.t0 = None

    def now(self) -> float:
        return time.monotonic() - self.t0

    def glide(self, target, steps=30):
        """Moves the cursor to the middle of a locator (or a point)."""
        if isinstance(target, tuple):
            x, y = target
        else:
            if not target.count():  # not on this page: skip it, rather than stop
                print("missing:", target, file=sys.stderr)
                return
            target = target.first
            target.scroll_into_view_if_needed()
            b = target.bounding_box()
            x, y = b["x"] + b["width"] / 2, b["y"] + b["height"] / 2
        self.page.mouse.move(x, y, steps=steps)
        self.x, self.y = x, y

    def frame(self):
        return next(f for f in self.page.frames if "/api/mira/" in f.url)

    def zoom(self, selector, most=1.6, wait=True):
        """Zooms in on what a CSS selector shows in Mira's page (all matches,
        by the extent of their content), so it fills most of the picture."""
        self.frame().evaluate(f"wtZoom({selector!r}, {most})")
        if wait:
            self.page.wait_for_timeout(950)

    def unzoom(self, wait=True):
        self.frame().evaluate("wtUnzoom()")
        if wait:
            self.page.wait_for_timeout(950)

    def say(self, text, during=None, click=None):
        """Says a line; during() runs meanwhile; click is clicked after it."""
        if self.dry:
            (OUT / "lines").mkdir(parents=True, exist_ok=True)
            wav = OUT / "lines" / f"line{len(self.lines):02d}.wav"
            self.lines[text] = (wav, tts(text, wav))
            return
        wav, length = self.lines[text]
        # The sync mark (cropped away later) turns white or black at each line.
        self.page.evaluate(f"document.getElementById('wt-mark').style.background = '{'#fff' if len(self.cues) % 2 == 0 else '#000'}'")
        start = self.now()
        if during:
            during()
        rest = start + length - self.now()
        if rest > 0:
            self.page.wait_for_timeout(rest * 1000)
        self.cues.append((length, text, wav))
        if click is not None:
            self.unzoom()
            self.glide(click, steps=15)
            self.page.wait_for_timeout(250)
            click.click()
        self.page.wait_for_timeout(GAP * 1000)


class _Any:
    """Stands in for the page in the dry run: every call and attribute works."""
    def __getattr__(self, name): return _Any()
    def __call__(self, *a, **k): return _Any()


def scenes(t: Tour):
    ui = t.ui or _Any()
    loc = ui.locator
    tab = lambda name: loc("mira-nav .tabs button", has_text=re.compile(f"^{name}$"))
    back = loc("mira-nav button.back")
    card = lambda g: loc(f".home-card[data-group={g}] h2")
    button = lambda text: loc("#view button:visible", has_text=re.compile(f"^{text}$")).first
    dialog = lambda text: loc("dialog[open] button:visible", has_text=re.compile(f"^{text}$")).first
    pause = lambda ms: t.page.wait_for_timeout(ms)

    def hover_all(sel, n, ms=600):
        for i in range(min(n, loc(sel).count())):
            t.glide(loc(sel).nth(i), steps=18); pause(ms)

    def scroll(dy, steps=12):
        for _ in range(steps):
            t.page.mouse.wheel(0, dy / steps); pause(60)

    def open_item(sel, i=0):
        t.glide(loc(sel).nth(i)); pause(300); loc(sel).nth(i).click(); pause(900)

    # The overview.
    t.say("Hi, I'm Mira, right here in your Home Assistant. Let me show you around.")
    t.say("I say hello and tell you what's waiting for you.",
          during=lambda: t.glide(loc(".mira-welcome h2")))
    t.say("This card is yours: your settings, your voice, your messages, and what I remember for you.",
          during=lambda: (t.glide(loc(".home-person h2")), pause(1200), t.glide(loc(".home-person .home-pages a").nth(3))))
    t.say("The rest lives in four places: your day, your home, what I can do, and the guide.",
          during=lambda: [(t.glide(card(g), steps=20), pause(700)) for g in ("daily", "setup", "abilities", "help")])
    t.say("Let's start with your day.", click=card("daily"))

    # Your day.
    t.say("Here are my messages. When nobody is around, they wait for you, so nothing gets lost.",
          during=lambda: hover_all("#view details.item summary", 3))
    t.say("Once you've heard one, you can tick it off.", during=lambda: t.glide(button("Done")))
    t.say("Tasks are things I keep an eye on for you. Just say: let me know if the gas usage is unusual.",
          during=lambda: (tab("Tasks").click(), pause(1200), hover_all("#view details.item.task summary", 3)))
    t.say("Each one is a Home Assistant automation, so you stay in control, and can pause it any time.",
          during=lambda: (open_item("#view details.item.task summary", 1), t.glide(button("Pause"))))
    t.say("When we cook or plan something together, step by step, you can follow along here.",
          during=lambda: (tab("Plans").click(), pause(1200), t.glide(loc("#view section.card h2"))))
    t.say("And this is what I remember for the whole house.",
          during=lambda: (tab("Memory").click(), pause(1200), hover_all("#view table.facts tr td:first-child, #view .fact", 3)))
    t.say("Something wrong? Fix it, or ask me to forget it. Of course, I'll check with you first.",
          during=lambda: (t.glide(button("Edit")), pause(800), t.glide(button("Forget")), pause(300), button("Forget").click(),
                          pause(1800), t.glide(dialog("Cancel")), pause(300), dialog("Cancel").click()))
    t.say("Now, your home.", during=lambda: (back.click(), pause(1200)), click=card("setup"))

    # Your home.
    t.say("Here you tell me where I can speak: every voice satellite, in its room.",
          during=lambda: hover_all("#view table tr td:first-child", 4, 500))
    t.say("These are the people I know, so I can tell who's talking, and reach the right person.",
          during=lambda: (tab("People").click(), pause(1200), hover_all("#view section.card h2, #view tr td:first-child", 3)))
    t.say("And you decide when I may speak up: quiet hours, so I never wake anyone, and what's urgent enough for right away.",
          during=lambda: (tab("Notifications").click(), pause(1200), hover_all("#view .grid input, #view .grid select", 3, 700)))
    t.say("Next, what I can do.", during=lambda: (back.click(), pause(1200)), click=card("abilities"))

    # What I can do.
    t.say("These are my skills: the waste calendar, alarms, recipes, and lots more.",
          during=lambda: (hover_all("mira-skills details.item summary", 3, 500), open_item("mira-skills details.item summary", 0)))
    t.say("More are ready to switch on, whenever you like.",
          during=lambda: (loc("mira-skills details.item summary").first.click(), pause(400), scroll(900), t.glide(button("Switch on"))))
    t.say("Behind the scenes, small helpers do the work for me, each with its own job.",
          during=lambda: (tab("Agents").click(), pause(1200), hover_all("details.item.agent summary", 3, 500),
                          open_item("details.item.agent summary", 0)))
    t.say("And these are the language models I think with: on your own computer, or in the cloud.",
          during=lambda: (tab("Models").click(), pause(1200), open_item("mira-models details.item summary", 1)))
    t.say("You can check that one answers,",
          during=lambda: (t.glide(button("Check now")), pause(200), button("Check now").click()))
    t.say("and adding a new one takes just a few clicks.",
          during=lambda: (dialog("Close").click(), pause(500), t.glide(loc("#view button", has_text="New model")), pause(300),
                          loc("#view button", has_text="New model").click(), pause(1200)))
    t.say("Let's leave that for now.",
          during=lambda: (t.glide(dialog("Cancel")), pause(300), dialog("Cancel").click()))
    t.say("If you ever want to know more, my guide has examples to try.",
          during=lambda: (back.click(), pause(1200), t.glide(card("help")), pause(300), card("help").click(), pause(1500), scroll(1600, 30)))

    # Your card, and goodbye.
    t.say("And last, your own card.", during=lambda: (back.click(), pause(1200)), click=loc(".home-person h2"))
    t.say("Your settings, your voice, so I know it's you, messages just for you, and what I remember for you, all in one place.",
          during=lambda: (pause(800), scroll(500), pause(1200), scroll(700), pause(1200), scroll(900)))
    t.say("That's me. Just talk to me, I'm here for you.",
          during=lambda: (back.click(), pause(1500)))
    if not t.dry:
        pause(2000)


def main():
    OUT.mkdir(parents=True, exist_ok=True)
    e = env()
    dry = Tour(None, dry=True)
    scenes(dry)
    with sync_playwright() as p:
        b = p.chromium.launch(env={**os.environ, "LANG": "en_US.UTF-8", "LC_ALL": "en_US.UTF-8"})
        # Log in without recording; the recording starts on Mira's page.
        ctx = b.new_context(viewport=VIEW)
        pg = ctx.new_page()
        pg.goto(f"{HA}/mira")
        pg.fill("input[name=username]", "admin"); pg.fill("input[name=password]", e["TESTHA_ADMIN_PASSWORD"])
        pg.keyboard.press("Enter")
        pg.wait_for_url("**/mira")
        pg.frame_locator("iframe").locator("mira-home .home-grid").wait_for()
        state = ctx.storage_state()
        ctx.close()

        ctx = b.new_context(viewport=VIEW, storage_state=state, locale="en-US",
                            record_video_dir=str(OUT / "raw"), record_video_size=VIEW)
        ctx.clock.set_system_time(datetime.datetime.fromisoformat("2026-10-26T09:30:00+01:00"))
        ctx.add_init_script(CURSOR)
        pg = ctx.new_page()
        pg.goto(f"{HA}/mira")
        # HA sometimes loads the page once more right after opening it; wait
        # until it has settled, and try again if it reloads under our hands.
        for attempt in range(5):
            try:
                pg.wait_for_load_state("networkidle")
                pg.frame_locator("iframe").locator("mira-home .home-grid").wait_for()
                pg.wait_for_timeout(3000)  # the panel builds its frame anew once
                pg.evaluate("""() => { const m = document.createElement('div'); m.id = 'wt-mark';
                  m.style.cssText = 'position:fixed;left:8px;top:8px;width:24px;height:24px;background:#808080;z-index:2147483647';
                  document.body.appendChild(m); }""")
                pg.wait_for_timeout(1500)
                if pg.evaluate("!!document.getElementById('wt-mark')"):
                    break
            except PlaywrightError as err:
                print("page reloaded, again:", err, file=sys.stderr)
        t = Tour(pg)
        t.lines = dry.lines
        t.t0 = time.monotonic()
        scenes(t)
        video = pg.video.path()
        ctx.close()
        b.close()

    # When each line really starts in the video: where the sync mark turns.
    log = subprocess.run(["ffmpeg", "-v", "error", "-i", video, "-vf",
                          "crop=16:16:12:12,signalstats,metadata=print:key=lavfi.signalstats.YAVG:file=-", "-f", "null", "-"],
                         capture_output=True, text=True, check=True).stdout.split("\n")
    frames, when = [], None
    for line in log:
        if "pts_time:" in line:
            when = float(line.split("pts_time:")[1].split()[0])
        elif "YAVG=" in line and when is not None:
            frames.append((when, float(line.split("=")[1])))
    # The page is white before it loads; the mark is grey until the first line.
    k = next(i for i, (_, y) in enumerate(frames) if 100 < y < 160)
    starts = []
    for i in range(len(t.cues)):
        white = i % 2 == 0
        while not (frames[k][1] > 190 if white else frames[k][1] < 60):
            k += 1
        starts.append(frames[k][0])
    cut = max(0, starts[0] - 0.3)
    cues = [(a - cut, a - cut + length, text, wav) for a, (length, text, wav) in zip(starts, t.cues)]
    # Subtitles and the lines as text.
    def ts(s):
        ms = int(round(s * 1000)); return f"{ms // 3600000:02d}:{ms // 60000 % 60:02d}:{ms // 1000 % 60:02d},{ms % 1000:03d}"
    (OUT / "walkthrough.srt").write_text("".join(f"{i + 1}\n{ts(a)} --> {ts(z)}\n{txt}\n\n" for i, (a, z, txt, _) in enumerate(cues)))
    (OUT / "walkthrough.txt").write_text("".join(f"{txt}\n\n" for _, _, txt, _ in cues))
    # The voice: each line where it starts, the video from the first line on, cropped to the page.
    inputs, filters = [], []
    for i, (a, _, _, wav) in enumerate(cues):
        inputs += ["-i", str(wav)]
        filters.append(f"[{i + 1}:a]adelay={int(a * 1000)}:all=1[a{i}]")
    mix = "".join(f"[a{i}]" for i in range(len(cues))) + f"amix=inputs={len(cues)}:normalize=0[a]"
    subprocess.run(["ffmpeg", "-y", "-loglevel", "error", "-ss", f"{cut:.3f}", "-i", video, *inputs,
                    "-filter_complex", f"[0:v]{CROP}[v];" + ";".join(filters) + ";" + mix,
                    "-map", "[v]", "-map", "[a]", "-c:v", "libx264", "-crf", "23", "-preset", "slow", "-pix_fmt", "yuv420p",
                    "-c:a", "aac", "-b:a", "128k", "-movflags", "+faststart", str(OUT / "walkthrough.mp4")], check=True)
    print(OUT / "walkthrough.mp4")


if __name__ == "__main__":
    main()
