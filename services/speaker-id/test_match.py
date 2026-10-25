"""Tests of SpeakerStore.match: run in the container, python3 -m unittest test_match."""

import threading
import time
import unittest
from collections import deque

from speaker_store import SpeakerStore, Utterance


def store_with(*utterances):
    s = SpeakerStore.__new__(SpeakerStore)  # no model needed for matching
    s.recent, s.lock = deque(utterances), threading.Lock()
    return s


def said(text, speaker, decision="accepted", ago=1.0, score=0.7):
    return Utterance(id=text[:4], at=time.time() - ago, seconds=2.0, transcript=text, embedding=[0.0],
                     speaker=speaker, score=score, decision=decision)


class MatchTest(unittest.TestCase):
    def test_found_by_text_whatever_case_and_spaces(self):
        s = store_with(said(" Mach das Licht an.", "anna"))
        self.assertEqual([u["person"] for u in s.match("mach  das licht AN.", 10)], ["anna"])

    def test_other_text_or_too_old_is_nothing(self):
        s = store_with(said("Mach das Licht an.", "anna", ago=30))
        self.assertEqual(s.match("Mach das Licht an.", 10), [])
        self.assertEqual(s.match("Wie spät ist es?", 60), [])

    def test_scores_come_back_whatever_the_service_decided(self):
        s = store_with(said("Hallo", "anna", decision="unknown", score=0.42))
        self.assertEqual(s.match("Hallo", 10)[0]["score"], 0.42)

    def test_every_satellite_that_heard_it(self):  # SP-07, SP-08 are Mira's decision
        s = store_with(said("Hallo", "anna"), said("Hallo", "ben", ago=1.2))
        self.assertEqual([u["person"] for u in s.match("Hallo", 10)], ["anna", "ben"])

    def test_too_short_has_no_voice_print(self):
        u = said("Ja", None, decision="unavailable")
        u.embedding = None
        self.assertEqual(store_with(u).match("Ja", 10), [])


if __name__ == "__main__":
    unittest.main()
