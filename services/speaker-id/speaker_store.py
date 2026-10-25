"""Voice prints: the embedding model, the enrolled speakers, and the recent utterances.

No audio is kept. Each utterance leaves only its embedding (a few hundred numbers), its transcript and
the decision, and only the last RECENT of them, in memory, so that they can be assigned to a speaker on
the enrollment page. Enrolled embeddings are stored in a JSON file.
"""

import json
import logging
import os
import threading
import time
import uuid
from collections import deque
from dataclasses import dataclass, field

import numpy as np
import sherpa_onnx

_LOGGER = logging.getLogger(__name__)

RECENT = 30
# Shorter than this and an embedding says little about who spoke.
MIN_SECONDS = 1.0
# The best speaker has to beat the second by this much, or the result is "uncertain". The page
# shows this decision with the service's own threshold; Mira decides with hers (/api/match).
MARGIN = 0.05


def normalize(text: str) -> str:
    """A transcript as compared: trimmed, case folded, whitespace collapsed."""
    return " ".join(text.split()).casefold()


@dataclass
class Utterance:
    id: str
    at: float
    seconds: float
    transcript: str = ""
    embedding: np.ndarray | None = None
    speaker: str | None = None
    score: float = 0.0
    second: str | None = None
    second_score: float = 0.0
    decision: str = "unavailable"  # accepted, uncertain, unknown, unavailable
    elapsed_ms: float = 0.0
    assigned: str | None = None

    def public(self) -> dict:
        return {
            "id": self.id,
            "at": self.at,
            "seconds": round(self.seconds, 2),
            "transcript": self.transcript,
            "speaker": self.speaker,
            "score": round(self.score, 3),
            "second": self.second,
            "second_score": round(self.second_score, 3),
            "decision": self.decision,
            "elapsed_ms": round(self.elapsed_ms, 1),
            "assigned": self.assigned,
        }


@dataclass
class SpeakerStore:
    model_path: str
    store_path: str
    threshold: float
    speakers: dict[str, list[np.ndarray]] = field(default_factory=dict)
    recent: deque = field(default_factory=lambda: deque(maxlen=RECENT))
    lock: threading.Lock = field(default_factory=threading.Lock)

    def __post_init__(self) -> None:
        config = sherpa_onnx.SpeakerEmbeddingExtractorConfig(
            model=self.model_path, num_threads=2, provider="cpu"
        )
        if not config.validate():
            raise ValueError(f"bad speaker model config: {config}")
        self.extractor = sherpa_onnx.SpeakerEmbeddingExtractor(config)
        _LOGGER.info("Speaker model %s, dim %d", self.model_path, self.extractor.dim)
        self._load()

    # -- persistence ------------------------------------------------------------------------------

    def _load(self) -> None:
        if not os.path.exists(self.store_path):
            return
        with open(self.store_path) as f:
            data = json.load(f)
        self.speakers = {
            name: [np.asarray(e, dtype=np.float32) for e in embs]
            for name, embs in data.get("speakers", {}).items()
        }
        _LOGGER.info(
            "Loaded %d speakers: %s",
            len(self.speakers),
            {n: len(e) for n, e in self.speakers.items()},
        )

    def _save(self) -> None:
        data = {"speakers": {n: [e.tolist() for e in embs] for n, embs in self.speakers.items()}}
        tmp = self.store_path + ".tmp"
        with open(tmp, "w") as f:
            json.dump(data, f)
        os.replace(tmp, self.store_path)

    # -- recognition ------------------------------------------------------------------------------

    def embed(self, audio: np.ndarray, rate: int) -> np.ndarray:
        stream = self.extractor.create_stream()
        stream.accept_waveform(sample_rate=rate, waveform=audio)
        stream.input_finished()
        emb = np.asarray(self.extractor.compute(stream), dtype=np.float32)
        return emb / (np.linalg.norm(emb) + 1e-9)

    def _scores(self, emb: np.ndarray) -> list[tuple[str, float]]:
        """Each speaker's score: the cosine to the mean of their samples."""
        out = []
        for name, embs in self.speakers.items():
            centroid = np.mean(embs, axis=0)
            centroid /= np.linalg.norm(centroid) + 1e-9
            out.append((name, float(np.dot(emb, centroid))))
        return sorted(out, key=lambda x: x[1], reverse=True)

    def recognize(self, audio: np.ndarray, rate: int) -> Utterance:
        """Recognize one utterance (float32 mono). Runs in a worker thread."""
        u = Utterance(id=uuid.uuid4().hex[:8], at=time.time(), seconds=len(audio) / rate)
        if u.seconds < MIN_SECONDS:
            u.decision = "unavailable"
        else:
            start = time.perf_counter()
            u.embedding = self.embed(audio, rate)
            with self.lock:
                scores = self._scores(u.embedding)
            u.elapsed_ms = (time.perf_counter() - start) * 1000
            if scores:
                u.speaker, u.score = scores[0]
                if len(scores) > 1:
                    u.second, u.second_score = scores[1]
            if not scores or u.score < self.threshold:
                u.decision = "unknown"
            elif u.second is not None and u.score - u.second_score < MARGIN:
                u.decision = "uncertain"
            else:
                u.decision = "accepted"
        with self.lock:
            self.recent.appendleft(u)
        return u

    # -- enrollment -------------------------------------------------------------------------------

    def assign(self, utterance_id: str, name: str) -> bool:
        name = name.strip()
        if not name:
            return False
        with self.lock:
            for u in self.recent:
                if u.id == utterance_id and u.embedding is not None:
                    if u.assigned:
                        self._unassign_locked(u)
                    self.speakers.setdefault(name, []).append(u.embedding)
                    u.assigned = name
                    self._save()
                    _LOGGER.info("Enrolled an utterance for %s (%d samples)", name, len(self.speakers[name]))
                    return True
        return False

    def _unassign_locked(self, u: Utterance) -> None:
        embs = self.speakers.get(u.assigned, [])
        for i, e in enumerate(embs):
            if e is u.embedding:
                del embs[i]
                break
        if not embs:
            self.speakers.pop(u.assigned, None)
        u.assigned = None

    def delete_speaker(self, name: str) -> bool:
        with self.lock:
            if self.speakers.pop(name, None) is None:
                return False
            for u in self.recent:
                if u.assigned == name:
                    u.assigned = None
            self._save()
            return True

    def match(self, text: str, within: float) -> list[dict]:
        """The utterances with this transcript in the last `within` seconds, latest first.

        Several are several satellites that heard the same request. Each gives its best and second-best
        speaker with their scores; whether that is somebody is the caller's decision (Mira's threshold,
        docs/spec/sprecher.md), not this service's.
        """
        want = normalize(text)
        if not want:
            return []
        since = time.time() - within
        with self.lock:
            found = [u for u in self.recent if u.at >= since and normalize(u.transcript) == want]
        return [
            {
                "person": u.speaker,
                "score": round(u.score, 3),
                "second": u.second,
                "second_score": round(u.second_score, 3),
                "seconds": round(u.seconds, 2),
            }
            for u in found
            if u.embedding is not None
        ]

    def heard_since(self, since: float) -> list[dict]:
        """The utterances since `since` (unix seconds), oldest first, for enrolling: each with its id,
        transcript, length and to whom it was assigned. Only those with a voice print."""
        with self.lock:
            found = [u for u in self.recent if u.at >= since and u.embedding is not None]
        return [
            {"id": u.id, "at": u.at, "seconds": round(u.seconds, 2), "transcript": u.transcript,
             "assigned": u.assigned}
            for u in reversed(found)
        ]

    def profiles(self) -> dict[str, int]:
        """The enrolled persons and how many samples each has."""
        with self.lock:
            return {n: len(e) for n, e in self.speakers.items()}
