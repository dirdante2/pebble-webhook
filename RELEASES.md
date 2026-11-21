# Releases

Summarised notes for **beta** and **stable** releases. Each section lists only what lasts since the previous beta or stable release – intermediate steps, reverted attempts and small fixes of things that were new in the same span are left out. Every single version with all its steps is in [CHANGELOG.md](CHANGELOG.md) (that is what the nightly channel shows).

A tag `vX.Y.Z` (stable) or `vX.Y.Z-beta.N` (beta) needs a section `## [X.Y.Z]` or `## [X.Y.Z-beta.N]` here; the release workflow uses it as the release notes.

## [1.26.0] - 2026-11-21

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

### Integration
- MCP server with OAuth: connect Claude, ChatGPT and others to read lists, appointments and notes and tick items off.
- Machine access with an API token for scripts.
- Optional one-way export to Joplin.

### Operation
- Setup wizard in the browser, every setting with explanation and validation.
- Updates from the web app with channels stable, beta and nightly, rollback and database backup before every change.
