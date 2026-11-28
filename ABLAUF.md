# Abläufe

Kurzüberblick über die wichtigsten Abläufe. Bedienung und Installation: [README.md](README.md), [docs/usage.md](docs/usage.md).

Stände eines Auftrags: `neu` → `transkribiert` → (`zugeordnet`, nur Planung) → `bearbeitet` → `fertig`. Jeder Schritt wird gespeichert; nach Neustart oder Fehler läuft nur der offene Schritt neu.

## Aufnahme vom Pebble (Notiz)

1. Pebble-App → `POST /api/v1/pebble/hook` (multipart: `audio`, `transcription`, `recordedAt`; Token-Header und HMAC-Signatur).
2. Prüfen: Rate-Limit → Token → Größe (`HOOK_MAX_MB`) → Signatur und Zeitfenster (5 min). Bekannte Zustellung → `200`, nichts doppelt.
3. Audio verschlüsselt speichern → Auftrag anlegen → erst dann `200`. Speicherfehler → `507`, die Pebble-App wiederholt.
4. Transkription nach der eingestellten Reihenfolge (Cloud, Whisper lokal oder Pebble-Transkript).
5. Entscheidungsmodell (falls eingerichtet) → KI-Auswertung: Titel, Kurzfassung, Eckdaten, Hinweise, Quellen, Tags, ggf. Termin oder Rückfragen.
6. Einsortieren in Gruppe und Liste → `fertig` → Live-Update an offene Web-Apps.

## Aufnahme vom Pebble (Planung)

1. Schritte 1–4 wie bei der Notiz.
2. Zuordnung zu einer offenen Planung („Weiter …“, Entscheidungsmodell oder KI), sonst neue Planung.
3. Recherche mit Websuche (Tiefe und Kostenlimit einstellbar) → neuer Stand der Planung; jede Runde bleibt als Version erhalten.
4. Rückfragen der KI lassen sich in der Web-App beantworten → neue Runde.
5. Planungen schließen sich von selbst nach `PLANNING_AUTOCLOSE_DAYS` ohne neue Runde.

## Aufnahme in der Web-App

1. „+“ → Text oder Sprachaufnahme (max. 60 s).
2. Ausgang im Browser (IndexedDB) → `POST /api/notizen`; ohne Verbindung bleibt der Eintrag vorgemerkt.
3. Weiter wie Notiz vom Pebble ab Transkription. Web-Aufnahmen zählen nicht zur Ring-Nutzung.

## KI-Anbieter je Bereich

1. Status → KI: Zugänge anlegen (OpenAI-kompatibel, lokal, SearXNG).
2. Je Bereich (Transkription, Kurztitel, Auswertung, Recherche, Bilder, Entscheidung) eine Reihenfolge mit Modell, Preisen und Monatslimit.
3. Aufruf: erster Eintrag unter seinem Limit; Fehler oder Limit → nächster. Kosten je Anfrage werden protokolliert.

## Entscheidungsmodell (Bereich Entscheidung)

Nur mit Eintrag im Bereich Entscheidung (OpenRouter-Decisions-API); Antwort gilt ab `DECISION_MIN_CONFIDENCE` (Vorgabe 0,7), sonst entscheidet die KI wie bisher.

1. Notiz, vor der Auswertung, eine Anfrage: Typ (Termin/Notiz), Inhalt, „passt eine vorhandene Liste?“ und welche (`L<n>`).
2. Sichere Liste → Auswertung sieht nur diese Liste und muss sie nehmen; Vorrang behalten „Liste N“ im Diktat, Anweisungen des Nutzers und die Liste einer Bezugsnotiz. Sicher kein Termin → `art` termin gilt nicht.
3. Abgeschlossene Planung und Nachtrag alter Notizen: „passt eine Liste?“ + welche → sicher: direkt einsortiert, sonst KI-Anfrage.
4. Planungs-Runde: „setzt eine offene Planung fort?“ + welche → sicher ja: angehängt (`entscheidung`), sicher nein ohne „Weiter“: neue Planung, sonst KI-Zuordnung.
5. Antworten (mit Sicherheit) stehen an der Notiz unter `entscheidung`; Kosten zählen zur Notiz bzw. Runde.

## Listen, Gruppen, Tags

1. Die KI legt Gruppen und Listen selbst an und sortiert jede Notiz als Punkt ein; Listen haben eine feste Nummer `L<n>`, Punkte die Nummer `#N` ihrer Notiz.
2. Steht ein Punkt schon in der Liste (andere Notiz oder Planung), kommt er nicht doppelt hinein; die Notiz bleibt unter Notizen, markiert „Doppelt“ mit Link aufs Original.
3. Erneute Auswertung (Rückfrage, Wortlaut, Neu-Transkription, Anweisung): die alten Punkte fallen erst weg, wenn die neue Auswertung vorliegt; eine von Hand gewählte Liste gilt weiter (bis zu einer neuen Anweisung), Haken gleicher Punkte bleiben.
4. Von Hand: Notiz → „In Liste“, Liste/Gruppe bearbeiten. Korrekturen landen als Vorschlag im Gedächtnis.
5. Tags: Auswahl aus dem Vorrat, neue nur als Vorschlag zum Übernehmen.

## Gedächtnis und Begriffe

1. Gedächtnis: allgemeiner Kontext über den Nutzer (Schreibweisen, Themen, Vorlieben). Neues entsteht als Vorschlag und gilt erst nach Bestätigung.
2. Wöchentlicher Lernlauf oder „Jetzt lernen“.
3. Begriffsliste für die Spracherkennung: von Hand, aus Korrekturen am Wortlaut oder von der KI; wird bei jeder Transkription mitgesendet.

## Termine

1. Erkennt die Auswertung einen Zeitpunkt, wird die Notiz zum Termin mit Erinnerung (Web-Push).
2. Verlauf zeigt verpasste, heutige und kommende Termine; bestätigen per Wischen oder auf der Detailseite.
3. Kalender-Abo: Status → Kalender-Abo → geheimer iCal-Link.

## Bearbeiten und Löschen

1. Wortlaut korrigieren, neu transkribieren, KI-Anweisung je Notiz → Neuauswertung.
2. Löschen legt in den Papierkorb (wiederherstellbar); endgültig nach `TRASH_RETENTION_DAYS` oder von Hand.
3. Leere Aufnahmen werden nach `EMPTY_RETENTION_HOURS` gelöscht.
4. Fehler: automatische Wiederholung mit wachsendem Abstand, danach „Erneut starten“.

## Anmeldung und Nutzer

1. Erste Einrichtung: Assistent im Browser (nur in den ersten Minuten nach dem Start), 6 Schritte mit Zusammenfassung → erster Admin legt einen Passkey an → Wiederherstellungscode (bei mehreren Admins zusätzlich der Code des Systemschlüssels).
2. Weitere Geräte per 4-stelligem Code, weitere Nutzer per Einladungslink.
3. Daten je Nutzer verschlüsselt; Wiederherstellungscode für den Schlüssel.
4. Neuer Code (Benutzer `/konto`, bis 10 min nach Passkey-Anmeldung) → neuer Schlüssel + Code in `KEY_DIR/<id>.key.neu` → Daten gesperrt, Backups warten → Aufnahmen/Bilder (`*.enc`) einzeln neu verschlüsselt → `daten.db` per VACUUM INTO neu, getauscht → system.db, Schlüsseldatei, Systemschlüssel-Bindung → `.key.neu` weg, Daten offen, Code einmal anzeigen → neues Backup. Fortschrittsbalken; Abbruch: beim Start oder per „Fortsetzen“ weiter, jeder Schritt wiederholbar.

## API-Token und MCP

1. Benutzer `/konto` → „App verbinden“ (`/konto/verbinden`) → App wählen → OAuth-Schritte oder Token mit Namen erzeugen (beliebig viele, je App eines) → Befehl/Beispiele mit eingesetztem Token → Liste unter „Apps und Zugänge“, einzeln löschen.
2. Ein API-Token erlaubt lesenden Zugriff auf Listen und Einträge sowie das Abhaken von Punkten.
3. `POST /api/v1/mcp` (JSON-RPC) mit API-Token oder OAuth: Listen, Termine, Suche, Notizen lesen und importieren, Listen aufräumen (nur mit Bestätigung).
4. Externe Quellen können per `POST /api/eintraege` Text-Notizen anlegen.

## Updates und Backups

1. Die App prüft Releases des eingestellten Kanals (stable, beta, nightly) → Status → Updates → „Installieren“.
2. Der Updater sichert den laufenden Stand, lädt das Release, prüft `SHA256SUMS`, startet neu und rollt bei Fehler zurück.
3. Datenbank-Migrationen laufen beim Start; ein neuerer DB-Stand als das Programm verhindert den Start.
4. Backups geplant oder von Hand; Wiederherstellen über die Statusseite.
5. Docker: Updates per `docker pull`; Wiederherstellen beim nächsten Start.

## Anonyme Statistik

1. Optional (Admin-Schalter): monatlich verschlüsselt App-ID, Version und Ring-Nutzung je anonymem Ring-Hash.
2. Ohne eingebauten Empfänger ist die Funktion aus.

## Adressen von außen

1. Alle Eingänge liegen versioniert unter `/api/v1/…`: Pebble-Webhook `/api/v1/pebble/hook`, Kalender-Abo `/api/v1/calendar/<token>.ics`, MCP `/api/v1/mcp` mit OAuth unter `/api/v1/oauth/…`, Rücksprung von OpenRouter `/api/v1/ai/connect/openrouter`. Die Einrichtung nennt nur diese.
2. Neue Version (`/api/v2/…`): die vorige bleibt gültig, jeder Aufruf wird gezählt, und die Web-App zeigt „Adresse geändert“ mit der neuen Adresse (30 Tage nach dem letzten Aufruf; „Erledigt“ blendet aus). Die Adressen vor v1 gibt es nicht mehr.

