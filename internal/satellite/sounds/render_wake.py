"""Render the alarm's wake sound (docs/spec/alarm.md, decision 4).

A marimba motif C-E-G-A-G-E, every 2.6 s, made here from sine partials, so it
carries no one else's licence. Two files:

- wake_rise.mp3: 24 s that rise from -30 dB to full over the first 20 s.
- wake_loop.mp3: four rounds at full (10.4 s), played again and again.

Both are raised by 8 dB under a limiter, as the sample chosen on 2026-11-22.
Needs Python 3 and ffmpeg with libmp3lame:

    python3 internal/satellite/sounds/render_wake.py internal/satellite/sounds
"""

import math
import os
import struct
import subprocess
import sys
import tempfile
import wave

RATE = 44100
LENGTH = 24.0  # seconds of the rise file
RISE = 20.0  # seconds from -30 dB to full
PERIOD = 2.6  # seconds between two rounds of the motif
MOTIF = [(523, 180), (659, 180), (784, 180), (880, 180), (784, 180), (659, 420)]  # Hz, ms to the next note
PARTIALS = [(1.0, 1.0), (4.0, 0.25), (10.0, 0.05)]  # ratio, gain: a marimba's bar
DECAY = 5.0
FILTER = "volume=8dB,alimiter=limit=0.95:level=false"


def note(freq):
    """One struck bar, two seconds."""
    out = []
    for i in range(int(2.0 * RATE)):
        t = i / RATE
        env = math.exp(-t * DECAY) * min(1.0, t / 0.004)
        s = sum(g * math.sin(2 * math.pi * freq * r * t) * math.exp(-t * DECAY * r * 0.4) for r, g in PARTIALS)
        out.append(s * env)
    return out


def render():
    """The motif every PERIOD seconds for LENGTH seconds, rising over RISE."""
    total = int(LENGTH * RATE)
    buf = [0.0] * total
    start = 0.0
    while start < LENGTH:
        t = start
        for freq, ms in MOTIF:
            off = int(t * RATE)
            for i, v in enumerate(note(freq)):
                if off + i >= total:
                    break
                buf[off + i] += v
            t += ms / 1000
        start += PERIOD
    peak = max(abs(v) for v in buf)
    for i in range(total):
        db = -30.0 + 30.0 * min(1.0, i / RATE / RISE)
        buf[i] = buf[i] / peak * 0.9 * 10 ** (db / 20)
    return buf


def write_wav(path, buf):
    with wave.open(path, "wb") as w:
        w.setnchannels(1)
        w.setsampwidth(2)
        w.setframerate(RATE)
        w.writeframes(b"".join(struct.pack("<h", int(v * 32767)) for v in buf))


def ffmpeg(*args):
    subprocess.run(["ffmpeg", "-hide_banner", "-loglevel", "error", "-y", *args], check=True)


def main(outdir):
    buf = render()
    fade = int(0.3 * RATE)
    rise = list(buf)
    for i in range(fade):
        rise[-1 - i] *= i / fade
    with tempfile.TemporaryDirectory() as tmp:
        raw, faded = os.path.join(tmp, "raw.wav"), os.path.join(tmp, "rise.wav")
        write_wav(raw, buf)
        write_wav(faded, rise)
        mp3 = ["-codec:a", "libmp3lame", "-b:a", "128k"]
        ffmpeg("-i", faded, "-af", FILTER, *mp3, os.path.join(outdir, "wake_rise.mp3"))
        # One round at full, taken where the rounds before it ring out into it, four times.
        size = int(PERIOD * RATE)
        ffmpeg("-ss", str(8 * PERIOD), "-t", str(PERIOD), "-i", raw,
               "-af", f"{FILTER},aloop=loop=3:size={size}", *mp3, os.path.join(outdir, "wake_loop.mp3"))


if __name__ == "__main__":
    main(sys.argv[1] if len(sys.argv) > 1 else os.path.dirname(os.path.abspath(__file__)))
