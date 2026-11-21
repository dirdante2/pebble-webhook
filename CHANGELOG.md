# Changelog

Every version gets a short list of what is new, changed or fixed. The web app shows these notes when it finds an update.

Version numbers X.Y.Z:
- **X** – breaking for database or app: needs a new database or is hard to roll back (e.g. migrations that change columns).
- **Y** – a new feature (e.g. appointments, multiple languages, theme choice with day/night mode).
- **Z** – all other UI changes and small bug fixes; safe to install or roll back at any time.

## [1.28.1] - 2026-11-22

### New
- Safer updates: after installing, the installer waits up to 30 s for the service to answer /healthz. If it does not, it goes back to the version that ran before – program and database from the backup – and reports "rolled back" on the update page.
- The service refuses to start on a damaged database (SQLite quick_check), which triggers that rollback after an update.

### Fixed
- Editing a setting from the update page (time of the automatic update) returns to the update page after saving or cancelling.

## [1.28.0] - 2026-11-22

### New
- Automatic update: once a day at a chosen time (default 04:00) the newest version of the update channel is installed if it is newer than the running one; a backup is taken first. Switched on or off on the update page (default on), the time is set under Configuration (`AUTO_UPDATE`, `AUTO_UPDATE_TIME`). The page shows the last run.

## [1.27.11] - 2026-11-22

### New
- Downloaded releases stay on the server (DATA_DIR/releases); switching back to one does not download it again (checksums are still verified). The update page marks them "lokal". Releases unused for 30 days are deleted, like backups.

## [1.27.10] - 2026-11-22

### Changed
- Updates: the cache keeps the last 20 releases per channel; older ones are dropped. The page shows 3, "Mehr" shows the rest of the chosen channel.

## [1.27.9] - 2026-11-22

### Changed
- Updates: the newest version of a channel is labelled "latest"; "new" only marks versions released later than the installed one (compared by date, since channel numbers are independent). Channel buttons show an arrow icon when that channel has something newer.
- The channel badge stands left of the version (installed version and backups).

## [1.27.8] - 2026-11-22

### Changed
- Updates and backups are two tabs on the update page.
- The installed version shows its channel (stable, beta, nightly, or local for own builds); a version counts as installed only if number and channel match. Coming from another channel, the newest version of the chosen one is marked "neueste" with a hint instead of "neu".

## [1.27.7] - 2026-11-22

### Fixed
- A backup holds only the live database files (no stray old copies).

## [1.27.6] - 2026-11-22

### Changed
- "Frühere Stände" is now "Backups": one per update or version switch, with version, channel (stable/beta/nightly, or local for own builds) and date, newest first.
- Backups older than 30 days are deleted automatically (daily, and by the installer); the newest one always stays.

## [1.27.5] - 2026-11-22

### Changed
- Update page rebuilt: installed version on top, channel as a button group, only the last 3 versions of the chosen channel as compact rows, earlier states below. Change lists open on demand in a sheet ("Änderungen") with a link to the full list on GitHub; no more long logs on the page.

## [1.27.4] - 2026-11-22

### New
- Updates: the last 3 versions of every channel (the chosen one first, the others folded).
- "Frühere Stände": go back to program and database as they were before an update; the current state is backed up first. The installer now saves the program next to the database backup (older backups hold only the database).

## [1.27.3] - 2026-11-22

### Fixed
- Update source defaults to the project repository; before, the update check was off unless the setting was saved once, so no releases showed up.
- Updates: versions of a channel are ordered by release date, not by number – channels are independent (a stable can have a higher number than a nightly). The newest one of the channel is offered unless it is installed; every other version of the channel can be installed.

### Changed
- Releases are cached in the database; only the background check (every 4 hours) or "Jetzt prüfen" asks GitHub.
- "Frühere Änderungen" shows the last 10 versions with a link to the full list on GitHub.

## [1.27.2] - 2026-11-21

### Fixed
- Updates: the page lists all versions of the chosen channel, newest first, separate from the installed one; every version can be installed (back to 1.14.0).
- Switching channel (e.g. nightly → stable) offers the newest version of the new channel, even if its number is lower than the installed one.
- Pre-release builds (e.g. 1.26.1-nightly.N) show the change list of their version.

## [1.27.1] - 2026-11-21

### Fixed
- English: appointment times no longer show a stray space before the full stop; the menu says "Appointments" (the filter bar keeps the short "Dates").

### Changed
- README and screenshots in English.

## [1.27.0] - 2026-11-21

### New
- Language setting German/English (`LANGUAGE`): chosen in the setup wizard (preset from the browser language, switches the wizard at once) and under Status → Configuration.
- English covers the web app, server messages, push notifications, MCP answers and tool descriptions, Joplin note headings, date formats, and what the AI writes; transcription follows the language. Existing entries stay as they are.

## [1.26.1] - 2026-11-21

### Fixed
- Detail page: with three main actions (e.g. a planning with open questions) the first one spans the full width; button labels no longer break mid-word.

### Changed
- Screenshots in the README and docs (demo data).

## [1.26.0] - 2026-11-21

### New
- Update page: "Frühere Änderungen" lists the changes of earlier versions. On nightly every single version (CHANGELOG.md); on beta and stable the summarised release notes (new RELEASES.md: only what lasts since the previous beta/stable, written when a release is tagged).
- Release workflow: beta and stable tags take their notes from RELEASES.md (required), nightly builds from the CHANGELOG.md section of their version.

## [1.25.1] - 2026-11-21

### Changed
- Reschedule window easier on phones: large date and time fields (tapping opens the system picker), ‹ › for one day or 15 minutes, quick picks (Heute, Morgen, +1 Woche; 07:00 … 18:00), weekday shown next to the date.

## [1.25.0] - 2026-11-21

### New
- Appointments can be rescheduled: second button "Verschieben" on the detail page opens a window with date, time and "ganztägig". The new time becomes the appointment, the old one is kept as a note ("Verschoben am …; vorher: …"); the reminder keeps its lead time and comes again, a confirmed or missed appointment is open again.

## [1.24.4] - 2026-11-21

### Changed
- Planning detail page: two main actions side by side, "Antworten" and "Schließen" (a closed planning shows "Wieder öffnen").

## [1.24.3] - 2026-11-21

### Fixed
- "⋯" sheet: focus on the sheet itself, really without highlighting the first entry.

## [1.24.2] - 2026-11-21

### Fixed
- The "⋯" sheet no longer highlights its first entry when it opens.

## [1.24.1] - 2026-11-21

### Changed
- Detail page of a note or planning tidied up: the number sits larger in the breadcrumb next to a "⋯" button; the title adapts its size to its length; below it only date, time and tags. One large main action (Erledigt, Antworten …), everything else (Teilen, Kopieren, Wortlaut, Liste, Tags, Papierkorb) in a sheet from the "⋯" button. List badge, type badge and "fertig" are gone; recording and transcript are collapsed.

## [1.24.0] - 2026-11-21

### New
- MCP tools beyond lists: `termine` (appointments with date, time and place; next N days or also past ones), `notizen_suchen` (full-text search in all notes, appointments and plannings) and `notiz_lesen` (any note or planning by its number).

## [1.23.0] - 2026-11-21

### New
- OAuth for the MCP server: Claude, ChatGPT and other apps connect as a connector with just the address `/mcp`; you approve them on a page of the web app (passkey sign-in). Status lists the connected apps; "Trennen" revokes access.

## [1.22.6] - 2026-11-21

### Fixed
- Listen: "Ohne Gruppe" lines up with the other group titles (space where the pencil icon sits).

## [1.22.5] - 2026-11-21

### Fixed
- Space for the scroll bar is always reserved (page and menu), so the layout no longer shifts when content gets long enough to scroll.

## [1.22.4] - 2026-11-21

### Changed
- Listen: group titles are buttons (open the group), with a pencil icon to edit and more space between groups.

## [1.22.3] - 2026-11-21

### Changed
- Breadcrumbs are large tap targets; the first one has a back arrow.
- Group page: pencil icon left of the group title to edit it; expanding or collapsing the open items is a large icon on the right of the title (also on Listen).

## [1.22.2] - 2026-11-21

### Fixed
- Breadcrumb of a note opened from a list shows the list number (L9).

## [1.22.1] - 2026-11-21

### Changed
- Breadcrumbs instead of back buttons: "Listen › Gruppe › Liste › #Nr" on a note or planning opened from a list, "Verlauf › #Nr" otherwise.
- A list group opens as its own sub-page (/listen/gruppe/…, in the menu too) with "Listen › Gruppe" on top instead of being a filter.

## [1.22.0] - 2026-11-21

### New
- MCP server at `/mcp` (Bearer API token): read lists and items, read an item in full, tick items off or re-open them – from Claude, OpenAI and other MCP clients.
- List items can be ticked off one by one: a note with several items gets ids #59.1, #59.2 …; the machine API returns them as `ref`.

## [1.21.4] - 2026-11-21

### Changed
- README and documentation brought up to date (menu, status tabs, Joplin, configuration with explanations and validation, themes, "new" across devices).

## [1.21.3] - 2026-11-21

### Changed
- "Last seen" is shared by all devices (stored on the server): what was already new on the phone is not new again on the desktop days later.

## [1.21.2] - 2026-11-21

### Changed
- An entry that has been opened is never marked as new (history, menu counters, list dots).
- History tabs on the page only show filters with open entries ("Alles" and the selected one always stay); the menu still lists all filters.

## [1.21.1] - 2026-11-21

### Fixed
- Tabs wrap into two rows up to 640 px width, so "Planungen" is readable on larger phones too.

## [1.21.0] - 2026-11-21

### New
- Joplin switch (aktiv/inaktiv) in the configuration; when active, Status has a "Joplin" tab to set up the connection and test it before saving.

### Changed
- On narrow screens the tabs of history and status wrap into two rows.
- Every save checks the values: number ranges, valid addresses (public address only https://host without path or invalid characters), IP of the reverse proxy, header and model names, update source. Invalid values are rejected with a message instead of breaking the service.

## [1.20.5] - 2026-11-21

### Changed
- Konfiguration: each value has a "?" (short explanation below it) and a pencil to change just that value; the page for editing all values at once is gone.
- Every setting has a short explanation.

## [1.20.4] - 2026-11-21

### Changed
- "New" now means: received after your last action of the previous visit (every tap, key press or page change stores its time; a new visit after a pause or a login takes that time as "last seen"). Before, it was the start of the previous visit.

## [1.20.3] - 2026-11-21

### Changed
- Lists: open items added since the previous visit get a muted yellow dot on the right, left of their number.

## [1.20.2] - 2026-11-21

### Changed
- History counters (tabs and menu) count only open entries; all tabs and menu items are always shown and clickable, done entries stay visible in the view.
- Empty views say "Keine Einträge vorhanden."

## [1.20.1] - 2026-11-21

### Fixed
- History filter counts use the time window of their own filter: "Planungen" no longer shows 0 (and disappears) while that view has entries.

## [1.20.0] - 2026-11-21

### New
- Status → Theme: day/night mode (automatic, light, dark) and colour palettes (blue, green, violet, amber, graphite) or a custom colour.
- Theme page previews the 3 latest entries and the button styles.
- Menu footer with the version and a theme icon stays at the bottom; "Abmelden" is the last menu item.
- Chosen per device; a new device takes over the last chosen appearance when it is opened for the first time.

## [1.19.7] - 2026-11-21

### Fixed
- History filters without entries (counter 0) are greyed out in the menu and not clickable; before, they jumped back to "Alles".

## [1.19.6] - 2026-11-21

### Changed
- On wide screens (from 1100 px) the menu stays visible on the left; the menu button is only used on narrow screens.
- Header in one row: menu button and search bar; on desktop the search bar sits centred.

## [1.19.5] - 2026-11-21

### Changed
- Menu: the list groups also show the muted yellow badge for open items added since the previous visit.

## [1.19.4] - 2026-11-21

### Changed
- Menu: a muted yellow badge next to each history filter counts the entries that are new since the previous visit.

## [1.19.3] - 2026-11-21

### Fixed
- The second gesture is called "Klick + Halten" (as in the Pebble app), not "Doppelklick + Halten".

## [1.19.2] - 2026-11-21

### Changed
- "Abmelden" button at the bottom of the menu, next to the version.

## [1.19.1] - 2026-11-21

### Changed
- Status has tabs: Übersicht, Pebble-App, Konfiguration (each its own page); Pebble-App left the menu.

## [1.19.0] - 2026-11-21

### New
- Add a device with a 4-digit code instead of a long link: the code is shown under Status → "Weiteres Gerät hinzufügen", is valid for at most 5 minutes and only while that page stays open; the new device enters it on the login page ("Mit Code hinzufügen"); at most one attempt every 5 seconds, the button counts down.
- Tapping a tag on the Tags page opens the history filtered to that tag.

### Changed
- Status page: short stats on top, then notifications, devices and the configuration (values collapsed, "Konfiguration bearbeiten").
- Pebble app is its own menu item again; "Konfiguration" is no longer a menu item.

## [1.18.0] - 2026-11-21

### New
- Einstellungen → Tags: every tag with its number of entries; deleting removes it from all notes and plannings (also in Joplin).
- The installed version is shown at the bottom of the menu.

### Changed
- Status page tidied up: Pebble app setup is part of it (values collapsible), the settings summary is gone; Konfiguration moved to the end of the menu.

## [1.17.1] - 2026-11-21

### Changed
- Menu: counters on the right of every sub-page (entries, open list items, memory suggestions, trash); Listen shows the 5 most recent groups plus "Ohne Gruppe".
- Update page shows the installed version with its change notes.

## [1.17.0] - 2026-11-21

### Changed
- Menu button (☰) on the left instead of the tab bar: Verlauf, Listen and Einstellungen with their sub-pages (history filters, list groups, status, settings, updates, memory, AI costs, trash, Pebble app).
- A dot on the menu button shows an available update or open memory suggestions.

### Removed
- Sub-tab bar on the status pages (now in the menu).

## [1.16.1] - 2026-11-21

### Fixed
- Update icon visible on the active "Status" button; long tab names in the status bar are shortened at the end.

## [1.16.0] - 2026-11-21

### New
- The service checks for updates in the background every 4 hours; an icon on "Status" and a card on the status page announce a new version.
- Switch the update channel with a dropdown on the update page (applies immediately).

### Changed
- Only releases of the active channel are offered (beta shows only betas, nightly only nightlies).

## [1.15.1] - 2026-11-21

### Fixed
- Update page: newer versions below the newest were offered as rollback.

## [1.15.0] - 2026-11-21

### New
- Update channels: stable (long-term), beta (new features, not mature yet), nightly (every change, untested); chosen in the settings.
- Roll back to any older release from the update page (down to 1.14.0).
- The database is backed up before every update or rollback (newest 5 kept).
- Releases are built by GitHub Actions: nightly on every push, beta and stable from tags.

## [1.14.0] - 2026-11-21

### New
- Setup wizard on first start: all settings are entered in the browser, prefilled with defaults; webhook token and signing secret are generated.
- Status → Einstellungen: view and edit every setting in the web app; the service restarts itself after saving.
- Update check under Status → Updates: shows newer releases with their change notes and installs them with one click.
- `install.sh`: installs or updates on Debian/Ubuntu with systemd (release download with checksum check, or build from source).

### Changed
- Settings live in the database; the env file only keeps `LISTEN`, `DATA_DIR` and `AUDIO_DIR`. An existing env file is imported once on the first start.

## [1.13.1] - 2026-11-21

### New
- Tag filter filters live while tags are tapped; tags that never occur together with the selection are greyed out.
- Active tag filter is shown above the history with a one-tap reset.

## [1.13.0] - 2026-11-21

### New
- Tag filter as a funnel icon in the search bar with multi-select (entries must carry all selected tags).
- Long-press a card to start multi-select; the selection bar can mark entries as done, not only delete them.
- Badge on the next-appointment line: appointments in the next 24 hours.

### Removed
- "Auswählen" and "Tags" buttons above the history.

## [1.12.2] - 2026-11-21

### Changed
- Appointments always have a time: without one they start at 07:00; all-day only when said explicitly.

## [1.12.0] - 2026-11-21

### New
- Trash: deleted entries can be restored under Status → Papierkorb and are removed for good after 183 days.

## [1.11.3] and earlier

- Voice notes from the Pebble Index 01 via webhook, transcription, notes, appointments, plannings with web research, lists and groups sorted by AI, memory, tags, search, Joplin export, web push, passkey login.
