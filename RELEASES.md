# Releases

Summarised notes for **beta** and **stable** releases. Each section lists only what lasts since the previous beta or stable release – intermediate steps, reverted attempts and small fixes of things that were new in the same span are left out. Every single version with all its steps is in [CHANGELOG.md](CHANGELOG.md) (that is what the nightly channel shows).

A tag `vX.Y.Z-stable` (stable; before 1.33.5 `vX.Y.Z`) or `vX.Y.Z-beta` (beta) needs a section `## [X.Y.Z]` or `## [X.Y.Z-beta]` here; the release workflow uses it as the release notes.

## [3.0.0] - 2026-11-24

### Multiple users (prepared)
- First user is admin; roles admin/user; invite, lock and delete users (admin).
- Everyone sees only own data and settings – admins included.
- Personal settings apply instantly; "Hello <name>" in the menu.

### Encryption
- Own encrypted database and encrypted recordings per user; keys never in backups.
- Recovery code reopens data after a restore on a new server.
- Update moves the old database automatically; Pebble app keeps working unchanged.

## [2.3.0] - 2026-11-24

### Preparing 3.0
- New database driver (no cgo), same database format; going back to 2.2 stays possible.
- Installer prepares the move to multiple users with encrypted data in 3.0 – update to 2.3 first.

## [2.2.0] - 2026-11-23

### Recording and AI
- Record notes directly in the web app ("+" next to the search, up to 60 s, offline queue); not counted as ring usage.
- Follow-up notes are linked to the previous topic and sorted into the same list.
- The AI searches earlier notes itself when context is missing.

### Lists
- Tag filter also on lists; overview shows 10 items per list with "Load 10 more".
- "Change list" grouped by group with search; sidebar always up to date.
- Films and series with year, streaming provider or buy link, director and cast.

### MCP
- Import entries as text, the AI sorts them; re-evaluate notes.
- Tidy up lists and groups with a proposal before executing.

## [2.1.0] - 2026-11-23

### Notes
- AI instruction per note: tell the AI what to change, it re-evaluates the note.
- External notes via API (token): e.g. feedback from Joplin, sorted by the AI.

## [2.0.0] - 2026-11-23

### Database
- Schema version: migrations run in order, also across skipped versions.
- Going back only without database changes in between, otherwise via a backup; versions before 2.0.0 are no longer offered.

### History
- Toggle "only open ↔ all" for all tabs (default only open); swipe hint below the list.
- One group per day; endless loading; counters include unloaded entries.
- Done entries show only header and title; last 24 h only the time.
- Number of planning rounds next to the type badge.

### Search
- On the server: all words must match, text/title before tags, newest first.
- Match shown per word; words highlighted on the detail page.

### Backups and status
- Upload a backup and restore it directly.
- Storage: database and app versions separate; delete old backups (older than 7 days) and old app versions.
- Inputs: words per recording (last 30 days).

### Other
- "App updated" card lists all changes since the last confirmed version.
- Ko-fi link in the menu and on the status page.
- Faster with 10k+ entries.

## [1.35.1] - 2026-11-22

### Releases
- One build per version and channel; stable and beta no longer get a nightly of the same number.

## [1.35.0] - 2026-11-22

### New
- Calendar subscription: secret iCal link with all appointments (Status → Kalender-Abo).
- Ring page: counters for misfires and deleted recordings.

### Changed
- beta also offers stable, nightly offers beta and stable.
- App manifest in English and German, picked by device language.

## [1.34.0] - 2026-11-22

### Ring usage
- Every recording is counted per ring (length, kind, gesture), also after deletion.
- Ring ID `#12345` via header `X-Ring-ID`; "Apply ring swap" when it changes.
- Hints and push at 60/80/95/100 % of the capacity (default 12 h); "+30 min" at 100 %.
- Ring page with recording time per month, forecast and comparison; usage bar in the menu.

### Updates
- Tags `vX.Y.Z-stable|beta|nightly`; same commit never counts as newer; publication time shown.
- Own builds show `X.Y.Z.A`.
- "App updated" card shows the changes right there.

### Lists
- A list whose last item was just ticked off stays active for a while.

## [1.33.9] - 2026-11-22

### Updates
- "New version available" card on the update page.
- Menu update icon follows the chosen channel.
- Backups have their own menu item.
- "App updated" card is confirmed once for all devices.

## [1.33.4] - 2026-11-22

### Updates
- Moving releases `stable`, `beta`, `nightly`; installer option `--channel`.
- Versions shown as X.Y.Z with a channel badge.
- Card "App updated" (old → new) until confirmed.

## [1.32.0] - 2026-11-22

### Offline and traffic
- The app shell (script, styles, icons) is cached per version and starts without the network; a new version loads once in the background and takes over on the next page change.
- All views show the stored state at once and then refresh; the app opens without waiting for the server.
- Unchanged data costs only a "304" without body (ETag); everything else is gzip-compressed. Recordings load only when played.

### Backups
- Every backup is one archive in `BACKUP_DIR` (default `/srv/pebble-backup`) with database, settings, start values, version and channel – optionally the recordings.
- Scheduled full backup: daily, weekly or monthly at a chosen time with retention in days (default daily 03:00, 30 days, without recordings).
- Restoring brings back data and settings and installs the version the backup was made with. Backups can be downloaded.
- Backups list with channel, version, date, reason and size; number and total space on top. Leftovers and old releases are cleaned up automatically.

### Updates
- Update page rebuilt: installed version with channel on top, channels as button group, last versions per channel, change lists on demand.
- Automatic update once a day at a chosen time (default 04:00), with a backup first.
- After installing, the service must answer within 30 s; otherwise program and database go back to the previous state. A damaged database stops the start.
- Downloaded releases stay on the server for switching back without a new download.

### Recordings
- Recordings are kept until their entry is deleted for good from the trash; empty recordings are deleted after 1 hour, failed entries never automatically.
- Stored by kind (`diktat/`, `planung/`); recordings without entry (e.g. after a restore) are listed on the status page with a red badge and can be imported again – notes first, then plannings in one round without questions.

### Status page
- Entries card (recordings with average size and length, in progress, failed) that opens to a ring chart of notes, appointments and plannings.
- Storage card for data, recordings and backups with size, bar, path and free/total.

## [1.32.0-beta.1] - 2026-11-22

### Offline and traffic
- The app shell (script, styles, icons) is cached per version and starts without the network; a new version loads once in the background and takes over on the next page change.
- All views show the stored state at once and then refresh; the app opens without waiting for the server.
- Unchanged data costs only a "304" without body (ETag); everything else is gzip-compressed. Recordings load only when played.

### Backups
- Every backup is one archive in `BACKUP_DIR` (default `/srv/pebble-backup`) with database, settings, start values, version and channel – optionally the recordings.
- Scheduled full backup: daily, weekly or monthly at a chosen time with retention in days (default daily 03:00, 30 days, without recordings).
- Restoring brings back data and settings and installs the version the backup was made with. Backups can be downloaded.
- Backups list with channel, version, date, reason and size; number and total space on top. Leftovers and old releases are cleaned up automatically.

### Updates
- Update page rebuilt: installed version with channel on top, channels as button group, last versions per channel, change lists on demand.
- Automatic update once a day at a chosen time (default 04:00), with a backup first.
- After installing, the service must answer within 30 s; otherwise program and database go back to the previous state. A damaged database stops the start.
- Downloaded releases stay on the server for switching back without a new download.

### Recordings
- Recordings are kept until their entry is deleted for good from the trash; empty recordings are deleted after 1 hour, failed entries never automatically.
- Stored by kind (`diktat/`, `planung/`); recordings without entry (e.g. after a restore) are listed on the status page with a red badge and can be imported again – notes first, then plannings in one round without questions.

### Status page
- Entries card (recordings with average size and length, in progress, failed) that opens to a ring chart of notes, appointments and plannings.
- Storage card for data, recordings and backups with size, bar, path and free/total.

## [1.27.4-beta.1] - 2026-11-22

First public beta.

### Voice notes
- Recordings from the Pebble Index 01 arrive by webhook; transcription, a short title and an evaluation follow automatically.
- Two gestures: dictation (click) – the AI makes a note, list item or appointment of it; planning with web research (click + hold).
- Notes are sorted into lists and groups by the AI; every note and planning has a fixed number (#42), every list one too (L12).
- Appointments with reminder by web push; reschedule them with date and time pickers (the old time stays as a note).
- Plannings run in rounds: answer the AI's questions or add a new recording, close them when done.
- Correct the transcript; changed words are learned as suggestions for the memory.

### Web app
- Installable app (PWA) with passkey login, works offline with the last state; new devices join with a 4-digit code.
- Menu with history filters, list groups and settings, each with counters for open and new entries; fixed on wide screens.
- History with search, tag filter, multi-select and swipe gestures; "new" is shared by all devices.
- Lists by group with breadcrumbs, single items can be ticked off; trash keeps deleted entries for 6 months.
- Detail pages with one main action and the rest in a "⋯" sheet.
- Themes: light/dark/automatic, five colour palettes or a custom colour.
- English or German: web app, messages, push notifications and the texts the AI writes; chosen in the setup wizard (preset from the browser).

### Integration
- MCP server with OAuth: connect Claude, ChatGPT and others to read lists, appointments and notes and tick items off.
- Machine access with an API token for scripts.
- Optional one-way export to Joplin.

### Operation
- Setup wizard in the browser, every setting with explanation and validation.
- Updates from the web app with channels stable, beta and nightly: the last 3 versions of each channel, any of them installable.
- Program and database are backed up before every change; "Frühere Stände" goes back to an earlier state in one step.

## [1.27.4] - 2026-11-22

First public release.

### Voice notes
- Recordings from the Pebble Index 01 arrive by webhook; transcription, a short title and an evaluation follow automatically.
- Two gestures: dictation (click) – the AI makes a note, list item or appointment of it; planning with web research (click + hold).
- Notes are sorted into lists and groups by the AI; every note and planning has a fixed number (#42), every list one too (L12).
- Appointments with reminder by web push; reschedule them with date and time pickers (the old time stays as a note).
- Plannings run in rounds: answer the AI's questions or add a new recording, close them when done.
- Correct the transcript; changed words are learned as suggestions for the memory.

### Web app
- Installable app (PWA) with passkey login, works offline with the last state; new devices join with a 4-digit code.
- Menu with history filters, list groups and settings, each with counters for open and new entries; fixed on wide screens.
- History with search, tag filter, multi-select and swipe gestures; "new" is shared by all devices.
- Lists by group with breadcrumbs, single items can be ticked off; trash keeps deleted entries for 6 months.
- Detail pages with one main action and the rest in a "⋯" sheet.
- Themes: light/dark/automatic, five colour palettes or a custom colour.
- English or German: web app, messages, push notifications and the texts the AI writes; chosen in the setup wizard (preset from the browser).

### Integration
- MCP server with OAuth: connect Claude, ChatGPT and others to read lists, appointments and notes and tick items off.
- Machine access with an API token for scripts.
- Optional one-way export to Joplin.

### Operation
- Setup wizard in the browser, every setting with explanation and validation.
- Updates from the web app with channels stable, beta and nightly: the last 3 versions of each channel, any of them installable.
- Program and database are backed up before every change; "Frühere Stände" goes back to an earlier state in one step.
