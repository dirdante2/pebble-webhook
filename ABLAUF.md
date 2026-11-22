# Abläufe

Stände eines Auftrags: `neu` → `transkribiert` → (`zugeordnet`, nur Planung) → `bearbeitet` → `fertig`. Jeder Schritt wird gespeichert; nach einem Neustart oder Fehler läuft nur der offene Schritt neu. Die KI arbeitet ausschließlich auf der internen Datenbank, Joplin ist reines Ziel und seit 20.11.2026 ausgeschaltet (`JOPLIN_URL` leer); die Joplin-Schritte unten gelten nur, wenn es wieder eingeschaltet wird.

## Aufnahme vom Pebble (Notiz)

In der Web-App und in Joplin heißt die Art „Notiz“ (Nutzervorgabe 15.11.2026); intern bleibt der Name `diktat` (Spalte `mode`, `?art=diktat`, `TRIGGER_*`).

1. Pebble-App → `POST /hook` (multipart: `audio`, `transcription`, `recordedAt`; Header `X-Index-Trigger`, `X-Index-Delivery`, `X-Index-Signature`, `X-Index-Timestamp`, eigener Token-Header).
2. Prüfen: Rate-Limit je Aufrufer → Token (constant-time) → `X-Index-Webhook-Version: 1` → Größe ≤ `HOOK_MAX_MB` → HMAC-SHA256 über `v1\n<ts>\n<delivery>\n<trigger>\n<0|1>\n` + Rohkörper, Zeitfenster 5 min.
3. Zustellungskennung schon bekannt → `200 bereits angenommen`, sonst nichts.
4. Geste → Aktion: `single-click-hold` = `TRIGGER_SINGLE`, `double-click-hold` (in der App „Klick + Halten“) = `TRIGGER_DOUBLE`, `test-event` = nur anzeigen; unbekannte Geste → Eintrag mit sichtbarem Fehler, keine Verarbeitung.
5. Freien Platz prüfen (`MIN_FREE_MB`) → Audio nach `AUDIO_DIR/JJJJ/MM/<id>.m4a` (fsync) → Auftrag in SQLite → **erst dann** `200 angenommen`. Speicherfehler → `507`, Pebble wiederholt (1×/min, bis 10×).
6. Worker: Transkription `POST /v1/audio/transcriptions` (bei `TRANSCRIPTION=pebble` das Pebble-Transkript); der Pebble-Text wird immer mitgespeichert. Liefert OpenAI nichts oder fehlt die Aufnahme, gilt der Pebble-Text; gibt es keinen Text → leere Aufnahme (siehe Löschen).
7. Auswertung `POST /v1/responses` (`OPENAI_DICTATION_MODEL`, höchstens eine Websuche, JSON-Schema): Kurztitel, Art (`notiz` | `listenpunkt`), `notiz` (kurze Fassung des Gesagten), `beschreibung` (Zusatzwissen, z. B. Inhalt eines Films), Eckdaten, höchstens 4 Hinweise, Quellen, Tags. Links stehen nur in den Quellen; Quellenverweise in Eckdaten werden entfernt. Kosten zählen wie Recherche (`PRICE_PLAN_*`, `RESEARCH_LIMIT_USD`). Scheitert sie auch beim letzten Versuch, bleibt das Diktat mit den ersten sechs Wörtern als Titel und ohne Auswertung.
8. Einsortieren in Gruppen und Listen siehe unten. `listenpunkt` → Punkte in die Liste (vorhandener Punkt kein zweites Mal). `notiz` → eigene Notiz und **immer** ein Punkt in einer Liste (Nutzervorgabe 21.11.2026): die Liste ihres Themas, bei zu wenig Information die generische Liste „Unsortiert“ (ohne Gruppe). Termine kommen in keine Liste. Einmaliger Nachtrag für Altbestand: `pebble notizen-einsortieren`.
9. Joplin-Push (siehe unten): Notiz nach `Sprachnotizen/Notizen` (Eckdaten, Hinweise, Wortlaut), Liste als eine Notiz mit Kästchen nach `Sprachnotizen/Listen` → `fertig` → SSE an offene Web-Apps. Kein Web-Push für Notizen. Das Notizbuch hieß bis 15.11.2026 „Diktate“: seine Id leitet sich weiter von diesem Namen ab, der Titel wird beim ersten Push einmal umgeschrieben (Merker `joplin_ordner_notizen` in `kv`).

## Aufnahme vom Pebble (Planung)

1. Schritte 1–6 wie bei der Notiz.
2. Zuordnung: keine offene Planung → neue. Beginnt der Text mit „Weiter“/„Fortsetzung“ und es gibt genau eine offene Planung → diese. Sonst KI-Zuordnung (`OPENAI_TITLE_MODEL`, JSON `{planung, sicher}`): sicher → diese Planung; unsicher → neue Planung (mit Schlüsselwort: die zuletzt bearbeitete offene). Geschlossene Planungen stehen nie zur Wahl.
3. Recherche `POST /v1/responses` (`OPENAI_PLAN_MODEL`, Tool `web_search`, JSON-Schema): Eingabe = bisheriger Stand + alle bisherigen Transkripte + neue Runde. Tiefe: 3 Suchen (`search_context_size: low`), bei „ausführlich/gründlich/…“ im Text bis 10 (`medium`), gedeckelt durch das Kostenlimit.
4. Kosten = Tokens × Preis + Suchen × Preis, je Auftrag aufsummiert. ≥ `RESEARCH_LIMIT_USD` → am Eintrag markiert; vor einem weiteren Versuch erreicht → Dauerfehler. Fehlt ein Preis in der Konfiguration, läuft keine Recherche.
5. Ergebnis (Kernfrage in einem Satz, Eckdaten als Kurzantwort, Hintergrund in kurzen Absätzen, 3–6 Tipps, Rückfragen nur wenn die Antwort sonst nicht stimmt – meist keine, je 3 Antwortvorschläge, „Annahme / unklar“, Quellen nur am Ende, in der Web-App eingeklappt; Zitate im Text werden entfernt) wird Stand der Planung; die Runde behält ihren Stand als Schnappschuss.
6. Joplin-Push der ganzen Planung als To-do in `Sprachnotizen/Planungsaufgaben` (aktueller Stand + Verlauf aller Runden mit Transkripten) → `fertig` → SSE, Web-Push „Planung fertig · n Fragen“. Web-Push gibt es nur für Planungen (fertig oder dauerhaft fehlgeschlagen).

## Antwort aus der Web-App (Fortsetzung)

1. Eintrag → „Antworten“ → Antwortseite: Text oder Aufnahme.
2. Die Antwort kommt mit eigener Kennung in den Ausgang (IndexedDB) und wird sofort gesendet: `POST /api/planungen/{id}/antwort`; ohne Verbindung bleibt sie vorgemerkt und geht bei Verbindung raus.
3. Server: Planung offen → neue Runde mit fester Zuordnung (Text überspringt die Transkription) → weiter wie Planung ab Schritt 3. Kennung schon bekannt → nichts doppelt.
4. Planung inzwischen geschlossen → `409`; die Web-App zeigt im Verlauf „Als neue Planung“ (→ `POST /api/planungen`) oder „Verwerfen“.

## Fragen der KI beantworten

1. Verlauf: Filterknopf „Fragen n“ (Summe über alle offenen Planungen; `GET /api/verlauf` liefert `fragen_offen`, `?art=fragen` nur Planungen mit Fragen). Karte und Planungsseite zeigen „n Fragen“ und „Beantworten“.
2. „Beantworten“ öffnet einen Drawer: immer eine Frage, drei Vorschläge oder „Eigene Antwort“, „Überspringen“, „Zurück“; am Ende Übersicht → „Senden“.
3. Gesendet wird **eine** Antwort mit allen Paaren „Frage: … / Antwort: …“ über `POST /api/planungen/{id}/antwort` → eine neue Runde, weiter wie Antwort aus der Web-App.
4. Solange diese Runde läuft, zählen die Fragen nicht; die neue Runde liefert den Stand ohne die beantworteten Fragen.

## Rückfragen an einer Notiz

1. Auswertung der Notiz: versteht die KI sie ohne Antwort nicht (unbekannter Name, Abkürzung, mehrdeutiges Ziel) und klärt das Gedächtnis es nicht, liefert sie bis zu 2 `rueckfragen` mit je 3 Antwortvorschlägen. Die Notiz wird trotzdem so gut wie möglich ausgewertet.
2. Verlauf: Karte zeigt „N Fragen“ + „Beantworten“; der Filter „Fragen“ zählt und zeigt Notizen und Planungen. Detailseite: Hinweis mit den Fragen und Knopf.
3. Drawer wie bei Planungen → `POST /api/eintraege/{id}/antwort` mit `antworten: [{frage, antwort}]` (offline vorgemerkt).
4. Server: Antworten an die Notiz und als Vorschlag „Rückfrage … – Antwort des Nutzers: …“ ins Gedächtnis; Listenpunkte der ersten Auswertung fallen weg; Stand zurück auf `transkribiert`.
5. Worker wertet neu aus, die Antworten stehen in der Eingabe. Wird die Notiz dabei zum Listenpunkt, verschwindet ihre frühere Joplin-Notiz. Danach wird nicht erneut gefragt (eine Fragerunde je Notiz).

## Listen

1. `GET /api/listen` (Name, offen/gesamt), `GET /api/listen/{id}` (Punkte: offene zuerst, dann erledigte).
2. Abhaken/wieder öffnen: `POST /api/listenpunkte/{id}/haken` (offline vorgemerkt) – in der Liste und auf der Detailseite der Notiz an ihren Punkten. Verschieben: eigene Seite → `POST /api/listenpunkte/{id}/verschieben` mit `ziel`. Löschen: `DELETE /api/listenpunkte/{id}`, ganze Liste `DELETE /api/listen/{id}` (Notizen bleiben).
3. Von Hand einsortieren: Notiz oder Planung → „In Liste“ (eigene Seite) → `POST /api/eintraege/{id}/liste` bzw. `POST /api/planungen/{id}/liste` mit `ziel` (Liste) oder `neu` (Name, vorhandener Name wird wiederverwendet). Nur fertige Notizen und Planungen mit Ergebnis; eine Planung zählt als Ganzes, nicht ihre Runden. Es entsteht ein Punkt mit Titel und Eckdaten; der Eintrag bleibt im Verlauf und in Joplin. Ein Eintrag steht in höchstens einer Liste: steht er schon in einer, zieht er um („Liste wechseln“). Herausnehmen = Punkt in der Liste löschen. Verschwindet die Planung, verschwindet ihr Punkt.
4. Jede Änderung markiert die Liste; der Worker schiebt die Notiz neu nach Joplin (`- [ ]` / `- [x]`). Haken aus Joplin kommen nicht zurück.

## Löschen

1. Leere Aufnahme (keine Sprache erkannt, kein Pebble-Text): Stand „leer“, kein Web-Push, grau im Verlauf; der Worker löscht sie `EMPTY_RETENTION_HOURS` (1) Stunden später.
2. Dauerhaft fehlgeschlagene Aufträge löscht der Worker `FAILED_RETENTION_HOURS` (24) Stunden nach dem Fehler (0 = nie). „Erneut starten“ hebt die Frist auf.
3. Von Hand, in jedem Stand (auch Fehlaufnahmen in Arbeit): „Löschen“ am Eintrag (`DELETE /api/eintraege/{id}`), „Runde löschen“ je Runde und „Planung löschen“ an der Planung, Statusseite „Alle fehlerhaften löschen“ (`DELETE /api/fehler`). Arbeitet der Worker gerade daran, wird die Verarbeitung abgebrochen (bis 30 s gewartet), dann in den Papierkorb gelegt.
4. Papierkorb (seit 21.11.2026): Von Hand Gelöschtes bekommt `deleted_at` (Jobs, Planungen, Listenpunkte; gemeinsam Gelöschtes trägt denselben Stempel) und verschwindet aus Verlauf, Listen, Suche, Terminen und Worker; die Joplin-Notiz wird entfernt, Aufnahme und Daten bleiben. Eine Planungsrunde fällt aus ihrer Planung: die steht danach auf dem Stand der übrigen Runden, ohne Runde geht sie mit in den Papierkorb. Nur 1. und 2. (Automatik) löschen sofort endgültig.
4a. Status → „Papierkorb“ (`GET /api/papierkorb`) → „Wiederherstellen“ (`POST /api/papierkorb/{eintrag|planung}/{id}/wiederherstellen`): Zeile und mitgelöschte Punkte zurück, Joplin-Notiz wird neu geschrieben, halb Verarbeitetes läuft weiter. Eine Runde bringt ihre gelöschte Planung mit; danach steht die Planung auf ihrer jüngsten Runde mit Ergebnis. Einzeln vorher gelöschte Runden bleiben im Papierkorb.
4b. Endgültig: „Endgültig löschen“ (`POST …/loeschen`) oder täglich bei der Bereinigung nach `TRASH_RETENTION_DAYS` (183) Tagen → Aufnahme, Punkte, Zeile weg.
5. Mehrere auf einmal: Verlauf → Karte lange drücken (0,5 s, seit 21.11.2026 statt Knopf „Auswählen“) → weitere Karten antippen (oder „Alle sichtbaren“) → „N erledigt“ (Notizen/Termine `POST /api/eintraege/{id}/erledigt`, Planungen `POST /api/planungen/{id}/schliessen`, je Eintrag) oder „N löschen“ → Rückfrage → `POST /api/verlauf/loeschen` mit `eintraege` und `planungen`. Eine Planung verschwindet dabei als Ganzes mit allen Runden und noch nicht zugeordneten Antworten darauf. `uebersprungen` zählt nur Fehlschläge.

## Planung schließen, Runde umhängen

1. „Planung schließen“ → `POST /api/planungen/{id}/schliessen` → Status `geschlossen` → Worker schiebt die Notiz erneut, To-do abgehakt. Offline wird die Aktion vorgemerkt.
   Automatisch: der Worker schließt offene Planungen `PLANNING_AUTOCLOSE_DAYS` (3) Tage nach Eingang ihrer jüngsten Runde, sofern keine Runde mehr in Arbeit ist; danach beginnt auch „Weiter …“ eine neue Planung.
2. „Umhängen“ (nur letzte Runde einer offenen Planung) → Zielseite → `POST /api/eintraege/{id}/umhaengen` mit `ziel` = Planungs-Id oder `neu`.
3. Die alte Planung fällt auf den Stand der Runde davor zurück (Joplin-Push); war es ihre einzige Runde, verschwindet sie samt Joplin-Notiz. Die Runde wird am Ziel neu recherchiert.

## Anzeige in der Web-App

1. Verlauf (`GET /api/verlauf?art=alles|diktat|planung|fragen`): Diktate einzeln, jede Planung als **ein** Eintrag mit dem Zeitpunkt und Text ihrer jüngsten Runde und einem Zeichen für offen/abgeschlossen.
2. Planungsseite (`GET /api/planungen/{id}`): aktueller Stand oben, darunter „Versionen“ – alle Runden, die neueste zuerst und als aktueller Stand markiert; frühere Stände aufklappbar.
3. Links auf eine einzelne Runde (`/eintrag/{id}`, z. B. aus Web-Push) führen auf die Seite ihrer Planung.

## Gelesen, Neu und Älter

1. Neuer Auftrag: `read_at = 0` (ungelesen). Beim Einführen der Spalte galt der Altbestand als gelesen.
2. Verlauf ohne `alle=1`: nur Ungelesenes und Einträge der letzten 24 Stunden, gruppiert in „Neu“ und „Letzte 24 Stunden“. Die Antwort nennt `aeltere` (ausgeblendete Einträge), `zahlen` je Filterknopf (seit 1.20.2 nur offene, jeder Filter mit seinem eigenen Zeitfenster; alle Knöpfe bleiben stehen) und `neu` je Filter (seit `seit=`).
3. Knopf „Alle anzeigen (N ältere)“ am Listenende → `GET /api/verlauf?alle=1` → zusätzlich die Gruppe „Älter“, darunter „Ältere laden“ in 50er-Schritten.
4. Eintrag oder Planung öffnen → Web-App ruft `POST /api/eintraege/{id}/gelesen` bzw. `POST /api/planungen/{id}/gelesen` (alle Runden). Das Vorladen der Details im Hintergrund markiert nichts. Offline wird nichts vorgemerkt; es zählt das nächste Öffnen mit Verbindung.
5. Eine neue Runde macht ihre Planung wieder ungelesen.
7. „Neu“ (gelb, seit 1.21.3 für alle Geräte gemeinsam): jede Aktion der Web-App (Tippen, Taste, Seitenwechsel, höchstens alle 20 s) → `POST /api/besuch` → kv `besuch` {zuletzt, gesehen}. Liegt `zuletzt` länger als `VISIT_GAP_MINUTES` zurück oder kommt `anmeldung: true` → `gesehen = zuletzt` (letzte Aktion des vorigen Besuchs, gleich auf welchem Gerät); dann `zuletzt = jetzt`. Das Gerät merkt sich `gesehen` (localStorage) für die Anzeige und schickt es als `seit=`. Neu = danach eingegangen und noch nicht aufgerufen (`read_at = 0`); Listenpunkte tragen dazu `gelesen` ihrer Notiz bzw. Planung.
6. Der Titel einer Notiz steht in der Web-App ohne das angehängte Datum; Datum und Uhrzeit stehen klein darunter. In Joplin bleibt das Datum im Titel.

## Gedächtnis

1. Tabelle `memory`: je Erkenntnis über den Nutzer ein Eintrag mit Zustand `vorschlag`, `aktiv` oder `geloescht` und Quelle (`ki`, `lernlauf`, `korrektur`, `antwort`, `nutzer`).
2. Jede Auswertung (Notiz, Planungsrunde, Lernlauf) bekommt mit: alle aktiven Einträge (höchstens 150), die offenen Vorschläge („schon vorgeschlagen“) und die letzten `MEMORY_REJECTED` (30) gelöschten als „nicht erwünscht“.
3. Neues entsteht immer als Vorschlag und wirkt erst nach Bestätigung. Das Gedächtnis enthält nur globalen Kontext (Schreibweisen, welcher Begriff zu welchem Thema/Projekt gehört, Abkürzungen, dauerhafte Vorlieben, Einsortier-Regeln), nie Einzelheiten einer Notiz (Nutzervorgabe 21.11.2026). Vorschläge formuliert immer die KI: Feld `merken` der Auswertung (bis 2), Lernlauf, Auswertung der Beobachtungen.
3a. Beobachtungen (Zustand `roh`, unsichtbar): Korrekturen des Nutzers (Punkt verschoben, von Hand einsortiert, Tags geändert, Tag-Vorschlag verworfen, Gruppen, berichtigter Wortlaut) und Antworten auf Rückfragen. 10 Minuten nach der jüngsten wertet der Worker sie aus (`learnObservations`, höchstens 4 allgemeine Vorschläge, meist keine) und entfernt sie; der Lernlauf nimmt offene Beobachtungen ebenfalls mit.
3c. Listen nennen KI und Gedächtnis immer mit Nummer und Gruppe (`listRef`: „L8 Notizen (Gruppe pebble webhook)“), weil gleiche Listennamen in mehreren Gruppen vorkommen. `pebble gedaechtnis-roh vorschlaege` macht offene Vorschläge zu Beobachtungen, damit die KI sie neu formuliert.
3b. `pebble gedaechtnis-zuruecksetzen` löscht alle Einträge samt Entscheidungen („nicht erwünscht“), Beobachtungen bleiben; danach „Jetzt lernen“. Einmal ausgeführt am 21.11.2026 (14 Einträge, Neulernen ergab 8 Vorschläge).
4. Derselbe Text wird nie zweimal angelegt, egal in welchem Zustand: Gelöschtes kommt nicht wieder; Sinngleiches verhindert die Liste „nicht erwünscht“.
5. Web-App: Status → „Gedächtnis“ (`GET /api/gedaechtnis`) mit den Reitern Vorschläge / Aktiv / Gelöscht. Vorschlag: „Übernehmen“, „Verwerfen“, „Ändern“; aktiv: „Bearbeiten“, „Löschen“; gelöscht: „Wiederherstellen“ → `POST /api/gedaechtnis/{id}` mit `aktion`. Ändern auf eigener Seite → `PUT /api/gedaechtnis/{id}` (Vorschlag gilt damit als übernommen). „Neuer Eintrag“ → `POST /api/gedaechtnis`, gilt sofort.
6. Lernlauf einmal pro Woche (Worker, `kv` `memory_train_at`; nach Fehler neuer Versuch nach 6 h) oder „Jetzt lernen“ (`POST /api/gedaechtnis/lernen`): nur die letzten `MEMORY_TRAIN_ENTRIES` (30) Notizen und Planungen (Kurzfassung) → höchstens 8 neue Vorschläge. Kosten stehen nur beim Lernlauf (`kv` `memory_train_info`).
7. Neue Themen-Listen: nennt ein aktiver Eintrag ein wiederkehrendes Thema, sortiert die KI passende kurze Notizen als Punkt in dessen Liste und legt sie bei Bedarf an.
8. Eine frühere Datei `DATA_DIR/memory.md` wird beim Start einmalig als Vorschläge übernommen und entfernt.

## Tags

1. Ein Vorrat für Diktate und Planungen; Tags sind klein geschrieben (wie Joplin seine Schlagwörter führt), höchstens 10 je Eintrag.
2. Bei Diktat (Auswertung) und Planung (Recherche-Aufruf) bekommt die KI die Liste „Vorhandene Tags“ und liefert `tags` (Auswahl aus dem Vorrat) und `neue_tags` (höchstens 2 Vorschläge).
3. Tags aus dem Vorrat werden sofort vergeben. Alles, was es noch nicht gibt, bleibt ein Vorschlag am Eintrag.
4. Web-App: Vorschlag „Übernehmen“ (→ Tag, ab dann im Vorrat) oder „Verwerfen“ (`POST …/tags/vorschlag`); „Tags ändern“ öffnet eine eigene Seite (`POST …/tags` setzt die Liste).
5. Verlauf: Trichter rechts in der Suchleiste (nur im Verlauf) → Dialog mit Mehrfachauswahl; jeder Tipp filtert sofort (`GET /api/verlauf?tag=a&tag=b`, UND, kombinierbar mit den Filterknöpfen). Zahlen und Ausgrauen rechnet die Web-App aus `kombis` von `GET /api/tags` (Tags je Eintrag): ausgegraut ist, was mit der Auswahl nie zusammen vorkommt. Aktiver Filter steht über der Liste mit „✕ Zurücksetzen“ (ohne Dialog). Zahl am Trichter = gewählte Tags.
6. Joplin: je Tag ein Schlagwort (Id aus dem Namen abgeleitet) und je Notiz eine Zuordnung; entfernte Tags lösen die Zuordnung. Geänderte Tags fertiger Einträge schiebt der Worker nach.
7. Löschen (seit 21.11.2026): Einstellungen → Tags listet jeden Tag mit Zahl und letzter Verwendung; „Löschen“ → `DELETE /api/tags/{name}` nimmt ihn aus Tags und Vorschlägen aller Notizen und Planungen (auch im Papierkorb), setzt `joplin_dirty`; der Worker löst die Joplin-Zuordnungen.

## Termine (seit 21.11.2026)

1. Auswertung der Notiz: `art = termin` für Termine und Erinnerungen – alles, was zu einem bestimmten Zeitpunkt passieren muss (Termin, Frist, „erinnere mich …“, Aufgabe mit Uhrzeit/Datum); geht vor Listenpunkt. Ohne Zeitangabe kein Termin. „Erinnere mich um 7“ = Erinnerung genau dann (`vorlauf_minuten 0`); Karte zeigt „Erinnerung“, wenn nur ein Erinnerungszeitpunkt gesagt ist, sonst „Termin“. Die KI bekommt die Aufnahmezeit („Aufgenommen am Samstag, 21.11.2026 um 09:14 Uhr“) als Bezug für „morgen“, „Dienstag“; sie liefert `termin` mit `beginn` (immer `JJJJ-MM-TTTHH:MM`), `ganztag`, `ort`, `vorlauf_minuten`, `in_minuten` (je -1 = nicht gesagt). Seit 21.11.2026: jeder Termin hat eine Uhrzeit; ohne genaue Uhrzeit 07:00 („Montag früh“ → 07:00), Tageszeiten vormittags 10, mittags 12, nachmittags 15, abends 18, nachts 22 Uhr; ganztägig nur bei ausdrücklichem „ganztägig“/„den ganzen Tag“.
2. Server rechnet um (`appointmentTimes`): liefert die KI doch nur ein Datum → 07:00, nicht ganztägig. Erinnerung = „in N Minuten“ ab Aufnahme, sonst gesagte Vorlaufzeit, sonst ganztägig bzw. ohne Uhrzeit 07:00 am Tag, sonst 1 Stunde vorher. Altbestand: bisher ganztägige Termine einmalig auf 07:00 umgestellt (kv `termine_7uhr`). Kein brauchbarer Zeitpunkt → bleibt Notiz mit Hinweis „Termin nicht erkannt“. Ein Termin kommt in keine Liste.
3. Spalten in `jobs`: `termin_at`, `termin_ganztag`, `erinnerung_at`, `erinnert_at`, `termin_ok_at` (bestätigt). Ändert eine Neuauswertung die Zeit, wird erneut erinnert.
4. Worker (`remindDue`, jede Runde; `NextWake` weckt zur Erinnerung): fällig und nicht gesendet → Web-Push „Termin: Titel“ mit Zeit und Ort → `/eintrag/{id}`. Ist der Termin schon vorbei, kein Push.
5. Verlauf: Filter „Termine“ (`GET /api/verlauf?art=termin`), „Notizen“ ohne Termine. Reihenfolge nach Termin: Verpasst (rot, bis bestätigt) → Heute → Kommend; „Erledigte anzeigen“ (`alle=1`). Jede Verlaufsantwort trägt `naechster_termin`, `termine_verpasst`, `termine_offen`, `termine_24h`; die Web-App zeigt darüber eine Zeile mit dem nächsten Termin (rechts Badge = offene Termine in den nächsten 24 h, ohne solche kein Badge) und ggf. „N verpasste Termine“.
6. Bestätigen/Erledigt bzw. „Wieder öffnen“: Wischen im Verlauf oder Knopf auf der Detailseite → `POST /api/eintraege/{id}/erledigt` mit `{erledigt: true|false}`. Nur mit Verbindung.
7. Nachtrag Altbestand (einmalig): `pebble termine-nachtragen [Tage]` (Standard 30) prüft fertige Notizen und Listenpunkte mit `OPENAI_TITLE_MODEL` nur auf Termin; Treffer werden `termin` und verlassen ihre Liste; Tags/Gedächtnis bleiben unberührt. Schon vergangene gelten als bestätigt.

## Wischen im Verlauf (seit 21.11.2026)

1. Karte nach rechts ziehen → grüne Box mit Haken = erledigt; nach links → rote Box mit Tonne = löschen. Auslösen ab 35 % der Breite (höchstens 120 px), sonst federt die Karte zurück. Im Auswahlmodus aus.
2. Erledigt je Art: Termin → bestätigt (`POST /api/eintraege/{id}/erledigt`), Notiz → alle ihre Listenpunkte abgehakt (dieselbe Route; ohne Liste 409 „erst einsortieren“), Planung → geschlossen (`/schliessen`). Erledigte bleiben stehen, abgeblendet und durchgestrichen.
3. Erledigtes nochmals nach rechts wischen (blaue Box mit Pfeil) → wieder offen: `POST /api/eintraege/{id}/erledigt` mit `{erledigt:false}` bzw. `POST /api/planungen/{id}/oeffnen` (Merker `closed_by = wiedergeoeffnet`, ab dann volle Frist bis zum automatischen Schließen). Detailseiten: „Erledigt“/„Wieder öffnen“ (Termin, Notiz mit Liste), Planung „Wieder öffnen“. Auf Terminkarten kein „Bestätigen“-Knopf, Bestätigen per Wischen.
4. Löschen über `POST /api/verlauf/loeschen` (Notiz oder ganze Planung).
5. Erledigt/offen ist ein fester Stand des Nutzers (`jobs.erledigt_at`, Planung: Status), unabhängig von Verarbeitung und Auswertung: geht sofort an den Server (feste Flags, kein Umschalter), „Rückgängig“ (5 s) sendet den anderen Stand hinterher. Löschen dagegen läuft erst nach 5 s; Seitenwechsel, App im Hintergrund oder die nächste Wischaktion führen es sofort aus.
6. `erledigt_at` schreibt nur `SetJobDone` (nie `SaveJob`): eine laufende Neuauswertung überschreibt ihn nicht und hakt neue Listenpunkte einer erledigten Notiz gleich ab. Abhaken in einer Liste setzt die Notiz auf erledigt, wenn alle ihre Punkte abgehakt sind (sonst offen). Die Recherche schreibt in die Planung nur Stand und Titel (`SavePlanResult`), Schließen/Öffnen währenddessen bleibt.

## Suche (seit 21.11.2026)

1. Feste Suchleiste im Kopf jeder Seite (nach Anmeldung). Tippen → nach 350 ms `/suche?q=…` (auf der Suchseite ersetzt jede Eingabe den Verlaufseintrag), Enter sofort.
2. `GET /api/suche?q=` (mind. 2 Zeichen): alle Notizen (ohne Planungsrunden und leere Aufnahmen) und Planungen. Durchsucht Titel, Wortlaut, alle Texte der Auswertung bzw. des Planungsergebnisses (ohne URLs) und Tags; alle Wörter müssen vorkommen, Groß-/Kleinschreibung egal. „#46“ oder „46“ findet die Nummer.
3. Reihenfolge: Treffer im Titel zuerst, sonst neueste; höchstens 100. Je Treffer die Zusammenfassung wie im Verlauf plus `fundstelle {feld, text}` (Ausschnitt um das erste Wort).
4. Ergebnisseite: flache Zeilen über die ganze Breite – Titel, #Nummer rechts, Art, Stand (offen/erledigt/verpasst/abgeschlossen/in Arbeit/Fehler), Datum, Fundstelle mit markierten Wörtern. Offline keine Suche.

## Wortlaut korrigieren (seit 21.11.2026)

1. Detailseite (Notiz, Termin) bzw. Planungsseite (letzte Runde) → „Wortlaut korrigieren“ → eigene Seite mit Textfeld → `PUT /api/eintraege/{id}/wortlaut` mit `{text}`. Nur wenn nichts mehr läuft; bei Planungen nur die letzte Runde.
2. Server: Wortlaut ersetzen (`transcript_source = nutzer`). Notiz: alte Listenpunkte weg (Projekt-Themen bleiben), Stand `transkribiert` → KI wertet neu aus (Titel, Text, Liste, Termin). Waren vorher alle Punkte abgehakt, werden die neuen ebenfalls abgehakt (Merker `liste_erledigt` in der Auswertung). Planungsrunde: Planung fällt auf den Stand davor zurück, Recherche dieser Runde neu (verworfene Kosten → Nebenkosten).
3. Lernen: alter und neuer Wortlaut wortweise verglichen; ersetzte Stellen (je Seite ≤ 4 Wörter, keine reinen Schreibweisen-/Satzzeichen-Änderungen, höchstens 10) werden Gedächtnis-Vorschläge „Spracherkennung: „X“ heißt richtig „Y“.“. Nach „Übernehmen“ gehen sie in jede Auswertung mit.
4. Offene Vorschläge stehen als „(n)“ an „Status“ und am Reiter „Gedächtnis“ (`GET /api/zaehler`, nach jeder Seite und bei SSE `gedaechtnis`).

## Fehler und „Erneut starten“

1. Schritt scheitert → Versuch zählen, nächster Versuch nach 30 s, 2 min, 10 min, 30 min, 2 h, 6 h; der Eintrag zeigt „wird wiederholt“.
2. Nach `MAX_ATTEMPTS` (oder bei Fehlern ohne Aussicht: Preise fehlen, kein Audio, leeres Transkript) → Dauerfehler, Eintrag steht in Verlauf und Statusseite, bis er gelöscht wird (siehe Löschen); Web-Push nur bei Planungen.
3. „Erneut starten“ → `POST /api/eintraege/{id}/neustart` → Zähler zurück, der gescheiterte Schritt läuft neu; fertige Zwischenstände bleiben.

## Joplin-Push (Einweg)

1. `POST /api/sessions` am Joplin-Server (Konto aus der Konfiguration), Sitzung wird wiederverwendet.
2. Notizbücher prüfen und bei Bedarf anlegen: `GET` / `PUT /api/items/root:/<id>.md:/content` (Ids fest aus dem Namen abgeleitet).
3. Notiz schreiben: `PUT /api/items/root:/<id>.md:/content` im Ablageformat der Clients; Id fest aus Auftrag bzw. Planung abgeleitet, eine Wiederholung überschreibt also dieselbe Notiz.
4. Die Joplin-Clients holen die Notiz beim nächsten Sync. Änderungen in Joplin fließen nicht zurück und gehen bei der nächsten Runde verloren. Voraussetzung: E2EE aus.

## Kalender-Abo (iCal, seit 22.11.2026)

1. Status → Kalender-Abo: `GET /api/kalender` erzeugt beim ersten Aufruf einen geheimen Token (kv `ical_token`) und zeigt den Link `<PUBLIC_URL>/kalender/<token>.ics`.
2. Der Nutzer abonniert den Link in Google Kalender („Per URL“), Apple oder Outlook (`webcal://`).
3. Der Kalender holt `GET /kalender/<token>.ics` (ohne Sitzung, Token zeitkonstant verglichen; falsch → 404) selbst ab – Google nur alle paar Stunden.
4. Inhalt: alle Termine (auch erledigte mit „✓ “), UID = Eintrags-ID, Zeiten in UTC, ganztägige als Datum, Ort, Notiz + Link, Erinnerung als VALARM. Papierkorb = nicht mehr drin.
5. „Neuen Link erzeugen“ (`POST /api/kalender/neu`) ersetzt den Token; der alte Link liefert sofort 404.

## Anmeldung

1. Erstes Gerät: auf dem Server `pebble enroll` → Einmal-Link (15 min) → im Browser öffnen → Passkey anlegen (`/api/auth/einrichten/start|ende`) → Sitzung (Cookie, 90 Tage gleitend).
2. Weiteres Gerät (seit 21.11.2026 per Kurz-Code): Status → „Weiteres Gerät hinzufügen“ (`/status/geraet`) → `POST /api/geraete/code` erzeugt 4 Ziffern (nur im Speicher, immer nur einer, höchstens 5 min) → die Seite meldet sich alle 10 s (`POST /api/geraete/code/halten`), ohne Meldung verfällt der Code nach 25 s, beim Verlassen `DELETE /api/geraete/code` → neues Gerät: Anmeldeseite „Mit Code hinzufügen“ → `POST /api/auth/code` (öffentlich; höchstens ein Versuch je 5 s für alle zusammen, sonst 429 „Nächster Versuch in N s“, der Knopf zählt herunter; nach 5 Fehlversuchen verfällt der Code) → Einmal-Token → `/einrichten#token` → Passkey anlegen. Der lange Einmal-Link (`POST /api/geraete/link`, `pebble enroll`) bleibt für die CLI.
3. Anmelden: `/api/auth/anmelden/start|ende` mit dem Passkey des Geräts. Kein Passwort.

## Bereinigung

0. Beim Start und danach täglich: Papierkorb älter als `TRASH_RETENTION_DAYS` endgültig löschen (siehe Löschen 4b).
1. Aufnahmen werden nie nach Alter gelöscht (seit 1.31.5, vorher `AUDIO_RETENTION_DAYS`), nur mit ihrem Eintrag bei der Endlöschung aus dem Papierkorb (Frist oder „Endgültig löschen“).
2. Leere Aufnahmen (keine Sprache) werden nach `EMPTY_RETENTION_HOURS` endgültig gelöscht, samt Datei. Fehlgeschlagene Aufträge werden nie automatisch gelöscht (Fehler beheben, dann „Neu starten“), nur von Hand.

## Anzeige in der Web-App (seit 17.11.2026)

1. Detailseite Notiz, feste Blöcke: Eckdaten → Liste mit Erledigt-Haken → Text (`notiz`, kurze KI-Fassung; ältere Einträge zeigen dort das Transkript) → Details (`beschreibung`, Hinweise, Links) → Karte Audio (Original-Wortlaut eingeklappt) → Technische Details (eingeklappt).
2. Planung: Eckdaten → Text → Details (Schritte, Ergebnisse, Fragen) → Links → Audio und Wortlaut je Runde.
3. Liste: je Punkt nur Haken und Titel; Titel führt zur Notiz bzw. Planung, „Mehr“ enthält Verschieben und Löschen.
4. Verlauf: Listenname als Badge oben, rechts Datum und Uhrzeit ohne Jahr; „fertig“ wird nicht angezeigt, andere Stände schon. Vorschau nur ab 100 Zeichen Transkript, sonst reicht der Titel.
5. Verlauf-Filter nach Tags: Trichter in der Suchleiste (siehe Tags 5).
6. Zurück-Knopf der Detailseite: aus einer Liste geöffnet → zurück zur Liste, sonst zum Verlauf.
7. Joplin-Ausgabe ist davon unberührt.

## Fortlaufende Nummer #N (seit 17.11.2026)

1. Notiz anlegen (`InsertJob`, nur Modus diktat) bzw. Planung anlegen (`CreatePlanning`) → nächste Nummer aus `kv.next_nr`, in derselben Transaktion. Ein Zähler für beide; Planungsrunden haben keine eigene Nummer.
2. Bestand: beim ersten Start mit der Spalte `nr` einmalig nach Eingang nummeriert, danach alles neu nach Joplin geschoben.
3. API: `nr` an Notiz, Planung, Runde (= Nummer der Planung) und Listenpunkt (= Nummer seines Eintrags).
4. Web-App: `#N` im Verlauf, im Kopf der Detailseite, an Listenpunkten.
5. Joplin: `#N ` vor dem Titel von Notiz und Planung, in Listen `- [ ] #N Text – Eckdaten`. Der gespeicherte Titel bleibt ohne Nummer.

## Gruppen und Listen: Einsortieren durch die KI (seit 20.11.2026)

Gruppe = größeres Thema (z. B. `servicemanager`, Spalte `lists.project`, Beschreibung in `list_groups`), darunter je Bereich eine Liste („Grundrissplan“, „Inventur“) mit den einzelnen Notizen als Punkte. Listen ohne Gruppe sind Sammellisten (Filme, Einkauf). Gruppen und Listen legt die KI selbst an. Löste am 20.11.2026 den Claude-Cron `~/apps/sprachnotizen-auswertung` ab.

1. Notiz-Auswertung (Schritt 7 oben): Eingabe enthält „Gruppen und Listen“ – je Gruppe ihre Beschreibung, je Liste Beschreibung und die 10 jüngsten Punkte (`#N Text`).
2. KI liefert `gruppe` + `liste`: bei `listenpunkt` die Liste für die Punkte; bei `notiz` nur für Ideen, Wünsche, Fehlerhinweise, Aufgaben zu einem Thema (sonst leer, im Zweifel leer, Doppeltes leer).
3. Liste suchen (Gruppe + Name, Groß/Klein egal; ohne Gruppe genannt und gleichnamige Liste in genau einer Gruppe → diese), sonst anlegen. Name leer oder > 40 Zeichen → nicht einsortiert, Hinweis „Nicht einsortiert: …“ an der Notiz.
4. `notiz` mit Liste: Notiz bleibt, zusätzlich ein Punkt (Kurztitel + Eckdaten) in der Liste. `listenpunkt`: Punkte wie bisher.
5. Planung geschlossen (Hand oder automatisch) → Worker `sortPlannings`: Planungen mit `sorted_at = 0` → KI (`DICTATION_MODEL`, Schema `einsortieren`) → ein Punkt in der Liste; Kosten an der letzten Runde. Schon von Hand einsortiert → nichts. Fehler → nächster Worker-Durchlauf, nach `MAX_ATTEMPTS` uneinsortiert (Log). Beim Einführen der Spalte galten geschlossene Planungen als geprüft.
6. Beschreibungen, Worker `describeIfDue`, einmal pro Woche (`kv.describe_at`, nach Fehler frühestens 6 h später neu): nur Listen und Gruppen mit Punkten jünger als ihre letzte Beschreibung → ein KI-Aufruf (`TITLE_MODEL`) mit den 20 jüngsten Punkten je Liste bzw. Gruppe → 1–2 Sätze. Ohne neue Punkte keine Änderung, kein Aufruf.
7. Web-App `/listen`: Listen ohne Gruppe, darunter je Gruppe ihre Beschreibung und Listen; Listenseite mit Beschreibung.
8. Von Hand: Listenseite → „Bearbeiten“ (`/liste/{id}/bearbeiten`) → `PUT /api/listen/{id}` mit `name`, `gruppe` (leer = ohne Gruppe, neue Gruppe wird angelegt); Gruppe + Name schon vergeben → 409.
9. Gruppe: Kopf in `/listen` → „Bearbeiten“ (`/gruppe/{name}/bearbeiten`) → umbenennen `PUT /api/gruppen` `{alt, name}` (Listen ziehen mit, Name vergeben → 409) oder „Gruppe auflösen“ `POST /api/gruppen/loeschen` `{name}`: Listen bleiben samt Punkten ohne Gruppe; gibt es eine gleichnamige Liste ohne Gruppe → 409.
10. Jede Änderung von Hand wird als Korrektur-Vorschlag ins Gedächtnis gelegt, damit die KI künftig so einsortiert.
11. Feste Listennummer `L<n>` (Spalte `lists.nr`, beim Anlegen MAX+1, Bestand nach Alter): angezeigt in Übersicht und Listenseite, an die KI als `L12 Name`. Nennt ein Diktat „Liste 12“, „Liste Nummer 12“ oder „L12“ (Ziffern, Regex), gilt diese Liste vor der Wahl der KI; ausgeschriebene Zahlen löst die KI über ihre Regel. Skill: `todos.py gruppe L12`.
12. Übersicht `/listen`: Gruppen (Listen ohne Gruppe unter „Ohne Gruppe“) und Listen nach jüngstem Punkt, neueste oben; Punkte einer Liste nach Eingang, neueste unten (offene vor erledigten). Klick auf den Gruppennamen filtert auf diese Gruppe, zweiter Klick oder „Alle Gruppen zeigen“ hebt auf (je Gerät gemerkt, `localStorage listen-filter`); Badge `offen/gesamt`; ganz erledigte Listen nur unter „Vergangen“; Gruppenbeschreibung nur auf der Bearbeiten-Seite der Gruppe.

## Maschinenzugang (Skill `sprachnotizen-todos`)

1. `GET /api/listen?punkte=1` (Gruppen, Listen, Punkte mit `nr`) → `GET /api/eintraege/{id}` bzw. `/api/planungen/{id}` (Volltext).
2. Abhaken: `POST /api/listenpunkte/{id}/erledigt` (wiederholbar; `{"offen":true}` nimmt den Haken zurück).

Token-Routen (alles andere bleibt bei der Passkey-Sitzung, kein Löschen): `GET /api/listen`, `GET /api/listen/{id}`, `GET /api/eintraege/{id}`, `GET /api/planungen/{id}`, `POST …/liste` (`ziel`, `neu` oder `projekt` = Gruppe + `thema` = Liste), `POST /api/listenpunkte/{id}/erledigt`. Mit `Authorization`-Header zählt nur der Token; `API_TOKEN` leer = abgeschaltet, mindestens 32 Zeichen, 300 Anfragen je Minute und Adresse.

## MCP-Server (seit 1.22.0)

1. Client → `POST /mcp` mit `Authorization: Bearer <API_TOKEN>` (Maschinenzugang) → JSON-RPC 2.0: `initialize`, `tools/list`, `tools/call`, `ping`; Benachrichtigungen → 202 ohne Inhalt.
2. Werkzeuge: `listen_uebersicht` (Gruppen, Listen, Punkte mit Kennung; Filter gruppe/liste/alle), `punkt_lesen` (Notiz mit Kurzfassung, Eckdaten, Hinweisen, Wortlaut bzw. Planungsstand), `punkte_abhaken` (einzeln, `offen: true` öffnet wieder) → gleicher Weg wie Abhaken in der Web-App (`setItemDone`: Liste, Joplin, Live-Update).
2a. Seit 1.24.0 außerdem: `termine` (`Appointments(true)`: kommende, verpasste unbestätigte; `tage`/`vergangene` begrenzen; Antwort beginnt mit heutigem Datum), `notizen_suchen` (ruft `handleSearch` intern auf, max. 30 Treffer), `notiz_lesen` (Notiz/Planung per #Nr aus `SearchableJobs`/`AllPlannings`).
3. Kennung je Punkt (`ItemRefs`): `59`, bei mehreren Punkten derselben Notiz/Planung `59.1`, `59.2` … nach Anlage über alle Listen. `#59` bei mehreren → Fehler mit den einzelnen Kennungen; `#59.*` = alle. Dieselbe Kennung liefert `GET /api/listen?punkte=1` als `ref` (Skill `sprachnotizen-todos`).

## MCP-OAuth (seit 1.23.0)

1. Client → `POST /mcp` ohne Token → 401 mit `WWW-Authenticate: Bearer resource_metadata=…/.well-known/oauth-protected-resource`.
2. Client liest Metadaten (`/.well-known/oauth-protected-resource`, `/.well-known/oauth-authorization-server`) → registriert sich (`POST /oauth/register`, Rücksprung nur https oder http://localhost; max. 50 Apps, unbenutzte nach 1 Tag weg).
3. Browser → `GET /oauth/authorize` (client_id, redirect_uri, PKCE S256, state) → geprüft → `/verbinden` in der Web-App (ohne Sitzung erst Passkey-Anmeldung, danach zurück) → „Erlauben“ (`POST /api/oauth/freigabe`) → Einmal-Code (5 min, nur im Speicher) an die Rücksprung-Adresse; „Ablehnen“ → `error=access_denied`.
4. Client → `POST /oauth/token` (Code + code_verifier) → Zugangs-Token (1 h) + Erneuerungs-Token (90 Tage, rotiert bei jeder Erneuerung).
5. `POST /mcp` mit Zugangs-Token (oder weiterhin API-Token) → wie oben. Tokens gelten nur für `/mcp`.
6. Speicher: kv `oauth` (Apps, Freigaben; Tokens nur als SHA-256). Status → „Verbundene Apps (MCP)“ zeigt Freigaben, „Trennen“ löscht sie sofort; `POST /oauth/revoke` ebenso.

## Ersteinrichtung und Einstellungen (seit 1.14.0)

1. `install.sh` legt Nutzer, Ordner, env-Datei (nur `LISTEN`, `DATA_DIR`, `AUDIO_DIR`) und systemd-Units an, startet den Dienst und gibt den Assistenten-Link aus.
2. Dienst ohne Einstellungen (oder mit ungültigen) → nur Einrichtungsassistent: `GET /api/ersteinrichtung?code=…` (Einmal-Code in `kv`, `pebble ersteinrichtung` gibt den Link aus; 20 Fehlversuche bis Neustart).
3. Formular mit allen Feldern aus `config.Fields`, Vorgaben vorausgefüllt, Token und Signatur-Secret erzeugt; „Prüfen“ → `POST /api/ersteinrichtung/openai` (GET /models).
4. Speichern → `POST /api/ersteinrichtung` → Prüfung wie beim Start (`config.FromValues`) → `kv` „einstellung:KEY“ → Code gelöscht → ohne Passkey Einmal-Link `/einrichten#token` → Dienst beendet sich, systemd startet neu.
5. Altbestand: hat die Datenbank keine öffentliche Adresse, übernimmt der Start einmalig alle Werte aus der Umgebung (alte env-Datei).
6. Danach: Status → „Alle Einstellungen“ (`GET /api/einstellungen`, Geheimnisse nur „gesetzt“) → „Bearbeiten“ (eigene Seite) → `PUT /api/einstellungen` (leeres Geheimnis = behalten) → Prüfung → speichern → Neustart.

## Updates (seit 1.14.0)

1. Hintergrund-Prüfung (`CheckUpdates`): 1 min nach dem Start, dann alle 4 h die Releases von `UPDATE_REPO`. Neue Version des aktiven Kanals → `GET /api/zaehler` liefert `update` → Pfeil-Symbol an „Status“ und Hinweiskarte auf der Statusseite → Klick öffnet Status → Updates (`GET /api/update`, aus dem Zwischenspeicher, „Jetzt prüfen“ = `?pruefen=1`): Versionen des aktiven Kanals (`UPDATE_CHANNEL`) samt stabilerer: stable nur stable, beta + stable, nightly alle drei (`channelsFor`); „neu“ = später veröffentlicht, aus fremdem Kanal zusätzlich höhere Nummer (außer eigener Build), Kanal per Auswahlliste → `POST /api/update/kanal` (sofort wirksam, ohne Neustart); „Ältere Version installieren“ bis 1.14.0 (ältere lesen die Einstellungen nicht aus der Datenbank). Reihenfolge: Basisversion, dann nightly < beta < stable, dann Nummer.
2. „Installieren“ → `POST /api/update` → App schreibt `DATA_DIR/update-request` („owner/repo X.Y.Z[-beta.N|-nightly.N]“) und `update-status`.
3. `pebble-webhook-update.path` → `install.sh --apply-request` als root: Anfrage prüfen (Muster), Dienst stoppen, laufenden Stand als Archiv nach `BACKUP_DIR` sichern (Art update, 30 Tage; Programm + DB zusätzlich nur für diesen Lauf in `$TMP`), Release laden (Vorrat `DATA_DIR/releases`), `SHA256SUMS` prüfen, Binary und Updater ersetzen, Dienst starten, 30 s `/healthz` – sonst Programm + DB zurück, `update-status` = `done X` bzw. `error: …`.
4. Web-App fragt alle 2 s nach, bis die neue Version läuft.
5. Ausrollen auf den eigenen Server: `deploy/deploy.sh` (verlangt Abschnitt in `CHANGELOG.md`, ruft `install.sh --binary`, sichert dabei ebenfalls die Datenbank).
6. Veröffentlichen: Push auf main baut nichts. Danach aktiv `deploy/release.sh [nightly|beta|stable]` (Vorgabe nightly) → prüft VERSION = gepushter Stand, setzt und pusht genau einen Tag `vX.Y.Z-<kanal>` → GitHub Actions (`.github/workflows/release.yml`) prüft Tag = `VERSION`, baut, veröffentlicht, verschiebt den Kanal-Release; nightly: Notizen aus `CHANGELOG.md`, 10 neueste bleiben; beta/stable: Abschnitt in `RELEASES.md` Pflicht. Bei beta/stable keine nightly derselben Version – die Update-Seite bietet über Kanäle hinweg an.


## Backups (seit 1.29.0)

1. Archiv `pebble-<version>-<kanal>-<zeit>-<art>[-audio].tar.gz` in `BACKUP_DIR` (Startwert in der env, Vorgabe `/srv/pebble-backup`, vom Installer angelegt und per `ReadWritePaths` freigegeben): `meta.json`, `pebble.db` (inkl. Einstellungen), `env`, optional `audio/`.
2. Geplant (`BackupSchedule`, minütlich): fällig nach `BACKUP_SCHEDULE` (täglich / montags / am 1.) ab `BACKUP_TIME`, einmal am Tag (kv `backup-letzt`) → `VACUUM INTO` → Archiv → Bereinigung (plan/manuell nach `BACKUP_RETENTION_DAYS`, update nach 30 Tagen, jüngstes bleibt).
3. „Jetzt sichern“ → `POST /api/backups` (Aufnahmen nach `BACKUP_AUDIO`); „Herunterladen“ → `GET /api/backups/{name}`.
4. „Wiederherstellen“ → `POST /api/update/zurueck` → `update-request` „restore <name> <repo>“ → Installer: Archiv als Nutzer pebble entpacken (keine Links), Version aus `meta.json`: gleich der laufenden → Programm bleibt; Kanal stable/beta/nightly → diese Version laden; lokal → Programm bleibt (Hinweis im Status). Dann wie Update: jetzigen Stand sichern, DB (+ Aufnahmen) einspielen, Programm ersetzen, Health-Check.
5. Aufnahmen ohne Eintrag (seit 1.30.0): beim Start, täglich (`Cleanup`) und bei `GET /api/status` Dateien in `AUDIO_DIR` gegen `jobs.audio_path` bzw. Id im Dateinamen prüfen (nur Pfad/Name, Dateien jünger als 1 min übersprungen) → Liste auf der Statusseite, Zähler `audio_unbekannt` in `GET /api/zaehler` (roter Zähler an „Status“, roter Punkt am Menüknopf) → „Importieren“ / „Alle importieren“ → `POST /api/audio-unbekannt/import` `{datei}` bzw. `{alle: true}` → neuer Auftrag (Quelle import, Modus diktat, Id aus dem Dateinamen wenn frei, Eingang = Dateizeit, Datei bleibt liegen) → Worker transkribiert und wertet aus. Seit 1.31.0: Ablage `AUDIO_DIR/<diktat|planung>/JJJJ/MM/<id>.<ext>`; beim Start `sortAudio`: bekannte Aufnahmen (über die Id im Dateinamen) in den Ordner ihrer Art verschieben, `audio_path` nach dem Ort der Datei richten. Import nimmt die Art aus dem Unterordner (ohne: diktat); importierte Planungsrunden hält `NextDue` zurück, bis keine importierte Notiz mehr offen ist, dann eine Runde ohne Rückfragen, Zuordnung durch die KI.

## Termin verschieben (seit 1.25.0)

1. Detailseite eines Termins → „Verschieben“ → Fenster (Datum, Uhrzeit, ganztägig) → `POST /api/eintraege/{id}/termin` `{beginn: "JJJJ-MM-TTTHH:MM" | "JJJJ-MM-TT", ganztag}`.
2. Prüfung: Notiz mit Termin, fertig ausgewertet (sonst 409), gültiges Datum, nicht gleich dem bisherigen.
3. Erinnerung: gleicher Vorlauf wie bisher; Wechsel auf ganztägig → 07:00 am Tag, auf Uhrzeit → 1 h vorher; `erinnert_at` zurück (erneut erinnern).
4. Auswertung: Hinweis „Verschoben am …; vorher: …“ angehängt, `termin_info` neu; bestätigter/verpasster Termin wieder offen; Joplin-Stand nachziehen (`joplin_dirty`), Live-Update, Worker wecken.

## Offline und Datenverbrauch

1. Erster Aufruf → Service Worker `/sw` legt die Hülle der Version (`/`, `app.js?v=…`, `app.css?v=…`, `theme.js?v=…`, Icons, Manifest) in den Cache `sprachnotizen-<v>`.
2. Jeder weitere Start → Hülle nur aus dem Cache; `/` wird im Hintergrund per ETag nachgefragt (meist 304).
3. Neue Version → Browser findet geänderten `/sw` (beim Start, sonst höchstens alle 10 min beim Zurückkehren) → neuer Cache, alter gelöscht → Seite lädt beim nächsten Seitenwechsel bzw. Zurückkehren ohne offene Eingabe neu.
4. Ansicht öffnen → gespeicherte Antwort aus IndexedDB (`antworten`) sofort zeigen → `GET /api/…` mit If-None-Match → 304 ohne Inhalt oder neuer Stand (gzip) → zeigen und speichern.
5. Live: SSE `/api/ereignisse` meldet Änderungen → nur die offene Ansicht fragt nach (ETag).
6. Audio → erst beim Abspielen (`preload="none"`), danach 30 Tage im Browser-Cache.

## Ring-Lebensdauer (seit 1.34.0)

1. Webhook nimmt Aufnahme an → Eintrag gespeichert → Zeile in `ring_recordings` (je Eintrag einmal): aktiver Ring, Ring-ID aus Header `RING_ID_HEADER` (optional), Länge aus dem Dateikopf (sonst aus den Transkriptionskosten), Art, Geste. Test-Events zählen nicht.
2. Transkription meldet Sekunden → Länge nachgetragen, wenn die Datei keine lieferte.
3. Löschen von Eintrag oder Audio → Zeile bleibt.
4. Erste Ring-ID für einen Ring ohne ID → übernommen. Andere ID als der aktive Ring → Hinweis auf allen Seiten, „Ring tauschen anwenden“ (`POST /api/ring/tauschen` mit `uebernehmen`) → neuer Ring ab der ersten Aufnahme mit der neuen ID.
5. Verbrauch erreicht 60/80/95/100 % der Kapazität → Push, Hinweis auf allen Seiten bis „Bestätigen“ (`POST /api/ring/bestaetigt`); bei 100 % „+30 min“ (`plus`) erhöht die Kapazität dauerhaft.
6. Von Hand: `/status/ring/tauschen` (Anleitung, ID, Kapazität) → bisheriger Ring endet jetzt. Ring bearbeiten: `PUT /api/ring/{nr}`.
7. Auswertung `GET /api/ring[?ring=N]`: Summen, Notizen/Planungen, Tastendrücke, Monate, Vorhersage 80/100 % (Mittel der letzten 30 Tage), Vergleich aller Ringe. Erster Start: Ring 1 aus dem Bestand (Einträge samt Papierkorb, Audiodateien ohne Eintrag).
