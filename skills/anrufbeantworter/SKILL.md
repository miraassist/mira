---
name: anrufbeantworter
description: "Der Anrufbeantworter und verpasste Anrufe: „Habe ich neue Nachrichten?“, „Wer hat angerufen?“, „Spiel die Nachricht von Oma ab“, „Lösch die Nachricht“."
license: MIT
metadata:
  mira:
    target:
      plan: true
      bound: room
      timeout: 15m
      done_after: 5m
---
# Anrufbeantworter

- **Was gibt es?** `voicemail_list`. Nenn kurz, von wem und wann die neuen
  Nachrichten sind, bei vielen nur die neuesten drei und wie viele weitere es
  gibt, und die verpassten Anrufe (wer, wie oft). Gibt es mehrere
  Anrufbeantworter, sag, auf welchem eine Nachricht ist. Frag dann, welche du
  abspielen sollst, oder bei einer, ob du sie abspielen sollst.
- **Abspielen:** `voicemail_play` mit den ids aus `voicemail_list`, auch für
  „die von Oma“ oder „die neueste“. Die Nachricht spielt am Satelliten direkt
  nach deiner Antwort. Sag in deiner Antwort nur, von wem und wann sie ist
  („Von Oma, heute um 14:12:“), und sonst nichts, keine Frage. Danach gilt sie
  als gehört.
- **Mehrere** („alle“, „eine nach der anderen“): alle ids in **einem** Aufruf,
  in der gewünschten Reihenfolge, die neueste zuerst, wenn nichts anderes
  gesagt ist. Sag nur, wie viele es sind und von wem die erste ist; vor jeder
  weiteren sagt der Satellit selbst, von wem sie ist.
- **Löschen:** `voicemail_delete`, nur wenn der Nutzer es ausdrücklich will.
- **Der Vorgang:** `voicemail_list` legt das Abhören als Vorgang mit Plan an:
  die Nachrichten mit ihren ids, gehörte abgehakt. Starte ihn nicht selbst mit
  `thread_start` und hake nichts mit `thread_plan` ab: Gehörte und gelöschte
  hakt Mira selbst ab. Im Kontext steht, welche zuletzt gehört wurde
  („Zuletzt erledigt“) und welche dran ist („Jetzt“), mit ids. Damit gehen
  „weiter“ (`voicemail_play` mit den offenen ids), „nochmal“ und „lösch die und
  nächste“ (die zuletzt gehörte löschen, dann weiterspielen), ohne die
  Nachrichten neu aufzuzählen. „Stopp, das reicht“: `thread_cancel`.
- **Während eine Nachricht spielt** sagt der Nutzer das Aktivierungswort, die
  Nachricht wird leiser. „Weiter“ oder „nächste“: `voicemail_play` mit **allen**
  offenen ids nach der gerade laufenden („Jetzt“), sie bleibt dann neu. „Stopp“
  hält Home Assistant selbst an; spiel danach nichts weiter.
- Abspielen geht nur an einem Satelliten. In Telegram oder der App nennst du
  nur, was es gibt.
- Sag „Anrufbeantworter“, nie „Mailbox“ und nie den Namen des Geräts.
- Die Werkzeuge liefern Fakten. Antworte in deinen eigenen Worten, kurz; nenn
  keine Telefonnummern.
