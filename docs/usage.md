# Using the web app

Install it to the home screen (PWA). On iPhone, web push only works from there.

## Menu

- Open it with ☰ (top left). It is always visible on wide screens.
- It lists the three main pages with sub-pages and counters.
- Grey counter = open entries.
- Muted yellow counter = new, not opened since your last visit (shared by all devices).
- A dot on the menu button = an update or open memory suggestions.
- At the bottom: version, theme, and *Sign out* as the last item.

## History

- Shows newest entries.
- Filters: notes, appointments, plannings, open questions.
- Swipe right = done. Swipe left = delete.
- Long-press a card to select several, then mark them done or delete them.
- The funnel in the search bar filters by tags. All selected tags must match.

## Appointments

- Every appointment has a time.
- Default time: 07:00 if none was said.
- All-day only when said.
- Reminders arrive as web push.
- The line above the history shows the next appointment and how many are due within 24 hours.

**Reschedule**

- "Reschedule" on an appointment opens date and time pickers.
- ‹ › step ±1 day / ±15 min. Quick picks are available.
- The old time is kept as a note.
- The reminder comes again.

## Detail page

- Breadcrumbs on top: History › #42 or Lists › group › list › #42.
- One main action: Done, or Answer/Close for plannings.
- Everything else is behind "⋯".
- Recording and transcript are collapsed.

## Plannings

- The AI researches the web and answers in rounds.
- Continue by voice or text.
- Answer its questions.

## Lists

- The AI sorts notes into groups and lists.
- Each group opens as its own page.
- Tick items off one by one. Move or delete them.
- The arrow icon in the title expands all open items.

## Search

Full text over notes and plannings.

## Ring usage

**Overview**

- The bar at the bottom of the menu shows the recording time used by the current ring.
- The ring page lists:
  - total, notes vs. plannings
  - button presses (hold / click + hold)
  - misfires (no speech)
  - deleted recordings
  - recording time per month
  - forecast for 80 % and 100 % at constant use (average of the last 30 days)

**Warnings**

- Hints appear at 60, 80, 95 and 100 %.
- A hint stays on every page until confirmed.
- At 100 %, "+30 min" raises the capacity if the battery lasts longer.

**Several rings**

- Each ring is recognised by the identifier the Pebble app sends with each recording.
- Its ID is `#` plus the last three characters (e.g. `#449`).
- Several rings can be used side by side and are counted separately.
- Nothing has to be set up or swapped.
- Each ring can get a description (e.g. its colour).
- A card tells you when the Pebble app was updated.

**Inactive rings**

- On the ring's edit page, "Set inactive" asks for a reason: ring lost, ring defective, battery empty.
- Inactive rings leave the sidebar and no longer warn.
- "Set active" brings them back.
- If a ring is unused for 14 days while another one is used, the app asks once whether to set it inactive.

## Calendar subscription

- Status → Calendar subscription shows a secret iCal link with all appointments.
- Done appointments are marked ✓.
- Subscribe in Google Calendar (calendar.google.com → Other calendars → + → From URL), Apple Calendar or Outlook.
- Google refreshes subscribed calendars only every few hours.
- "Create new link" invalidates the old one.

## Settings

Status has these tabs:

- **Overview**: stats, notifications, devices/passkeys.
- **Pebble app**: setup.
- **Joplin**: only when switched on. Set up and test the connection.
- **Configuration**: ? = explanation, pencil = change one value.
- **Theme**: day/night mode, colour palette or custom colour, preview. Per device; new devices take over the last choice.

Also available:

- Updates
- Memory
- Tags: tap = its entries; delete from all entries.
- AI costs
- Trash: restore deleted entries for 183 days.

## Users

- The menu greets you by name.
- Admins find *Updates*, *Backups* and *Users* in the menu.
- Other users see only their own pages.
- *Status → Access and security*: own API token and a new recovery code.
- If your data is locked (key missing after a move to a new server), the app asks for your recovery code.

## Another device

1. Status → "Add another device" shows a 6-digit code.
2. The code is valid up to 5 minutes, only while the page is open.
3. On the new device, open the app and choose "Add with code".

## Anonymous statistics

**What is sent**

- Once a month (on the 1st, random time).
- App version, update channel, app ID (a hash created once per installation).
- Per ring: an anonymised identifier (a salted hash of the ring identifier), total recording time, active/inactive and the reason.
- Setting a ring inactive or active again is sent right away.
- Everything is encrypted for the receiver.
- No names, texts, recordings or IP-based data.

**Control**

- On by default.
- An admin switches it on or off for the whole installation under Status → Anonymous statistics.
- While this installation sends, every user sees the ring lifetime of all installations there and on the ring usage page.
- Only official builds include the receiver. Self-built binaries send nothing.

## Feedback

*Send feedback* on the status page sends a comment or bug report to the developer. It includes the app version and an optional e-mail address for a reply.

Detailed flows (German): [ABLAUF.md](../ABLAUF.md).

## Screenshots

Demo data, English UI, mobile, dark mode: [all screenshots](screenshots/).
