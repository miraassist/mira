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
    return Utterance(id=text[:4], at=time.time() - ago, seconds=2.0, transcript=text,
                     speaker=speaker, score=score, decision=decision)


class MatchTest(unittest.TestCase):
    def test_found_by_text_whatever_case_and_spaces(self):
        s = store_with(said(" Mach das Licht an.", "anna"))
        self.assertEqual(s.match("mach  das licht AN.", 10)["person"], "anna")

    def test_other_text_or_too_old_is_nothing(self):
        s = store_with(said("Mach das Licht an.", "anna", ago=30))
        self.assertIsNone(s.match("Mach das Licht an.", 10))
        self.assertIsNone(s.match("Wie spät ist es?", 60))

    def test_not_accepted_names_nobody(self):
        s = store_with(said("Hallo", "anna", decision="unknown"))
        r = s.match("Hallo", 10)
        self.assertIsNone(r["person"])
        self.assertEqual(r["decision"], "unknown")

    def test_two_satellites_same_person(self):  # SP-07
        s = store_with(said("Hallo", "anna"), said("Hallo", "anna", ago=1.2))
        r = s.match("Hallo", 10)
        self.assertEqual((r["person"], r["decision"], r["count"]), ("anna", "accepted", 2))

    def test_two_satellites_disagree(self):  # SP-08
        for other in (said("Hallo", "ben", ago=1.2), said("Hallo", "anna", decision="uncertain", ago=1.2)):
            s = store_with(said("Hallo", "anna"), other)
            r = s.match("Hallo", 10)
            self.assertEqual((r["person"], r["decision"]), (None, "uncertain"))


if __name__ == "__main__":
    unittest.main()
