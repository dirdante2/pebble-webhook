# Using the web app

Install it to the home screen (PWA) – on iPhone web push only works from there.

The menu (☰ top left; always visible on wide screens) lists the three main pages with their sub-pages and counters: grey = open entries, muted yellow = new and not yet opened since you were last here (shared by all devices). A dot on the menu button means an update or open memory suggestions. At the bottom: version, theme, and *Abmelden* as the last item.

- **History (Verlauf)**: newest entries, filters for notes, appointments, plannings and open questions. Swipe right = done, swipe left = delete. Long-press a card to select several and mark them done or delete them. The funnel in the search bar filters by tags (all selected tags must match).
- **Appointments**: every appointment has a time (07:00 if none was said; all-day only when said). Reminders arrive as web push. The line above the history shows the next appointment and how many are due within 24 hours. "Verschieben" on an appointment opens date and time pickers (‹ › for ±1 day / ±15 min, quick picks); the old time is kept as a note, the reminder comes again.
- **Detail page**: breadcrumbs on top (Verlauf › #42 or Listen › group › list › #42), one main action (Erledigt, Antworten/Schließen for plannings), everything else behind "⋯". Recording and transcript are collapsed.
- **Plannings**: the AI researches the web and answers in rounds; continue by voice or text and answer its questions.
- **Lists (Listen)**: the AI sorts notes into groups and lists; each group opens as its own page, tick items off one by one, move or delete them. The arrow icon in the title expands all open items.
- **Search**: full text over notes and plannings.
- **Ring usage (Nutzung)**: the bar at the bottom of the menu shows how much recording time the current ring has used. The ring page lists the total, notes vs. plannings, button presses (hold / click + hold), misfires (no speech) and deleted recordings, recording time per month and a forecast for 80 % and 100 % at constant use (average of the last 30 days). At 60, 80, 95 and 100 % a hint appears on every page until confirmed; at 100 % "+30 min" raises the capacity if the battery lasts longer. Every ring is recognised by the identifier the Pebble app sends with each recording; its ID is `#` plus the last three characters (e.g. `#449`). Several rings can be used side by side and are counted separately; nothing has to be set up or swapped. Each ring can get a description (e.g. its colour), and a card tells you when the Pebble app was updated.
- **Calendar subscription**: Status → Kalender-Abo shows a secret iCal link with all appointments (done ones marked ✓). Subscribe to it in Google Calendar (calendar.google.com → Other calendars → + → From URL), Apple Calendar or Outlook. Google refreshes subscribed calendars only every few hours. "New link" invalidates the old one.
- **Settings (Einstellungen)**: status with tabs Overview (stats, notifications, devices/passkeys), Pebble app setup, Joplin (only when switched on: set up and test the connection), Configuration (? = explanation, pencil = change one value) and Theme (day/night mode, colour palette or custom colour, preview; per device, new devices take over the last choice); updates, memory, tags (tap = its entries, delete from all entries), AI costs, trash (restore deleted entries for 183 days).
- **Users**: the menu greets you by name. Admins find *Updates*, *Backups* and *Users* in the menu; other users only their own pages. *Status → Access and security*: own API token and a new recovery code. If your data is locked (key missing after a move to a new server), the app asks for your recovery code.
- **Another device**: Status → "Weiteres Gerät hinzufügen" shows a 4-digit code (valid up to 5 minutes, only while the page is open); on the new device open the app and choose "Mit Code hinzufügen".

Detailed flows (German): [ABLAUF.md](../ABLAUF.md).

## Screenshots

Demo data, English UI, mobile, dark mode: [all screenshots](screenshots/).
