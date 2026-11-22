// Sprachnotizen – Web-App ohne Build: Verlauf, Detail, Antwortseite, Status, Offline-Warteschlange.

const seite = document.getElementById('seite');
const nav = document.getElementById('nav');
const menue = document.getElementById('menue');
const menueKnopf = document.getElementById('menueknopf');

// ---------- Sprache ----------
// Deutsch steht im Code; für Englisch liefert der Server das Wörterbuch (deutscher Text → englischer) in index.html mit.
// tx('Text {0}', x) übersetzt einen Text; T`<p>Text</p>` und T('<p>…</p>') übersetzen die Textstücke und Attribute
// title/aria-label/placeholder/alt fester HTML-Teile – eingesetzte Werte (Nutzerdaten) bleiben unberührt.
const SPRACHE = document.documentElement.lang === 'en' ? 'en' : 'de';
const LOCALE = SPRACHE === 'en' ? 'en-GB' : 'de-DE';
const WOERTER = (() => { try { return Object.assign(Object.create(null), JSON.parse(document.getElementById('woerter')?.textContent || '{}')); } catch { return Object.create(null); } })();
const UEBERSETZT = Object.keys(WOERTER).length > 0;
function tx(s, ...a) {
  const k = UEBERSETZT ? s.trim() : '';
  const r = k in WOERTER ? s.replace(k, WOERTER[k]) : s;
  return a.length ? r.replace(/\{(\d)\}/g, (_, i) => a[i]) : r;
}
// i18n:teile-anfang
const I18N_ATTR = /(\s(?:title|aria-label|placeholder|alt)=")([^"]*)(")/g;
function uebersetzeTeile(teile, nachschlagen) {
  let inTag = false, q = '';
  const attr = (s) => s.replace(I18N_ATTR, (m, a, v, z) => { const u = v.trim() ? nachschlagen(v.trim()) : undefined; return u != null ? a + v.replace(v.trim(), u) + z : m; });
  return teile.map((s) => {
    let out = '', text = '', tag = '';
    const flush = () => {
      if (!text) return;
      const k = text.trim();
      const u = k ? nachschlagen(k) : undefined;
      out += u != null ? text.replace(k, u) : text;
      text = '';
    };
    for (const c of s) {
      if (inTag) {
        tag += c;
        if (q) { if (c === q) q = ''; } else if (c === '"') q = c; else if (c === '>') { inTag = false; out += attr(tag); tag = ''; }
      } else if (c === '<') { flush(); inTag = true; tag = c; } else text += c;
    }
    flush();
    return out + attr(tag);
  });
}
// i18n:teile-ende
const tCache = new WeakMap(), tHtml = new Map();
const tSuche = (k) => WOERTER[k];
function T(teile, ...werte) {
  if (typeof teile === 'string') {
    if (!UEBERSETZT) return teile;
    if (!tHtml.has(teile)) tHtml.set(teile, uebersetzeTeile([teile], tSuche)[0]);
    return tHtml.get(teile);
  }
  let fest = teile;
  if (UEBERSETZT) {
    fest = tCache.get(teile);
    if (!fest) { fest = uebersetzeTeile([...teile], tSuche); tCache.set(teile, fest); }
  }
  let s = fest[0];
  for (let i = 0; i < werte.length; i++) s += werte[i] + fest[i + 1];
  return s;
}

// ---------- Hilfen ----------

const esc = (s) => String(s ?? '').replace(/[&<>"']/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]));
const zeit = (ms) => new Date(ms).toLocaleString(LOCALE, { day: '2-digit', month: '2-digit', year: 'numeric', hour: '2-digit', minute: '2-digit' });
// Datum und Uhrzeit ohne Jahr, für die Karten im Verlauf.
const kurz = (ms) => new Date(ms).toLocaleString(LOCALE, { day: '2-digit', month: '2-digit', hour: '2-digit', minute: '2-digit' });
const uhr = (ms) => new Date(ms).toLocaleTimeString(LOCALE, { hour: '2-digit', minute: '2-digit' });
const tag = (ms) => new Date(ms).toLocaleDateString(LOCALE, { weekday: 'long', day: '2-digit', month: '2-digit', year: 'numeric' });
const neueId = () => (crypto.randomUUID ? crypto.randomUUID() : Array.from(crypto.getRandomValues(new Uint8Array(16)), (b) => b.toString(16).padStart(2, '0')).join(''));

// „diktat“ ist der interne Name der Art; angezeigt wird sie als Notiz (Nutzervorgabe 15.11.2026).
const MODUS = { diktat: tx('Notiz'), planung: tx('Planung'), test: tx('Test'), unbekannt: tx('Unbekannte Geste') };
const STATUS = { fertig: tx('fertig'), in_arbeit: tx('in Arbeit'), wiederholung: tx('wird wiederholt'), fehler: tx('Fehler'), leer: tx('keine Sprache') };
// Termin: „Di 25.11., 10:00“ bzw. „Di 25.11., ganztägig“; heute und morgen beim Namen.
const terminZeit = (t) => {
  const d = new Date(t.beginn), h = new Date();
  const morgen = new Date(h.getFullYear(), h.getMonth(), h.getDate() + 1);
  const name = d.toDateString() === h.toDateString() ? tx('heute') : d.toDateString() === morgen.toDateString() ? tx('morgen')
    : d.toLocaleDateString(LOCALE, { weekday: 'short', day: '2-digit', month: '2-digit' }).replace('.,', '');
  return `${name}, ${t.ganztag ? tx('ganztägig') : uhr(t.beginn)}`;
};
// „Erinnerung“, wenn nur ein Erinnerungszeitpunkt gesagt ist („erinnere mich in 10 Minuten“), sonst „Termin“.
const terminArt = (t) => (t.erinnerung === t.beginn ? tx('Erinnerung') : tx('Termin'));
const fragenText = (n) => (n === 1 ? tx('1 Frage') : tx('{0} Fragen', n));
const frageText = (f) => (typeof f === 'string' ? f : f.frage);
const ZUORDNUNG = {
  schluesselwort: tx('per Schlüsselwort „Weiter“ fortgesetzt'), ki: tx('von der KI einer offenen Planung zugeordnet'), neu: tx('als neue Planung angelegt'),
  web: tx('Antwort aus der Web-App'), umgehaengt: tx('von Hand umgehängt'), neu_weil_geschlossen: tx('neue Planung, weil das Ziel inzwischen geschlossen war'),
};

let meldungTimer;
function melde(text) {
  const el = document.getElementById('meldung');
  el.textContent = text;
  el.hidden = false;
  clearTimeout(meldungTimer);
  meldungTimer = setTimeout(() => { el.hidden = true; }, 3500);
}

class ApiFehler extends Error {
  constructor(status, code, msg) { super(msg); this.status = status; this.code = code; }
}
class NetzFehler extends Error {}

async function api(pfad, opt = {}) {
  const kopf = { 'X-App': '1', 'X-Sprache': SPRACHE, ...(opt.headers || {}) };
  let body = opt.body;
  if (body && !(body instanceof FormData) && typeof body !== 'string') {
    body = JSON.stringify(body);
    kopf['Content-Type'] = 'application/json';
  }
  let antwort;
  try {
    // keepalive: Schreibaufrufe (z. B. vorgemerkte Wisch-Aktionen beim Verlassen der Seite) laufen auch beim Neuladen zu Ende.
    // Nur für kleine JSON-Rümpfe – der Browser begrenzt keepalive auf 64 KB.
    const keepalive = (opt.method || 'GET') !== 'GET' && (body === undefined || (typeof body === 'string' && body.length < 60000));
    antwort = await fetch(pfad, { method: opt.method || 'GET', headers: kopf, body, credentials: 'same-origin', keepalive });
  } catch (e) {
    setzeNetz(false);
    throw new NetzFehler(tx('Keine Verbindung'));
  }
  setzeNetz(true);
  let daten = null;
  try { daten = await antwort.json(); } catch (e) { /* leer */ }
  if (!antwort.ok) {
    if (antwort.status === 401 && !pfad.startsWith('/api/auth/')) { try { localStorage.setItem('angemeldet', '0'); } catch { /* nur Startabkürzung */ } geheZu('/anmelden', true); }
    throw new ApiFehler(antwort.status, daten?.code || '', daten?.fehler || T`Fehler ${antwort.status}`);
  }
  return daten;
}

// Kleines, sicheres Markdown für Rechercheergebnisse: erst maskieren, dann wenige Muster.
function md(text) {
  const inline = (s) => esc(s)
    .replace(/\*\*([^*]+)\*\*/g, '<strong>$1</strong>')
    .replace(/`([^`]+)`/g, '<code>$1</code>')
    .replace(/\[([^\]]+)\]\((https?:\/\/[^\s)]+)\)/g, T('<a href="$2" target="_blank" rel="noopener noreferrer">$1</a>'));
  const aus = [];
  let liste = null, tabelle = null;
  const zu = () => {
    if (liste) { aus.push(`<${liste.art}>${liste.punkte.map((p) => T`<li>${p}</li>`).join('')}</${liste.art}>`); liste = null; }
    if (tabelle) {
      const [kopf, ...rest] = tabelle;
      aus.push(T`<table><thead><tr>${kopf.map((z) => T`<th>${z}</th>`).join('')}</tr></thead><tbody>${rest.map((r) => T`<tr>${r.map((z) => T`<td>${z}</td>`).join('')}</tr>`).join('')}</tbody></table>`);
      tabelle = null;
    }
  };
  for (const roh of String(text || '').split('\n')) {
    const z = roh.trim();
    let m;
    if (!z) { zu(); continue; }
    if (z.startsWith('|')) {
      if (liste) zu();
      const zellen = z.replace(/^\||\|$/g, '').split('|').map((c) => c.trim());
      if (zellen.every((c) => /^:?-+:?$/.test(c))) continue;
      (tabelle ||= []).push(zellen.map(inline));
      continue;
    }
    if ((m = z.match(/^(#{1,6})\s+(.*)$/))) { zu(); aus.push(`<h3>${inline(m[2])}</h3>`); continue; }
    if ((m = z.match(/^[-*•]\s+(?:\[[ xX]\]\s+)?(.*)$/))) {
      if (tabelle || (liste && liste.art !== 'ul')) zu();
      (liste ||= { art: 'ul', punkte: [] }).punkte.push(inline(m[1]));
      continue;
    }
    if ((m = z.match(/^\d+[.)]\s+(.*)$/))) {
      if (tabelle || (liste && liste.art !== 'ol')) zu();
      (liste ||= { art: 'ol', punkte: [] }).punkte.push(inline(m[1]));
      continue;
    }
    zu();
    aus.push(`<p>${inline(z)}</p>`);
  }
  zu();
  return aus.join('');
}

// ---------- IndexedDB: Verlauf, Einträge, Ausgang (vorgemerkte Aktionen), Antworten weiterer Ansichten ----------

let dbZusage;
function db() {
  dbZusage ||= new Promise((ok, nein) => {
    const r = indexedDB.open('sprachnotizen', 2);
    r.onupgradeneeded = () => {
      const d = r.result;
      if (!d.objectStoreNames.contains('verlauf')) d.createObjectStore('verlauf', { keyPath: 'id' });
      if (!d.objectStoreNames.contains('eintraege')) d.createObjectStore('eintraege', { keyPath: 'id' });
      if (!d.objectStoreNames.contains('ausgang')) d.createObjectStore('ausgang', { keyPath: 'client_id' });
      // Letzte Antwort je API-Pfad (Listen, Status, Gedächtnis, Kosten), damit die Ansichten offline laden.
      if (!d.objectStoreNames.contains('antworten')) d.createObjectStore('antworten', { keyPath: 'pfad' });
    };
    r.onsuccess = () => ok(r.result);
    r.onerror = () => nein(r.error);
  });
  return dbZusage;
}
async function lager(name, modus, fn) {
  const d = await db();
  return new Promise((ok, nein) => {
    const tx = d.transaction(name, modus);
    const erg = fn(tx.objectStore(name));
    tx.oncomplete = () => ok(erg?.result);
    tx.onerror = () => nein(tx.error);
  });
}
const lies = (name, key) => lager(name, 'readonly', (s) => s.get(key));
const alle = (name) => lager(name, 'readonly', (s) => s.getAll());
const lege = (name, wert) => lager(name, 'readwrite', (s) => s.put(wert));
const loesche = (name, key) => lager(name, 'readwrite', (s) => s.delete(key));

// GET mit Ablage: online frisch und gespeichert, ohne Verbindung die zuletzt gespeicherte Antwort.
// Ohne gespeicherte Antwort bleibt der NetzFehler stehen.
// vorab: nur die gespeicherte Antwort (sofort zeigen, bevor das Netz antwortet); ohne sie KeinVorrat.
class KeinVorrat extends Error {}
async function apiAbgelegt(pfad, vorab = false) {
  if (vorab) {
    const alt = await lies('antworten', pfad).catch(() => null);
    if (!alt) throw new KeinVorrat(pfad);
    return alt.daten;
  }
  try {
    const daten = await api(pfad);
    lege('antworten', { pfad, daten, zeit: Date.now() }).catch(() => {});
    return daten;
  } catch (f) {
    if (!(f instanceof NetzFehler)) throw f;
    const alt = await lies('antworten', pfad).catch(() => null);
    if (!alt) throw f;
    return alt.daten;
  }
}
// Gespeicherte Antwort lokal nachführen (z. B. nach einem vorgemerkten Haken).
async function aendereAbgelegt(pfad, fn) {
  const alt = await lies('antworten', pfad).catch(() => null);
  if (alt) await lege('antworten', { ...alt, daten: fn(alt.daten) }).catch(() => {});
}

// ---------- Ausgang: Aktionen vormerken und bei Verbindung senden ----------

let sendetGerade = false;
async function sendeAusgang() {
  if (sendetGerade) return;
  sendetGerade = true;
  try {
    const posten = (await alle('ausgang')).filter((p) => p.zustand === 'wartet').sort((a, b) => a.zeit - b.zeit);
    for (const p of posten) {
      try {
        if (p.typ === 'antwort') {
          const form = new FormData();
          form.set('client_id', p.client_id);
          form.set('zeit', String(p.zeit));
          if (p.audio) form.set('audio', p.audio, 'antwort');
          else form.set('text', p.text);
          await api(p.planung ? T`/api/planungen/${p.planung}/antwort` : '/api/planungen', { method: 'POST', body: form });
        } else if (p.typ === 'notizantwort') {
          await api(T`/api/eintraege/${p.eintrag}/antwort`, { method: 'POST', body: { antworten: p.antworten } });
        } else if (p.typ === 'schliessen') {
          await api(T`/api/planungen/${p.planung}/schliessen`, { method: 'POST' });
        } else if (p.typ === 'neustart') {
          await api(T`/api/eintraege/${p.eintrag}/neustart`, { method: 'POST' });
        } else if (p.typ === 'haken') {
          await api(T`/api/listenpunkte/${p.punkt}/haken`, { method: 'POST' });
        }
        await loesche('ausgang', p.client_id);
      } catch (e) {
        if (e instanceof NetzFehler || (e instanceof ApiFehler && (e.status === 401 || e.status >= 500))) break; // später erneut
        if (e instanceof ApiFehler && e.code === 'geschlossen' && p.typ === 'antwort') {
          await lege('ausgang', { ...p, zustand: 'konflikt' });
        } else if (e instanceof ApiFehler && e.status === 409) {
          await loesche('ausgang', p.client_id); // Aktion hat sich erledigt (z. B. läuft schon wieder)
        } else {
          await lege('ausgang', { ...p, zustand: 'fehler', fehler: e.message });
        }
      }
    }
  } finally {
    sendetGerade = false;
  }
  if (aktuell?.name === 'verlauf') zeigeAusgang();
}

async function merkeVor(posten) {
  await lege('ausgang', { client_id: neueId(), zeit: Date.now(), zustand: 'wartet', ...posten });
  await sendeAusgang();
  const offen = (await alle('ausgang')).filter((p) => p.zustand === 'wartet').length;
  return offen === 0; // true = schon gesendet
}

// ---------- Router ----------

const routen = [
  [/^\/$/, 'verlauf', zeigeVerlauf],
  [/^\/suche$/, 'suche', zeigeSuche],
  [/^\/eintrag\/([0-9a-f]+)$/, 'eintrag', zeigeEintrag],
  [/^\/eintrag\/([0-9a-f]+)\/umhaengen$/, 'umhaengen', zeigeUmhaengen],
  [/^\/eintrag\/([0-9a-f]+)\/wortlaut$/, 'wortlaut', zeigeWortlaut],
  [/^\/eintrag\/([0-9a-f]+)\/liste$/, 'einsortieren', (id) => zeigeEinsortieren(id, 'eintrag')],
  [/^\/planung\/([0-9a-f]+)\/liste$/, 'einsortieren', (id) => zeigeEinsortieren(id, 'planung')],
  [/^\/planung\/([0-9a-f]+)$/, 'planung', zeigePlanung],
  [/^\/planung\/([0-9a-f]+)\/antwort$/, 'antwort', zeigeAntwort],
  [/^\/tags\/(eintrag|planung)\/([0-9a-f]+)$/, 'tags', zeigeTags],
  [/^\/listen$/, 'listen', () => zeigeListen('')],
  [/^\/listen\/gruppe\/([^/]+)$/, 'listen', (g) => zeigeListen(decodeURIComponent(g))],
  [/^\/liste\/([0-9a-f]+)$/, 'liste', zeigeListe],
  [/^\/liste\/([0-9a-f]+)\/bearbeiten$/, 'listeBearbeiten', zeigeListeBearbeiten],
  [/^\/gruppe\/([^/]+)\/bearbeiten$/, 'gruppeBearbeiten', (n) => zeigeGruppeBearbeiten(decodeURIComponent(n))],
  [/^\/liste\/([0-9a-f]+)\/punkt\/([0-9a-f]+)\/verschieben$/, 'verschieben', zeigeVerschieben],
  [/^\/status$/, 'status', zeigeStatus],
  [/^\/status\/kosten$/, 'kosten', zeigeKosten],
  [/^\/status\/papierkorb$/, 'papierkorb', zeigePapierkorb],
  [/^\/status\/gedaechtnis$/, 'gedaechtnis', zeigeGedaechtnis],
  [/^\/status\/gedaechtnis\/neu$/, 'gedaechtnisBearbeiten', () => zeigeGedaechtnisBearbeiten('')],
  [/^\/status\/gedaechtnis\/([0-9a-f]+)\/bearbeiten$/, 'gedaechtnisBearbeiten', zeigeGedaechtnisBearbeiten],
  [/^\/pebble$/, 'pebble', zeigePebble],
  [/^\/status\/geraet$/, 'geraetNeu', zeigeGeraetNeu],
  [/^\/status\/tags$/, 'tagsVerwalten', zeigeTagsVerwalten],
  [/^\/status\/darstellung$/, 'darstellung', zeigeDarstellung],
  [/^\/status\/joplin$/, 'joplin', zeigeJoplin],
  [/^\/anmelden$/, 'anmelden', zeigeAnmelden],
  [/^\/verbinden$/, 'verbinden', zeigeVerbinden],
  [/^\/ersteinrichtung$/, 'ersteinrichtung', zeigeErsteinrichtung],
  [/^\/status\/einstellungen$/, 'einstellungen', zeigeEinstellungen],
  [/^\/status\/update$/, 'update', () => zeigeUpdate('')],
  [/^\/status\/backups$/, 'backups', () => zeigeUpdate('backups')],
  [/^\/status\/update\/backups$/, 'backups', () => geheZu('/status/backups', true)],
  [/^\/status\/einstellungen\/bearbeiten$/, 'einstellungenBearbeiten', () => geheZu('/status/einstellungen', true)],
  [/^\/status\/einstellungen\/([A-Z_]+)$/, 'einstellungBearbeiten', zeigeEinstellungBearbeiten],
  [/^\/einrichten$/, 'einrichten', zeigeEinrichten],
];
let aktuell = null;
let aufraeumen = null;

function geheZu(pfad, ersetzen = false) {
  if (location.pathname + location.search + location.hash === pfad) return zeige();
  history[ersetzen ? 'replaceState' : 'pushState'](null, '', pfad);
  zeige();
}

// Stand des Verlaufs (Liste, nachgeladene Seiten, Scrollposition), damit „Zurück“ dort weitermacht, wo man war.
let verlaufStand = null;
// Liste, aus der die gerade offene Notiz oder Planung aufgerufen wurde (Pfad), sonst null.
let herkunft = null;
const DETAIL = ['eintrag', 'planung', 'tags', 'antwort', 'umhaengen', 'einsortieren'];

async function zeige() {
  besuche.aktiv();
  // Vorgemerkte Wisch-Aktionen laufen beim Seitenwechsel sofort: ihr „Rückgängig“ gehört zur alten Ansicht.
  alleVorgemerktenAusfuehren();
  if (neueHuelle) { location.reload(); return; }
  if (aufraeumen) { aufraeumen(); aufraeumen = null; }
  const vorher = aktuell?.name, vorherArg = aktuell?.arg;
  if (vorher === 'verlauf' && verlaufStand) verlaufStand.scroll = window.scrollY;
  const pfad = location.pathname;
  for (const [muster, name, fn] of routen) {
    const m = pfad.match(muster);
    if (!m) continue;
    aktuell = { name, arg: m[1] };
    if (name === 'verlauf') aktuell.zurueck = DETAIL.includes(vorher);
    // Unterseiten (Tags, Antwort …) und der Wechsel Runde → Planung behalten die Herkunft.
    if (name === 'eintrag' || name === 'planung') { if (vorher === 'liste') herkunft = T`/liste/${vorherArg}`; else if (vorher === 'listen') herkunft = vorherArg ? T`/listen/gruppe/${vorherArg}` : '/listen'; else if (!DETAIL.includes(vorher)) herkunft = null; }
    if (name !== 'verlauf') window.scrollTo(0, 0);
    const offen = name === 'anmelden' || name === 'einrichten' || name === 'ersteinrichtung';
    menueKnopf.hidden = offen;
    if (menue.open && menue.matches(':modal')) menue.close();
    menueModus();
    suchForm.hidden = offen;
    if (!offen) ladeZaehler();
    // Außerhalb der Suchseite ist das Feld leer; auf ihr zeigt es die Suche aus der Adresse.
    if (name !== 'suche') suchFeld.value = '';
    tagFilterKnopf.hidden = true;
    suchForm.classList.remove('mitfilter');
    zeigeLeeren();
    if (!offen) starteLive();
    maleMenue();
    const diese = aktuell;
    try {
      await fn(...m.slice(1));
    } catch (e) {
      // Nur melden, wenn die Seite noch offen ist: ein spät gescheiterter Abruf überschreibt sonst die nächste Ansicht.
      if (aktuell === diese && !(e instanceof ApiFehler && e.status === 401)) seite.innerHTML = T`<div class="hinweis schlecht">${esc(e.message)}</div>`;
    }
    return;
  }
  aktuell = null;
  seite.innerHTML = T('<p class="leer">Seite nicht gefunden.</p><p class="mitte"><a class="btn" href="/">Zum Verlauf</a></p>');
}

document.addEventListener('click', (e) => {
  const frage = e.target.closest('[data-fragen]');
  if (frage) { e.preventDefault(); oeffneFragen(frage.dataset.fragen, frage.dataset.ftyp); return; }
  const fertig = e.target.closest('[data-fertig]');
  if (fertig) { e.preventDefault(); setzeErledigt(fertig.dataset.fertig, fertig.dataset.erledigt !== '0'); return; }
  const a = e.target.closest('a[href]');
  if (!a || a.target || e.metaKey || e.ctrlKey || a.origin !== location.origin || a.hasAttribute('download')) return;
  e.preventDefault();
  // Menüpunkte für Filter (Verlauf-Art, Listen-Gruppe) merken ihren Wert, die Seite liest ihn beim Öffnen.
  if (a.dataset.setze) { try { localStorage.setItem(a.dataset.setze, a.dataset.wert || ''); } catch { /* nur Komfort */ } }
  geheZu(a.pathname + a.search + a.hash);
});
window.addEventListener('popstate', zeige);

// ---------- Suche (feste Leiste oben, Ergebnisse unter /suche?q=) ----------

const suchForm = document.getElementById('suche');
const suchFeld = document.getElementById('suchfeld');
let suchTimer;
const sucheStarten = (sofort) => {
  clearTimeout(suchTimer);
  suchTimer = setTimeout(() => {
    const q = suchFeld.value.trim();
    const ziel = q ? T`/suche?q=${encodeURIComponent(q)}` : '/suche';
    // Tippen auf der Suchseite ersetzt den Verlaufseintrag statt für jeden Buchstaben einen neuen anzulegen.
    geheZu(ziel, aktuell?.name === 'suche');
  }, sofort ? 0 : 350);
};
const suchLeeren = document.getElementById('suchleeren');
// Tag-Filter des Verlaufs: Trichter rechts in der Suchleiste, nur im Verlauf sichtbar (zeigeVerlauf belegt ihn).
const tagFilterKnopf = document.getElementById('suchfilter');
const zeigeLeeren = () => { suchLeeren.hidden = !suchFeld.value; };
suchFeld.addEventListener('input', () => { zeigeLeeren(); sucheStarten(false); });
suchLeeren.addEventListener('click', () => {
  suchFeld.value = '';
  zeigeLeeren();
  suchFeld.focus();
  if (aktuell?.name === 'suche') sucheStarten(true);
});
suchForm.addEventListener('submit', (ev) => { ev.preventDefault(); suchFeld.blur(); sucheStarten(true); });

// Fundstelle mit hervorgehobenen Suchwörtern (erst maskieren, dann markieren).
function markiere(text, q) {
  let html = esc(text);
  for (const w of q.split(/\s+/).filter(Boolean).sort((a, b) => b.length - a.length)) {
    const muster = esc(w).replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
    html = html.replace(new RegExp(`(${muster})(?![^<]*>)`, 'gi'), '<mark>$1</mark>');
  }
  return html;
}

// Ein Wort für den Stand eines Eintrags: Verarbeitung, Termin, Planung, Liste.
function standText(e) {
  if (e.status && e.status !== 'fertig') return [STATUS[e.status] || e.status, e.status === 'fehler' ? 'fehler' : 'in_arbeit'];
  if (e.termin) return e.termin.bestaetigt ? [tx('erledigt'), 'fertig'] : e.termin.verpasst ? [tx('verpasst'), 'fehler'] : [terminZeit(e.termin), 'neutral'];
  if (e.typ === 'planung') return e.planung_status === 'offen' ? [tx('offen'), 'in_arbeit'] : [tx('abgeschlossen'), 'fertig'];
  return e.erledigt ? ['erledigt', 'fertig'] : ['offen', 'neutral'];
}

async function zeigeSuche() {
  const q = new URLSearchParams(location.search).get('q') || '';
  if (document.activeElement !== suchFeld) suchFeld.value = q;
  zeigeLeeren();
  if (q.trim().length < 2) {
    seite.innerHTML = T('<h1>Suche</h1><p class="leise">Mindestens zwei Zeichen eingeben. Gesucht wird in Titel, Wortlaut, Auswertung und Tags aller Notizen, Termine und Planungen; alle Wörter müssen vorkommen. „#46“ findet die Nummer.</p>');
    return;
  }
  seite.innerHTML = T('<p class="leer">Sucht …</p>');
  let d;
  try {
    d = await api(T`/api/suche?q=${encodeURIComponent(q)}`);
  } catch (f) {
    seite.innerHTML = T`<p class="leer">${f instanceof NetzFehler ? tx('Suche nur mit Verbindung.') : esc(f.message)}</p>`;
    return;
  }
  if (aktuell?.name !== 'suche' || (new URLSearchParams(location.search).get('q') || '') !== q) return;
  const zeile = (e) => {
    const [stand, klasse] = standText(e);
    const art = e.termin ? terminArt(e.termin) : MODUS[e.modus] || e.modus;
    const f = e.fundstelle || {};
    return T`<a class="treffer" href="${e.typ === 'planung' ? '/planung/' : '/eintrag/'}${e.id}">
      <div class="trefferkopf"><span class="treffertitel">${markiere(e.titel || tx('Ohne Titel'), q)}</span>${e.nr ? T`<span class="nr">#${e.nr}</span>` : ''}</div>
      <div class="trefferinfo"><span class="badge ${e.termin ? 'termin' : esc(e.modus)}">${esc(art)}</span><span class="badge ${klasse}">${esc(stand)}</span><span class="leise">${kurz(e.aufgenommen)}</span></div>
      ${f.text && f.feld !== 'Titel' ? T`<div class="fund"><span class="leise">${esc(tx(f.feld))}:</span> ${markiere(f.text, q)}</div>` : ''}</a>`;
  };
  seite.innerHTML = T`<h1>Suche</h1><p class="leise">${d.anzahl ? T`${d.anzahl} ${d.anzahl === 1 ? tx('Treffer') : tx('Treffer')} für „${esc(q)}“${d.anzahl > d.treffer.length ? T`, die ersten ${d.treffer.length}` : ''}` : T`Nichts gefunden für „${esc(q)}“.`}</p>
    <div class="trefferliste">${d.treffer.map(zeile).join('')}</div>`;
}

// ---------- Verlauf ----------

// Zustand einer Planung als Zeichen mit Beschriftung: offener Kreis = offen, Haken im Kreis = abgeschlossen.
// Grüner Haken neben dem Listen-Badge: alle Punkte dieser Notiz bzw. Planung sind abgehakt.
const erledigtZeichen = T('<span class="zustand zu" title="In der Liste abgehakt"><svg viewBox="0 0 20 20" aria-hidden="true"><circle cx="10" cy="10" r="8.5" fill="currentColor"/><path d="M6 10.3l2.7 2.7L14 7.6" fill="none" stroke="var(--flaeche)" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/></svg><span>erledigt</span></span>');
const planungZeichen = (offen) => (offen
  ? T('<span class="zustand offen" title="Planung offen"><svg viewBox="0 0 20 20" aria-hidden="true"><circle cx="10" cy="10" r="7.5" fill="none" stroke="currentColor" stroke-width="2"/></svg><span>offen</span></span>')
  : T('<span class="zustand zu" title="Planung abgeschlossen"><svg viewBox="0 0 20 20" aria-hidden="true"><circle cx="10" cy="10" r="8.5" fill="currentColor"/><path d="M6 10.3l2.7 2.7L14 7.6" fill="none" stroke="var(--flaeche)" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/></svg><span>abgeschlossen</span></span>'));

// Fortlaufende Nummer eines Eintrags (#42): dieselbe in Web-App und Joplin. Ohne Nummer (noch nicht vom Server) nichts.
const nrHtml = (nr) => (nr ? T`<span class="nr">#${nr}</span> ` : '');
// Liste einer Gruppe mit dem Gruppennamen davor; Listen ohne Gruppe nur mit ihrem Namen.
const listenName = (l) => (l.projekt ? `${l.projekt} › ${l.name}` : l.name);
// Feste Nummer einer Liste (L12): so lässt sie sich im Diktat („Liste 12“) und im Skill ansprechen.
const listenNr = (l) => (l.nr ? T`<span class="nr">L${l.nr}</span> ` : '');

// Rechts am Listenpunkt: gelber Punkt, wenn neu seit zuletzt gesehen (offen), dann die Nummer.
const punktEnde = (p) => {
  const neu = !p.erledigt && !p.gelesen && p.angelegt >= besuche.grenze();
  if (!neu && !p.nr) return '';
  return T`<span class="lpende">${neu ? T('<span class="neupunkt" title="Neu seit dem letzten Besuch" aria-label="neu"></span>') : ''}${p.nr ? T`<span class="nr">#${p.nr}</span>` : ''}</span>`;
};

// Kennzeichen einer Karte für die Mehrfachauswahl im Verlauf.
const kartenDaten = (e) => T`data-id="${e.id}" data-typ="${e.typ === 'planung' ? 'planung' : 'eintrag'}" data-frei="${e.loeschbar ? 1 : 0}"`;

function karte(e) {
  const weg = e.loeschung ? ` · ${new Date(e.loeschung).toDateString() === new Date().toDateString() ? tx('wird um {0} gelöscht', uhr(e.loeschung)) : tx('wird am {0} gelöscht', zeit(e.loeschung))}` : '';
  const neu = istNeu(e) ? ' neu' : '';
  if (e.status === 'leer') {
    return T`<a class="karte leerkarte${neu}" href="/eintrag/${e.id}" ${kartenDaten(e)}><div class="zeile"><span>${nrHtml(e.nr)}<span class="badge neutral">${esc(MODUS[e.modus] || e.modus)}</span> <span class="leise">${zeit(e.aufgenommen)}</span></span>
      <span class="badge neutral">keine Sprache</span></div><div class="leise">Keine Sprache erkannt${weg}</div></a>`;
  }
  const fehler = e.status === 'fehler' ? T`<div class="leise">${esc(e.fehler || '')}${weg}</div>` : '';
  const schritt = e.status === 'in_arbeit' || e.status === 'wiederholung' ? ` · ${esc(e.schritt || '')}` : '';
  const planung = e.typ === 'planung';
  const runde = planung && e.runden > 1 ? T` · ${e.runden} Runden` : '';
  // „fertig“ ist der Normalfall und wird nicht angezeigt; rechts oben steht stattdessen Datum und Uhrzeit.
  const stand = e.status === 'fertig' ? '' : T`<span class="badge ${esc(e.status)}">${esc(STATUS[e.status] || e.status)}${schritt}</span> `;
  // Kurze Notizen sagt der Titel schon ganz: die Vorschau käme nur doppelt.
  const vorschau = (e.vorschau || '').length >= 100 ? T`<div class="vorschau">${esc(e.vorschau)}</div>` : '';
  const t = e.termin;
  const termin = t ? T`<div class="terminkarte${t.verpasst ? ' verpasst' : ''}"><span>${t.verpasst ? T('<span class="badge alarm">verpasst</span> ') : ''}${t.bestaetigt ? `${erledigtZeichen} ` : ''}${esc(terminZeit(t))}${t.ort ? ` · ${esc(t.ort)}` : ''}</span></div>` : ''; // Bestätigen per Wischen nach rechts
  return T`<a class="karte${neu}${t?.verpasst ? ' alarmkarte' : ''}${istErledigt(e) ? ' erledigt' : ''}" href="${planung ? '/planung/' : '/eintrag/'}${e.id}" ${kartenDaten(e)}>
    <div class="zeile"><span>${nrHtml(e.nr)}${t ? T`<span class="badge termin">${terminArt(e.termin)}</span>` : T`<span class="badge ${esc(e.modus)}">${esc(MODUS[e.modus] || e.modus)}</span>`} ${planung ? planungZeichen(e.planung_status === 'offen') : ''}${e.liste ? T` <span class="badge liste">${esc(listenName(e.liste))}</span>${e.liste.erledigt ? ` ${erledigtZeichen}` : ''}` : ''}</span>
      <span class="kartenzeit">${stand}<span class="leise">${kurz(e.aufgenommen)}${runde}</span></span></div>
    <div class="titel">${esc(e.titel || tx('Ohne Titel'))}</div>
    ${termin}${vorschau}${e.tags?.length ? T`<div class="tags">${e.tags.map((t) => T`<span class="tagchip">${esc(t)}</span>`).join('')}</div>` : ''}${fehler}
    ${e.fragen ? T`<div class="fragezeile"><span class="badge frage">${fragenText(e.fragen)}</span><span class="btn klein haupt" role="button" data-fragen="${e.id}" data-ftyp="${planung ? 'planung' : 'eintrag'}">Beantworten</span></div>` : ''}</a>`;
}

// Zuletzt gesehen – für alle Geräte gemeinsam auf dem Server (POST /api/besuch). Jede Aktion (Tippen, Taste,
// Seitenwechsel) meldet sich dort, höchstens alle 20 s. Nach einer Pause (VISIT_GAP_MINUTES) oder bei der Anmeldung
// wird die letzte Aktion davor – egal auf welchem Gerät – zur Grenze „gesehen“. Neu ist, was danach eingegangen und
// noch nicht aufgerufen ist. Das Gerät merkt sich die Grenze für die Anzeige (auch offline).
const besuche = (() => {
  const lies = () => { try { return JSON.parse(localStorage.getItem('besuche')) || {}; } catch { return {}; } };
  const schreib = (b) => { try { localStorage.setItem('besuche', JSON.stringify(b)); } catch { /* nur Anzeige */ } };
  let gesendet = 0;
  const melde = async (anmeldung) => {
    // Ohne Sitzung (Anmelde- und Einrichtungsseiten) nichts melden; die Anmeldung selbst meldet sich danach.
    if (!anmeldung && ['anmelden', 'einrichten', 'ersteinrichtung'].includes(aktuell?.name)) return false;
    gesendet = Date.now();
    const b = lies();
    try {
      const r = await api('/api/besuch', { method: 'POST', body: { anmeldung: !!anmeldung, gesehen: b.gesehen || 0 }, keepalive: true });
      const alt = b.gesehen;
      schreib({ gesehen: r.gesehen || 0 });
      return alt !== r.gesehen; // Grenze hat sich geändert: Ansicht neu zeichnen
    } catch { return false; /* offline: alte Grenze bleibt */ }
  };
  let laufend = null;
  return {
    // Beim Öffnen oder Zurückkehren: sofort melden (neuer Besuch entscheidet der Server); eine eben gestartete Meldung zählt mit.
    async pruefe() { if (await (laufend && Date.now() - gesendet < 3000 ? laufend : (laufend = melde(false)))) aktuell?.neuLaden?.(); },
    aktiv() { if (Date.now() - gesendet > 20000) laufend = melde(false); },
    anmeldung() { return melde(true); },
    pause() { /* Pausenlänge gilt jetzt auf dem Server */ },
    // Zuletzt gesehen; ohne Stand gibt es noch nichts Neues.
    grenze() { const g = lies().gesehen; return g > 0 ? g : Infinity; },
  };
})();
for (const t of ['pointerdown', 'keydown']) document.addEventListener(t, () => besuche.aktiv(), { capture: true, passive: true });
// Neu: seit zuletzt gesehen eingegangen und noch nicht aufgerufen (ein geöffneter Eintrag ist nie neu).
const istNeu = (e) => e.eingegangen >= besuche.grenze() && !e.gelesen;

// Standardansicht des Verlaufs: Neues und die letzten 24 Stunden (mindestens die jüngsten Einträge, Anzahl laut Server),
// nach Eingang sortiert; Älteres erst über „Alle anzeigen“. Die Grenze liefert der Server, sie gilt auch für den Zwischenspeicher.
const verlaufGrenze = {
  wert() { try { const g = Number(localStorage.getItem('verlauf-grenze')); return Number.isFinite(g) && localStorage.getItem('verlauf-grenze') !== null ? g : Date.now() - 24 * 3600 * 1000; } catch { return Date.now() - 24 * 3600 * 1000; } },
  setze(g) { try { localStorage.setItem('verlauf-grenze', String(g)); } catch { /* nur Zwischenspeicher */ } },
};
const frisch = (e) => e.eingegangen >= verlaufGrenze.wert();
const sichtbar = (e) => frisch(e) || istNeu(e);

// ---------- Wischen im Verlauf: rechts = erledigt, links = löschen, mit „Rückgängig“ ----------

// Erledigt: fester Stand der Notiz bzw. des Termins (erledigt_at), Planung geschlossen.
const istErledigt = (e) => !!(e.typ === 'planung' ? e.planung_status === 'geschlossen' : e.termin ? e.termin.bestaetigt : e.erledigt);
const ICON_HAKEN = T('<svg viewBox="0 0 24 24" aria-hidden="true"><path d="M5 12.5l4.5 4.5L19 7.5" fill="none" stroke="currentColor" stroke-width="2.6" stroke-linecap="round" stroke-linejoin="round"/></svg>');
const ICON_MUELL = T('<svg viewBox="0 0 24 24" aria-hidden="true"><path d="M4 7h16M9.5 7V4.5h5V7M6.5 7l1 13h9l1-13M10 11v5.5M14 11v5.5" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/></svg>');
const ICON_OEFFNEN = T('<svg viewBox="0 0 24 24" aria-hidden="true"><path d="M4.5 12a7.5 7.5 0 1 0 2.2-5.3M4.5 4v4h4" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round"/></svg>');
// Rechts: offener Eintrag → Haken (erledigt), erledigter → Pfeil (wieder offen).
const wischKarte = (e) => T`<div class="wisch" data-wid="${e.id}">${istErledigt(e) ? T`<div class="wisch-box gut auf" aria-hidden="true">${ICON_OEFFNEN}</div>` : T`<div class="wisch-box gut" aria-hidden="true">${ICON_HAKEN}</div>`}<div class="wisch-box schlecht" aria-hidden="true">${ICON_MUELL}</div>${karte(e)}</div>`;

// Vorgemerkte Aktionen: laufen nach WISCH_FRIST, sofort bei Seitenwechsel oder neuer Aktion; „Rückgängig“ bricht ab.
const WISCH_FRIST = 5000;
const vorgemerkt = new Map();
let toastTimer;
function zeigeToast(text, rueckgaengig) {
  let el = document.getElementById('toast');
  if (!el) {
    el = document.createElement('div');
    el.id = 'toast';
    el.className = tx('meldung toast');
    el.setAttribute('role', 'status');
    document.body.append(el);
  }
  el.innerHTML = T`<span>${esc(text)}</span>${rueckgaengig ? T('<button class="btn klein">Rückgängig</button>') : ''}`;
  el.hidden = false;
  clearTimeout(toastTimer);
  toastTimer = setTimeout(() => { el.hidden = true; }, WISCH_FRIST);
  const b = el.querySelector('button');
  if (b) b.onclick = () => { el.hidden = true; rueckgaengig(); };
}
function vormerken(text, ausfuehren, zurueck) {
  alleVorgemerktenAusfuehren();
  const key = neueId();
  const timer = setTimeout(() => fuehreVorgemerktAus(key), WISCH_FRIST);
  vorgemerkt.set(key, { timer, ausfuehren, zurueck });
  zeigeToast(text, () => {
    const v = vorgemerkt.get(key);
    if (!v) return;
    clearTimeout(v.timer);
    vorgemerkt.delete(key);
    v.zurueck();
  });
}
async function fuehreVorgemerktAus(key) {
  const v = vorgemerkt.get(key);
  if (!v) return;
  clearTimeout(v.timer);
  vorgemerkt.delete(key);
  try {
    await v.ausfuehren();
  } catch (f) {
    v.zurueck();
    melde(f instanceof NetzFehler ? tx('Offline – nicht ausgeführt') : f.message);
  }
}
function alleVorgemerktenAusfuehren() {
  for (const key of [...vorgemerkt.keys()]) fuehreVorgemerktAus(key);
}
window.addEventListener('pagehide', alleVorgemerktenAusfuehren);
document.addEventListener('visibilitychange', () => { if (document.hidden) alleVorgemerktenAusfuehren(); });

// Wischgeste auf den Karten einer Liste. aktion(richtung, e, wisch) gibt true zurück, wenn die Karte weggleiten soll.
function wischbar(liste, { finde, aktiv, aktion }) {
  let g = null, klickSperre = false;
  const schwelle = (el) => Math.min(120, el.offsetWidth * 0.35);
  const zuruecksetzen = (w, karte) => {
    karte.style.transition = '';
    karte.style.transform = '';
    for (const b of w.querySelectorAll('.wisch-box')) { b.style.transition = ''; b.style.width = '0'; b.classList.remove('bereit'); }
  };
  liste.addEventListener('pointerdown', (ev) => {
    if (!aktiv() || (ev.pointerType === 'mouse' && ev.button !== 0)) return;
    const karte = ev.target.closest('.wisch > .karte');
    if (!karte || ev.target.closest('[data-fragen],[data-fertig],button')) return;
    g = { x: ev.clientX, y: ev.clientY, dx: 0, karte, w: karte.parentElement, zustand: 'warte', id: ev.pointerId };
  });
  liste.addEventListener('pointermove', (ev) => {
    if (!g || ev.pointerId !== g.id) return;
    const dx = ev.clientX - g.x, dy = ev.clientY - g.y;
    if (g.zustand === 'warte') {
      if (Math.abs(dy) > 10 && Math.abs(dy) > Math.abs(dx)) { g = null; return; }
      if (Math.abs(dx) < 10 || Math.abs(dx) < Math.abs(dy) * 1.5) return;
      g.zustand = 'wisch';
      g.karte.setPointerCapture(ev.pointerId);
      g.karte.style.transition = 'none';
      for (const b of g.w.querySelectorAll('.wisch-box')) b.style.transition = 'none';
    }
    ev.preventDefault();
    g.dx = Math.max(-g.karte.offsetWidth, Math.min(g.karte.offsetWidth, dx));
    g.karte.style.transform = T`translateX(${g.dx}px)`;
    const gut = g.w.querySelector('.wisch-box.gut'), schlecht = g.w.querySelector('.wisch-box.schlecht');
    gut.style.width = T`${Math.max(0, g.dx)}px`;
    schlecht.style.width = T`${Math.max(0, -g.dx)}px`;
    const bereit = Math.abs(g.dx) >= schwelle(g.karte);
    (g.dx > 0 ? gut : schlecht).classList.toggle('bereit', bereit);
  });
  const ende = (ev) => {
    if (!g || ev.pointerId !== g.id) return;
    const { zustand, dx, karte, w } = g;
    g = null;
    if (zustand !== 'wisch') return;
    klickSperre = true;
    setTimeout(() => { klickSperre = false; }, 400);
    const e = finde(w.dataset.wid);
    if (Math.abs(dx) < schwelle(karte) || !e || ev.type === 'pointercancel') return zuruecksetzen(w, karte);
    const richtung = dx > 0 ? 'rechts' : 'links';
    if (aktion(richtung, e, w)) {
      karte.style.transition = '';
      karte.style.transform = T`translateX(${dx > 0 ? '' : '-'}110%)`;
      w.classList.add('weg');
    } else {
      zuruecksetzen(w, karte);
    }
  };
  liste.addEventListener('pointerup', ende);
  liste.addEventListener('pointercancel', ende);
  // Nach einem Wischen keinen Klick auf die Karte (sie ist ein Link).
  liste.addEventListener('click', (ev) => { if (klickSperre) { ev.preventDefault(); ev.stopPropagation(); klickSperre = false; } }, true);
}

// Termine: verpasste (bis bestätigt) zuerst, dann heute, dann kommende – jeweils der früheste oben; bestätigte zuletzt.
function terminListeHtml(eintraege) {
  const heute = new Date().toDateString();
  const sortiert = [...eintraege].filter((e) => e.termin).sort((a, b) => a.termin.beginn - b.termin.beginn);
  const offen = sortiert.filter((e) => !e.termin.bestaetigt);
  const gruppen = [
    [tx('Verpasst'), offen.filter((e) => e.termin.verpasst)],
    [tx('Heute'), offen.filter((e) => !e.termin.verpasst && new Date(e.termin.beginn).toDateString() === heute)],
    [tx('Kommend'), offen.filter((e) => !e.termin.verpasst && new Date(e.termin.beginn).toDateString() !== heute)],
    [tx('Erledigt'), sortiert.filter((e) => e.termin.bestaetigt)],
  ];
  const html = gruppen.filter(([, liste]) => liste.length).map(([name, liste]) => T`<div class="tag${name === tx('Verpasst') ? ' alarm' : ''}">${name}</div>${liste.map(wischKarte).join('')}`).join('');
  return html || T('<p class="leer">Keine Einträge vorhanden.</p>');
}

// Einzeiler über dem Verlauf: nächster Termin, rechts die Zahl der Termine in den nächsten 24 Stunden;
// darüber verpasste, solange nicht bestätigt.
function terminZeileHtml(t) {
  if (!t) return '';
  const n = t.verpasst || 0;
  const bald = t.in24h ? T`<span class="zaehler" title="Termine in den nächsten 24 Stunden">${t.in24h}</span>` : '';
  const naechster = t.naechster ? T`<a class="terminzeile" href="/eintrag/${t.naechster.id}"><span class="tz-text"><strong>${esc(terminZeit(t.naechster))}</strong> · ${esc(t.naechster.titel)}</span>${bald}</a>` : '';
  const verpasst = n ? T`<button class="terminzeile alarm" data-art-wahl="termin">${n === 1 ? tx('1 verpasster Termin') : T`${n} verpasste Termine`} – bitte bestätigen</button>` : '';
  return verpasst + naechster;
}

// Erledigt bzw. wieder offen (Knöpfe auf Karte und Detailseite): Termin bestätigen, Listenpunkte der Notiz abhaken.
async function setzeErledigt(id, erledigt) {
  try {
    const e = await api(T`/api/eintraege/${id}/erledigt`, { method: 'POST', body: { erledigt } });
    await lege('verlauf', e);
    melde(erledigt ? tx('Erledigt') : tx('Wieder offen'));
  } catch (f) {
    return melde(f instanceof NetzFehler ? tx('Offline – nur mit Verbindung') : f.message);
  }
  if (aktuell?.neuLaden) aktuell.neuLaden();
  else zeige();
}

function listeHtml(eintraege, alleZeigen) {
  const neu = eintraege.filter(istNeu), rest = eintraege.filter((e) => !istNeu(e));
  const gruppen = [[tx('Neu'), neu], [tx('Zuletzt'), rest.filter(frisch)]];
  if (alleZeigen) gruppen.push([tx('Älter'), eintraege.filter((e) => !sichtbar(e))]);
  const html = gruppen.filter(([, liste]) => liste.length).map(([name, liste]) => T`<div class="tag">${name}</div>${liste.map(wischKarte).join('')}`).join('');
  return html || T('<p class="leer">Keine Einträge vorhanden.</p>');
}

async function zeigeAusgang() {
  const box = document.getElementById('ausgang');
  if (!box) return;
  const posten = (await alle('ausgang')).sort((a, b) => a.zeit - b.zeit);
  const name = (p) => (p.typ === 'antwort' ? (p.audio ? tx('Antwort (Aufnahme)') : T`Antwort: ${esc((p.text || '').slice(0, 80))}`) : p.typ === 'notizantwort' ? tx('Antwort auf Rückfrage') : p.typ === 'schliessen' ? tx('Planung schließen') : p.typ === 'haken' ? tx('Punkt abhaken') : tx('Erneut starten'));
  box.innerHTML = posten.map((p) => {
    if (p.zustand === 'konflikt') {
      return T`<div class="hinweis warn"><strong>Die Planung wurde inzwischen geschlossen.</strong><br>${name(p)} <span class="leise">(${zeit(p.zeit)})</span>
        <div class="leiste"><button class="btn klein" data-neu="${p.client_id}">Als neue Planung</button><button class="btn klein gefahr" data-weg="${p.client_id}">Verwerfen</button></div></div>`;
    }
    if (p.zustand === 'fehler') {
      return T`<div class="hinweis schlecht">${name(p)} – nicht angenommen: ${esc(p.fehler)}
        <div class="leiste"><button class="btn klein" data-nochmal="${p.client_id}">Erneut senden</button><button class="btn klein gefahr" data-weg="${p.client_id}">Verwerfen</button></div></div>`;
    }
    return T`<div class="hinweis">Vorgemerkt, wird bei Verbindung gesendet: ${name(p)} <span class="leise">(${zeit(p.zeit)})</span></div>`;
  }).join('');
  box.onclick = async (ev) => {
    const b = ev.target.closest('button');
    if (!b) return;
    const id = b.dataset.neu || b.dataset.weg || b.dataset.nochmal;
    const p = await lies('ausgang', id);
    if (!p) return;
    if (b.dataset.weg) {
      if (!confirm(tx('Vorgemerkte Aktion verwerfen?'))) return;
      await loesche('ausgang', id);
    } else {
      await lege('ausgang', { ...p, zustand: 'wartet', planung: b.dataset.neu ? '' : p.planung });
      await sendeAusgang();
    }
    zeigeAusgang();
  };
}

const ARTEN = [['alles', tx('Alles')], ['diktat', tx('Notizen')], ['termin', SPRACHE === 'en' ? 'Dates' : 'Termine'] /* „Appointments“ passt nicht in die Leiste */, ['planung', tx('Planungen')], ['fragen', tx('Fragen')]];
const passtArt = (e, art) => (art === 'alles' || (art === 'termin' ? !!e.termin : art === 'diktat' ? e.modus === 'diktat' && !e.termin : e.modus === art));
const passt = (e, art, tags) => (art === 'fragen' ? e.fragen > 0 : passtArt(e, art) && tags.every((t) => (e.tags || []).includes(t)));
// Gewählte Tags des Verlaufs (alle müssen passen); früher nur einer unter „verlauf-tag“.
function gewaehlteTags() {
  try {
    const v = JSON.parse(localStorage.getItem('verlauf-tags') || 'null');
    if (Array.isArray(v)) return v.filter((t) => typeof t === 'string' && t);
  } catch (f) { /* leer */ }
  const alt = localStorage.getItem('verlauf-tag');
  return alt ? [alt] : [];
}

async function zeigeVerlauf() {
  let art = localStorage.getItem('verlauf-art');
  if (!ARTEN.some(([a]) => a === art)) art = 'alles';
  seite.innerHTML = T`<div id="ausgang"></div><nav class="btn-group unter reiter4" id="filter" aria-label="Verlauf filtern"></nav>
    <div id="tagleiste" class="tagleiste" hidden></div>
    <div id="termine"></div>
    <div id="liste"><p class="leer">Lädt …</p></div><div id="mehr" class="mitte"></div>
    <div id="wahlleiste" class="wahlleiste" hidden></div>`;
  const liste = document.getElementById('liste');
  aufraeumen = () => seite.classList.remove('auswahl');
  const tags = art === 'fragen' ? [] : gewaehlteTags();
  // Filterknöpfe mit der Zahl offener Einträge.
  const filter = document.getElementById('filter');
  const maleFilter = (z) => {
    // Reiter nur mit offenen Einträgen; „Alles“ und der gewählte (auch über das Menü mit 0) bleiben immer stehen.
    const da = ARTEN.filter(([a]) => a === 'alles' || a === art || !z || z[a] > 0);
    filter.innerHTML = da.map(([a, n]) => T`<button class="btn${a === art ? ' aktiv' : ''}" data-art="${a}">${n}${z ? T` <span class="zaehler${a === 'fragen' && z[a] ? '' : ' still'}">${z[a] ?? 0}</span>` : ''}</button>`).join('');
  };
  let zahlen = null;
  try { zahlen = JSON.parse(localStorage.getItem('verlauf-zahlen') || 'null'); } catch (f) { /* leer */ }
  maleFilter(zahlen);
  const terminBox = document.getElementById('termine');
  // In der Termin-Ansicht stehen die verpassten schon oben in der Liste: dort nur der nächste Termin.
  // Gewischte Einträge bis zur Ausführung: null = gelöscht, sonst der erledigte Stand. Übersteht Neuladen vom Server.
  const ausstehend = new Map();
  let termineStand = null;
  // eintraege wird weiter unten angelegt; vorher (Zwischenspeicher-Anzeige) gibt es nichts Ausstehendes.
  const eintraegeRef = () => { try { return eintraege; } catch { return []; } };
  const maleTermine = (t) => {
    termineStand = t;
    if (!t) { terminBox.innerHTML = ''; return; }
    // Verpasste, die gerade per Wischen bestätigt werden, zählen nicht mehr.
    let verpasst = t.verpasst || 0;
    for (const [id, x] of ausstehend) {
      const vorher = eintraegeRef().find((e) => e.id === id);
      if (vorher?.termin?.verpasst && !vorher.termin.bestaetigt && (x === null || x.termin?.bestaetigt)) verpasst--;
    }
    // In der Termin-Ansicht stehen die verpassten schon oben in der Liste: dort nur der nächste Termin.
    terminBox.innerHTML = terminZeileHtml({ ...t, verpasst: art === 'termin' ? 0 : Math.max(0, verpasst) });
  };
  try { maleTermine(JSON.parse(localStorage.getItem('verlauf-termine') || 'null')); } catch (f) { /* leer */ }
  terminBox.onclick = (ev) => {
    const b = ev.target.closest('button[data-art-wahl]');
    if (!b) return;
    localStorage.setItem('verlauf-art', b.dataset.artWahl);
    zeigeVerlauf();
  };
  // Tag-Filter: Trichter in der Suchleiste, Dialog mit Mehrfachauswahl (nach Menge sortiert); alle gewählten müssen passen.
  let vorrat = [];
  const maleTags = (liste) => {
    vorrat = liste.filter((t) => t && t.name);
    tagFilterKnopf.hidden = art === 'fragen' || (!vorrat.length && !tags.length);
    suchForm.classList.toggle('mitfilter', !tagFilterKnopf.hidden);
    tagFilterKnopf.classList.toggle('an', tags.length > 0);
    const z = tagFilterKnopf.querySelector('.zaehler');
    z.hidden = !tags.length;
    z.textContent = tags.length;
    // Aktiver Filter sichtbar über der Liste, mit Knopf zum sofortigen Zurücksetzen.
    const tl = document.getElementById('tagleiste');
    tl.hidden = !tags.length;
    tl.innerHTML = tags.length ? T`<span class="tags">${tags.map((t) => T`<span class="tagchip">${esc(t)}</span>`).join('')}</span>
      <button class="btn klein" data-tags-weg="1" aria-label="Tag-Filter zurücksetzen">✕ Zurücksetzen</button>` : '';
  };
  // Zurücksetzen ohne Dialog: Filter weg, Verlauf neu.
  const setzeTags = (liste) => {
    localStorage.setItem('verlauf-tags', JSON.stringify(liste));
    localStorage.removeItem('verlauf-tag');
    tags.splice(0, tags.length, ...liste);
    maleTags(vorrat);
    aktuell.neuLaden?.();
  };
  document.getElementById('tagleiste').onclick = (ev) => { if (ev.target.closest('[data-tags-weg]')) setzeTags([]); };
  let gespeichert = [], kombis = [];
  try { gespeichert = JSON.parse(localStorage.getItem('tag-vorrat') || '[]'); } catch (f) { /* leer */ }
  try { kombis = JSON.parse(localStorage.getItem('tag-kombis') || '[]'); } catch (f) { /* leer */ }
  maleTags(gespeichert);
  // Dialog: jeder Tipp filtert sofort; Tags, die mit der Auswahl nie zusammen vorkommen, sind ausgegraut.
  // Die Zahlen zählen Einträge, die alle gewählten Tags und diesen Tag tragen.
  tagFilterKnopf.onclick = () => {
    const wahlTags = new Set(tags);
    const namen = [...new Set([...vorrat.map((t) => t.name), ...tags])];
    const gesamt = (n) => vorrat.find((t) => t.name === n)?.anzahl ?? 0;
    namen.sort((a, b) => gesamt(b) - gesamt(a) || a.localeCompare(b, 'de'));
    const dialog = document.createElement('dialog');
    dialog.className = 'tagdialog';
    const maleDialog = () => {
      const passend = kombis.filter((k) => [...wahlTags].every((t) => k.includes(t)));
      const anzahl = (n) => (wahlTags.size ? passend.filter((k) => k.includes(n)).length : gesamt(n));
      dialog.innerHTML = T`<h2>Nach Tags filtern</h2><p class="leise">${wahlTags.size ? T`${passend.length} ${passend.length === 1 ? tx('Eintrag trägt') : tx('Einträge tragen')} alle gewählten Tags.` : tx('Gezeigt werden Einträge mit allen gewählten Tags.')}</p>
        <div class="tags wahl">${namen.map((n) => {
          const an = wahlTags.has(n), z = anzahl(n), aus = !an && wahlTags.size > 0 && z === 0;
          return T`<button class="tagchip wahl${an ? ' an' : ''}${aus ? ' aus' : ''}" data-tag="${esc(n)}" aria-pressed="${an}"${aus ? ' disabled' : ''}>${esc(n)} <span class="zahl">${z}</span></button>`;
        }).join('')}</div>
        <div class="leiste">${wahlTags.size ? T('<button class="btn" data-aus="1">Alle abwählen</button>') : ''}<button class="btn haupt" data-zu="1">Fertig</button></div>`;
    };
    // Sofort filtern: Auswahl speichern und den Verlauf hinter dem Dialog neu aufbauen.
    const uebernehmen = () => setzeTags([...wahlTags]);
    dialog.onclick = (e2) => {
      const k = e2.target.closest('button');
      if (e2.target === dialog || k?.dataset.zu) return dialog.close();
      if (k?.dataset.aus) { wahlTags.clear(); maleDialog(); return uebernehmen(); }
      if (k?.dataset.tag && !k.disabled) {
        if (!wahlTags.delete(k.dataset.tag)) wahlTags.add(k.dataset.tag);
        maleDialog();
        uebernehmen();
      }
    };
    maleDialog();
    dialog.onclose = () => dialog.remove();
    document.body.append(dialog);
    dialog.showModal();
  };
  api('/api/tags').then((d) => {
    localStorage.setItem('tag-vorrat', JSON.stringify(d.tags));
    kombis = d.kombis || [];
    try { localStorage.setItem('tag-kombis', JSON.stringify(kombis)); } catch (f) { /* voll */ }
    if (aktuell?.name === 'verlauf') maleTags(d.tags);
  }).catch(() => {});
  zeigeAusgang();
  // Zwischenspeicher aus einer früheren Fassung (eine Zeile je Runde) verwerfen.
  for (const e of await alle('verlauf')) if (!e.typ) await loesche('verlauf', e.id);
  // Zurück von einer Detailseite mit gleichem Filter: Liste samt nachgeladenen Seiten und Scrollposition wiederherstellen.
  const stand = aktuell.zurueck && verlaufStand && verlaufStand.art === art && verlaufStand.tags === tags.join('\n') ? verlaufStand : null;
  let alleZeigen = stand ? stand.alle : false;
  // Die Fragen-Ansicht zeigt jede offene Rückfrage, auch an alten Planungen.
  const gezeigt = (e) => alleZeigen || art === 'fragen' || (art === 'termin' ? !e.termin?.bestaetigt : sichtbar(e));
  let eintraege = stand ? stand.eintraege.filter(gezeigt) : (await alle('verlauf')).filter((e) => passt(e, art, tags) && gezeigt(e)).sort((a, b) => b.eingegangen - a.eingegangen);
  let mehrBekannt = stand ? stand.mehr : false;
  let aeltere = stand ? stand.aeltere : 0;
  const merke = () => { verlaufStand = { art, tags: tags.join('\n'), eintraege, mehr: mehrBekannt, alle: alleZeigen, aeltere, scroll: window.scrollY }; };
  filter.onclick = (ev) => {
    const b = ev.target.closest('button[data-art]');
    if (!b || b.dataset.art === art) return;
    localStorage.setItem('verlauf-art', b.dataset.art);
    zeigeVerlauf();
    maleMenue();
  };
  // seit: zuletzt gesehen, damit der Server auch Neues älter als 24 Stunden liefert.
  const seit = () => (Number.isFinite(besuche.grenze()) ? T`seit=${besuche.grenze()}` : '');
  const abfrage = () => [art === 'alles' ? '' : T`art=${art}`, ...tags.map((t) => T`tag=${encodeURIComponent(t)}`), alleZeigen ? 'alle=1' : seit()].filter(Boolean).join('&');

  // Mehrfachauswahl (langes Drücken auf eine Karte): wahl = Menge der Schlüssel „typ:id“, null = Auswahl aus.
  let wahl = null;
  const leiste = document.getElementById('wahlleiste');
  const schluessel = (k) => `${k.dataset.typ}:${k.dataset.id}`;
  const maleWahl = () => {
    seite.classList.toggle('auswahl', !!wahl);
    leiste.hidden = !wahl;
    if (!wahl) return;
    const karten = [...liste.querySelectorAll('.karte[data-id]')];
    const da = new Set(karten.map(schluessel));
    for (const k of [...wahl]) if (!da.has(k)) wahl.delete(k);
    for (const k of karten) k.classList.toggle('gewaehlt', wahl.has(schluessel(k)));
    const aus = wahl.size ? '' : ' disabled';
    leiste.innerHTML = T`<button class="btn klein" data-w="alle">Alle sichtbaren</button>
      <button class="btn klein haupt" data-w="erledigt"${aus}>${wahl.size} erledigt</button>
      <button class="btn klein gefahr" data-w="loeschen"${aus}>${wahl.size} löschen</button>
      <button class="btn klein" data-w="aus">Abbrechen</button>`;
  };
  const male = () => {
    const sicht = eintraege.filter((x) => ausstehend.get(x.id) !== null).map((x) => ausstehend.get(x.id) || x);
    liste.innerHTML = art === 'termin' ? terminListeHtml(sicht) : listeHtml(sicht, alleZeigen || art === 'fragen');
    maleWahl();
  };
  // Langes Drücken (0,5 s ohne Bewegung) startet die Auswahl mit dieser Karte; der folgende Klick öffnet sie nicht.
  let druck = null, langSperre = false;
  const druckAus = () => { if (druck) { clearTimeout(druck.timer); druck = null; } };
  liste.addEventListener('pointerdown', (ev) => {
    if (wahl || (ev.pointerType === 'mouse' && ev.button !== 0)) return;
    const k = ev.target.closest('.karte[data-id]');
    if (!k || ev.target.closest('[data-fragen],[data-fertig],button')) return;
    druckAus();
    druck = { x: ev.clientX, y: ev.clientY, id: ev.pointerId, timer: setTimeout(() => {
      druck = null;
      if (k.dataset.frei !== '1') return melde(tx('Wird noch verarbeitet – nicht auswählbar'));
      langSperre = true;
      wahl = new Set([schluessel(k)]);
      navigator.vibrate?.(30);
      maleWahl();
    }, 500) };
  });
  liste.addEventListener('pointermove', (ev) => {
    if (druck && ev.pointerId === druck.id && Math.hypot(ev.clientX - druck.x, ev.clientY - druck.y) > 10) druckAus();
  });
  for (const t of ['pointerup', 'pointercancel']) liste.addEventListener(t, () => { druckAus(); if (langSperre) setTimeout(() => { langSperre = false; }, 400); });
  // Das Kontextmenü (Link-Vorschau) beim langen Drücken unterdrücken.
  liste.addEventListener('contextmenu', (ev) => { if (ev.target.closest('.karte[data-id]')) ev.preventDefault(); });
  // Wischen: rechts erledigt, links löschen; ausgeführt erst nach der Frist für „Rückgängig“.
  const fertigMit = (id) => { ausstehend.delete(id); if (aktuell?.name === 'verlauf') { male(); maleTermine(termineStand); } };
  wischbar(liste, {
    finde: (id) => ausstehend.get(id) || eintraege.find((x) => x.id === id),
    aktiv: () => !wahl,
    aktion: (richtung, e) => {
      const planung = e.typ === 'planung';
      if (richtung === 'links') {
        if (!e.loeschbar) { melde(tx('Wird noch verarbeitet – nicht löschbar')); return false; }
        ausstehend.set(e.id, null);
        setTimeout(male, 250);
        maleTermine(termineStand);
        vormerken(T`Gelöscht: ${e.titel || tx('Eintrag')}`, async () => {
          await api('/api/verlauf/loeschen', { method: 'POST', body: planung ? { eintraege: [], planungen: [e.id] } : { eintraege: [e.id], planungen: [] } });
          await loesche('verlauf', e.id);
          await loesche('eintraege', e.id);
          eintraege = eintraege.filter((x) => x.id !== e.id);
          fertigMit(e.id);
          if (aktuell?.name === 'verlauf') aktuell.neuLaden?.();
        }, () => fertigMit(e.id));
        return true;
      }
      // Erledigt/offen ist ein fester Stand, unabhängig von Verarbeitung und Auswertung. Er geht sofort an den Server;
      // „Rückgängig“ sendet den anderen Stand hinterher (für den seltenen Fehlgriff).
      const erledigt = !istErledigt(e);
      const stand = (wert) => {
        const at = wert ? Date.now() : 0;
        if (planung) return { ...e, planung_status: wert ? 'geschlossen' : 'offen' };
        if (!e.termin) return { ...e, erledigt: at };
        const ende = e.termin.ganztag ? e.termin.beginn + 86400000 : e.termin.beginn;
        return { ...e, erledigt: at, termin: { ...e.termin, bestaetigt: at, verpasst: !wert && ende <= Date.now() } };
      };
      const zeigeStand = (wert) => { ausstehend.set(e.id, stand(wert)); male(); maleTermine(termineStand); };
      const senden = async (wert) => {
        try {
          const d = await api(planung ? T`/api/planungen/${e.id}/${wert ? 'schliessen' : 'oeffnen'}` : T`/api/eintraege/${e.id}/erledigt`, { method: 'POST', body: planung ? undefined : { erledigt: wert } });
          const neu = planung ? stand(wert) : d;
          await lege('verlauf', neu);
          eintraege = eintraege.map((x) => (x.id === e.id ? neu : x));
        } catch (f) {
          melde(f instanceof NetzFehler ? tx('Offline – nicht gespeichert') : f.message);
        }
        fertigMit(e.id);
        // Zahlen und „verpasste Termine“ vom Server nachziehen.
        if (aktuell?.name === 'verlauf') aktuell.neuLaden?.();
      };
      zeigeStand(erledigt);
      senden(erledigt);
      zeigeToast(`${erledigt ? tx('Erledigt') : tx('Wieder offen')}: ${e.titel || tx('Eintrag')}`, () => { zeigeStand(!erledigt); senden(!erledigt); });
      return false;
    },
  });
  liste.onclick = (ev) => {
    if (langSperre) { ev.preventDefault(); ev.stopPropagation(); langSperre = false; return; }
    if (!wahl) return;
    // Im Auswahlmodus wählt ein Tipp die Karte, statt sie zu öffnen.
    ev.preventDefault();
    ev.stopPropagation();
    const k = ev.target.closest('.karte[data-id]');
    if (!k) return;
    if (k.dataset.frei !== '1') return melde(tx('Wird noch verarbeitet – nicht löschbar'));
    if (!wahl.delete(schluessel(k))) wahl.add(schluessel(k));
    maleWahl();
  };
  leiste.onclick = async (ev) => {
    const b = ev.target.closest('button[data-w]');
    if (!b || !wahl) return;
    if (b.dataset.w === 'aus') { wahl = null; return maleWahl(); }
    if (b.dataset.w === 'alle') {
      for (const k of liste.querySelectorAll('.karte[data-id][data-frei="1"]')) wahl.add(schluessel(k));
      return maleWahl();
    }
    if (!wahl.size) return;
    if (b.dataset.w === 'erledigt') {
      // Erledigt: Notizen und Termine abhaken, Planungen schließen.
      b.disabled = true;
      let n = 0, fehler = 0;
      for (const k of [...wahl]) {
        const [typ, id] = [k.slice(0, k.indexOf(':')), k.slice(k.indexOf(':') + 1)];
        try {
          if (typ === 'planung') {
            await api(T`/api/planungen/${id}/schliessen`, { method: 'POST' });
            const e = eintraege.find((x) => x.id === id);
            if (e) await lege('verlauf', { ...e, planung_status: 'geschlossen' });
          } else {
            await lege('verlauf', await api(T`/api/eintraege/${id}/erledigt`, { method: 'POST', body: { erledigt: true } }));
          }
          n++;
        } catch (f) {
          if (f instanceof NetzFehler) { melde(tx('Offline – nur mit Verbindung')); b.disabled = false; return; }
          fehler++;
        }
      }
      wahl = null;
      melde(T`${n} erledigt${fehler ? T`, ${fehler} fehlgeschlagen` : ''}`);
      return laden();
    }
    if (!confirm(T`${wahl.size} ${wahl.size === 1 ? tx('Eintrag') : tx('Einträge')} in den Papierkorb legen? Planungen gehen mit allen Runden, Punkte in Listen ebenfalls. Unter Status → Papierkorb wiederherstellbar.`)) return;
    b.disabled = true;
    const ids = (typ) => [...wahl].filter((k) => k.startsWith(`${typ}:`)).map((k) => k.slice(typ.length + 1));
    try {
      const d = await api('/api/verlauf/loeschen', { method: 'POST', body: { eintraege: ids('eintrag'), planungen: ids('planung') } });
      for (const id of [...ids('eintrag'), ...ids('planung')]) { await loesche('verlauf', id); await loesche('eintraege', id); }
      wahl = null;
      melde(T`${d.geloescht} gelöscht${d.uebersprungen ? T`, ${d.uebersprungen} fehlgeschlagen` : ''}`);
      await laden();
    } catch (f) {
      b.disabled = false;
      melde(f.message);
    }
  };

  const mehrKnopf = (mehr) => {
    const box = document.getElementById('mehr');
    const zeigeAlle = !alleZeigen && aeltere > 0;
    box.innerHTML = `${mehr ? T('<button class="btn" data-m="mehr">Ältere laden</button>') : ''}${zeigeAlle ? T` <button class="btn" data-m="alle">${art === 'termin' ? T`Erledigte anzeigen (${aeltere})` : T`Alle anzeigen (${aeltere} ältere)`}</button>` : ''}`;
    box.onclick = async (ev) => {
      const b = ev.target.closest('button[data-m]');
      if (!b) return;
      b.disabled = true;
      if (b.dataset.m === 'alle') {
        alleZeigen = true;
        await laden();
        return;
      }
      const d = await api(T`/api/verlauf?vor=${eintraege[eintraege.length - 1].eingegangen}&${abfrage()}`);
      eintraege = eintraege.concat(d.eintraege);
      mehrBekannt = d.mehr;
      male();
      mehrKnopf(d.mehr);
      merke();
    };
  };
  if (eintraege.length) male();
  if (stand) {
    mehrKnopf(stand.mehr);
    window.scrollTo(0, stand.scroll);
  }
  const laden = async () => {
    try {
      const d = await api(T`/api/verlauf?${abfrage()}`);
      if (d.besuch_pause_minuten) besuche.pause(d.besuch_pause_minuten);
      if (typeof d.grenze === 'number' && !alleZeigen && art !== 'fragen' && art !== 'termin') verlaufGrenze.setze(d.grenze);
      if (aktuell?.name !== 'verlauf') return;
      const termine = { naechster: d.naechster_termin || null, verpasst: d.termine_verpasst || 0, in24h: d.termine_24h || 0 };
      localStorage.setItem('verlauf-termine', JSON.stringify(termine));
      maleTermine(termine);
      // Zahlen der Filterknöpfe; die Fragen- und die Termin-Ansicht liefern nur ihre eigenen.
      zahlen = d.zahlen || { ...(zahlen || { alles: 0, diktat: 0, planung: 0 }), fragen: d.fragen_offen || 0, termin: d.termine_offen || 0 };
      localStorage.setItem('verlauf-zahlen', JSON.stringify(zahlen));
      if (d.neu) localStorage.setItem('verlauf-neu', JSON.stringify(d.neu));
      maleMenue();
      maleFilter(zahlen);
      aeltere = d.aeltere || 0;
      // Zwischenspeicher = erste Seite des Servers; ältere bleiben, Entferntes fällt heraus.
      // In der Standardansicht liefert der Server nur Neues und die letzten 24 Stunden: der Rest bleibt liegen.
      const alt = (await alle('verlauf')).filter((e) => passt(e, art, tags) && gezeigt(e));
      const grenze = d.eintraege.length ? d.eintraege[d.eintraege.length - 1].eingegangen : 0;
      const neu = new Set(d.eintraege.map((e) => e.id));
      for (const e of alt) if (!neu.has(e.id) && (e.eingegangen >= grenze || !d.mehr)) await loesche('verlauf', e.id);
      for (const e of d.eintraege) await lege('verlauf', e);
      holeDetails(d.eintraege);
      if (aktuell?.name !== 'verlauf') return;
      // Erste Seite frisch vom Server; bereits nachgeladene ältere Einträge bleiben stehen.
      const aeltereSeiten = d.mehr ? eintraege.filter((e) => !neu.has(e.id) && e.eingegangen < grenze) : [];
      if (!aeltereSeiten.length) mehrBekannt = d.mehr;
      eintraege = d.eintraege.concat(aeltereSeiten);
      const y = window.scrollY;
      male();
      mehrKnopf(mehrBekannt);
      window.scrollTo(0, y);
      merke();
    } catch (e) {
      if (e instanceof NetzFehler && !eintraege.length) liste.innerHTML = T('<p class="leer">Offline – noch nichts zwischengespeichert.</p>');
      else if (!(e instanceof NetzFehler)) throw e;
    }
  };
  aktuell.neuLaden = laden;
  await laden();
}

// Gelesen wird ein Eintrag erst durch das Öffnen seiner Seite – nicht durch das Vorladen der Details.
async function merkeGelesen(typ, id) {
  try {
    await api(T`/api/${typ === 'planung' ? 'planungen' : 'eintraege'}/${id}/gelesen`, { method: 'POST' });
  } catch (f) {
    return; // offline: beim nächsten Öffnen mit Verbindung
  }
  const v = await lies('verlauf', id);
  if (v) await lege('verlauf', { ...v, gelesen: true });
  const st = verlaufStand?.eintraege.find((x) => x.id === id);
  if (st) st.gelesen = true;
}

// Details der gezeigten Einträge im Hintergrund zwischenspeichern, damit sie offline lesbar sind.
let holtDetails = false;
async function holeDetails(eintraege) {
  if (holtDetails) return;
  holtDetails = true;
  try {
    for (const e of eintraege) {
      const alt = await lies('eintraege', e.id);
      if (alt && alt.geaendert === e.geaendert) continue;
      await lege('eintraege', await api(`${e.typ === 'planung' ? '/api/planungen/' : '/api/eintraege/'}${e.id}`));
    }
  } catch (f) { /* offline oder abgemeldet: beim nächsten Laden */ } finally { holtDetails = false; }
}

// Symbole für Aus-/Einklappen (Pfeile auseinander bzw. zusammen).
const AUSKLAPPEN = T('<svg viewBox="0 0 24 24" aria-hidden="true"><path d="M7 9l5-5 5 5M7 15l5 5 5-5" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round"/></svg>');
const EINKLAPPEN = T('<svg viewBox="0 0 24 24" aria-hidden="true"><path d="M7 4l5 5 5-5M7 20l5-5 5 5" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round"/></svg>');
// Brotkrumen: [[Name, Pfad], …, [Name]] – alle bis auf den letzten Teil sind Links.
const krumen = (teile) => T`<nav class="krumen" aria-label="Pfad">${teile.map(([n, h], i) => (i < teile.length - 1 && h ? T`<a href="${h}">${esc(n)}</a><span class="trenner" aria-hidden="true">›</span>` : T`<span aria-current="page">${esc(n)}</span>`)).join('')}</nav>`;
const gruppenPfad = (projekt) => T`/listen/gruppe/${encodeURIComponent(projekt || '-')}`;
const gruppenTitel = (projekt) => (projekt && projekt !== '-' ? projekt : tx('Ohne Gruppe'));
// Pfad einer Liste: Listen › Gruppe › Liste (optional ein weiterer Teil dahinter).
const listenKrumen = (l, ...rest) => krumen([[tx('Listen'), '/listen'], [gruppenTitel(l.projekt), gruppenPfad(l.projekt)], [`${l.nr ? `L${l.nr} ` : ''}${l.name}`, T`/liste/${l.id}`], ...rest]);
// Notiz oder Planung: aus einer Liste geöffnet Listen › Gruppe › Liste › #Nr, sonst Verlauf › #Nr.
// Zurück zum Verlauf stellt Filter, Liste und Scrollstand wieder her (verlaufStand).
const zurueckHtml = (x) => {
  const nr = x.nr ? `#${x.nr}` : tx('Eintrag');
  if (herkunft && x.liste) return listenKrumen(x.liste, [nr]);
  if (herkunft) return krumen([[tx('Listen'), '/listen'], [nr]]);
  return krumen([[tx('Verlauf'), '/'], [nr]]);
};
document.addEventListener('click', (e) => {
  const knopf = e.target.closest('button[data-zurueck]');
  if (knopf) geheZu(knopf.dataset.zurueck);
});

// Tags in der Ansicht: oben als Marken; KI-Vorschläge mit „Übernehmen“/„Verwerfen“. Ändern über den Knopf in der Aktionsleiste.
// Kopf einer Detailseite: Brotkrumen (mit großer #Nr) und rechts der Mehr-Knopf; Titel passt seine Größe der Länge an;
// darunter nur Zeitpunkt, Zustand (falls nötig) und Tags. Hauptaktion groß darunter, alles Weitere im Mehr-Blatt.
const MEHR = T('<svg viewBox="0 0 24 24" aria-hidden="true"><circle cx="5" cy="12" r="2" fill="currentColor"/><circle cx="12" cy="12" r="2" fill="currentColor"/><circle cx="19" cy="12" r="2" fill="currentColor"/></svg>');
function detailKopf(x, titel, meta, haupt, mehr) {
  const t = titel || tx('Ohne Titel');
  const groesse = t.length > 70 ? ' lang' : t.length > 40 ? ' mittel' : '';
  const chips = (x.tags || []).map((tg) => T`<span class="tagchip">${esc(tg)}</span>`).join('');
  return T`<div class="detailkopf">${zurueckHtml(x)}${mehr.length ? T`<button type="button" class="titelknopf" data-mehr="1" aria-haspopup="dialog" aria-label="Weitere Aktionen" title="Weitere Aktionen">${MEHR}</button>` : ''}</div>
    <h1 class="detailtitel${groesse}">${esc(t)}</h1>
    <div class="detailmeta">${meta}${chips}</div>
    ${haupt.length ? T`<div class="hauptaktion">${haupt.join('')}</div>` : ''}
    ${mehr.length ? T`<dialog class="mehrblatt" aria-label="Weitere Aktionen" tabindex="-1"><div class="griff" aria-hidden="true"></div>${mehr.join('')}</dialog>` : ''}`;
}
// Mehr-Blatt: öffnen, Klick daneben oder auf einen Punkt schließt es (die Aktion selbst läuft über die Seite).
document.addEventListener('click', (e) => {
  const knopf = e.target.closest('[data-mehr]');
  if (knopf) {
    const d = knopf.closest('.detailkopf')?.parentElement.querySelector('dialog.mehrblatt');
    if (d) { d.showModal(); d.focus(); } // Fokus aufs Blatt, nicht auf den ersten Punkt
    return;
  }
  const blatt = e.target.closest('dialog.mehrblatt');
  if (blatt && (e.target === blatt || e.target.closest('a, button'))) blatt.close();
});
// Nur die Tag-Vorschläge der KI (die Tags selbst stehen im Kopf).
const tagVorschlaegeHtml = (e, offline) => tagsHtml({ ...e, tags: [] }, offline);

function tagsHtml(e, offline) {
  const tags = e.tags || [], vor = e.tag_vorschlaege || [];
  if (!tags.length && !vor.length) return '';
  return `${tags.length ? T`<div class="tags">${tags.map((t) => T`<span class="tagchip">${esc(t)}</span>`).join('')}</div>` : ''}
    ${vor.length ? T`<div class="vorschlaege"><div class="leise">Vorschläge der KI für neue Tags:</div>${vor.map((t) => T`<div class="vorschlag"><span class="tagchip neu">${esc(t)}</span>
      <button class="btn klein" data-akt="tag-ja" data-tag="${esc(t)}"${offline ? ' disabled' : ''}>Übernehmen</button><button class="btn klein gefahr" data-akt="tag-nein" data-tag="${esc(t)}"${offline ? ' disabled' : ''}>Verwerfen</button></div>`).join('')}</div>` : ''}`;
}

async function tagAktion(b, art, id) {
  await api(T`/api/${art === 'planung' ? 'planungen' : 'eintraege'}/${id}/tags/vorschlag`, { method: 'POST', body: { tag: b.dataset.tag, aktion: b.dataset.akt === 'tag-ja' ? 'uebernehmen' : 'verwerfen' } });
  melde(b.dataset.akt === 'tag-ja' ? tx('Tag übernommen') : tx('Vorschlag verworfen'));
}

// ---------- Tags ändern (eigene Seite) ----------

async function zeigeTags(art, id) {
  const pfad = art === 'planung' ? 'planungen' : 'eintraege';
  const zurueck = `/${art}/${id}`;
  const [e, vorrat] = await Promise.all([api(T`/api/${pfad}/${id}`), api('/api/tags')]);
  const gewaehlt = new Set(e.tags || []);
  const namen = [...new Set([...vorrat.tags.map((t) => t.name), ...gewaehlt, ...(e.tag_vorschlaege || [])])].sort((a, b) => a.localeCompare(b, 'de'));
  const male = () => {
    seite.innerHTML = T`<h1>Tags ändern</h1><p class="leise">${esc(e.titel || '')}</p>
      <label>Tags antippen zum An- und Abwählen</label>
      <div class="tags wahl">${namen.length ? namen.map((t) => T`<button class="tagchip wahl${gewaehlt.has(t) ? ' an' : ''}" data-tag="${esc(t)}" aria-pressed="${gewaehlt.has(t)}">${esc(t)}</button>`).join('') : T('<span class="leise">Noch keine Tags vorhanden.</span>')}</div>
      <label for="neu">Neuer Tag</label>
      <div class="zeile"><input type="text" id="neu" maxlength="30" placeholder="z. B. garten"><button class="btn" id="dazu">Hinzufügen</button></div>
      <div class="leiste"><button class="btn haupt" id="speichern">Speichern</button><a class="btn" href="${zurueck}">Abbrechen</a></div>`;
    document.getElementById('dazu').onclick = () => {
      const name = document.getElementById('neu').value.toLowerCase().replace(/["%_\\,#\[\]]/g, ' ').replace(/\s+/g, ' ').trim().slice(0, 30);
      if (!name) return;
      const da = namen.find((n) => n.toLowerCase() === name.toLowerCase());
      if (!da) namen.push(name);
      gewaehlt.add(da || name);
      male();
    };
    document.getElementById('neu').onkeydown = (ev) => { if (ev.key === 'Enter') document.getElementById('dazu').click(); };
    document.getElementById('speichern').onclick = async (ev) => {
      ev.target.disabled = true;
      try {
        await api(T`/api/${pfad}/${id}/tags`, { method: 'POST', body: { tags: [...gewaehlt] } });
        melde(tx('Tags gespeichert'));
        geheZu(zurueck, true);
      } catch (f) { ev.target.disabled = false; melde(f.message); }
    };
  };
  seite.onclick = (ev) => {
    const b = ev.target.closest('button.tagchip');
    if (!b) return;
    if (gewaehlt.has(b.dataset.tag)) gewaehlt.delete(b.dataset.tag); else gewaehlt.add(b.dataset.tag);
    male();
  };
  aufraeumen = () => { seite.onclick = null; };
  male();
}

// ---------- Eintrag ----------

// Wert eines Eckdatums: Quellenverweise der KI ([Seite](https://…)) werden kleine Links statt Rohtext.
const wertHtml = (wert) => esc(wert).replace(/\(?\[([^\]]+)\]\((https?:\/\/[^\s)]+)\)\)?/g, T('<a class="beleg" href="$2" target="_blank" rel="noopener noreferrer">$1</a>'));
const eckdatenHtml = (liste) => (liste?.length ? T`<dl class="eckdaten">${liste.map((f) => T`<dt>${esc(f.name)}</dt><dd>${wertHtml(f.wert)}</dd>`).join('')}</dl>` : '');
const linksHtml = (quellen) => (quellen?.length ? T`<ul class="links">${quellen.map((q) => (/^https?:\/\//.test(q.url) ? T`<li><a href="${esc(q.url)}" target="_blank" rel="noopener noreferrer">${esc(q.titel || q.url)}</a></li>` : T`<li>${esc(q.titel || q.url)}</li>`)).join('')}</ul>` : '');
// Ein Block der Detailseite; ohne Inhalt entfällt er samt Überschrift.
const block = (titel, inhalt, klasse = '') => (inhalt ? T`<section class="block ${klasse}"><h2>${titel}</h2>${inhalt}</section>` : '');
const notizHtml = (text) => (text ? T`<div class="notiztext">${esc(text)}</div>` : '');

// Stand einer Planung in fester Reihenfolge: Eckdaten, Notiz, Details (Hintergrund, Tipps, Fragen), Quellen eingeklappt.
function planHtml(plan, fragenKnopf = '') {
  const liste = (titel, punkte) => (punkte?.length ? T`<h3>${titel}</h3><ul>${punkte.map((p) => T`<li>${md(p).replace(/^<p>|<\/p>$/g, '')}</li>`).join('')}</ul>` : '');
  const planung = `${plan.ergebnisse ? T`<h3>Hintergrund</h3><div class="md">${md(plan.ergebnisse)}</div>` : ''}
    ${liste(tx('Tipps'), plan.tipps)}
    ${liste(tx('Nächste Schritte'), plan.naechste_schritte)}
    ${plan.offene_fragen?.length ? T`<h3>Offene Fragen</h3><ul>${plan.offene_fragen.map((f) => T`<li>${esc(frageText(f))}</li>`).join('')}</ul>${fragenKnopf}` : ''}
    ${plan.annahmen?.length ? T`<details><summary>Annahmen / unklar (${plan.annahmen.length})</summary><ul>${plan.annahmen.map((p) => T`<li>${esc(p)}</li>`).join('')}</ul></details>` : ''}`;
  return `${block(tx('Eckdaten'), `${plan.vorhaben ? T`<p class="vorhaben">${esc(plan.vorhaben)}</p>` : ''}${eckdatenHtml(plan.eckdaten)}`)}
    ${block(tx('Text'), notizHtml(plan.notiz), 'notiz')}
    ${block(tx('Details'), planung.trim(), 'zusatz')}
    ${plan.quellen?.length ? T`<details class="einklapp quellen"><summary>Quellen (${plan.quellen.length})</summary>${linksHtml(plan.quellen)}</details>` : ''}`;
}

// Auswertung eines Diktats in fester Reihenfolge: Eckdaten (bei Listenpunkten je Punkt), Notiz, Hinweise und Links.
function analyseHtml(e, aufnahme = '') {
  const an = e.analyse;
  let html = '';
  if (an && e.fragen && an.rueckfragen?.length) {
    html += T`<div class="hinweis warn"><strong>Die KI hat ${fragenText(e.fragen)} zu dieser Notiz:</strong><ul>${an.rueckfragen.map((f) => T`<li>${esc(f.frage)}</li>`).join('')}</ul>
      <div class="leiste"><button class="btn haupt" data-fragen="${e.id}" data-ftyp="eintrag">${tx('{0} beantworten', fragenText(e.fragen))}</button></div></div>`;
  }
  if (an?.antworten?.length) html += T`<h2>Deine Antworten</h2><dl>${an.antworten.map((x) => T`<dt>${esc(x.frage)}</dt><dd>${esc(x.antwort)}</dd>`).join('')}</dl>`;
  // Eckdaten zuerst (bei mehreren Punkten je Punkt mit Namen), darunter getrennt der Listenbezug mit Erledigt-Haken.
  const lp = e.liste?.punkte || [];
  const mehrere = lp.length > 1;
  html += block(tx('Eckdaten'), lp.map((p) => (p.eckdaten?.length ? `${mehrere ? `<h3>${esc(p.text)}</h3>` : ''}${eckdatenHtml(p.eckdaten)}` : '')).join('') + eckdatenHtml(an?.eckdaten));
  if (e.liste) {
    html += block(T`Liste <a href="/liste/${e.liste.id}">${esc(listenName(e.liste))}</a>`, lp.map((p) => T`<div class="lp${p.erledigt ? ' erledigt' : ''}"><button class="haken" data-haken="${p.id}" aria-pressed="${!!p.erledigt}" aria-label="${p.erledigt ? tx('Wieder öffnen') : tx('Abhaken')}: ${esc(p.text)}"><span class="hakenbild" aria-hidden="true"></span></button><div class="lptext"><div>${esc(p.text)}</div></div></div>`).join(''), 'listenbezug');
  }
  // Text: kurze KI-Fassung des Gesagten; ältere Einträge ohne diese Fassung zeigen hier das Transkript.
  html += block(tx('Text'), notizHtml(an?.notiz || e.transkript), 'notiz');
  if (!an) return html + aufnahme;
  // Details: Zusatzwissen (z. B. Inhalt eines Films), Hinweise, Links. Die Aufnahme steht als eigene Karte am Ende.
  const mehr = `${an.beschreibung ? T`<div class="beschreibung">${esc(an.beschreibung)}</div>` : ''}${an.hinweise?.length ? T`<ul>${an.hinweise.map((h) => T`<li>${md(h).replace(/^<p>|<\/p>$/g, '')}</li>`).join('')}</ul>` : ''}${an.quellen?.length ? T`<h3>Links</h3>${linksHtml(an.quellen)}` : ''}`;
  return html + block(tx('Details'), mehr, 'zusatz') + aufnahme;
}

function eintragHtml(e, offline) {
  const p = e.planung_daten;
  const offen = p && p.status === 'offen';
  const fertig = e.status === 'fertig';
  // Zustand nur, wenn er etwas sagt (läuft, Fehler, leer); „fertig“ ist der Normalfall.
  const meta = T`<span class="leise">${zeit(e.aufgenommen)}</span>${fertig ? '' : T` <span class="badge ${esc(e.status)}">${esc(STATUS[e.status] || e.status)}${e.schritt ? ` · ${esc(e.schritt)}` : ''}</span>`}`;
  const mitTags = e.tags && !e.planung && (e.modus === 'diktat' || e.modus === 'planung');
  let html = mitTags ? tagVorschlaegeHtml(e, offline) : '';

  const wann = e.loeschung ? T` Wird am ${zeit(e.loeschung)} automatisch gelöscht.` : '';
  if (e.status === 'leer') html += T`<div class="hinweis">Keine Sprache erkannt – vermutlich versehentlich ausgelöst.${wann}</div>`;
  else if (e.status === 'fehler') html += T`<div class="hinweis schlecht"><strong>Dauerhaft fehlgeschlagen</strong> (${esc(e.schritt || '')}): ${esc(e.fehler)}${wann}</div>`;
  else if (e.status === 'wiederholung') html += T`<div class="hinweis warn">${esc(e.schritt || '')} fehlgeschlagen, Versuch ${e.versuche}: ${esc(e.fehler)}<br>Nächster Versuch ${e.naechster_versuch ? zeit(e.naechster_versuch) : tx('gleich')}.</div>`;
  if (e.limit_erreicht) html += T`<div class="hinweis warn">Kostenlimit von ${e.limit_usd.toFixed(2)} $ in dieser Runde erreicht – die Recherche wurde begrenzt.</div>`;
  if (p && !p.aktuell && p.plan) html += T('<div class="hinweis">Stand nach dieser Runde – es gibt eine neuere Runde.</div>');
  const t = e.termin;
  if (t) {
    const wann = `${tag(t.beginn)}${t.ganztag ? tx(', ganztägig') : `, ${tx('{0} Uhr', uhr(t.beginn))}`}`;
    // Gesendet nur, wenn die Erinnerung vor dem Termin rausging (nachgetragene oder verspätete zählen nicht).
    const gesendet = t.erinnert && t.erinnert <= t.beginn + (t.ganztag ? 86400000 : 0);
    const erinnerung = gesendet ? T`Erinnerung gesendet ${zeit(t.erinnert)}` : t.bestaetigt || t.verpasst ? '' : T`Erinnerung ${zeit(t.erinnerung)}`;
    html += T`<div class="hinweis termin${t.verpasst ? ' schlecht' : t.bestaetigt ? ' gut' : ''}"><strong>${t.verpasst ? T`${terminArt(t)} verpasst` : t.bestaetigt ? T`${terminArt(t)} erledigt` : terminArt(t)}:</strong> ${esc(wann)}${t.ort ? ` · ${esc(t.ort)}` : ''}${erinnerung ? T`<br><span class="leise">${erinnerung}</span>` : ''}</div>`;
  }

  // Hauptaktion groß unter dem Kopf, alles Weitere im Mehr-Blatt (Punkte mit Klasse mehrpunkt).
  const haupt = [], mehr = [];
  const punkt = (art, attr, text, klasse = '') => (art === 'a' ? T`<a class="mehrpunkt${klasse}" ${attr}>${text}</a>` : T`<button type="button" class="mehrpunkt${klasse}" ${attr}>${text}</button>`);
  if (e.modus === 'diktat' && !e.planung && !offline) {
    const zu = istErledigt(e);
    haupt.push(zu ? T`<button class="btn" data-fertig="${e.id}" data-erledigt="0">Wieder öffnen</button>` : T`<button class="btn haupt" data-fertig="${e.id}">${t?.verpasst ? tx('Bestätigen') : tx('Erledigt')}</button>`);
  }
  // Termin: zweiter Knopf zum Verschieben (eigenes Fenster mit Datum und Uhrzeit).
  if (t && fertig && !offline) haupt.push(T('<button class="btn" data-akt="verschieben">Verschieben</button>'));
  if (e.status === 'fehler' || e.status === 'wiederholung' || e.status === 'leer') haupt.push(T`<button class="btn${e.status === 'leer' ? '' : ' haupt'}" data-akt="neustart">Erneut starten</button>`);
  if (offen && fertig) haupt.push(T`<a class="btn haupt" href="/planung/${p.id}/antwort">Antworten</a>`);
  if (fertig || e.transkript) {
    if (navigator.share) mehr.push(punkt('b', 'data-akt="teilen"', tx('Teilen')));
    mehr.push(punkt('b', 'data-akt="kopieren"', tx('Kopieren')));
  }
  if (e.joplin?.aktiv && e.joplin.uebertragen) mehr.push(punkt('a', T`href="joplin://x-callback-url/openNote?id=${esc(e.joplin.id)}" target="_self"`, tx('In Joplin öffnen')));
  if (offen) mehr.push(punkt('b', 'data-akt="schliessen"', tx('Planung schließen')));
  if (offen && p.letzte_runde && (fertig || e.status === 'fehler') && !offline) mehr.push(punkt('a', T`href="/eintrag/${e.id}/umhaengen"`, tx('Umhängen')));
  // Wortlaut korrigieren: Notizen und die letzte Runde einer Planung, sobald nichts mehr läuft.
  if (e.transkript && (fertig || e.status === 'fehler') && !offline && (e.modus === 'diktat' || (e.modus === 'planung' && (!p || p.letzte_runde)))) mehr.push(punkt('a', T`href="/eintrag/${e.id}/wortlaut"`, tx('Wortlaut korrigieren')));
  if (!p && e.modus === 'planung' && e.status === 'fehler' && e.transkript && !offline) mehr.push(punkt('a', T`href="/eintrag/${e.id}/umhaengen"`, tx('Umhängen')));
  if (e.modus === 'diktat' && !e.termin && fertig && !offline) mehr.push(punkt('a', T`href="/eintrag/${e.id}/liste"`, e.liste ? tx('Liste wechseln') : tx('In Liste')));
  if (mitTags && !offline) mehr.push(punkt('a', T`href="/tags/eintrag/${e.id}"`, tx('Tags ändern')));
  if (e.loeschbar && !offline) mehr.push(punkt('b', 'data-akt="loeschen"', tx('In den Papierkorb'), ' gefahr'));
  html = detailKopf(e, e.titel, meta, haupt, mehr) + html;
  // Feste Reihenfolge: Eckdaten, Liste, Notiz, Aufnahme (Wortlaut eingeklappt), Hinweise, Links, Details eingeklappt.
  // Feste Reihenfolge: Eckdaten, Notiz, Hinweise und Links, zuletzt Aufnahme und Wortlaut.
  let aufnahme = T('<p class="leise">Keine Aufnahme (Text).</p>');
  if (e.audio === 'da') aufnahme = offline ? T('<p class="leise">Wiedergabe nur mit Verbindung.</p>') : T`<audio controls preload="none" src="/api/eintraege/${e.id}/audio"></audio>`;
  else if (e.audio === 'abgelaufen') aufnahme = T('<p class="leise">Aufnahme abgelaufen – das Audio wurde nach der Aufbewahrungsfrist gelöscht.</p>');
  // Wiedergabe direkt unter dem Text; der Original-Wortlaut ist eingeklappt – offen nur, wenn es keinen bereinigten Text gibt.
  // Notizen zeigen das Gesagte schon als Text; der Original-Wortlaut liegt eingeklappt bei der Aufnahme – nur wenn er nicht schon oben steht.
  const obenText = e.modus === 'diktat' && !e.analyse?.notiz;
  const wortlaut = obenText ? '' : e.transkript ? T`<details class="einklapp"${e.modus === 'planung' && !p?.plan?.notiz ? ' open' : ''}><summary>Original-Wortlaut</summary><div class="wortlaut">${esc(e.transkript)}</div></details>` : T('<p class="leise">Noch kein Transkript.</p>');
  // Notiz: Aufnahme und Wortlaut eingeklappt; Planungsrunde wie bisher.
  const quelle = e.modus === 'diktat' ? T`<details class="einklapp quelle"><summary>Aufnahme und Wortlaut</summary>${aufnahme}${obenText ? '' : e.transkript ? T`<div class="wortlaut">${esc(e.transkript)}</div>` : ''}</details>`
    : block(tx('Audio dieser Runde'), aufnahme + wortlaut, 'quelle');
  if (p?.plan) html += planHtml(p.plan);
  if (e.modus === 'diktat') html += analyseHtml(e, quelle);
  else html += quelle;
  if (e.modus === 'diktat' && fertig && !e.analyse) html += T('<p class="leise">Keine Auswertung vorhanden.</p>');

  if (p?.runden?.length > 1) {
    html += T`<h2>Runden</h2>${p.runden.map((r) => T`<a class="karte" href="/eintrag/${r.id}"><div class="zeile"><span class="leise">Runde ${r.nr} · ${zeit(r.zeit)} · ${r.quelle === 'web' ? tx('Web-App') : r.quelle === 'import' ? tx('Import') : tx('Pebble')}</span>
      ${r.id === e.id ? T('<span class="badge neutral">diese</span>') : T`<span class="badge ${esc(r.status)}">${esc(STATUS[r.status] || r.status)}</span>`}</div><div class="vorschau">${esc(r.transkript)}</div></a>`).join('')}`;
  }

  const details = [];
  if (e.zuordnung) details.push([tx('Zuordnung'), ZUORDNUNG[e.zuordnung] || e.zuordnung]);
  if (e.transkript_quelle) details.push([tx('Transkript von'), { openai: tx('OpenAI'), pebble: tx('Pebble'), text: tx('getippt'), nutzer: tx('von Hand korrigiert') }[e.transkript_quelle] || e.transkript_quelle]);
  if (e.kosten_usd != null) details.push([e.modus === 'diktat' ? tx('Kosten der Auswertung') : tx('Recherchekosten'), `${e.kosten_usd.toFixed(3)} $`]);
  if (e.joplin?.aktiv) details.push([tx('Joplin'), e.joplin.fehler ? T`Übertragung fehlgeschlagen: ${e.joplin.fehler}` : e.joplin.offen ? tx('noch nicht übertragen') : T`übertragen ${zeit(e.joplin.uebertragen)}`]);
  const pebbleText = e.pebble_text && e.pebble_text !== e.transkript ? T`<h3>Transkript der Pebble-App</h3><blockquote>${esc(e.pebble_text)}</blockquote>` : '';
  if (details.length || pebbleText) html += T`<details class="einklapp technik"><summary>Technische Details</summary>${details.length ? T`<dl>${details.map(([k, v]) => T`<dt>${esc(k)}</dt><dd>${esc(v)}</dd>`).join('')}</dl>` : ''}${pebbleText}</details>`;
  return html;
}

// Termin verschieben: Fenster mit Datum, Uhrzeit und „ganztägig“; der alte Termin bleibt als Hinweis in der Notiz.
// Mobil gut bedienbar: große Felder (Tippen öffnet die Auswahl des Systems), ‹ › für ±1 Tag bzw. ±15 min, Schnellwahl.
function terminVerschieben(e, danach) {
  const t = e.termin;
  const zwei = (n) => String(n).padStart(2, '0');
  const alsDatum = (d) => `${d.getFullYear()}-${zwei(d.getMonth() + 1)}-${zwei(d.getDate())}`;
  const d0 = new Date(t.beginn);
  const WT = Array.from({ length: 7 }, (_, i) => new Date(2026, 10, 15 + i).toLocaleDateString(LOCALE, { weekday: 'long' }));
  const pfeil = (r) => T`<svg viewBox="0 0 24 24" aria-hidden="true"><path d="${r ? 'M9 5l7 7-7 7' : 'M15 5l-7 7 7 7'}" fill="none" stroke="currentColor" stroke-width="2.4" stroke-linecap="round" stroke-linejoin="round"/></svg>`;
  const dlg = document.createElement('dialog');
  dlg.className = tx('formblatt terminblatt');
  dlg.innerHTML = T`<form method="dialog" id="vform"><h2>Termin verschieben</h2>
    <p class="leise">Bisher: ${esc(tag(t.beginn))}${t.ganztag ? tx(', ganztägig') : `, ${tx('{0} Uhr', uhr(t.beginn))}`}. Der alte Termin bleibt als Hinweis in der Notiz.</p>
    <label for="vdatum">Datum <span class="leise" id="vwt"></span></label>
    <div class="stepper"><button type="button" class="rundknopf" data-tage="-1" aria-label="Einen Tag früher">${pfeil(false)}</button>
      <input type="date" id="vdatum" value="${alsDatum(d0)}" required><button type="button" class="rundknopf" data-tage="1" aria-label="Einen Tag später">${pfeil(true)}</button></div>
    <div class="chips"><button type="button" data-ab="0">Heute</button><button type="button" data-ab="1">Morgen</button><button type="button" data-plus="7">+1 Woche</button></div>
    <div id="vzeitblock"${t.ganztag ? ' hidden' : ''}>
      <label for="vzeit">Uhrzeit</label>
      <div class="stepper"><button type="button" class="rundknopf" data-min="-15" aria-label="15 Minuten früher">${pfeil(false)}</button>
        <input type="time" id="vzeit" step="300" value="${t.ganztag ? '07:00' : `${zwei(d0.getHours())}:${zwei(d0.getMinutes())}`}"><button type="button" class="rundknopf" data-min="15" aria-label="15 Minuten später">${pfeil(true)}</button></div>
      <div class="chips">${['07:00', '09:00', '12:00', '15:00', '18:00'].map((z) => T`<button type="button" data-zeit="${z}">${z}</button>`).join('')}</div>
    </div>
    <label class="schalter"><input type="checkbox" id="vganz"${t.ganztag ? ' checked' : ''}> ganztägig</label>
    <div class="leiste"><button class="btn haupt" type="submit" id="vok">Verschieben</button><button class="btn" type="button" id="vab">Abbrechen</button></div></form>`;
  document.body.append(dlg);
  const zu = () => { dlg.close(); dlg.remove(); };
  const datum = dlg.querySelector('#vdatum'), zeitFeld = dlg.querySelector('#vzeit'), ganz = dlg.querySelector('#vganz');
  const lies = () => { const [y, m, dd] = datum.value.split('-').map(Number); return new Date(y, m - 1, dd); };
  const wochentag = () => { dlg.querySelector('#vwt').textContent = datum.value ? `· ${WT[lies().getDay()]}` : ''; };
  wochentag();
  datum.addEventListener('input', wochentag);
  // Tippen aufs Feld öffnet die Auswahl des Systems (wo unterstützt), nicht nur das kleine Symbol.
  for (const f of [datum, zeitFeld]) f.addEventListener('click', () => { try { f.showPicker(); } catch { /* ältere Browser: Standard */ } });
  ganz.onchange = () => { dlg.querySelector('#vzeitblock').hidden = ganz.checked; };
  dlg.addEventListener('click', (ev) => {
    if (ev.target === dlg) return zu();
    const b = ev.target.closest('button');
    if (!b) return;
    const heute = new Date();
    if (b.dataset.tage) { const d = datum.value ? lies() : heute; d.setDate(d.getDate() + Number(b.dataset.tage)); datum.value = alsDatum(d); }
    else if (b.dataset.ab) { const d = new Date(heute); d.setDate(d.getDate() + Number(b.dataset.ab)); datum.value = alsDatum(d); }
    else if (b.dataset.plus) { const d = datum.value ? lies() : heute; d.setDate(d.getDate() + Number(b.dataset.plus)); datum.value = alsDatum(d); }
    else if (b.dataset.zeit) zeitFeld.value = b.dataset.zeit;
    else if (b.dataset.min) {
      const [h, m] = (zeitFeld.value || '07:00').split(':').map(Number);
      let min = h * 60 + m + Number(b.dataset.min);
      min = ((Math.round(min / 15) * 15) % 1440 + 1440) % 1440;
      zeitFeld.value = `${zwei(Math.floor(min / 60))}:${zwei(min % 60)}`;
    } else return;
    wochentag();
  });
  dlg.querySelector('#vab').onclick = zu;
  dlg.addEventListener('cancel', () => dlg.remove());
  dlg.querySelector('#vform').onsubmit = async (ev) => {
    ev.preventDefault();
    const tagWert = datum.value;
    if (!tagWert || (!ganz.checked && !zeitFeld.value)) return melde(tx('Datum und Uhrzeit angeben'));
    const ok = dlg.querySelector('#vok');
    ok.disabled = true;
    try {
      await api(T`/api/eintraege/${e.id}/termin`, { method: 'POST', body: { beginn: ganz.checked ? tagWert : `${tagWert}T${zeitFeld.value}`, ganztag: ganz.checked } });
      zu();
      melde(tx('Termin verschoben'));
      verlaufStand = null;
      await danach();
    } catch (f) { ok.disabled = false; melde(f.message); }
  };
  dlg.showModal();
  dlg.querySelector('#vok').focus({ focusVisible: false });
}

async function zeigeEintrag(id) {
  let e = await lies('eintraege', id);
  let offline = false;
  let gemalt = false;
  const male = () => {
    if (aktuell?.name !== 'eintrag' || aktuell.arg !== id) return;
    // Eine Runde gehört zu ihrer Planung: dort steht alles auf einer Seite.
    if (e?.planung) return geheZu(T`/planung/${e.planung}`, true);
    // Laufende Wiedergabe nicht durch Neuzeichnen abbrechen.
    const spieler = seite.querySelector('audio');
    if (spieler && !spieler.paused) return;
    // Beim ersten Zeichnen gilt die Vorgabe (auf/zu) aus dem HTML, danach bleibt, was der Nutzer aufgeklappt hat.
    const auf = gemalt ? [...seite.querySelectorAll('details')].map((d) => d.open) : null;
    seite.innerHTML = eintragHtml(e, offline);
    const neu = seite.querySelectorAll('details');
    if (auf && auf.length === neu.length) neu.forEach((d, i) => { d.open = auf[i]; });
    gemalt = true;
  };
  if (e) { offline = !navigator.onLine || !netzDa; male(); } else seite.innerHTML = T('<p class="leer">Lädt …</p>');
  const laden = async () => {
    try {
      e = await api(T`/api/eintraege/${id}`);
      offline = false;
      await lege('eintraege', e);
      male();
      if (e.gelesen === false && !e.planung && aktuell?.name === 'eintrag' && aktuell.arg === id) merkeGelesen('eintrag', id);
    } catch (f) {
      if (f instanceof NetzFehler) {
        offline = true;
        if (e) male(); else seite.innerHTML = T('<p class="leer">Offline – dieser Eintrag ist nicht zwischengespeichert.</p>');
      } else if (f instanceof ApiFehler && f.status === 404) {
        await loesche('eintraege', id);
        await loesche('verlauf', id);
        seite.innerHTML = T('<p class="leer">Eintrag nicht gefunden.</p>');
      } else throw f;
    }
  };
  aktuell.neuLaden = laden;
  seite.onclick = async (ev) => {
    const haken = ev.target.closest('button[data-haken]');
    if (haken && e?.liste) {
      // Punkt der Liste gleich hier abhaken: sofort umschalten, gesendet wird im Hintergrund (ohne Verbindung vorgemerkt).
      const punkt = e.liste.punkte.find((x) => x.id === haken.dataset.haken);
      if (!punkt) return;
      punkt.erledigt = punkt.erledigt ? 0 : Date.now();
      await lege('eintraege', e);
      male();
      await merkeVor({ typ: 'haken', punkt: punkt.id });
      return;
    }
    const b = ev.target.closest('button[data-akt]');
    if (!b || !e) return;
    const akt = b.dataset.akt;
    try {
      if (akt === 'tag-ja' || akt === 'tag-nein') { b.disabled = true; await tagAktion(b, 'eintrag', e.id); await laden(); }
      else if (akt === 'verschieben') terminVerschieben(e, laden);
      else if (akt === 'teilen') await navigator.share({ title: e.titel, text: e.text });
      else if (akt === 'kopieren') { await navigator.clipboard.writeText(e.text); melde(tx('Text kopiert')); }
      else if (akt === 'loeschen') {
        if (!confirm((e.liste ? tx('Eintrag in den Papierkorb legen? Seine Punkte verschwinden auch aus der Liste.') : tx('Eintrag in den Papierkorb legen?')) + tx(' Unter Status → Papierkorb wiederherstellbar.') + (e.status !== 'fertig' && e.status !== 'fehler' ? tx(' Die laufende Verarbeitung wird abgebrochen.') : ''))) return;
        b.disabled = true;
        await api(T`/api/eintraege/${e.id}`, { method: 'DELETE' });
        await loesche('eintraege', e.id);
        await loesche('verlauf', e.id);
        verlaufStand = null;
        melde(tx('Gelöscht'));
        geheZu('/', true);
      } else if (akt === 'neustart') {
        b.disabled = true;
        melde((await merkeVor({ typ: 'neustart', eintrag: e.id })) ? tx('Neu gestartet') : tx('Vorgemerkt – startet bei Verbindung'));
        await laden();
      } else if (akt === 'schliessen') {
        if (!confirm(tx('Planung schließen? Sie wird nicht mehr fortgesetzt.'))) return;
        b.disabled = true;
        melde((await merkeVor({ typ: 'schliessen', planung: e.planung_daten.id })) ? tx('Planung geschlossen') : tx('Vorgemerkt – schließt bei Verbindung'));
        await laden();
      }
    } catch (f) {
      if (f.name !== 'AbortError') melde(f.message);
    }
  };
  aufraeumen = () => { seite.onclick = null; };
  await laden();
}

// ---------- Planung (eine Seite je Planung) ----------

function planungHtml(p, offline) {
  const offen = p.planung_status === 'offen';
  const runden = p.rundenliste;
  const letzte = runden[0];
  const laeuft = letzte.status !== 'fertig';
  const meta = T`<span class="leise">${zeit(letzte.zeit)}</span> ${planungZeichen(offen)}${laeuft ? T` <span class="badge ${esc(letzte.status)}">Runde ${letzte.nr}: ${esc(STATUS[letzte.status])}${letzte.schritt ? ` · ${esc(letzte.schritt)}` : ''}</span>` : ''}`;
  let html = tagVorschlaegeHtml(p, offline);

  if (!offen) html += T`<div class="hinweis">${p.geschlossen_von === 'automatik' ? T`Automatisch abgeschlossen am ${zeit(p.geschlossen)}, weil ${p.schliesst_nach_tagen} Tage keine neue Runde kam.` : T`Abgeschlossen am ${zeit(p.geschlossen)}.`} Eine abgeschlossene Planung wird nicht fortgesetzt.</div>`;
  if (letzte.status === 'fehler') html += T`<div class="hinweis schlecht"><strong>Runde ${letzte.nr} dauerhaft fehlgeschlagen</strong> (${esc(letzte.schritt || '')}): ${esc(letzte.fehler)}${p.plan ? T('<br>Unten steht der Stand davor.') : ''}</div>`;
  else if (letzte.status === 'wiederholung') html += T`<div class="hinweis warn">Runde ${letzte.nr}: ${esc(letzte.schritt || '')} fehlgeschlagen, Versuch ${letzte.versuche}: ${esc(letzte.fehler)}<br>Nächster Versuch ${letzte.naechster_versuch ? zeit(letzte.naechster_versuch) : tx('gleich')}.</div>`;
  else if (laeuft) html += T`<div class="hinweis">Runde ${letzte.nr} wird verarbeitet${p.plan ? tx(' – unten steht noch der Stand davor') : ''}.</div>`;
  if (runden.find((r) => r.aktuell)?.limit_erreicht) html += T`<div class="hinweis warn">Kostenlimit von ${p.limit_usd.toFixed(2)} $ in der letzten Recherche erreicht – sie wurde begrenzt.</div>`;

  const haupt = [], mehr = [];
  const punkt = (art, attr, text, klasse = '') => (art === 'a' ? T`<a class="mehrpunkt${klasse}" ${attr}>${text}</a>` : T`<button type="button" class="mehrpunkt${klasse}" ${attr}>${text}</button>`);
  if (letzte.status === 'fehler' || letzte.status === 'wiederholung') haupt.push(T`<button class="btn haupt" data-akt="neustart" data-id="${letzte.id}">Erneut starten</button>`);
  if (p.fragen && !offline) haupt.push(T`<button class="btn haupt" data-fragen="${p.id}">${tx('{0} beantworten', fragenText(p.fragen))}</button>`);
  // Offene Planung: zwei gleichrangige Aktionen, Antworten oder Schließen; abgeschlossene: Wieder öffnen.
  if (offen && !laeuft) haupt.push(T`<a class="btn${p.fragen ? '' : ' haupt'}" href="/planung/${p.id}/antwort">Antworten</a>`);
  if (offen) haupt.push(T('<button class="btn" data-akt="schliessen">Schließen</button>'));
  else if (!offline) haupt.push(T('<button class="btn" data-akt="oeffnen">Wieder öffnen</button>'));
  if (p.text) {
    if (navigator.share) mehr.push(punkt('b', 'data-akt="teilen"', tx('Teilen')));
    mehr.push(punkt('b', 'data-akt="kopieren"', tx('Kopieren')));
  }
  if (!laeuft && !offline && letzte.transkript && (letzte.status === 'fertig' || letzte.status === 'fehler')) mehr.push(punkt('a', T`href="/eintrag/${letzte.id}/wortlaut"`, tx('Wortlaut korrigieren')));
  if (p.joplin?.aktiv && p.joplin.uebertragen) mehr.push(punkt('a', T`href="joplin://x-callback-url/openNote?id=${esc(p.joplin.id)}" target="_self"`, tx('In Joplin öffnen')));
  if (p.plan && !offline) mehr.push(punkt('a', T`href="/planung/${p.id}/liste"`, p.liste ? tx('Liste wechseln') : tx('In Liste')));
  if (!offline) mehr.push(punkt('a', T`href="/tags/planung/${p.id}"`, tx('Tags ändern')));
  if (letzte.status === 'fehler' && !offline) mehr.push(punkt('b', T`data-akt="loeschen" data-id="${letzte.id}"`, tx('Fehlgeschlagene Runde löschen'), ' gefahr'));
  if (!offline) mehr.push(punkt('b', 'data-akt="planung-loeschen"', tx('Planung in den Papierkorb'), ' gefahr'));
  html = detailKopf(p, p.titel, meta, haupt, mehr) + html;


  if (p.plan) html += planHtml(p.plan, p.fragen && !offline ? T`<div class="leiste"><button class="btn haupt" data-fragen="${p.id}">${tx('{0} beantworten', fragenText(p.fragen))}</button></div>` : '');

  html += T`<section class="block quelle"><h2>Audio und Wortlaut</h2><p class="leise">Jede Runde ergibt einen neuen Stand. Die neueste steht oben.</p>`;
  for (const r of runden) {
    const marke = r.aktuell ? T('<span class="badge fertig">aktueller Stand</span>') : r.status !== 'fertig' ? T`<span class="badge ${esc(r.status)}">${esc(STATUS[r.status])}${r.schritt ? ` · ${esc(r.schritt)}` : ''}</span>` : T('<span class="badge neutral">früherer Stand</span>');
    const meta = [];
    if (r.zuordnung) meta.push(ZUORDNUNG[r.zuordnung] || r.zuordnung);
    if (r.kosten_usd != null) meta.push(`${r.kosten_usd.toFixed(3)} $`);
    if (r.limit_erreicht) meta.push(tx('Kostenlimit erreicht'));
    let audio = '';
    if (r.audio === 'da') audio = offline ? T('<p class="leise">Wiedergabe nur mit Verbindung.</p>') : T`<audio controls preload="none" src="/api/eintraege/${r.id}/audio"></audio>`;
    else if (r.audio === 'abgelaufen') audio = T('<p class="leise">Aufnahme abgelaufen.</p>');
    html += T`<div class="karte version${r.aktuell ? ' aktuell' : ''}">
      <div class="zeile"><strong>Runde ${r.nr}</strong>${marke}</div>
      <div class="leise">${zeit(r.zeit)} · ${r.quelle === 'web' ? tx('Web-App') : r.quelle === 'import' ? tx('Import') : tx('Pebble')}${meta.length ? ` · ${esc(meta.join(' · '))}` : ''}</div>
      ${r.transkript ? T`<blockquote>${esc(r.transkript)}</blockquote>` : T('<p class="leise">Noch kein Transkript.</p>')}${audio}
      ${offline ? '' : T`<div class="leiste">${r.letzte && offen && (r.status === 'fertig' || r.status === 'fehler') ? T`<a class="btn klein" href="/eintrag/${r.id}/umhaengen">Diese Runde umhängen</a>` : ''}<button class="btn klein gefahr" data-akt="loeschen" data-id="${r.id}">Runde löschen</button></div>`}
      ${r.plan && !r.aktuell ? T`<details><summary>Stand nach Runde ${r.nr} ansehen</summary>${planHtml(r.plan)}</details>` : ''}</div>`;
  }
  html += '</section>';

  const details = [];
  if (p.kosten_usd != null) details.push([tx('KI-Kosten gesamt'), `${p.kosten_usd.toFixed(3)} $`]);
  if (offen && p.schliesst_nach_tagen > 0) details.push([tx('Schließt automatisch'), T`${zeit(letzte.zeit + p.schliesst_nach_tagen * 86400000)}, wenn keine neue Runde kommt`]);
  if (p.joplin?.aktiv) details.push([tx('Joplin'), p.joplin.fehler ? T`Übertragung fehlgeschlagen: ${p.joplin.fehler}` : p.joplin.offen ? tx('noch nicht übertragen') : T`übertragen ${zeit(p.joplin.uebertragen)}`]);
  if (details.length) html += T`<details class="einklapp technik"><summary>Technische Details</summary><dl>${details.map(([k, v]) => T`<dt>${esc(k)}</dt><dd>${esc(v)}</dd>`).join('')}</dl></details>`;
  return html;
}

async function zeigePlanung(id) {
  let p = await lies('eintraege', id);
  if (p && p.typ !== 'planung') p = null;
  let offline = false;
  const male = () => {
    if (aktuell?.name !== 'planung' || aktuell.arg !== id) return;
    // Laufende Wiedergabe und aufgeklappte Versionen nicht durch Neuzeichnen stören.
    if ([...seite.querySelectorAll('audio')].some((a) => !a.paused)) return;
    const auf = [...seite.querySelectorAll('details')].map((d) => d.open);
    seite.innerHTML = planungHtml(p, offline);
    seite.querySelectorAll('details').forEach((d, i) => { d.open = !!auf[i]; });
  };
  if (p) { offline = !navigator.onLine || !netzDa; male(); } else seite.innerHTML = T('<p class="leer">Lädt …</p>');
  const laden = async () => {
    try {
      p = await api(T`/api/planungen/${id}`);
      offline = false;
      await lege('eintraege', p);
      male();
      if (p.gelesen === false && aktuell?.name === 'planung' && aktuell.arg === id) merkeGelesen('planung', id);
    } catch (f) {
      if (f instanceof NetzFehler) {
        offline = true;
        if (p) male(); else seite.innerHTML = T('<p class="leer">Offline – diese Planung ist nicht zwischengespeichert.</p>');
      } else if (f instanceof ApiFehler && f.status === 404) {
        await loesche('eintraege', id);
        await loesche('verlauf', id);
        seite.innerHTML = T('<p class="leer">Planung nicht gefunden.</p><p class="mitte"><a class="btn" href="/">Zum Verlauf</a></p>');
      } else throw f;
    }
  };
  aktuell.neuLaden = laden;
  seite.onclick = async (ev) => {
    const b = ev.target.closest('button[data-akt]');
    if (!b || !p) return;
    try {
      if (b.dataset.akt === 'tag-ja' || b.dataset.akt === 'tag-nein') { b.disabled = true; await tagAktion(b, 'planung', p.id); await laden(); }
      else if (b.dataset.akt === 'teilen') await navigator.share({ title: p.titel, text: p.text });
      else if (b.dataset.akt === 'kopieren') { await navigator.clipboard.writeText(p.text); melde(tx('Text kopiert')); }
      else if (b.dataset.akt === 'loeschen') {
        const r = p.rundenliste.find((x) => x.id === b.dataset.id);
        const einzige = p.rundenliste.length === 1;
        if (!confirm(T`Runde ${r ? r.nr : ''} in den Papierkorb legen?${r && r.status !== 'fertig' && r.status !== 'fehler' ? tx(' Ihre Verarbeitung wird abgebrochen.') : ''}${einzige ? tx(' Es ist die einzige Runde – die Planung verschwindet mit.') : ''}`)) return;
        b.disabled = true;
        await api(T`/api/eintraege/${b.dataset.id}`, { method: 'DELETE' });
        melde(tx('Runde gelöscht'));
        if (einzige) {
          await loesche('eintraege', p.id);
          await loesche('verlauf', p.id);
          verlaufStand = null;
          geheZu('/', true);
        } else await laden();
      } else if (b.dataset.akt === 'planung-loeschen') {
        if (!confirm(T`Planung mit ${p.rundenliste.length} ${p.rundenliste.length === 1 ? tx('Runde') : tx('Runden')} in den Papierkorb legen? Laufende Verarbeitung wird abgebrochen, ihr Listenpunkt verschwindet ebenfalls. Unter Status → Papierkorb wiederherstellbar.`)) return;
        b.disabled = true;
        await api('/api/verlauf/loeschen', { method: 'POST', body: { eintraege: [], planungen: [p.id] } });
        await loesche('eintraege', p.id);
        await loesche('verlauf', p.id);
        verlaufStand = null;
        melde(tx('Planung gelöscht'));
        geheZu('/', true);
      } else if (b.dataset.akt === 'neustart') {
        b.disabled = true;
        melde((await merkeVor({ typ: 'neustart', eintrag: b.dataset.id })) ? tx('Neu gestartet') : tx('Vorgemerkt – startet bei Verbindung'));
        await laden();
      } else if (b.dataset.akt === 'schliessen') {
        if (!confirm(tx('Planung schließen? Sie wird nicht mehr fortgesetzt.'))) return;
        b.disabled = true;
        melde((await merkeVor({ typ: 'schliessen', planung: p.id })) ? tx('Planung geschlossen') : tx('Vorgemerkt – schließt bei Verbindung'));
        await laden();
      } else if (b.dataset.akt === 'oeffnen') {
        b.disabled = true;
        await api(T`/api/planungen/${p.id}/oeffnen`, { method: 'POST' });
        melde(tx('Planung wieder offen'));
        await laden();
      }
    } catch (f) {
      if (f.name !== 'AbortError') melde(f.message);
    }
  };
  aufraeumen = () => { seite.onclick = null; };
  await laden();
}

// ---------- Umhängen ----------

async function zeigeUmhaengen(id) {
  const [e, offen] = await Promise.all([api(T`/api/eintraege/${id}`), api('/api/planungen/offen')]);
  const ziele = offen.planungen.filter((p) => p.id !== e.planung);
  seite.innerHTML = T`<h1>Runde umhängen</h1>
    <p class="leise">Die Aufnahme „${esc(e.vorschau)}“ wird aus ihrer Planung gelöst und am neuen Ziel neu recherchiert.</p>
    <div class="leiste"><button class="btn haupt" data-ziel="neu">Als neue Planung</button></div>
    ${ziele.length ? T('<h2>Zu einer offenen Planung</h2>') : T('<p class="leise">Keine andere offene Planung vorhanden.</p>')}
    ${ziele.map((p) => T`<div class="karte"><div class="titel">${nrHtml(p.nr)}${esc(p.titel)}</div><div class="vorschau">${esc(p.vorhaben)}</div>
      <div class="leiste"><button class="btn klein" data-ziel="${p.id}">Hierher umhängen</button></div></div>`).join('')}
    <div class="leiste"><a class="btn" href="/eintrag/${id}">Abbrechen</a></div>`;
  seite.onclick = async (ev) => {
    const b = ev.target.closest('button[data-ziel]');
    if (!b) return;
    b.disabled = true;
    try {
      await api(T`/api/eintraege/${id}/umhaengen`, { method: 'POST', body: { ziel: b.dataset.ziel } });
      melde(tx('Umgehängt – wird neu recherchiert'));
      geheZu(T`/eintrag/${id}`, true);
    } catch (f) { b.disabled = false; melde(f.message); }
  };
  aufraeumen = () => { seite.onclick = null; };
}

// ---------- Wortlaut korrigieren (eigene Bearbeiten-Seite) ----------

async function zeigeWortlaut(id) {
  const e = await api(T`/api/eintraege/${id}`);
  const zurueck = e.planung ? T`/planung/${e.planung}` : T`/eintrag/${id}`;
  seite.innerHTML = T`<h1>Wortlaut korrigieren</h1>
    <p class="leise">Falsch erkannte Wörter (z. B. Namen) berichtigen. Danach wertet die KI ${e.planung ? tx('diese Runde der Planung') : tx('die Notiz')} neu aus; was davon allgemein gilt (z. B. die Schreibweise eines Namens), schlägt die KI fürs Gedächtnis vor.</p>
    <label for="wortlaut">Wortlaut</label>
    <textarea id="wortlaut" rows="8">${esc(e.transkript || '')}</textarea>
    <div class="leiste"><button class="btn haupt" data-akt="speichern">Speichern und neu auswerten</button><a class="btn" href="${zurueck}">Abbrechen</a></div>`;
  const feld = document.getElementById('wortlaut');
  feld.style.height = T`${Math.max(feld.scrollHeight + 4, 160)}px`;
  seite.onclick = async (ev) => {
    const b = ev.target.closest('button[data-akt="speichern"]');
    if (!b) return;
    const text = feld.value.trim();
    if (!text) return melde(tx('Der Wortlaut darf nicht leer sein'));
    b.disabled = true;
    try {
      const d = await api(T`/api/eintraege/${id}/wortlaut`, { method: 'PUT', body: { text } });
      melde(!d.geaendert ? tx('Nichts geändert') : T`Wird neu ausgewertet${d.gelernt ? tx(' · die KI prüft, ob die Berichtigung ins Gedächtnis gehört') : ''}`);
      ladeZaehler();
      geheZu(zurueck, true);
    } catch (f) {
      b.disabled = false;
      melde(f instanceof NetzFehler ? tx('Offline – nur mit Verbindung') : f.message);
    }
  };
  aufraeumen = () => { seite.onclick = null; };
}

// ---------- Antwortseite ----------

async function zeigeAntwort(planung) {
  let aufnahme = null, rekorder = null, strom = null;
  const entwurfKey = T`entwurf:${planung}`;
  seite.innerHTML = T`<h1>Antworten</h1>
    <p class="leise">Die Antwort wird als neue Runde in die Planung eingearbeitet. Tippen oder aufnehmen.</p>
    <label for="text">Antwort als Text</label>
    <textarea id="text" placeholder="Antworten auf die offenen Fragen, Ergänzungen, Änderungen …"></textarea>
    <label>Oder als Aufnahme</label>
    <div class="aufnahme" id="aufnahme"></div>
    <div class="leiste"><button class="btn haupt" id="senden">Senden</button><button class="btn" id="zurueck">Abbrechen</button></div>`;
  const text = document.getElementById('text');
  const box = document.getElementById('aufnahme');
  text.value = localStorage.getItem(entwurfKey) || '';
  text.oninput = () => localStorage.setItem(entwurfKey, text.value);
  document.getElementById('zurueck').onclick = () => history.back();

  const stoppeStrom = () => { strom?.getTracks().forEach((t) => t.stop()); strom = null; };
  const male = () => {
    if (rekorder?.state === 'recording') box.innerHTML = T('<span class="punkt"></span><span>Aufnahme läuft …</span><button class="btn klein" data-a="stopp">Stopp</button>');
    else if (aufnahme) {
      box.innerHTML = T('<audio controls></audio><button class="btn klein gefahr" data-a="weg">Aufnahme verwerfen</button>');
      box.querySelector('audio').src = URL.createObjectURL(aufnahme);
    } else box.innerHTML = navigator.mediaDevices?.getUserMedia ? T('<button class="btn" data-a="start">Aufnehmen</button>') : T('<span class="leise">Aufnahme wird von diesem Browser nicht unterstützt.</span>');
  };
  box.onclick = async (ev) => {
    const a = ev.target.closest('button')?.dataset.a;
    if (a === 'start') {
      try {
        strom = await navigator.mediaDevices.getUserMedia({ audio: true });
        const teile = [];
        rekorder = new MediaRecorder(strom);
        rekorder.ondataavailable = (d) => d.data.size && teile.push(d.data);
        rekorder.onstop = () => { aufnahme = new Blob(teile, { type: rekorder.mimeType || 'audio/webm' }); stoppeStrom(); male(); };
        rekorder.start();
      } catch (f) { melde(tx('Mikrofon nicht verfügbar')); }
    } else if (a === 'stopp') rekorder.stop();
    else if (a === 'weg') aufnahme = null;
    male();
  };
  male();

  document.getElementById('senden').onclick = async (ev) => {
    if (rekorder?.state === 'recording') return melde(tx('Erst die Aufnahme stoppen'));
    const inhalt = text.value.trim();
    if (!aufnahme && !inhalt) return melde(tx('Antwort ist leer'));
    if (aufnahme && inhalt && !confirm(tx('Es gibt Text und Aufnahme. Gesendet wird die Aufnahme, der Text entfällt. Fortfahren?'))) return;
    ev.target.disabled = true;
    const gesendet = await merkeVor(aufnahme ? { typ: 'antwort', planung, audio: aufnahme } : { typ: 'antwort', planung, text: inhalt });
    localStorage.removeItem(entwurfKey);
    melde(gesendet ? tx('Antwort gesendet') : tx('Antwort vorgemerkt – wird bei Verbindung gesendet'));
    geheZu(T`/planung/${planung}`, true);
  };
  aufraeumen = () => { if (rekorder?.state === 'recording') rekorder.stop(); stoppeStrom(); };
}

// ---------- Fragen der KI (Drawer): eine Frage nach der anderen, drei Vorschläge oder eigene Antwort ----------

// Fragen der KI im Drawer beantworten: an einer Planung (neue Runde) oder an einer Notiz (neue Auswertung).
async function oeffneFragen(planung, typ = 'planung') {
  if (document.querySelector('dialog.drawer')) return;
  const notiz = typ === 'eintrag';
  let p;
  try { p = await api(T`/api/${notiz ? 'eintraege' : 'planungen'}/${planung}`); await lege('eintraege', p); } catch (f) {
    if (f instanceof NetzFehler) p = await lies('eintraege', planung); else return melde(f.message);
  }
  const fragen = ((notiz ? p?.analyse?.rueckfragen : p?.plan?.offene_fragen) || []).map((f) => (typeof f === 'string' ? { frage: f, antworten: [] } : f));
  if (!p || (!notiz && p.planung_status !== 'offen') || !p.fragen || !fragen.length) return melde(tx('Keine offenen Fragen'));
  const antworten = fragen.map(() => null); // null = übersprungen
  let i = 0, eigene = false;
  const d = document.createElement('dialog');
  d.className = 'drawer';
  const male = () => {
    const kopf = T`<div class="griff" aria-hidden="true"></div><div class="zeile"><span class="leise">${nrHtml(p.nr)}${esc(p.titel || (notiz ? tx('Notiz') : tx('Planung')))}</span><button class="btn klein rund" data-d="zu" aria-label="Schließen">×</button></div>`;
    if (i >= fragen.length) {
      const n = antworten.filter((a) => a).length;
      d.innerHTML = T`${kopf}<h2>Antworten senden</h2>
        <div class="dliste">${fragen.map((f, k) => T`<button class="dzeile" data-d="gehe" data-k="${k}"><span class="leise">${esc(f.frage)}</span><span>${antworten[k] ? esc(antworten[k]) : T('<em class="leise">übersprungen</em>')}</span></button>`).join('')}</div>
        <p class="leise">${n ? (notiz ? tx('Die Notiz wird mit den Antworten neu ausgewertet; die KI merkt sie sich.') : tx('Die Planung wird mit den Antworten neu recherchiert.')) : tx('Noch keine Frage beantwortet.')}</p>
        <div class="dfuss"><button class="btn klein rund" data-d="zurueck">‹ Zurück</button><span></span><button class="btn haupt pille" data-d="senden"${n ? '' : ' disabled'}>Senden</button></div>`;
      return;
    }
    const f = fragen[i];
    const frei = antworten[i] && !f.antworten.includes(antworten[i]) ? antworten[i] : '';
    const feld = eigene || frei || !f.antworten.length;
    d.innerHTML = T`${kopf}<h2>${esc(f.frage)}</h2>
      <div class="dliste">${f.antworten.map((a, k) => T`<button class="dzeile wahl${antworten[i] === a ? ' an' : ''}" data-d="wahl" data-k="${k}"><span class="kreis" aria-hidden="true"></span><span>${esc(a)}</span></button>`).join('')}
        ${feld ? T`<div class="deigene"><input type="text" id="deigene" maxlength="300" placeholder="Eigene Antwort …" value="${esc(frei)}"><button class="btn klein haupt" data-d="eigene-ok">OK</button></div>` : T('<button class="dzeile wahl" data-d="eigene"><span class="kreis" aria-hidden="true"></span><span class="leise">Eigene Antwort …</span></button>')}</div>
      <div class="dfuss"><span class="pager"><button class="btn klein rund" data-d="zurueck"${i ? '' : ' disabled'} aria-label="Vorige Frage">‹</button><span>Frage ${i + 1} von ${fragen.length}</span></span>
        <button class="btn pille" data-d="weiter">${antworten[i] ? tx('Weiter') : tx('Überspringen')}</button></div>`;
    if (eigene) d.querySelector('#deigene')?.focus();
  };
  const weiter = () => { i += 1; eigene = false; male(); };
  const nimmEigene = () => {
    const wert = d.querySelector('#deigene')?.value.trim();
    if (!wert) return melde(tx('Antwort ist leer'));
    antworten[i] = wert;
    weiter();
  };
  d.onclick = async (ev) => {
    if (ev.target === d) return d.close();
    const b = ev.target.closest('[data-d]');
    if (!b) return;
    switch (b.dataset.d) {
      case 'zu': return d.close();
      case 'wahl': antworten[i] = fragen[i].antworten[Number(b.dataset.k)]; return weiter();
      case 'eigene': eigene = true; return male();
      case 'eigene-ok': return nimmEigene();
      case 'weiter': {
        const wert = d.querySelector('#deigene')?.value.trim();
        if (wert) antworten[i] = wert;
        return weiter();
      }
      case 'zurueck': i = Math.max(0, i - 1); eigene = false; return male();
      case 'gehe': i = Number(b.dataset.k); eigene = false; return male();
      case 'senden': {
        b.disabled = true;
        const text = fragen.map((f, k) => (antworten[k] ? T`Frage: ${f.frage}\nAntwort: ${antworten[k]}` : '')).filter(Boolean).join('\n\n');
        const gesendet = notiz
          ? await merkeVor({ typ: 'notizantwort', eintrag: planung, antworten: fragen.map((f, k) => ({ frage: f.frage, antwort: antworten[k] })).filter((x) => x.antwort) })
          : await merkeVor({ typ: 'antwort', planung, text });
        d.close();
        melde(gesendet ? (notiz ? tx('Antworten gesendet – die Notiz wird aktualisiert') : tx('Antworten gesendet – die Planung wird aktualisiert')) : tx('Vorgemerkt – wird bei Verbindung gesendet'));
        aktuell?.neuLaden?.();
      }
    }
  };
  d.onkeydown = (ev) => { if (ev.key === 'Enter' && ev.target.id === 'deigene') { ev.preventDefault(); nimmEigene(); } };
  // Nach unten wischen schließt.
  let startY = null;
  d.ontouchstart = (ev) => { startY = ev.target.closest('.dliste') ? null : ev.touches[0].clientY; };
  d.ontouchend = (ev) => { if (startY !== null && ev.changedTouches[0].clientY - startY > 90) d.close(); startY = null; };
  d.onclose = () => d.remove();
  document.body.append(d);
  male();
  d.showModal();
}

// ---------- Listen: Punkte aus Notizen zum Abhaken ----------

// Alle Listen, oder als Unterseite /listen/gruppe/<name> nur eine Gruppe ('-' = ohne Gruppe).
async function zeigeListen(filter) {
  seite.innerHTML = T('<p class="leer">Lädt …</p>');
  let d = null, vergangeneZeigen = false;
  // Ausgeklappt: unter jeder aktiven Liste ihre offenen (und kürzlich abgehakten) Punkte; je Gerät gemerkt.
  let ausgeklappt = false;
  try { ausgeklappt = localStorage.getItem('listen-ausgeklappt') === '1'; } catch { /* Standard: eingeklappt */ }
  const punktHtml = (p) => T`<div class="lp${p.erledigt ? ' erledigt' : ''}">
    <button class="haken" data-haken="${p.id}" aria-pressed="${!!p.erledigt}" aria-label="${p.erledigt ? tx('Wieder öffnen') : tx('Abhaken')}: ${esc(p.text)}"><span class="hakenbild" aria-hidden="true"></span></button>
    <div class="lptext"><div class="lpzeile">${p.eintrag || p.planung ? T`<a class="lptitel" href="${p.eintrag ? T`/eintrag/${p.eintrag}` : T`/planung/${p.planung}`}">${esc(p.text)}</a>${punktEnde(p)}` : `${esc(p.text)}${punktEnde(p)}`}</div></div></div>`;
  const male = () => {
    if (aktuell?.name !== 'listen' || (aktuell.arg ? decodeURIComponent(aktuell.arg) : '') !== filter) return;
    const zeile = (l) => T`<a class="karte listenkarte" href="/liste/${l.id}"><span class="titel">${listenNr(l)}${esc(l.name)}</span>
        <span class="badge ${l.offen ? 'in_arbeit' : 'fertig'}" title="${tx('{0} von {1} offen', l.offen, l.gesamt)}">${l.offen}/${l.gesamt}</span></a>`;
    const mitPunkten = (l) => {
      const sichtbar = ausgeklappt ? (l.punkte || []).filter((p) => !p.erledigt || p.erledigt > d.vergangen_vor) : [];
      return zeile(l) + (sichtbar.length ? T`<div class="lpgruppe">${sichtbar.map(punktHtml).join('')}</div>` : '');
    };
    // Je Gruppe ihre Listen; Listen ohne Gruppe unter „Ohne Gruppe“. Gruppen und Listen nach dem jüngsten Punkt, neueste oben.
    // Der Gruppenname öffnet die Gruppe als Unterseite; dort steht kein Gruppenkopf.
    const gruppiert = (listen, fn) => {
      if (filter) return listen.map(fn).join('');
      const gruppen = [];
      for (const l of [...listen].sort((a, b) => (b.neuester || 0) - (a.neuester || 0))) {
        const g = gruppen.find(([projekt]) => projekt === (l.projekt || ''));
        if (g) g[1].push(l); else gruppen.push([l.projekt || '', [l]]);
      }
      return gruppen.map(([projekt, ls]) => {
        const wert = projekt || '-';
        return T`<div class="tag gruppenkopf">${projekt ? T`<a class="titelknopf" href="/gruppe/${encodeURIComponent(projekt)}/bearbeiten" aria-label="${esc(tx('Gruppe {0} bearbeiten', projekt))}" title="Gruppe bearbeiten">${STIFT}</a>` : T('<span class="titelknopf platz" aria-hidden="true"></span>')}<a class="gruppenname" href="${gruppenPfad(wert)}" title="Gruppe öffnen">${esc(gruppenTitel(projekt))}</a></div>${ls.map(fn).join('')}`;
      }).join('');
    };
    // Aktiv: nur Listen mit offenen Punkten; ganz erledigte und leere stehen eingeklappt unter „Vergangen“ (Nutzervorgabe 20.11.2026).
    const istAktiv = (l) => l.offen > 0;
    // Filter nach Gruppe (je Gerät gemerkt): '' = alle, '-' = ohne Gruppe, sonst der Gruppenname.
    const namen = [...new Set(d.listen.map((l) => l.projekt).filter(Boolean))];
    const gruppe = filter && filter !== '-' ? namen.find((n) => n.toLowerCase() === filter.toLowerCase()) : '';
    const passt = (l) => !filter || (filter === '-' ? !l.projekt : (l.projekt || '').toLowerCase() === filter.toLowerCase());
    const sichtbar = d.listen.filter(passt);
    const aktiv = sichtbar.filter(istAktiv), vergangen = sichtbar.filter((l) => !istAktiv(l));
    // Titelzeile: links Stift (Gruppe bearbeiten), rechts großes Symbol zum Aus-/Einklappen der offenen Punkte.
    const klapp = aktiv.length ? T`<button class="titelknopf gross" data-ausklappen="1" aria-pressed="${ausgeklappt}" aria-label="${ausgeklappt ? tx('Offene Punkte einklappen') : tx('Alle offenen Punkte ausklappen')}" title="${ausgeklappt ? tx('Offene Punkte einklappen') : tx('Alle offenen Punkte ausklappen')}">${ausgeklappt ? EINKLAPPEN : AUSKLAPPEN}</button>` : '';
    const titel = gruppenTitel(gruppe || filter);
    const kopf = filter
      ? T`${krumen([[tx('Listen'), '/listen'], [titel]])}<div class="titelzeile">${gruppe ? T`<a class="titelknopf" href="/gruppe/${encodeURIComponent(gruppe)}/bearbeiten" aria-label="${esc(tx('Gruppe {0} bearbeiten', gruppe))}" title="Gruppe bearbeiten">${STIFT}</a>` : ''}<h1>${esc(titel)}</h1>${klapp}</div>`
      : T`<div class="titelzeile"><h1>Listen</h1>${klapp}</div><p class="leise">Die KI sortiert Notizen selbst als Punkte in Listen, Listen eines größeren Themas in Gruppen. Von Hand über „In Liste“ an der Notiz.</p>`;
    seite.innerHTML = `${kopf}
      ${filter && !sichtbar.length ? T('<p class="leer">Keine Einträge vorhanden.</p>') : !d.listen.length ? T('<p class="leer">Noch keine Liste. Einfach einen Filmtitel oder eine Idee diktieren.</p>') : aktiv.length ? gruppiert(aktiv, mitPunkten) : T('<p class="leer">Alles erledigt.</p>')}
      ${vergangen.length ? T`<div class="leiste"><button class="btn klein" data-vergangen="1">${vergangeneZeigen ? tx('Vergangene ausblenden') : T`Vergangene Listen anzeigen (${vergangen.length})`}</button></div>
        ${vergangeneZeigen ? T`<h2>Vergangen</h2>${gruppiert(vergangen, zeile)}` : ''}` : ''}`;
  };
  const laden = async (vorab) => {
    try { d = await apiAbgelegt('/api/listen?punkte=1', vorab); } catch (f) {
      if (vorab) return;
      if (f instanceof NetzFehler) { if (!d && aktuell?.name === 'listen') seite.innerHTML = T('<p class="leer">Offline – die Listen sind noch nicht gespeichert.</p>'); return; }
      throw f;
    }
    // Gruppen fürs Menü (neueste zuerst; '-' = Listen ohne Gruppe).
    // [Name, offene Punkte, davon neu seit zuletzt gesehen]
    const gruppen = [];
    const grenze = besuche.grenze();
    for (const l of [...d.listen].sort((a, b) => (b.neuester || 0) - (a.neuester || 0))) {
      const neu = (l.punkte || []).filter((p) => !p.erledigt && !p.gelesen && p.angelegt >= grenze).length;
      const g = gruppen.find(([n]) => n === (l.projekt || '-'));
      if (g) { g[1] += l.offen; g[2] += neu; } else gruppen.push([l.projekt || '-', l.offen, neu]);
    }
    try { localStorage.setItem('listen-gruppen', JSON.stringify(gruppen)); } catch { /* nur Komfort */ }
    maleMenue();
    male();
  };
  aktuell.neuLaden = laden;
  seite.onclick = async (ev) => {
    const b = ev.target.closest('button');
    if (!b || !d) return;
    if (b.dataset.vergangen) { vergangeneZeigen = !vergangeneZeigen; male(); return; }
    if (b.dataset.ausklappen) {
      ausgeklappt = !ausgeklappt;
      try { localStorage.setItem('listen-ausgeklappt', ausgeklappt ? '1' : '0'); } catch { /* nur Komfort */ }
      male();
      return;
    }
    if (b.dataset.haken) {
      // Sofort umschalten; gesendet wird im Hintergrund, ohne Verbindung vorgemerkt.
      const l = d.listen.find((x) => x.punkte?.some((p) => p.id === b.dataset.haken));
      const p = l.punkte.find((x) => x.id === b.dataset.haken);
      p.erledigt = p.erledigt ? 0 : Date.now();
      l.offen += p.erledigt ? -1 : 1;
      if (p.erledigt) l.erledigt_am = Math.max(l.erledigt_am || 0, p.erledigt);
      male();
      try {
        await merkeHakenAbgelegt(l.id, p);
        await merkeVor({ typ: 'haken', punkt: p.id });
      } catch (f) { melde(f.message); laden(); }
    }
  };
  aufraeumen = () => { seite.onclick = null; };
  await laden(true).catch(() => {});
  await laden();
}

// Haken in den gespeicherten Antworten nachführen: Liste und Übersicht stimmen auch offline.
async function merkeHakenAbgelegt(listeId, punkt) {
  await aendereAbgelegt(T`/api/listen/${listeId}`, (l) => ({ ...l, punkte: l.punkte.map((x) => (x.id === punkt.id ? { ...x, erledigt: punkt.erledigt } : x)) }));
  await aendereAbgelegt('/api/listen?punkte=1', (d) => ({ ...d, listen: d.listen.map((x) => (x.id === listeId
    ? { ...x, offen: x.offen + (punkt.erledigt ? -1 : 1), erledigt_am: punkt.erledigt ? Math.max(x.erledigt_am || 0, punkt.erledigt) : x.erledigt_am,
      punkte: x.punkte?.map((y) => (y.id === punkt.id ? { ...y, erledigt: punkt.erledigt } : y)) }
    : x)) }));
}

async function zeigeListe(id) {
  seite.innerHTML = T('<p class="leer">Lädt …</p>');
  let l = null, vergangeneZeigen = false;
  const punkt = (p) => T`<div class="lp${p.erledigt ? ' erledigt' : ''}">
    <button class="haken" data-haken="${p.id}" aria-pressed="${!!p.erledigt}" aria-label="${p.erledigt ? tx('Wieder öffnen') : tx('Abhaken')}: ${esc(p.text)}"><span class="hakenbild" aria-hidden="true"></span></button>
    <div class="lptext"><div class="lpzeile">${p.eintrag || p.planung ? T`<a class="lptitel" href="${p.eintrag ? T`/eintrag/${p.eintrag}` : T`/planung/${p.planung}`}">${esc(p.text)}</a>${punktEnde(p)}` : `${esc(p.text)}${punktEnde(p)}`}</div>
      <details class="lpmenue"><summary aria-label="${esc(tx('Mehr zu {0}', p.text))}">Mehr</summary><div class="leiste"><a class="btn klein" href="/liste/${id}/punkt/${p.id}/verschieben">Verschieben</a><button class="btn klein gefahr" data-weg="${p.id}">Löschen</button></div></details></div></div>`;
  const male = () => {
    if (aktuell?.name !== 'liste' || aktuell.arg !== id) return;
    const offen = l.punkte.filter((p) => !p.erledigt), fertig = l.punkte.filter((p) => p.erledigt);
    // Frisch Erledigtes bleibt eine Weile bei den aktiven Punkten, älteres steht eingeklappt unter „Vergangen“.
    const kuerzlich = fertig.filter((p) => p.erledigt > l.vergangen_vor), vergangen = fertig.filter((p) => p.erledigt <= l.vergangen_vor);
    const aktiv = [...offen, ...kuerzlich];
    const auf = new Set([...seite.querySelectorAll('details[open] [data-weg]')].map((b) => b.dataset.weg));
    seite.innerHTML = T`${krumen([[tx('Listen'), '/listen'], [gruppenTitel(l.projekt), gruppenPfad(l.projekt)], [`${l.nr ? `L${l.nr} ` : ''}${l.name}`]])}
      <h1>${listenNr(l)}${esc(l.name)}</h1><div class="leise">${l.projekt ? T`Gruppe ${esc(l.projekt)} · ` : ''}${offen.length} offen · ${fertig.length} erledigt</div>
      ${l.beschreibung ? T`<p class="leise">${esc(l.beschreibung)}</p>` : ''}
      ${aktiv.length ? aktiv.map(punkt).join('') : T`<p class="leer">${fertig.length ? tx('Alles erledigt.') : tx('Die Liste ist leer.')}</p>`}
      ${vergangen.length ? T`<div class="leiste"><button class="btn klein" data-vergangen="1">${vergangeneZeigen ? tx('Vergangene ausblenden') : T`Vergangene anzeigen (${vergangen.length})`}</button></div>
        ${vergangeneZeigen ? T`<h2>Vergangen</h2>${vergangen.map(punkt).join('')}` : ''}` : ''}
      <div class="leiste">${l.joplin_id ? T`<a class="btn" href="joplin://x-callback-url/openNote?id=${esc(l.joplin_id)}" target="_self">In Joplin öffnen</a>` : ''}<a class="btn" href="/liste/${id}/bearbeiten">Bearbeiten</a><button class="btn gefahr" data-liste-weg="1">Liste löschen</button></div>`;
    seite.querySelectorAll('details.lpmenue').forEach((dt) => { if (auf.has(dt.querySelector('[data-weg]')?.dataset.weg)) dt.open = true; });
  };
  const laden = async (vorab) => {
    const hier = () => aktuell?.name === 'liste' && aktuell.arg === id;
    try { l = await apiAbgelegt(T`/api/listen/${id}`, vorab); male(); } catch (f) {
      if (vorab) return;
      if (f instanceof NetzFehler) { if (!l && hier()) seite.innerHTML = T('<p class="leer">Offline – diese Liste ist noch nicht gespeichert.</p>'); }
      else if (f instanceof ApiFehler && f.status === 404) { if (hier()) seite.innerHTML = T('<p class="leer">Liste nicht gefunden.</p><p class="mitte"><a class="btn" href="/listen">Alle Listen</a></p>'); }
      else throw f;
    }
  };
  aktuell.neuLaden = laden;
  seite.onclick = async (ev) => {
    const b = ev.target.closest('button');
    if (!b || !l) return;
    try {
      if (b.dataset.vergangen) {
        vergangeneZeigen = !vergangeneZeigen;
        male();
      } else if (b.dataset.haken) {
        // Sofort umschalten; gesendet wird im Hintergrund, ohne Verbindung vorgemerkt.
        const p = l.punkte.find((x) => x.id === b.dataset.haken);
        p.erledigt = p.erledigt ? 0 : Date.now();
        male();
        await merkeHakenAbgelegt(id, p);
        await merkeVor({ typ: 'haken', punkt: p.id });
      } else if (b.dataset.weg) {
        if (!confirm(tx('Punkt aus der Liste löschen?'))) return;
        await api(T`/api/listenpunkte/${b.dataset.weg}`, { method: 'DELETE' });
        await laden();
      } else if (b.dataset.listeWeg) {
        if (!confirm(T`Liste „${l.name}“ mit ${l.punkte.length} Punkten löschen? Die Notizen bleiben im Verlauf.`)) return;
        await api(T`/api/listen/${id}`, { method: 'DELETE' });
        melde(tx('Liste gelöscht'));
        geheZu('/listen', true);
      }
    } catch (f) { melde(f.message); laden(); }
  };
  aufraeumen = () => { seite.onclick = null; };
  await laden(true).catch(() => {});
  await laden();
}

async function zeigeVerschieben(liste, punkt) {
  const [l, alleListen] = await Promise.all([api(T`/api/listen/${liste}`), api('/api/listen')]);
  const p = l.punkte.find((x) => x.id === punkt);
  const ziele = alleListen.listen.filter((x) => x.id !== liste);
  if (!p) { seite.innerHTML = T`<p class="leer">Punkt nicht gefunden.</p><p class="mitte"><a class="btn" href="/liste/${liste}">Zur Liste</a></p>`; return; }
  seite.innerHTML = T`<h1>Punkt verschieben</h1><p class="leise">„${esc(p.text)}“ aus „${esc(l.name)}“ in eine andere Liste.</p>
    ${ziele.length ? ziele.map((z) => T`<div class="karte listenkarte"><span class="titel">${esc(listenName(z))}</span><span class="leise">${z.gesamt} ${z.gesamt === 1 ? tx('Punkt') : tx('Punkte')}</span><button class="btn klein haupt" data-ziel="${z.id}">Hierher</button></div>`).join('') : T('<p class="leise">Es gibt keine andere Liste.</p>')}
    <div class="leiste"><a class="btn" href="/liste/${liste}">Abbrechen</a></div>`;
  seite.onclick = async (ev) => {
    const b = ev.target.closest('button[data-ziel]');
    if (!b) return;
    b.disabled = true;
    try {
      await api(T`/api/listenpunkte/${punkt}/verschieben`, { method: 'POST', body: { ziel: b.dataset.ziel } });
      melde(tx('Verschoben'));
      geheZu(T`/liste/${b.dataset.ziel}`, true);
    } catch (f) { b.disabled = false; melde(f.message); }
  };
  aufraeumen = () => { seite.onclick = null; };
}

// Eigene Seite: Liste umbenennen oder in eine andere Gruppe verschieben (auch „ohne Gruppe“).
async function zeigeListeBearbeiten(id) {
  const [l, alle] = await Promise.all([api(T`/api/listen/${id}`), api('/api/listen')]);
  const gruppen = (alle.gruppen || []).map((g) => g.name);
  const opt = (wert, text) => T`<option value="${esc(wert)}"${wert.toLowerCase() === (l.projekt || '').toLowerCase() ? ' selected' : ''}>${esc(text)}</option>`;
  seite.innerHTML = T`${listenKrumen({ ...l, id }, [tx('Bearbeiten')])}<h1>Liste bearbeiten</h1>
    <label for="lname">Name</label><input type="text" id="lname" maxlength="40" value="${esc(l.name)}">
    <label for="lgruppe">Gruppe</label>
    <select id="lgruppe">${opt('', tx('ohne Gruppe'))}${gruppen.map((g) => opt(g, g)).join('')}<option value="__neu__">Neue Gruppe …</option></select>
    <div id="lneuwrap" hidden><label for="lneu">Name der neuen Gruppe</label><input type="text" id="lneu" maxlength="40"></div>
    <div class="leiste"><button class="btn haupt" id="lsichern">Speichern</button><a class="btn" href="/liste/${id}">Abbrechen</a></div>`;
  const wahl = document.getElementById('lgruppe');
  wahl.onchange = () => { document.getElementById('lneuwrap').hidden = wahl.value !== '__neu__'; };
  document.getElementById('lsichern').onclick = async (ev) => {
    const name = document.getElementById('lname').value.trim();
    const gruppe = wahl.value === '__neu__' ? document.getElementById('lneu').value.trim() : wahl.value;
    if (!name) return melde(tx('Name fehlt'));
    if (wahl.value === '__neu__' && !gruppe) return melde(tx('Name der neuen Gruppe fehlt'));
    ev.target.disabled = true;
    try {
      await api(T`/api/listen/${id}`, { method: 'PUT', body: { name, gruppe } });
      melde(tx('Gespeichert'));
      geheZu(T`/liste/${id}`, true);
    } catch (f) { ev.target.disabled = false; melde(f.message); }
  };
}

// Eigene Seite: Gruppe umbenennen oder auflösen (ihre Listen bleiben dann ohne Gruppe).
async function zeigeGruppeBearbeiten(name) {
  const alle = await api('/api/listen');
  const g = (alle.gruppen || []).find((x) => x.name.toLowerCase() === name.toLowerCase());
  if (!g) { seite.innerHTML = T('<p class="leer">Gruppe nicht gefunden.</p><p class="mitte"><a class="btn" href="/listen">Zu den Listen</a></p>'); return; }
  const listen = alle.listen.filter((l) => (l.projekt || '').toLowerCase() === g.name.toLowerCase());
  seite.innerHTML = T`${krumen([[tx('Listen'), '/listen'], [g.name, gruppenPfad(g.name)], [tx('Bearbeiten')]])}<h1>Gruppe bearbeiten</h1>
    <p class="leise">${listen.length} ${listen.length === 1 ? tx('Liste') : tx('Listen')}: ${listen.map((l) => esc(l.name)).join(', ')}</p>
    ${g.beschreibung ? T`<p class="leise">Beschreibung der KI (wöchentlich aktualisiert, hilft beim Einsortieren): ${esc(g.beschreibung)}</p>` : ''}
    <label for="gname">Name</label><input type="text" id="gname" maxlength="40" value="${esc(g.name)}">
    <div class="leiste"><button class="btn haupt" id="gsichern">Speichern</button><a class="btn" href="/listen">Abbrechen</a></div>
    <div class="leiste"><button class="btn gefahr" id="gweg">Gruppe auflösen</button></div>
    <p class="leise">Auflösen löscht nichts: die Listen bleiben samt Punkten, nur ohne Gruppe.</p>`;
  document.getElementById('gsichern').onclick = async (ev) => {
    const neu = document.getElementById('gname').value.trim();
    if (!neu) return melde(tx('Name fehlt'));
    ev.target.disabled = true;
    try {
      await api('/api/gruppen', { method: 'PUT', body: { alt: g.name, name: neu } });
      melde(tx('Gespeichert'));
      geheZu('/listen', true);
    } catch (f) { ev.target.disabled = false; melde(f.message); }
  };
  document.getElementById('gweg').onclick = async (ev) => {
    if (!confirm(T`Gruppe „${g.name}“ auflösen? Die Listen bleiben ohne Gruppe.`)) return;
    ev.target.disabled = true;
    try {
      await api('/api/gruppen/loeschen', { method: 'POST', body: { name: g.name } });
      melde(tx('Gruppe aufgelöst'));
      geheZu('/listen', true);
    } catch (f) { ev.target.disabled = false; melde(f.message); }
  };
}

// Eine Notiz oder Planung von Hand in eine Liste einsortieren: sie steht dort als eigener Punkt zum Abhaken.
async function zeigeEinsortieren(id, art) {
  const pfad = art === 'planung' ? 'planungen' : 'eintraege';
  const zurueck = `/${art}/${id}`;
  const [e, alleListen] = await Promise.all([api(T`/api/${pfad}/${id}`), api('/api/listen')]);
  const ziele = alleListen.listen.filter((l) => l.id !== e.liste?.id);
  const was = art === 'planung' ? tx('Die Planung') : tx('Die Notiz');
  seite.innerHTML = T`<h1>${e.liste ? tx('Liste wechseln') : tx('In Liste einsortieren')}</h1>
    <p class="leise">„${esc(e.titel)}“ ${e.liste ? T`steht in „${esc(listenName(e.liste))}“ und zieht in die gewählte Liste um.` : T`wird ein Punkt zum Abhaken. ${was} bleibt im Verlauf.`}</p>
    ${ziele.map((z) => T`<div class="karte listenkarte"><span class="titel">${esc(listenName(z))}</span><span class="leise">${z.gesamt} ${z.gesamt === 1 ? tx('Punkt') : tx('Punkte')}</span><button class="btn klein haupt" data-ziel="${z.id}">Hierher</button></div>`).join('')}
    <label for="neueliste">Neue Liste</label>
    <div class="zeile"><input type="text" id="neueliste" maxlength="40" placeholder="z. B. Filme"><button class="btn" data-neu="1">Anlegen</button></div>
    <div class="leiste">${e.liste ? T`<a class="btn" href="/liste/${e.liste.id}">Zur Liste „${esc(listenName(e.liste))}“</a>` : ''}<a class="btn" href="${zurueck}">Abbrechen</a></div>`;
  seite.onclick = async (ev) => {
    const b = ev.target.closest('button[data-ziel], button[data-neu]');
    if (!b) return;
    const name = seite.querySelector('#neueliste').value.trim();
    if (b.dataset.neu && !name) { melde(tx('Name der Liste fehlt')); return; }
    b.disabled = true;
    try {
      const r = await api(T`/api/${pfad}/${id}/liste`, { method: 'POST', body: b.dataset.neu ? { neu: name } : { ziel: b.dataset.ziel } });
      melde(T`In „${r.liste.name}“ einsortiert`);
      geheZu(zurueck, true);
    } catch (f) { b.disabled = false; melde(f.message); }
  };
  aufraeumen = () => { seite.onclick = null; };
}

// ---------- Status ----------

async function pushAbo() {
  if (!('serviceWorker' in navigator) || !('PushManager' in window)) return null;
  const reg = await navigator.serviceWorker.ready;
  return reg.pushManager.getSubscription();
}

function b64zuBytes(s) {
  const b = atob(s.replace(/-/g, '+').replace(/_/g, '/').padEnd(Math.ceil(s.length / 4) * 4, '='));
  return Uint8Array.from(b, (c) => c.charCodeAt(0));
}
function bytesZuB64(buf) {
  return btoa(String.fromCharCode(...new Uint8Array(buf))).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '');
}

async function zeigeStatus() {
  seite.innerHTML = T`${statusReiter('status')}<p class="leer">Lädt …</p>`;
  const laden = async (vorab) => {
    let s, g, o;
    try {
      [s, g, o] = await Promise.all([apiAbgelegt('/api/status', vorab), apiAbgelegt('/api/geraete', vorab), apiAbgelegt('/api/oauth/apps', vorab).catch(() => ({ apps: [] })), vorab || ladeZaehler()]);
    } catch (f) {
      if (vorab) return;
      if (f instanceof NetzFehler) { if (aktuell?.name === 'status') seite.innerHTML = T('<p class="leer">Offline – der Status ist noch nicht gespeichert.</p>'); return; }
      throw f;
    }
    if (aktuell?.name !== 'status') return;
    const abo = await pushAbo().catch(() => null);
    const pushDa = 'PushManager' in window;
    // Eingänge: zugeklappt Zahl, Aufnahmen und nur Auffälliges (in Arbeit, Fehler); aufgeklappt Kreis nach Art.
    const eingaenge = (s) => {
      const t = s.auftraege_teile || {}, d = s.audio_dateien || {};
      const kachel = (n, name, art) => T`<div class="zahlkachel ${art}"><strong>${n}</strong><span>${esc(name)}</span></div>`;
      const auffaellig = [s.warteschlange ? kachel(s.warteschlange, tx('in Arbeit'), 'warn') : '', s.fehlgeschlagen ? kachel(s.fehlgeschlagen, tx('fehlgeschlagen'), 'schlecht') : ''].join('');
      const teile = [[t.notizen - t.termine, tx('Notizen'), 'diktat'], [t.termine, tx('Termine'), 'termin'], [t.runden, tx('Planungsrunden'), 'planung']].filter(([n]) => n > 0);
      const summe = teile.reduce((a, [n]) => a + n, 0);
      // Ring aus Kreisbögen (SVG-Attribute statt Inline-Styles: die CSP erlaubt keine style-Attribute).
      let start = 0;
      const ring = summe ? teile.map(([n, , art]) => {
        const anteil = (n / summe) * 100, bogen = T`<circle class="ring ${art}" r="15.9155" cx="18" cy="18" stroke-dasharray="${anteil} ${100 - anteil}" stroke-dashoffset="${25 - start}"/>`;
        start += anteil;
        return bogen;
      }).join('') : '';
      return T`<details class="karte eingangskarte"><summary><span class="zeile kartentitel"><strong>Eingänge</strong><span class="eingangszahl">${s.auftraege}</span></span>
          ${d.anzahl ? T`<span class="leise">${d.anzahl} Aufnahmen${d.schnitt_groesse ? T` · Ø ${groesse(d.schnitt_groesse)}` : ''}${d.schnitt_sekunden ? T` · Ø ${dauer(d.schnitt_sekunden)} · zusammen ${dauer(d.gesamt_sekunden)}` : ''}</span>` : ''}
          ${auffaellig ? T`<span class="zahlkacheln">${auffaellig}</span>` : ''}</summary>
        <div class="eingangsdetail">${summe ? T`<svg class="ringdiagramm" viewBox="-2 -2 40 40" role="img" aria-label="${esc(teile.map(([n, name]) => `${n} ${name}`).join(', '))}">${ring}</svg>` : ''}
          <ul class="legende">${teile.map(([n, name, art]) => T`<li><span class="punkt-farbe ${art}"></span>${esc(name)}<strong>${n}</strong></li>`).join('')}</ul></div>
        <p class="leise">Planungen: ${t.planungen} (${t.runden} Runden) · ${t.audio} mit Aufnahme · ${t.web} Web-Antworten${t.nur_text ? T` · ${t.nur_text} nur Pebble-Text` : ''}</p></details>`;
    };
    // Speicher: zugeklappt je Ablage Name, belegt und Balken; aufgeklappt Pfad und frei/gesamt.
    const balken = (p) => T`<meter class="balken${p.knapp ? ' knapp' : ''}" min="0" max="${p.gesamt}" value="${p.gesamt - p.frei}" aria-label="${esc(tx('{0} von {1} belegt', groesse(p.gesamt - p.frei), groesse(p.gesamt)))}"></meter>`;
    const speicher = (liste) => T`<details class="karte speicherkarte"><summary><span class="zeile kartentitel"><strong>Speicher</strong><span class="eingangszahl">${groesse(liste.reduce((n, p) => n + (p.belegt || 0), 0))}</span></span>${liste.map((p) => T`<span class="speicherzeile"><span class="zeile"><span>${esc(tx(p.name))}</span>${p.fehler ? T`<span class="schlecht">nicht lesbar</span>` : T`<span>${groesse(p.belegt)}</span>`}</span>
        ${p.fehler ? '' : balken(p)}${p.knapp ? T('<span class="hinweis schlecht">Knapp – neue Aufnahmen werden abgewiesen.</span>') : ''}</span>`).join('')}</summary>
      <div class="speicherdetails">${liste.map((p) => T`<div><strong>${esc(tx(p.name))}</strong>
        <div class="pfadzeile leise"><span class="pfad">${esc(p.pfad)}</span>${p.fehler ? '' : T`<span class="frei">frei ${groesse(p.frei)} von ${groesse(p.gesamt)}</span>`}</div>
        ${p.fehler ? T`<div class="hinweis schlecht">nicht lesbar: ${esc(p.fehler)}</div>` : ''}</div>`).join('')}</div></details>`;
    const joplin = !s.joplin.aktiv ? tx('ausgeschaltet') : s.joplin.fehler ? T`<strong>Fehler:</strong> ${esc(s.joplin.fehler)}` : T`erreichbar${s.joplin.offene_planungen ? T`, ${s.joplin.offene_planungen} Planung(en) noch nicht übertragen` : ''}`;
    const updateKarte = neueVersion ? T`<a class="karte updatekarte" href="/status/update">${updateSymbol}<span><strong>Update ${esc(versionNr(neueVersion))} verfügbar</strong><br><span class="leise">Änderungen ansehen und installieren</span></span></a>` : '';
    seite.innerHTML = T`${statusReiter('status')}<h1>Status</h1>${updateKarte}
      ${s.fehlgeschlagen ? T`<div class="hinweis schlecht">${s.fehlgeschlagen} Auftrag/Aufträge dauerhaft fehlgeschlagen.</div>` : T('<div class="hinweis gut">Keine fehlgeschlagenen Aufträge.</div>')}
      ${(s.audio_unbekannt || []).length ? T`<h2>Aufnahmen ohne Eintrag <span class="zaehler fehler">${s.audio_unbekannt.length}</span></h2>
        <div class="hinweis schlecht">Im Audio-Ordner liegen Aufnahmen, die die Datenbank nicht kennt – z. B. nach dem Wiederherstellen eines älteren Backups. Importieren legt je Aufnahme einen neuen Eintrag an (Eingang = Zeitpunkt der Datei); er wird neu transkribiert und von der KI ausgewertet. Planungen kommen danach dran: eine Runde ohne Rückfragen, die KI ordnet sie einer Planung zu.</div>
        ${s.audio_unbekannt.map((f) => T`<div class="karte"><div class="zeile"><span><span class="badge fehler">ohne Eintrag</span> <span class="badge ${esc(f.modus)}">${esc(MODUS[f.modus] || f.modus)}</span> ${zeit(f.zeit)}</span><button class="btn klein" data-import="${esc(f.datei)}">Importieren</button></div>
          <div class="leise">${esc(f.datei)} · ${groesse(f.groesse)}</div></div>`).join('')}
        ${s.audio_unbekannt.length > 1 ? T('<div class="leiste"><button class="btn haupt" data-import-alle="1">Alle importieren</button></div>') : ''}` : ''}
      ${s.joplin.aktiv ? T`<dl><dt>Joplin</dt><dd>${joplin}</dd></dl>` : ''}
      ${s.joplin.aktiv ? T('<div class="leiste"><button class="btn klein" data-a="pruefen">Joplin jetzt prüfen</button></div>') : ''}
      ${eingaenge(s)}
      ${(s.speicher || []).length ? speicher(s.speicher) : ''}
      ${s.probleme.length ? T`<h2>Aufträge mit Fehlern</h2>${s.fehlgeschlagen ? T('<div class="leiste"><button class="btn gefahr" data-a="fehler-weg">Alle fehlerhaften löschen</button></div>') : ''}${s.probleme.map(karte).join('')}` : ''}
      <h2>Benachrichtigungen</h2>
      ${!pushDa ? T('<p class="leise">Dieser Browser unterstützt keine Benachrichtigungen. Auf dem iPhone die App erst zum Home-Bildschirm hinzufügen.</p>')
    : T`<p>${abo ? tx('Auf diesem Gerät eingeschaltet.') : tx('Auf diesem Gerät ausgeschaltet.')} <span class="leise">Geräte mit Benachrichtigung: ${s.push_abos}</span></p>
        <div class="leiste">${abo ? T('<button class="btn" data-a="push-aus">Ausschalten</button><button class="btn" data-a="push-test">Test senden</button>') : T('<button class="btn haupt" data-a="push-an">Einschalten</button>')}</div>`}
      <h2>Geräte (Passkeys)</h2>
      ${g.geraete.map((d) => T`<div class="karte"><div class="zeile"><span><strong>${esc(d.name)}</strong>${d.dieses ? T(' <span class="badge neutral">dieses Gerät</span>') : ''}</span>
        ${g.geraete.length > 1 && !d.dieses ? T`<button class="btn klein gefahr" data-geraet="${esc(d.id)}">Entfernen</button>` : ''}</div>
        <div class="leise">angelegt ${zeit(d.angelegt)}${d.zuletzt ? T` · zuletzt angemeldet ${zeit(d.zuletzt)}` : ''}</div></div>`).join('')}
      <div class="leiste"><a class="btn" href="/status/geraet">Weiteres Gerät hinzufügen</a></div>
      <h2>Verbundene Apps (MCP)</h2>
      ${o.apps.length ? o.apps.map((x) => T`<div class="karte"><div class="zeile"><span><strong>${esc(x.name)}</strong></span><button class="btn klein gefahr" data-app="${esc(x.id)}">Trennen</button></div>
        <div class="leise">verbunden ${zeit(x.angelegt)}${x.zuletzt ? T` · zuletzt genutzt ${zeit(x.zuletzt)}` : ''}</div></div>`).join('') : T('<p class="leise">Keine. Claude, ChatGPT & Co. verbinden sich über die Adresse <code>/mcp</code> dieses Dienstes und fragen hier nach Freigabe.</p>')}
      <div class="leiste"><button class="btn gefahr" data-a="abmelden">Abmelden</button></div>`;
  };
  aktuell.neuLaden = laden;
  seite.onclick = async (ev) => {
    const b = ev.target.closest('button');
    if (!b) return;
    try {
      if (b.dataset.app) {
        if (!confirm(tx('App trennen? Sie verliert sofort den Zugriff und muss neu freigegeben werden.'))) return;
        await api(T`/api/oauth/apps/${b.dataset.app}`, { method: 'DELETE' });
        return laden();
      }
      if (b.dataset.import || b.dataset.importAlle) {
        b.disabled = true;
        const r = await api('/api/audio-unbekannt/import', { method: 'POST', body: b.dataset.importAlle ? { alle: true } : { datei: b.dataset.import } });
        melde(tx('{0} Aufnahme(n) importiert – werden ausgewertet', r.importiert));
        verlaufStand = null;
        await ladeZaehler();
        return laden();
      }
      if (b.dataset.geraet) {
        if (!confirm(tx('Gerät entfernen? Es kann sich danach nicht mehr anmelden.'))) return;
        await api(T`/api/geraete/${b.dataset.geraet}`, { method: 'DELETE' });
        return laden();
      }
      switch (b.dataset.a) {
        case 'fehler-weg': {
          if (!confirm(tx('Alle dauerhaft fehlgeschlagenen und leeren Einträge in den Papierkorb legen?'))) return;
          b.disabled = true;
          const d = await api('/api/fehler', { method: 'DELETE' });
          for (const e of await alle('verlauf')) if (e.status === 'fehler' || e.status === 'leer') await loesche('verlauf', e.id);
          verlaufStand = null;
          melde(T`${d.geloescht} gelöscht`);
          return laden();
        }
        case 'pruefen': b.disabled = true; await api('/api/status?pruefen=1'); melde(tx('Joplin geprüft')); return laden();
        case 'push-an': {
          if ((await Notification.requestPermission()) !== 'granted') return melde(tx('Benachrichtigungen wurden im Browser nicht erlaubt'));
          const { schluessel } = await api('/api/push/schluessel');
          const reg = await navigator.serviceWorker.ready;
          const abo = await reg.pushManager.subscribe({ userVisibleOnly: true, applicationServerKey: b64zuBytes(schluessel) });
          await api('/api/push/abo', { method: 'POST', body: abo.toJSON() });
          melde(tx('Benachrichtigungen eingeschaltet'));
          return laden();
        }
        case 'push-aus': {
          const abo = await pushAbo();
          if (abo) { await api('/api/push/abo/entfernen', { method: 'POST', body: { endpoint: abo.endpoint } }); await abo.unsubscribe(); }
          melde(tx('Benachrichtigungen ausgeschaltet'));
          return laden();
        }
        case 'push-test': await api('/api/push/test', { method: 'POST' }); return melde(tx('Test gesendet'));
        case 'abmelden': await api('/api/auth/abmelden', { method: 'POST' }); try { localStorage.setItem('angemeldet', '0'); } catch { /* nur Startabkürzung */ } return geheZu('/anmelden', true);
      }
    } catch (f) { b.disabled = false; melde(f.message); }
  };
  aufraeumen = () => { seite.onclick = null; };
  await laden(true).catch(() => {});
  await laden();
}

// ---------- KI-Kosten ----------


// ---------- Gedächtnis: was die KI über den Nutzer gelernt hat ----------

const GED_REITER = [['vorschlaege', tx('Vorschläge')], ['aktiv', tx('Aktiv')], ['geloescht', tx('Gelöscht')]];
const GED_QUELLE = { ki: 'KI', lernlauf: tx('Lernlauf'), korrektur: tx('Korrektur'), antwort: tx('Antwort'), nutzer: tx('von dir') };

async function zeigeGedaechtnis() {
  seite.innerHTML = T`<p class="leer">Lädt …</p>`;
  let wartet = false, g = null;
  let reiter = localStorage.getItem('gedaechtnis-reiter');
  const male = () => {
    if (!GED_REITER.some(([r]) => r === reiter)) reiter = g.vorschlaege.length ? 'vorschlaege' : 'aktiv';
    const l = g.lernlauf;
    const lauf = !l ? tx('Noch kein Lernlauf.') : l.fehler ? T`Letzter Lernlauf ${zeit(l.zeit)} fehlgeschlagen: ${esc(l.fehler)}`
      : T`Letzter Lernlauf ${zeit(l.zeit)}: ${l.notizen} Notizen und ${l.planungen} Planungen ausgewertet, ${l.neu} neue Vorschläge${l.kosten_usd != null ? `, ${usd(l.kosten_usd)}` : ''}.`;
    const knoepfe = (e) => (reiter === 'vorschlaege'
      ? T`<button class="btn klein haupt" data-g="uebernehmen" data-id="${e.id}">Übernehmen</button><button class="btn klein" data-g="verwerfen" data-id="${e.id}">Verwerfen</button><a class="btn klein" href="/status/gedaechtnis/${e.id}/bearbeiten">Ändern</a>`
      : reiter === 'aktiv'
        ? T`<a class="btn klein" href="/status/gedaechtnis/${e.id}/bearbeiten">Bearbeiten</a><button class="btn klein gefahr" data-g="loeschen" data-id="${e.id}">Löschen</button>`
        : T`<button class="btn klein" data-g="wiederherstellen" data-id="${e.id}">Wiederherstellen</button>`);
    const leer = { vorschlaege: tx('Keine offenen Vorschläge.'), aktiv: tx('Noch nichts bestätigt.'), geloescht: tx('Nichts gelöscht.') }[reiter];
    const hinweis = { vorschlaege: tx('Neues von der KI, aus deinen Korrekturen und Antworten. Es wirkt erst, wenn du es übernimmst.'),
      aktiv: tx('Geht bei jeder Notiz und Planung an die KI mit.'),
      geloescht: T`Kommt nicht wieder. Die letzten ${g.unerwuenscht} gehen als „nicht erwünscht“ an die KI mit.` }[reiter];
    seite.innerHTML = T`<h1>Gedächtnis</h1>
      <p class="leise">Was die KI über dich weiß, damit sie kurze Notizen richtig versteht und einsortiert.</p>
      <div class="leiste"><a class="btn" href="/status/gedaechtnis/neu">Neuer Eintrag</a><button class="btn" data-g="lernen"${wartet || g.lernlauf_laeuft ? ' disabled' : ''}>${wartet || g.lernlauf_laeuft ? tx('Lernlauf läuft …') : tx('Jetzt lernen')}</button></div>
      <nav class="btn-group unter" aria-label="Gedächtnis">${GED_REITER.map(([r, n]) => T`<button class="btn${r === reiter ? ' aktiv' : ''}" data-reiter="${r}">${n} <span class="zaehler${r === 'vorschlaege' && g[r].length ? '' : ' still'}">${g[r].length}</span></button>`).join('')}</nav>
      <p class="leise">${hinweis}</p>
      ${g[reiter].length ? g[reiter].map((e) => T`<div class="karte gedkarte"><div>${esc(e.text)}</div><div class="leise">${GED_QUELLE[e.quelle] || esc(e.quelle)} · ${zeit(e.geaendert)}</div><div class="leiste">${knoepfe(e)}</div></div>`).join('') : T`<p class="leer">${leer}</p>`}
      <p class="leise">${lauf}${g.naechster_lernlauf ? T` Nächster ab ${zeit(g.naechster_lernlauf)}.` : ''} Ausgewertet werden die letzten ${g.lernlauf_eintraege} Einträge.</p>`;
  };
  const laden = async (vorab) => {
    try { g = await apiAbgelegt('/api/gedaechtnis', vorab); } catch (f) {
      if (vorab) return;
      if (f instanceof NetzFehler) { if (aktuell?.name === 'gedaechtnis') seite.innerHTML = T`<p class="leer">Offline – das Gedächtnis ist noch nicht gespeichert.</p>`; return; }
      throw f;
    }
    if (aktuell?.name !== 'gedaechtnis') return;
    if (wartet && !g.lernlauf_laeuft && g.lernlauf && g.lernlauf.zeit >= wartet) wartet = false;
    const y = window.scrollY;
    male();
    window.scrollTo(0, y);
  };
  aktuell.neuLaden = laden;
  seite.onclick = async (ev) => {
    const b = ev.target.closest('button');
    if (!b || !g) return;
    if (b.dataset.reiter) { reiter = b.dataset.reiter; localStorage.setItem('gedaechtnis-reiter', reiter); return male(); }
    if (!b.dataset.g) return;
    b.disabled = true;
    try {
      if (b.dataset.g === 'lernen') {
        wartet = Date.now();
        await api('/api/gedaechtnis/lernen', { method: 'POST' });
        melde(tx('Lernlauf gestartet'));
      } else {
        if (b.dataset.g === 'loeschen' && !confirm(tx('Eintrag löschen? Die KI schlägt ihn nicht wieder vor.'))) { b.disabled = false; return; }
        await api(T`/api/gedaechtnis/${b.dataset.id}`, { method: 'POST', body: { aktion: b.dataset.g } });
        melde({ uebernehmen: tx('Übernommen'), verwerfen: tx('Verworfen'), loeschen: tx('Gelöscht'), wiederherstellen: tx('Wiederhergestellt') }[b.dataset.g]);
      }
      await laden();
    } catch (f) { if (b.dataset.g === 'lernen') wartet = false; b.disabled = false; melde(f.message); }
  };
  aufraeumen = () => { seite.onclick = null; };
  await laden(true).catch(() => {});
  await laden();
}

// Eigene Seite zum Anlegen (id leer) oder Ändern eines Eintrags; ein geänderter Vorschlag gilt als übernommen.
async function zeigeGedaechtnisBearbeiten(id) {
  let e = null;
  if (id) {
    const g = await api('/api/gedaechtnis');
    e = [...g.vorschlaege, ...g.aktiv, ...g.geloescht].find((x) => x.id === id);
    if (!e) { seite.innerHTML = T('<p class="leer">Eintrag nicht gefunden.</p><p class="mitte"><a class="btn" href="/status/gedaechtnis">Zum Gedächtnis</a></p>'); return; }
  }
  seite.innerHTML = T`<p><a class="btn klein" href="/status/gedaechtnis">← Gedächtnis</a></p><h1>${e ? tx('Eintrag ändern') : tx('Neuer Eintrag')}</h1>
    <p class="leise">Ein kurzer Satz, z. B. „Bolle ist unser Hund“ oder „Podcasts gehören in die Liste Hören“.${e?.status === 'vorschlag' ? tx(' Mit dem Speichern ist der Vorschlag übernommen.') : ''}</p>
    <textarea id="gtext" maxlength="250">${esc(e?.text || '')}</textarea>
    <div class="leiste"><button class="btn haupt" id="gsichern">Speichern</button><a class="btn" href="/status/gedaechtnis">Abbrechen</a></div>`;
  document.getElementById('gsichern').onclick = async (ev) => {
    const text = document.getElementById('gtext').value.trim();
    if (!text) return melde(tx('Text fehlt'));
    ev.target.disabled = true;
    try {
      await api(e ? T`/api/gedaechtnis/${e.id}` : '/api/gedaechtnis', { method: e ? 'PUT' : 'POST', body: { text } });
      localStorage.setItem('gedaechtnis-reiter', 'aktiv');
      melde(tx('Gespeichert'));
      geheZu('/status/gedaechtnis', true);
    } catch (f) { ev.target.disabled = false; melde(f.message); }
  };
}

const usd = (v) => `${v.toLocaleString(LOCALE, { minimumFractionDigits: v < 0.1 ? 3 : 2, maximumFractionDigits: v < 0.1 ? 3 : 2 })} $`;
const kurzTag = (iso, mitWochentag) => new Date(`${iso}T12:00:00`).toLocaleDateString(LOCALE, mitWochentag ? { weekday: 'short', day: '2-digit', month: '2-digit' } : { day: '2-digit', month: '2-digit' });

// Runde Obergrenze und drei Achsenwerte für kleine Dollarbeträge.
function achse(max) {
  if (max <= 0) return { oben: 0.01, schritte: [0, 0.005, 0.01] };
  const pot = 10 ** Math.floor(Math.log10(max));
  const oben = [1, 2, 5, 10].map((f) => f * pot).find((v) => v >= max);
  return { oben, schritte: [0, oben / 2, oben] };
}

// Gestapelte Säulen je Tag: Diktate unten, Planungen oben, 2 px Abstand, runde Kappe am Ende.
function saeulen(tage, id, wochentag) {
  const B = 340, H = 170, links = 46, unten = 22, oben = 8;
  const plotB = B - links - 4, plotH = H - unten - oben;
  const { oben: max, schritte } = achse(Math.max(...tage.map((t) => t.diktat + t.planung)));
  const stellen = max < 0.1 ? 3 : 2;
  const band = plotB / tage.length, breite = Math.max(3, Math.min(24, band - 2));
  const y = (v) => oben + plotH - (v / max) * plotH;
  const kappe = (x, yo, h, r) => (h <= 0 ? '' : `M${x},${yo + h}V${yo + Math.min(r, h)}Q${x},${yo} ${x + Math.min(r, breite / 2)},${yo}H${x + breite - Math.min(r, breite / 2)}Q${x + breite},${yo} ${x + breite},${yo + Math.min(r, h)}V${yo + h}Z`);
  let marken = '', treffer = '', achseX = '';
  const jede = tage.length > 10 ? 7 : 1;
  tage.forEach((t, i) => {
    const x = links + i * band + (band - breite) / 2;
    const hD = (t.diktat / max) * plotH, hP = (t.planung / max) * plotH;
    const luecke = hD > 0 && hP > 0 ? 2 : 0;
    if (hP > 0) {
      marken += T`<path class="s2" d="${kappe(x, y(t.diktat + t.planung), Math.max(hP - luecke, 1), 4)}"/>`;
      if (hD > 0) marken += T`<rect class="s1" x="${x}" y="${y(t.diktat)}" width="${breite}" height="${hD}"/>`;
    } else if (hD > 0) marken += T`<path class="s1" d="${kappe(x, y(t.diktat), hD, 4)}"/>`;
    treffer += T`<rect class="treffer" data-i="${i}" x="${links + i * band}" y="${oben}" width="${band}" height="${plotH + unten}" tabindex="0" role="img" aria-label="${esc(tx('{0}: Notizen {1}, Planungen {2}', kurzTag(t.tag, true), usd(t.diktat), usd(t.planung)))}"/>`;
    const letzter = i === tage.length - 1 && !wochentag;
    const name = wochentag ? new Date(`${t.tag}T12:00:00`).toLocaleDateString(LOCALE, { weekday: 'short' }).replace('.', '') : kurzTag(t.tag, false);
    if ((tage.length - 1 - i) % jede === 0) achseX += T`<text class="achse" x="${letzter ? B - 4 : links + i * band + band / 2}" y="${H - 6}" text-anchor="${letzter ? 'end' : 'middle'}">${esc(name)}</text>`;
  });
  const gitter = schritte.map((s) => T`<line class="gitter" x1="${links}" x2="${B - 4}" y1="${y(s)}" y2="${y(s)}"/><text class="achse" x="${links - 6}" y="${y(s) + 4}" text-anchor="end">${s.toLocaleString(LOCALE, { minimumFractionDigits: stellen, maximumFractionDigits: stellen })} $</text>`).join('');
  return T`<svg class="diagramm" id="${id}" viewBox="0 0 ${B} ${H}" role="group" aria-label="Kosten je Tag in Dollar">${gitter}${marken}${achseX}${treffer}</svg>`;
}

// ---------- Einstellungen: Ersteinrichtung, Ansicht, Bearbeiten ----------

// Formular aus den Feldern des Servers, nach Gruppen. Geheimnisse: im Assistenten erzeugte sichtbar (zum
// Übertragen in die Pebble-App), sonst Passwortfeld – leer lassen behält den gespeicherten Wert.
// Sprachen heißen in ihrer eigenen Sprache.
const WAHL = { de: 'Deutsch', en: 'English' };
function einstellungsFormular(felder, { assistent }) {
  const gruppen = [...new Set(felder.map((f) => f.gruppe))];
  const feld = (f) => {
    const id = `f-${f.key}`;
    const hilfe = f.hilfe ? T`<p class="leise hilfe">${esc(tx(f.hilfe))}</p>` : '';
    let eingabe;
    if (f.art === 'choice') {
      eingabe = T`<select id="${id}" name="${f.key}">${f.auswahl.map((c) => T`<option value="${esc(c)}"${c === f.wert ? ' selected' : ''}>${esc(WAHL[c] || tx(c))}</option>`).join('')}</select>`;
    } else if (f.art === 'secret') {
      const sichtbar = assistent && f.wert;
      const platz = f.gesetzt ? tx('gesetzt – leer lassen zum Behalten') : '';
      eingabe = T`<input type="${sichtbar ? 'text' : 'password'}" id="${id}" name="${f.key}" value="${esc(f.wert || '')}" placeholder="${platz}" autocomplete="off" spellcheck="false">`;
      if (f.key === 'OPENAI_API_KEY' && assistent) eingabe = T`<div class="mitknopf">${eingabe}<button type="button" class="btn klein" data-pruefen="1">Prüfen</button></div>`;
    } else {
      const modus = f.art === 'int' ? ' inputmode="numeric"' : f.art === 'num' ? ' inputmode="decimal"' : '';
      eingabe = T`<input type="${f.art === 'url' ? 'url' : 'text'}" id="${id}" name="${f.key}" value="${esc(f.wert || '')}"${modus} autocomplete="off" spellcheck="false">`;
    }
    return T`<label for="${id}">${esc(tx(f.name))}${f.pflicht ? ' *' : ''}</label>${eingabe}${hilfe}`;
  };
  return T`<form class="einstellungen" novalidate>${gruppen.map((g) => T`<fieldset><legend>${esc(tx(g))}</legend>${felder.filter((f) => f.gruppe === g).map(feld).join('')}</fieldset>`).join('')}
    <div class="leiste"><button class="btn haupt" type="submit">Speichern</button></div></form>`;
}

const einstellungsWerte = (form) => Object.fromEntries([...form.querySelectorAll('[name]')].map((e) => [e.name, e.value]));

// Nach dem Speichern startet der Dienst neu: warten, bis er wieder antwortet.
async function warteAufNeustart() {
  await new Promise((r) => setTimeout(r, 2000));
  for (let i = 0; i < 30; i++) {
    try { if ((await fetch('/healthz', { cache: 'no-store' })).ok) return true; } catch (f) { /* startet noch */ }
    await new Promise((r) => setTimeout(r, 1000));
  }
  return false;
}

async function zeigeErsteinrichtung() {
  const code = new URLSearchParams(location.search).get('code') || '';
  seite.innerHTML = T('<p class="leer">Lädt …</p>');
  let d;
  try {
    d = await api(T`/api/ersteinrichtung?code=${encodeURIComponent(code)}`);
  } catch (f) {
    seite.innerHTML = T`<h1>Ersteinrichtung</h1><div class="hinweis schlecht">${esc(f.message)}</div>`;
    return;
  }
  // Öffentliche Adresse: vorgeschlagen ist die, unter der der Assistent gerade läuft (falls https).
  for (const f of d.felder) if (f.key === 'PUBLIC_URL' && !f.wert && location.protocol === 'https:') f.wert = location.origin;
  // Sprache: die Seite läuft schon in der gewählten (Browser, ?lang= oder gespeichert); Umschalten lädt neu.
  for (const f of d.felder) if (f.key === 'LANGUAGE') f.wert = SPRACHE;
  seite.innerHTML = T`<h1>Ersteinrichtung</h1>
    <p class="leise">Alle Werte sind mit Vorgaben gefüllt; Pflichtfelder tragen *. Webhook-Token und Signatur-Secret sind neu erzeugt – nach der Einrichtung stehen sie unter Status → Pebble zum Kopieren. Später änderbar unter Status → Einstellungen.</p>
    ${d.fehler ? T`<div class="hinweis schlecht">Die gespeicherten Einstellungen sind ungültig: ${esc(d.fehler)}</div>` : ''}
    ${einstellungsFormular(d.felder, { assistent: true })}<div id="ergebnis"></div>`;
  const form = seite.querySelector('form');
  const sprache = form.querySelector('[name=LANGUAGE]');
  if (sprache) sprache.onchange = () => { const u = new URL(location.href); u.searchParams.set('lang', sprache.value); location.replace(u); };
  form.onclick = async (ev) => {
    const b = ev.target.closest('[data-pruefen]');
    if (!b) return;
    b.disabled = true;
    try {
      const w = einstellungsWerte(form);
      const r = await api('/api/ersteinrichtung/openai', { method: 'POST', body: { code, key: w.OPENAI_API_KEY, base_url: w.OPENAI_BASE_URL } });
      melde(r.ok ? tx('Schlüssel funktioniert') : T`Schlüssel nicht gültig: ${r.fehler}`);
    } catch (f) { melde(f.message); }
    b.disabled = false;
  };
  form.onsubmit = async (ev) => {
    ev.preventDefault();
    const knopf = form.querySelector('[type=submit]');
    knopf.disabled = true;
    try {
      const r = await api('/api/ersteinrichtung', { method: 'POST', body: { code, werte: einstellungsWerte(form) } });
      form.hidden = true;
      const box = document.getElementById('ergebnis');
      box.innerHTML = T`<div class="hinweis gut">Gespeichert. Der Dienst startet neu …</div>`;
      window.scrollTo(0, 0);
      await warteAufNeustart();
      box.innerHTML = r.weiter
        ? T`<div class="hinweis gut">Eingerichtet. Jetzt unter der öffentlichen Adresse den ersten Passkey anlegen (Link 15 Minuten gültig).</div><div class="leiste"><a class="btn haupt" href="${esc(r.weiter)}" target="_self">Passkey einrichten</a></div>`
        : T`<div class="hinweis gut">Eingerichtet.</div><div class="leiste"><a class="btn haupt" href="${esc(r.adresse)}/" target="_self">Zur Web-App</a></div>`;
    } catch (f) {
      knopf.disabled = false;
      melde(f.message);
    }
  };
}

// Reiter der Statusseite (Buttongruppe): Übersicht, Pebble-App, Joplin (nur wenn aktiv), Konfiguration, Theme.
let joplinAktiv = (() => { try { return localStorage.getItem('joplin-aktiv') === '1'; } catch { return false; } })();
const statusReiter = (aktiv) => T`<nav class="btn-group unter reiter4" aria-label="Status">${[['status', '/status', tx('Übersicht')], ['pebble', '/pebble', tx('Pebble-App')],
  ...(joplinAktiv ? [['joplin', '/status/joplin', tx('Joplin')]] : []), ['einstellungen', '/status/einstellungen', tx('Konfiguration')], ['darstellung', '/status/darstellung', tx('Theme')]]
  .map(([n, href, text]) => T`<a class="btn${n === aktiv ? ' aktiv' : ''}" href="${href}"${n === aktiv ? ' aria-current="page"' : ''}>${text}</a>`).join('')}</nav>`;

// Konfiguration: je Wert „?“ (Erklärung darunter ein-/ausklappen) und ✎ (eigene Bearbeitungsseite für diesen Wert).
const STIFT = T('<svg viewBox="0 0 24 24" aria-hidden="true"><path d="M4 20h4L19 9l-4-4L4 16zM14 6l4 4" fill="none" stroke="currentColor" stroke-width="2" stroke-linejoin="round" stroke-linecap="round"/></svg>');
async function zeigeEinstellungen() {
  seite.innerHTML = T`${statusReiter('einstellungen')}<p class="leer">Lädt …</p>`;
  const d = await api('/api/einstellungen');
  if (aktuell?.name !== 'einstellungen') return;
  // Joplin-Zugang wird unter Status → Joplin eingerichtet; hier steht nur der Schalter.
  d.felder = d.felder.filter((f) => f.gruppe !== 'Joplin' || f.key === 'JOPLIN_ENABLED');
  const gruppen = [...new Set(d.felder.map((f) => f.gruppe))];
  const wert = (f) => (f.art === 'secret' ? (f.gesetzt ? tx('gesetzt') : T('<span class="leise">leer</span>')) : f.wert ? esc(f.art === 'choice' ? WAHL[f.wert] || tx(f.wert) : f.wert) : T('<span class="leise">leer</span>'));
  const zeile = (f) => T`<div class="konfzeile">
      <button class="konfhilfe" data-hilfe="${f.key}" aria-expanded="false" aria-controls="hilfe-${f.key}" aria-label="${esc(tx('Erklärung zu {0}', tx(f.name)))}">?</button>
      <span class="konfname">${esc(tx(f.name))}</span>
      <a class="konfedit" href="/status/einstellungen/${f.key}" aria-label="${esc(tx('{0} bearbeiten', tx(f.name)))}">${STIFT}</a>
      <span class="konfwert">${wert(f)}</span>
      <p class="konfinfo" id="hilfe-${f.key}" hidden>${esc(f.hilfe ? tx(f.hilfe) : tx('Keine Erklärung hinterlegt.'))}${f.vorgabe ? T` <span class="leise">Vorgabe: ${esc(f.vorgabe)}</span>` : ''}</p></div>`;
  seite.innerHTML = T`${statusReiter('einstellungen')}<h1>Konfiguration</h1>
    <p class="leise">? erklärt einen Wert, ✎ ändert ihn. Nach dem Speichern startet der Dienst kurz neu.</p>
    ${gruppen.map((g) => T`<h2>${esc(tx(g))}</h2><div class="karte konfliste">${d.felder.filter((f) => f.gruppe === g).map(zeile).join('')}</div>`).join('')}`;
  seite.onclick = (ev) => {
    const b = ev.target.closest('[data-hilfe]');
    if (!b) return;
    const info = document.getElementById(T`hilfe-${b.dataset.hilfe}`);
    info.hidden = !info.hidden;
    b.setAttribute('aria-expanded', String(!info.hidden));
  };
  aufraeumen = () => { seite.onclick = null; };
}

// Joplin einrichten und testen (Reiter nur bei JOPLIN_ENABLED = aktiv).
async function zeigeJoplin() {
  seite.innerHTML = T`${statusReiter('joplin')}<p class="leer">Lädt …</p>`;
  const [d, st] = await Promise.all([api('/api/einstellungen'), api('/api/status').catch(() => null)]);
  if (aktuell?.name !== 'joplin') return;
  const felder = d.felder.filter((f) => f.gruppe === 'Joplin' && f.key !== 'JOPLIN_ENABLED');
  const j = st?.joplin;
  const zustand = !j?.aktiv ? T('<div class="hinweis warn">Noch nicht eingerichtet: Server, Konto und Passwort eintragen, testen, speichern.</div>')
    : j.fehler ? T`<div class="hinweis schlecht"><strong>Fehler:</strong> ${esc(j.fehler)}</div>` : T`<div class="hinweis gut">Verbunden${j.offene_planungen ? T`, ${j.offene_planungen} Planung(en) noch nicht übertragen` : ''}.</div>`;
  seite.innerHTML = T`${statusReiter('joplin')}<h1>Joplin</h1>
    <p class="leise">Fertige Notizen, Planungen und Listen werden auf deinen Joplin-Server übertragen (nur schreibend, E2EE muss aus sein). Abschalten unter Konfiguration → Joplin.</p>
    ${zustand}
    ${einstellungsFormular(felder, { assistent: false }).replace(/<fieldset><legend>[^<]*<\/legend>/, '<fieldset>').replace(T('<div class="leiste"><button class="btn haupt" type="submit">Speichern</button></div>'),
      T('<div class="leiste"><button class="btn" type="button" data-testen="1">Verbindung testen</button><button class="btn haupt" type="submit">Speichern</button></div>'))}
    <div id="testergebnis"></div>`;
  const form = seite.querySelector('form');
  const werte = () => einstellungsWerte(form);
  form.onclick = async (ev) => {
    const b = ev.target.closest('[data-testen]');
    if (!b) return;
    b.disabled = true;
    const box = document.getElementById('testergebnis');
    box.innerHTML = T('<div class="hinweis">Teste …</div>');
    try {
      const w = werte();
      const r = await api('/api/joplin/pruefen', { method: 'POST', body: { url: w.JOPLIN_URL, email: w.JOPLIN_EMAIL, passwort: w.JOPLIN_PASSWORD } });
      box.innerHTML = r.ok ? T('<div class="hinweis gut">Verbindung und Anmeldung in Ordnung.</div>') : T`<div class="hinweis schlecht">Test fehlgeschlagen: ${esc(r.fehler)}</div>`;
    } catch (f) { box.innerHTML = T`<div class="hinweis schlecht">${esc(f.message)}</div>`; }
    b.disabled = false;
  };
  form.onsubmit = async (ev) => {
    ev.preventDefault();
    const knopf = form.querySelector('[type=submit]');
    knopf.disabled = true;
    try {
      await api('/api/einstellungen', { method: 'PUT', body: { werte: werte() } });
      melde(tx('Gespeichert – Dienst startet neu'));
      await warteAufNeustart();
      geheZu('/status/joplin', true);
    } catch (f) { knopf.disabled = false; melde(f.message); }
  };
  aufraeumen = () => { form.onclick = null; };
}

// Einen Wert der Konfiguration ändern (eigene Seite, Formular mit nur diesem Feld).
async function zeigeEinstellungBearbeiten(key) {
  // Zurück dorthin, woher man kam (z. B. Updates → Uhrzeit), sonst zur Konfiguration.
  const zurueckParam = new URLSearchParams(location.search).get('zurueck') || '';
  const zurueck = /^\/status\/[a-z/]*$/.test(zurueckParam) ? zurueckParam : '/status/einstellungen';
  seite.innerHTML = T`${statusReiter('einstellungen')}<p class="leer">Lädt …</p>`;
  const d = await api('/api/einstellungen');
  if (aktuell?.name !== 'einstellungBearbeiten') return;
  const f = d.felder.find((x) => x.key === key);
  if (!f) { seite.innerHTML = T`${statusReiter('einstellungen')}<div class="hinweis schlecht">Unbekannte Einstellung.</div>`; return; }
  seite.innerHTML = T`${statusReiter('einstellungen')}<h1>${esc(tx(f.name))}</h1><p class="leise">${esc(tx(f.gruppe))}</p>
    ${einstellungsFormular([f], { assistent: false }).replace(/<fieldset><legend>[^<]*<\/legend>/, '<fieldset>')}
    <div class="leiste"><a class="btn" href="${zurueck}">Abbrechen</a></div>`;
  const form = seite.querySelector('form');
  form.querySelector('input,select')?.focus();
  form.onsubmit = async (ev) => {
    ev.preventDefault();
    const werte = einstellungsWerte(form);
    if (key === 'PUBLIC_URL' && werte.PUBLIC_URL !== f.wert && !confirm(tx('Die öffentliche Adresse ändert sich: alle Passkeys werden ungültig. Trotzdem speichern?'))) return;
    const knopf = form.querySelector('[type=submit]');
    knopf.disabled = true;
    try {
      await api('/api/einstellungen', { method: 'PUT', body: { werte } });
      melde(tx('Gespeichert – Dienst startet neu'));
      await warteAufNeustart();
      if (key === 'LANGUAGE') { location.replace(zurueck); return; }
      await ladeZaehler();
      geheZu(zurueck, true);
    } catch (fehler) {
      knopf.disabled = false;
      melde(fehler.message);
    }
  };
}


// ---------- Updates: neue Versionen von GitHub mit Änderungsliste, Installation über den Updater ----------

// Änderungsliste (Markdown aus CHANGELOG.md): Überschriften und Stichpunkte, alles andere als Absatz.
function aenderungenHtml(text) {
  const zeilen = (text || '').split('\n').map((z) => z.trimEnd());
  let html = '', liste = false;
  for (const z of zeilen) {
    const punkt = z.match(/^\s*[-*] (.*)$/);
    if (punkt) { if (!liste) { html += '<ul>'; liste = true; } html += T`<li>${esc(punkt[1])}</li>`; continue; }
    if (liste) { html += '</ul>'; liste = false; }
    const kopf = z.match(/^#{1,6} (.*)$/);
    if (kopf) html += `<h3>${esc(kopf[1])}</h3>`;
    else if (z.trim()) html += `<p>${esc(z)}</p>`;
  }
  return html + (liste ? '</ul>' : '');
}

// Updates: installierte Version, Versionen des gewählten Kanals (die letzten 3), frühere Stände.
// Änderungslisten nur auf Knopfdruck in einem Blatt; die vollständige Liste liegt auf GitHub.
const SYM_LADEN = '<svg viewBox="0 0 24 24" aria-hidden="true"><path d="M12 4v11m-5-5 5 5 5-5M5 20h14" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/></svg>';
const SYM_ZURUECK = '<svg viewBox="0 0 24 24" aria-hidden="true"><path d="M4 12a8 8 0 1 0 2.4-5.7M4 4v4.5h4.5" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/></svg>';
// Dauer lesbar: 0:42, 12:05, 1:02:10.
const dauer = (s) => { s = Math.round(s); const h = Math.floor(s / 3600), m = Math.floor(s / 60) % 60, z = String(s % 60).padStart(2, '0'); return h ? `${h}:${String(m).padStart(2, '0')}:${z}` : `${m}:${z}`; };
// Dateigröße lesbar (KB/MB/GB).
const groesse = (n) => (!n ? "0 KB" : n < 1024 * 1024 ? `${Math.max(1, Math.round(n / 1024))} KB` : n < 1024 ** 3 ? `${(n / 1024 / 1024).toFixed(1)} MB` : `${(n / 1024 ** 3).toFixed(1)} GB`);

async function zeigeUpdate(teil) {
  seite.innerHTML = T`<p class="leer">Lädt …</p>`;
  const KANAL_TEXT = { stable: tx('stable – Langzeit-Versionen'), beta: tx('beta – neue Funktionen, noch nicht ausgereift'), nightly: tx('nightly – jede Änderung, ungetestet') };
  let u = null;
  // Updates und Backups sind eigene Menüpunkte; beide nutzen dieselbe Antwort von /api/update.
  const reiter = teil === 'backups' ? 'backups' : 'updates', meinName = teil === 'backups' ? 'backups' : 'update';
  let alle = false; // „Mehr“: alle gespeicherten Versionen des Kanals statt der letzten 3
  const zeigeLog = (titel, text) => {
    document.querySelector('dialog.logblatt')?.remove();
    const d = document.createElement('dialog');
    d.className = 'mehrblatt logblatt';
    d.tabIndex = -1;
    d.innerHTML = T`<div class="griff" aria-hidden="true"></div><h2>${esc(titel)}</h2>
      ${text ? aenderungenHtml(text) : T('<p class="leise">Keine Änderungsliste zu dieser Version.</p>')}
      ${u.alle_aenderungen ? T`<p><a href="${esc(u.alle_aenderungen)}" target="_blank" rel="noopener">Alle Änderungen auf GitHub</a></p>` : ''}
      <div class="leiste"><button class="btn" data-zu="1">Schließen</button></div>`;
    d.onclick = (ev) => { if (ev.target === d || ev.target.closest('[data-zu]')) d.close(); };
    d.onclose = () => d.remove();
    document.body.append(d);
    d.showModal();
    d.focus();
  };
  const laden = async (pruefen) => {
    u = await api(`/api/update${pruefen ? '?pruefen=1' : ''}`);
    if (aktuell?.name !== meinName) return;
    const liste = u.versionen || [];
    const laeuft = /^(requested|running)/.test(u.status || '');
    // Installiert ist eine Version aus einem anderen Kanal (oder ein eigener Build): die neueste des gewählten Kanals ist der Wechsel.
    const wechsel = liste.length > 0 && u.aktuell_kanal !== u.kanal;
    const status = !u.status ? '' : u.status.startsWith('error') ? T`<div class="hinweis schlecht">Letztes Update: ${esc(u.status)}</div>`
      : laeuft ? T`<div class="hinweis warn">Update läuft (${esc(u.status)}) …</div>` : '';
    const zeile = (r) => {
      const neueste = r === liste[0];
      const knopf = u.installierbar && !r.installiert
        ? T`<button class="btn klein${neueste ? ' haupt' : ''}" data-installieren="${esc(r.version)}"${laeuft ? ' disabled' : ''}>Installieren</button>` : '';
      return T`<div class="karte versionzeile"><div class="versionkopf"><span class="versionzeile1"><span class="badge neutral">${esc(tx(r.kanal))}</span><strong>${esc(versionNr(r.version))}</strong></span>
          ${r.installiert ? T('<span class="badge fertig">installiert</span>') : ''}${neueste ? '<span class="badge neutral">latest</span>' : ''}${r.neu ? '<span class="badge in_arbeit">new</span>' : ''}${r.vorhanden && !r.installiert ? T('<span class="badge neutral" title="Liegt lokal vor, Wechsel ohne Download">lokal</span>') : ''}
          <span class="leise">${esc(versionZeit(r.version) || r.datum || '')}</span></div>
        <div class="versionknoepfe"><button class="btn klein" data-log="${esc(r.version)}">Änderungen</button>${knopf}</div></div>`;
    };
    const kopf = T`<h1>${reiter === 'backups' ? tx('Backups') : tx('Updates')}</h1>
      ${status}${u.fehler ? T`<div class="hinweis warn">${esc(u.fehler)}</div>` : ''}`;
    // Neueste Version des gewählten Kanals, die später als die installierte erschienen ist: eigene Karte über „Installiert“.
    const neu = liste.find((r) => r.neu);
    const neuKarte = neu ? T`<div class="karte versionzeile neuekarte"><div class="versionkopf"><span class="leise">${updateSymbol} ${tx('Neue Version verfügbar')}</span><span class="versionzeile1"><span class="badge neutral">${esc(tx(neu.kanal))}</span><strong class="versiongross">${esc(versionNr(neu.version))}</strong></span><span class="leise">${esc(versionZeit(neu.version) || neu.datum || '')}</span></div>
        <div class="versionknoepfe"><button class="btn klein" data-log="${esc(neu.version)}">Änderungen</button>${u.installierbar ? T`<button class="btn klein haupt" data-installieren="${esc(neu.version)}"${laeuft ? ' disabled' : ''}>Installieren</button>` : ''}</div></div>` : '';
    const updates = T`${neuKarte}<div class="karte versionzeile aktuell"><div class="versionkopf"><span class="leise">Installiert</span><span class="versionzeile1"><span class="badge neutral">${esc(tx(u.aktuell_kanal || ''))}</span><strong class="versiongross">${esc(versionNr(u.aktuell))}</strong></span>${versionZeit(u.aktuell) ? T`<span class="leise">${esc(versionZeit(u.aktuell))}</span>` : ''}</div>
        <div class="versionknoepfe"><button class="btn klein" data-log="">Änderungen</button></div></div>
      <h2>Kanal</h2>
      <nav class="btn-group unter" aria-label="Kanal">${(u.kanaele || []).map((k) => T`<button class="btn${k === u.kanal ? ' aktiv' : ''}" data-kanal="${k}" aria-pressed="${k === u.kanal}">${k}${u.kanal_neu?.[k] ? T` <span class="kanalneu" title="Neuere Version als die installierte">${updateSymbol}</span>` : ''}</button>`).join('')}</nav>
      <p class="leise">${esc(KANAL_TEXT[u.kanal] || '')}</p>
      ${wechsel ? T`<div class="hinweis warn">${esc(tx('Installiert ist {0} aus {1}. Zum Wechsel auf {2} die neueste Version dort installieren.', versionNr(u.aktuell), tx(u.aktuell_kanal), u.kanal))}</div>` : ''}
      ${liste.length ? (alle ? liste : liste.slice(0, 3)).map(zeile).join('') : u.fehler ? '' : T('<p class="leise">In diesem Kanal gibt es noch keine Version.</p>')}
      ${!alle && liste.length > 3 ? T`<div class="leiste"><button class="btn" data-mehr-versionen="1">Mehr (${liste.length - 3})</button></div>` : ''}
      ${liste.length && !liste.some((r) => r.neu) ? T('<p class="leise">Keine neuere Version als die installierte in diesem Kanal.</p>') : ''}
      ${u.installierbar ? T('<p class="leise">Vor jedem Wechsel werden Datenbank, Einstellungen, Version und Kanal gesichert.</p>') : T('<div class="hinweis">Installieren aus der App braucht die Installation per <code>install.sh</code>; sonst das Skript mit <code>--version</code> ausführen.</div>')}
      <h2>Automatisches Update</h2>
      <nav class="btn-group unter" aria-label="Automatisches Update">${[[true, tx('An')], [false, tx('Aus')]].map(([an, n]) => T`<button class="btn${u.auto === an ? ' aktiv' : ''}" data-auto="${an ? '1' : '0'}" aria-pressed="${u.auto === an}">${n}</button>`).join('')}</nav>
      <p class="leise">${u.auto ? esc(tx('Täglich um {0} die neueste Version aus {1} installieren, wenn sie neuer ist (vorher Backup).', u.auto_zeit, u.kanal)) : tx('Aus – Updates nur von Hand.')}
        <a class="konfedit" href="/status/einstellungen/AUTO_UPDATE_TIME?zurueck=/status/update" aria-label="${esc(tx('Uhrzeit ändern'))}" title="${esc(tx('Uhrzeit ändern'))}">${STIFT}</a></p>
      ${u.auto_letzt ? T`<p class="leise">${esc(tx('Letzter Lauf: {0}', u.auto_letzt))}</p>` : ''}
      <p class="leise">${esc(u.quelle || 'aus')}${u.geprueft ? T` · geprüft ${zeit(u.geprueft)}` : ''}</p>
      <div class="leiste"><button class="btn" data-pruefen="1">Jetzt prüfen</button></div>`;
    const b = u.backup || {};
    const ART = { update: tx('Update'), plan: tx('geplant'), manuell: tx('manuell') };
    const PLAN_NAME = { 'täglich': tx('täglich'), 'wöchentlich': tx('wöchentlich (montags)'), monatlich: tx('monatlich (am 1.)'), aus: tx('aus') };
    const letzt = (b.letzt || '').match(/^(\S+ \S+) (ok|fehler) ?(.*)$/);
    const konf = (name, wert, key) => T`<div class="konfzeile ohnehilfe"><span class="konfname">${esc(name)}</span>
      ${key ? T`<a class="konfedit" href="/status/einstellungen/${key}?zurueck=/status/backups" aria-label="${esc(tx('{0} bearbeiten', name))}">${STIFT}</a>` : '<span></span>'}<span class="konfwert">${wert}</span></div>`;
    const backups = T`<h2>Automatisches Backup</h2>
      <div class="karte konfliste">${konf(tx('Plan'), esc(PLAN_NAME[b.plan] || b.plan || ''), 'BACKUP_SCHEDULE')}
        ${b.plan !== 'aus' ? konf(tx('Uhrzeit'), esc(b.zeit || ''), 'BACKUP_TIME') : ''}
        ${konf(tx('Behalten'), esc(tx('{0} Tage', b.tage)), 'BACKUP_RETENTION_DAYS')}
        ${konf(tx('Aufnahmen'), b.audio ? tx('mit') : tx('ohne'), 'BACKUP_AUDIO')}
        ${konf(tx('Ordner'), esc(b.ordner || ''), '')}</div>
      <p class="leise">${b.plan === 'aus' ? tx('Aus – Backups nur von Hand und vor Updates.') : tx('Backups vor Updates bleiben 30 Tage.')} ${tx('Den Ordner legt der Startwert BACKUP_DIR in /etc/pebble-webhook/env fest; danach install.sh erneut ausführen.')}</p>
      ${letzt ? (letzt[2] === 'ok' ? T`<p class="leise">${esc(tx('Letzter Lauf: {0}', letzt[1]))}</p>` : T`<div class="hinweis schlecht">${esc(tx('Letztes geplantes Backup fehlgeschlagen ({0}): {1}', letzt[1], letzt[3]))}</div>`) : ''}
      <div class="leiste"><button class="btn haupt" data-sichern="1">Jetzt sichern</button></div>
      <h2>Backups</h2>
      ${(u.backups || []).length ? T`<p class="backupsumme"><strong>${u.backups.length}</strong> Backups · <strong>${groesse(u.backups.reduce((n, st) => n + (st.groesse || 0), 0))}</strong> belegt</p>` : ''}
      <p class="leise">Jedes Backup enthält Datenbank mit allen Einstellungen, Version und Kanal. Wiederherstellen spielt die Daten ein und installiert die Version aus dem Backup; was seitdem dazukam, fehlt danach. Der jetzige Stand wird vorher gesichert.</p>
      ${(u.backups || []).length ? u.backups.map((st) => T`<div class="karte backupzeile"><div class="backupinfo">
          <span class="versionzeile1">${st.kanal ? T`<span class="badge neutral">${esc(tx(st.kanal))}</span>` : ''}<strong>${esc(versionNr(st.version))}</strong></span>
          <span class="leise backupzeit">${esc(new Date(st.zeit).toLocaleString(LOCALE, { day: '2-digit', month: '2-digit', year: new Date(st.zeit).getFullYear() === new Date().getFullYear() ? undefined : '2-digit', hour: '2-digit', minute: '2-digit' }))} · ${esc(ART[st.art] || st.art)}${st.audio ? T` · mit Aufnahmen` : ''}</span></div>
        <div class="backupknoepfe">${st.groesse ? T`<span class="leise backupgroesse">${groesse(st.groesse)}</span>` : ''}
          ${st.alt ? '<span class="symbolplatz"></span>' : T`<a class="btn klein rund symbol" href="/api/backups/${encodeURIComponent(st.name)}" download aria-label="${esc(tx('Herunterladen'))}" title="${esc(tx('Herunterladen'))}">${SYM_LADEN}</a>`}
          ${st.wiederherstellbar ? (u.installierbar ? T`<button class="btn klein rund symbol" data-restore="${esc(st.name)}" data-version="${esc(versionNr(st.version))}" aria-label="${esc(tx('Wiederherstellen'))}" title="${esc(tx('Wiederherstellen'))}"${laeuft ? ' disabled' : ''}>${SYM_ZURUECK}</button>` : '')
    : T('<span class="leise">unvollständig</span>')}</div></div>`).join('') : T('<p class="leise">Noch keine Backups.</p>')}`;
    seite.innerHTML = kopf + (reiter === 'backups' ? backups : updates);
  };
  // Nach einem angestoßenen Wechsel warten, bis der Updater fertig ist (sichert, lädt, ersetzt, startet neu).
  const warte = async (fertig) => {
    for (let i = 0; i < 90; i++) {
      await new Promise((r) => setTimeout(r, 2000));
      try {
        const s = await api('/api/update');
        if (fertig(s) || (s.status || '').startsWith('error')) return location.reload();
      } catch (f) { /* Neustart */ }
    }
    laden(false);
  };
  aktuell.neuLaden = () => laden(false);
  seite.onclick = async (ev) => {
    const b = ev.target.closest('button');
    if (!b || !u) return;
    if (b.dataset.log !== undefined) {
      const r = (u.versionen || []).find((x) => x.version === b.dataset.log);
      return r ? zeigeLog(T`${versionNr(r.version)} · ${tx(r.kanal)}`, r.aenderungen) : zeigeLog(T`${versionNr(u.aktuell)} · ${tx(u.aktuell_kanal || '')}`, u.aktuell_aenderungen);
    }
    if (b.dataset.mehrVersionen) { alle = true; return laden(false); }
    b.disabled = true;
    try {
      if (b.dataset.pruefen) { await laden(true); return ladeZaehler(); }
      if (b.dataset.auto) {
        await api('/api/update/auto', { method: 'POST', body: { aktiv: b.dataset.auto === '1' } });
        return laden(false);
      }
      if (b.dataset.kanal) {
        if (b.dataset.kanal === u.kanal) return;
        alle = false;
        await api('/api/update/kanal', { method: 'POST', body: { kanal: b.dataset.kanal } });
        ladeZaehler();
        return laden(false);
      }
      if (b.dataset.sichern) {
        u = await api('/api/backups', { method: 'POST', body: {} });
        melde(tx('Backup angelegt'));
        return laden(false);
      }
      if (b.dataset.restore) {
        if (!confirm(tx('Backup von Version {0} wiederherstellen? Daten und Einstellungen werden ersetzt, die Version aus dem Backup wird installiert; was seitdem dazukam, fehlt danach. Der jetzige Stand wird vorher gesichert.', b.dataset.version))) { b.disabled = false; return; }
        await api('/api/update/zurueck', { method: 'POST', body: { name: b.dataset.restore } });
        melde(tx('Wiederherstellen angestoßen'));
        return warte((s) => (s.status || '').startsWith('done'));
      }
      const ziel = b.dataset.installieren;
      if (!ziel || !confirm(tx('Version {0} installieren? Die Datenbank wird vorher gesichert, der Dienst startet neu.', `${versionNr(ziel)} (${tx(u.kanal)})`))) { b.disabled = false; return; }
      await api('/api/update', { method: 'POST', body: { version: ziel } });
      melde(tx('Update angestoßen'));
      return warte((s) => s.aktuell === ziel);
    } catch (f) {
      b.disabled = false;
      melde(f.message);
    }
  };
  await laden(false);
}

// ---------- Papierkorb: gelöschte Einträge und Planungen, wiederherstellbar bis zur Endlöschung ----------

async function zeigePapierkorb() {
  seite.innerHTML = T`<p class="leer">Lädt …</p>`;
  const laden = async (vorab) => {
    const k = await apiAbgelegt('/api/papierkorb', vorab);
    if (aktuell?.name !== 'papierkorb') return;
    const titel = (e) => e.typ === 'planung'
      ? T`${esc(e.titel || tx('Planung ohne Titel'))} <span class="leise">· ${e.runden} ${e.runden === 1 ? tx('Runde') : tx('Runden')}</span>`
      : e.runde ? T`${esc(e.planung || tx('Planung'))} <span class="leise">· Runde ${e.runde}: ${esc(e.titel || '')}</span>` : esc(e.titel || tx('ohne Titel'));
    seite.innerHTML = T`<h1>Papierkorb</h1>
      <p class="leise">Gelöschte Einträge mit Listenpunkten und Aufnahme${k.tage > 0 ? T`; nach ${k.tage} Tagen endgültig gelöscht` : ''}.</p>
      ${k.eintraege.length ? k.eintraege.map((e) => T`<div class="karte"><div class="zeile"><span>${nrHtml(e.nr)}<span class="badge ${esc(e.modus)}">${esc(MODUS[e.modus] || e.modus)}</span> ${titel(e)}</span></div>
        <div class="leise">${e.eingang ? T`eingegangen ${zeit(e.eingang)} · ` : ''}gelöscht ${zeit(e.geloescht)}${e.endgueltig ? T` · endgültig ab ${zeit(e.endgueltig)}` : ''}</div>
        <div class="leiste"><button class="btn klein haupt" data-a="wiederherstellen" data-typ="${esc(e.typ)}" data-id="${esc(e.id)}">Wiederherstellen</button>
          <button class="btn klein gefahr" data-a="loeschen" data-typ="${esc(e.typ)}" data-id="${esc(e.id)}">Endgültig löschen</button></div></div>`).join('')
    : T('<p class="leer">Der Papierkorb ist leer.</p>')}`;
  };
  aktuell.neuLaden = laden;
  seite.onclick = async (ev) => {
    const b = ev.target.closest('button[data-a]');
    if (!b) return;
    const pfad = T`/api/papierkorb/${b.dataset.typ}/${b.dataset.id}`;
    try {
      if (b.dataset.a === 'loeschen' && !confirm(tx('Endgültig löschen? Das lässt sich nicht rückgängig machen.'))) return;
      b.disabled = true;
      await api(`${pfad}/${b.dataset.a}`, { method: 'POST' });
      verlaufStand = null;
      melde(b.dataset.a === 'wiederherstellen' ? tx('Wiederhergestellt') : tx('Endgültig gelöscht'));
      await laden();
    } catch (f) {
      b.disabled = false;
      melde(f.message);
    }
  };
  await laden(true).catch(() => {});
  await laden();
}

async function zeigeKosten() {
  seite.innerHTML = T`<p class="leer">Lädt …</p>`;
  const laden = async (vorab) => {
    let k;
    try { k = await apiAbgelegt('/api/kosten', vorab); } catch (f) {
      if (vorab) return;
      if (f instanceof NetzFehler) { if (aktuell?.name === 'kosten') seite.innerHTML = T`<p class="leer">Offline – die Kosten sind noch nicht gespeichert.</p>`; return; }
      throw f;
    }
    if (aktuell?.name !== 'kosten') return;
    const t7 = k.tage.slice(-7);
    const summe = (liste, feld) => liste.reduce((s, t) => s + t[feld], 0);
    const d = k.summen.diktat, p = k.summen.planung;
    const kachel = (titel, s) => T`<div class="kachel"><div class="leise">${titel}</div><div class="zahl">${usd(s.usd)}</div>
      <div class="leise">${s.anzahl} ${s.anzahl === 1 ? tx('Auftrag') : tx('Aufträge')}${s.anzahl ? ` · Ø ${usd(s.usd / s.anzahl)}` : ''}</div>
      ${s.ohne_kosten ? T`<div class="leise">${s.ohne_kosten} ohne vollständigen Betrag</div>` : ''}</div>`;
    const legende = T('<div class="legende"><span><i class="s1"></i>Notizen</span><span><i class="s2"></i>Aufgaben (Planungen)</span></div>');
    const arten = [[tx('Recherche'), k.arten.recherche], [tx('Transkription'), k.arten.transkription], [tx('Kurztitel und Zuordnung'), k.arten.titel_zuordnung]];
    const artMax = Math.max(...arten.map((a) => a[1]), 1e-9);
    const fehlt = Object.entries({ transkription: tx('Transkription (PRICE_TRANSCRIBE_PER_MIN)'), titel: tx('Kurztitel/Zuordnung (PRICE_TITLE_*)'), recherche: tx('Recherche (PRICE_PLAN_*, PRICE_SEARCH_PER_CALL)') }).filter(([s]) => !k.preise[s]).map(([, n]) => n);
    const tabelle = (tage) => T`<details><summary>Als Tabelle</summary><table class="werte"><thead><tr><th>Tag</th><th>Notizen</th><th>Aufgaben</th><th>Summe</th></tr></thead><tbody>${tage.map((t) => T`<tr><td>${esc(kurzTag(t.tag, true))}</td><td>${usd(t.diktat)}</td><td>${usd(t.planung)}</td><td>${usd(t.diktat + t.planung)}</td></tr>`).join('')}</tbody></table></details>`;
    seite.innerHTML = T`<h1>KI-Kosten</h1>
      <p class="leise">OpenAI-Kosten je Auftrag, berechnet aus Verbrauch und den Preisen der Konfiguration. Zugeordnet nach Eingangstag.</p>
      ${fehlt.length ? T`<div class="hinweis warn">Preis fehlt für: ${esc(fehlt.join(', '))}. Diese Anteile sind nicht enthalten.</div>` : ''}
      <h2>Letzte 30 Tage</h2>
      <div class="kacheln">${kachel(tx('Gesamt'), { usd: d.usd + p.usd, anzahl: d.anzahl + p.anzahl, ohne_kosten: d.ohne_kosten + p.ohne_kosten })}${kachel(tx('Notizen'), d)}${kachel(tx('Aufgaben (Planungen)'), p)}</div>
      <h2>Letzte 7 Tage – ${usd(summe(t7, 'diktat') + summe(t7, 'planung'))}</h2>
      <div class="karte">${legende}${saeulen(t7, 'd7', true)}<div class="ablesen" id="d7-wert">Säule antippen für die Werte des Tages.</div>${tabelle(t7)}</div>
      <h2>Verlauf 30 Tage</h2>
      <div class="karte">${legende}${saeulen(k.tage, 'd30', false)}<div class="ablesen" id="d30-wert">Säule antippen für die Werte des Tages.</div>${tabelle(k.tage.filter((t) => t.diktat + t.planung > 0))}</div>
      <h2>Wofür – 30 Tage</h2>
      <div class="karte">${arten.map(([name, v]) => T`<div class="balkenzeile"><span>${esc(name)}</span><span class="balken"><i class="${v > 0 ? '' : 'leer'}" data-anteil="${Math.round((v / artMax) * 100)}"></i></span><span class="wert">${usd(v)}</span></div>`).join('')}
        <p class="leise">Kostenlimit je Rechercheauftrag: ${usd(k.limit_usd)}.</p></div>`;
    seite.querySelectorAll('.balken i').forEach((el) => { el.style.width = `${Math.max(Number(el.dataset.anteil), el.classList.contains('leer') ? 0 : 1)}%`; });
    for (const [id, tage] of [['d7', t7], ['d30', k.tage]]) {
      const svg = document.getElementById(id), wert = document.getElementById(T`${id}-wert`);
      const zeigeTag = (ev) => {
        const r = ev.target.closest('.treffer');
        if (!r) return;
        const t = tage[Number(r.dataset.i)];
        svg.querySelectorAll('.treffer.an').forEach((e) => e.classList.remove('an'));
        r.classList.add('an');
        wert.innerHTML = T`<strong>${esc(kurzTag(t.tag, true))}</strong> · Notizen ${usd(t.diktat)} · Aufgaben ${usd(t.planung)} · Summe ${usd(t.diktat + t.planung)}`;
      };
      svg.addEventListener('pointerover', zeigeTag);
      svg.addEventListener('click', zeigeTag);
      svg.addEventListener('focusin', zeigeTag);
    }
  };
  aktuell.neuLaden = laden;
  await laden(true).catch(() => {});
  await laden();
}

// ---------- Pebble einrichten ----------

// Pebble-App: Gesten und die Werte für die Webhook-Einrichtung.
async function zeigePebble() {
  seite.innerHTML = T`${statusReiter('pebble')}<p class="leer">Lädt …</p>`;
  let d;
  try { d = await api('/api/pebble'); } catch (f) {
    if (f instanceof NetzFehler) { if (aktuell?.name === 'pebble') seite.innerHTML = T('<p class="leer">Offline – die Einrichtungswerte brauchen eine Verbindung.</p>'); return; }
    throw f;
  }
  if (aktuell?.name !== 'pebble') return;
  seite.innerHTML = statusReiter('pebble') + pebbleSeite(d);
  seite.onclick = (ev) => { const b = ev.target.closest('button'); if (b) pebbleKlick(b, d); };
  aufraeumen = () => { seite.onclick = null; };
}
function pebbleSeite(d) {
  const wert = (name, schluessel, geheim) => T`<div class="karte"><div class="zeile"><strong>${esc(name)}</strong>
    <span>${geheim ? T`<button class="btn klein" data-zeigen="${schluessel}">Anzeigen</button> ` : ''}<button class="btn klein" data-kopieren="${schluessel}">Kopieren</button></span></div>
    <div class="link" id="wert-${schluessel}">${geheim ? '••••••••••••••••' : esc(d[schluessel])}</div></div>`;
  return T`<h1>Pebble-App</h1>
    <dl><dt>Halten</dt><dd>${esc(MODUS[d.halten])}</dd><dt>Klick + Halten</dt><dd>${esc(MODUS[d.doppelklick_halten])}</dd>
      <dt>Transkription</dt><dd>${d.transkription === 'openai' ? tx('OpenAI') : tx('Pebble-App')}</dd></dl>
    <h2>Einrichten</h2>
    <p class="leise">In der Pebble-App unter Index 01 → Webhooks, für beide Gesten gleich eintragen.</p>
    ${wert(tx('Webhook-URL'), 'url')}
    <dl><dt>Payload-Modus</dt><dd><strong>Both</strong> (Aufnahme und Transkript)</dd><dt>Größe</dt><dd>bis ${d.max_mb} MB je Aufnahme</dd></dl>
    <p class="leise">Unter „Headers“ ein Paar aus Name und Wert anlegen:</p>
    ${wert(tx('Header-Name'), 'header')}${wert(tx('Header-Wert'), 'token', true)}
    <p class="leise">„Signed requests“ einschalten und dieses Secret eintragen, ohne Leerzeichen:</p>
    ${wert(tx('Signatur-Secret'), 'secret', true)}
    <ol><li>„Send test event“ – im Verlauf erscheint „Test-Event“.</li>
      <li>Je Geste eine kurze Aufnahme.</li>
      <li>Kommt nichts an: „Recent runs“ in der Pebble-App zeigt die Antwort (401 = Header oder Signatur falsch).</li></ol>
    <p class="leise">Pebble wiederholt fehlgeschlagene Zustellungen jede Minute, bis zu zehnmal; doppelte erzeugen keinen zweiten Eintrag.</p>`;
}
async function pebbleKlick(b, d) {
  if (b.dataset.zeigen) {
    const el = document.getElementById(T`wert-${b.dataset.zeigen}`);
    const offen = b.textContent === tx('Verbergen');
    el.textContent = offen ? '••••••••••••••••' : d[b.dataset.zeigen];
    b.textContent = offen ? tx('Anzeigen') : tx('Verbergen');
    return true;
  }
  if (b.dataset.kopieren) {
    try { await navigator.clipboard.writeText(d[b.dataset.kopieren]); melde(tx('Kopiert')); } catch (f) { melde(tx('Kopieren nicht möglich – Wert anzeigen und von Hand übernehmen')); }
    return true;
  }
  return false;
}

// ---------- Weiteres Gerät: 4-stelliger Code, gültig nur solange diese Seite offen ist ----------

async function zeigeGeraetNeu() {
  seite.innerHTML = T('<p class="leer">Lädt …</p>');
  const d = await api('/api/geraete/code', { method: 'POST' });
  if (aktuell?.name !== 'geraetNeu') { api('/api/geraete/code', { method: 'DELETE' }).catch(() => {}); return; }
  let gueltig = true;
  const male = () => {
    const rest = Math.max(0, Math.round((d.bis - Date.now()) / 1000));
    if (rest === 0) gueltig = false;
    seite.innerHTML = T`<h1>Weiteres Gerät hinzufügen</h1>
      ${gueltig ? T`<p class="geraetecode" aria-label="Code ${d.code.split('').join(' ')}">${esc(d.code)}</p>
      <p class="mitte leise">noch ${Math.floor(rest / 60)}:${String(rest % 60).padStart(2, '0')} min · nur solange diese Seite offen ist</p>`
    : T('<div class="hinweis warn">Der Code ist abgelaufen.</div><div class="leiste"><button class="btn haupt" data-a="neu">Neuen Code erzeugen</button></div>')}
      <ol><li>Auf dem neuen Gerät diese Adresse öffnen: <strong>${esc(location.host)}</strong></li>
        <li>„Mit Code hinzufügen“ antippen und den Code eingeben.</li>
        <li>Namen eingeben und den Passkey anlegen.</li></ol>
      <div class="leiste"><a class="btn" href="/status">Fertig</a></div>`;
  };
  male();
  const uhr = setInterval(() => { if (gueltig) male(); }, 1000);
  // Lebenszeichen: ohne sie verfällt der Code nach 25 s auf dem Server.
  const halten = setInterval(async () => {
    try { const r = await api('/api/geraete/code/halten', { method: 'POST' }); if (!r.gueltig && gueltig) { gueltig = false; male(); melde(tx('Code verbraucht oder abgelaufen')); } } catch { /* offline: verfällt */ }
  }, 10000);
  const weg = () => { fetch('/api/geraete/code', { method: 'DELETE', headers: { 'X-App': '1' }, credentials: 'same-origin', keepalive: true }).catch(() => {}); };
  window.addEventListener('pagehide', weg);
  const stopp = () => { clearInterval(uhr); clearInterval(halten); window.removeEventListener('pagehide', weg); seite.onclick = null; };
  // Neuer Code ersetzt den alten auf dem Server; kein Löschen, das den neuen treffen könnte.
  seite.onclick = (ev) => { if (ev.target.closest('[data-a=neu]')) { stopp(); aufraeumen = null; zeigeGeraetNeu(); } };
  aufraeumen = () => { stopp(); weg(); };
}

// ---------- Darstellung: Tag/Nacht und Farbpalette (theme.js), je Gerät; der Server merkt sich die letzte Wahl ----------

async function zeigeDarstellung() {
  const D = window.darstellung;
  // Vorschau: die letzten 3 Einträge des Verlaufs, wie sie dort aussehen (nur Ansicht, Klicks führen zum Eintrag).
  let beispiele = null;
  let d = { modus: 'auto', palette: 'blau', ...(D.lies() || {}) };
  const male = () => {
    const eigene = D.gueltigeFarbe(d.farbe) ? d.farbe : '#2f6fde';
    seite.innerHTML = T`${statusReiter('darstellung')}<h1>Theme</h1>
      <p class="leise">Gilt auf diesem Gerät. Ein neues Gerät übernimmt beim ersten Öffnen das zuletzt gewählte Theme.</p>
      <h2>Tag/Nacht</h2>
      <nav class="btn-group unter" aria-label="Tag/Nacht">${[['auto', tx('Automatisch')], ['hell', tx('Hell')], ['dunkel', tx('Dunkel')]]
    .map(([m, n]) => T`<button class="btn${d.modus === m ? ' aktiv' : ''}" data-modus="${m}" aria-pressed="${d.modus === m}">${n}</button>`).join('')}</nav>
      <h2>Farbe</h2>
      <div class="farbwahl">${Object.entries(D.PALETTEN).map(([k, [n, hell, dunkel]]) => T`<button class="${d.palette === k ? 'an' : ''}" data-palette="${k}" aria-pressed="${d.palette === k}">
          <span class="farbpunkt" data-hell="${hell}" data-dunkel="${dunkel}"></span>${tx(n)}</button>`).join('')}
        <label class="${d.palette === 'eigene' ? 'an' : ''}"><input type="color" id="eigenefarbe" value="${eigene}" aria-label="Eigene Farbe wählen">Eigene</label></div>
      <h2>Vorschau</h2>
      <div id="vorschau">${beispiele ? beispiele.map(karte).join('') || T('<p class="leer">Noch keine Einträge.</p>') : T('<p class="leer">Lädt …</p>')}</div>
      <div class="leiste"><button class="btn haupt" data-beispiel="1">Beispiel-Knopf</button><button class="btn">Normaler Knopf</button></div>`;
    // Farbpunkte über CSSOM (die CSP sperrt style-Attribute im HTML): links hell, rechts dunkel.
    for (const p of seite.querySelectorAll('.farbpunkt')) p.style.background = T`linear-gradient(135deg, ${p.dataset.hell} 50%, ${p.dataset.dunkel} 50%)`;
  };
  const speichern = async () => {
    D.setze(d);
    male();
    try { await api('/api/darstellung', { method: 'PUT', body: d }); } catch (f) { if (!(f instanceof NetzFehler)) melde(f.message); }
  };
  male();
  apiAbgelegt('/api/verlauf?alle=1').then((v) => { beispiele = (v.eintraege || []).slice(0, 3); if (aktuell?.name === 'darstellung') male(); })
    .catch(() => { beispiele = []; if (aktuell?.name === 'darstellung') male(); });
  seite.onclick = (ev) => {
    const b = ev.target.closest('button');
    if (b?.dataset.beispiel) return;
    if (b?.dataset.modus) { d.modus = b.dataset.modus; speichern(); }
    else if (b?.dataset.palette) { d.palette = b.dataset.palette; speichern(); }
  };
  // input: Vorschau beim Ziehen; change: übernehmen und speichern.
  seite.oninput = (ev) => { if (ev.target.id === 'eigenefarbe') D.anwenden({ ...d, palette: 'eigene', farbe: ev.target.value }); };
  seite.onchange = (ev) => { if (ev.target.id === 'eigenefarbe') { d.palette = 'eigene'; d.farbe = ev.target.value; speichern(); } };
  aufraeumen = () => { seite.onclick = null; seite.oninput = null; seite.onchange = null; };
}

// ---------- Tags verwalten ----------

async function zeigeTagsVerwalten() {
  seite.innerHTML = T('<p class="leer">Lädt …</p>');
  const laden = async (vorab) => {
    const d = await apiAbgelegt('/api/tags', vorab);
    if (aktuell?.name !== 'tagsVerwalten') return;
    try { localStorage.setItem('tag-vorrat', JSON.stringify(d.tags)); } catch { /* nur Zwischenspeicher */ }
    maleMenue();
    seite.innerHTML = T`<h1>Tags</h1><p class="leise">Tippen auf einen Tag zeigt seine Einträge. Löschen nimmt ihn aus allen Notizen und Planungen.</p>
      ${d.tags.length ? T`<div class="karte tagverwaltung">${d.tags.map((t) => T`<div class="tagzeile"><span><a class="tagchip" href="/" data-zeige-tag="${esc(t.name)}" title="Einträge mit diesem Tag zeigen">${esc(t.name)}</a><br><span class="leise">${t.anzahl} ${t.anzahl === 1 ? tx('Eintrag') : tx('Einträge')}${t.zuletzt ? T` · zuletzt ${kurz(t.zuletzt)}` : ''}</span></span>
        <button class="btn klein gefahr" data-tag="${esc(t.name)}">Löschen</button></div>`).join('')}</div>` : T('<p class="leer">Noch keine Tags.</p>')}`;
  };
  aktuell.neuLaden = laden;
  seite.onclick = async (ev) => {
    const z = ev.target.closest('[data-zeige-tag]');
    if (z) {
      // Verlauf mit genau diesem Tag (alle Arten); der globale Link-Klick öffnet danach „/“.
      try { localStorage.setItem('verlauf-tags', JSON.stringify([z.dataset.zeigeTag])); localStorage.setItem('verlauf-art', 'alles'); } catch { /* leer */ }
      verlaufStand = null;
      return;
    }
    const b = ev.target.closest('button[data-tag]');
    if (!b) return;
    if (!confirm(T`Tag „${b.dataset.tag}“ aus allen Einträgen entfernen?`)) return;
    b.disabled = true;
    try {
      const r = await api(T`/api/tags/${encodeURIComponent(b.dataset.tag)}`, { method: 'DELETE' });
      // Gewählter Tag-Filter im Verlauf ohne den gelöschten Tag.
      try { localStorage.setItem('verlauf-tags', JSON.stringify(gewaehlteTags().filter((t) => t.toLowerCase() !== b.dataset.tag.toLowerCase()))); } catch { /* leer */ }
      verlaufStand = null;
      melde(T`„${b.dataset.tag}“ gelöscht (${r.eintraege} ${r.eintraege === 1 ? tx('Eintrag') : tx('Einträge')})`);
      await laden();
    } catch (f) { b.disabled = false; melde(f.message); }
  };
  aufraeumen = () => { seite.onclick = null; };
  await laden(true).catch(() => {});
  await laden();
}

// ---------- Anmeldung (Passkey) ----------

function optionenAusJson(o, anlegen) {
  const pk = { ...o.publicKey, challenge: b64zuBytes(o.publicKey.challenge) };
  if (anlegen) {
    pk.user = { ...pk.user, id: b64zuBytes(pk.user.id) };
    pk.excludeCredentials = (pk.excludeCredentials || []).map((c) => ({ ...c, id: b64zuBytes(c.id) }));
  } else {
    pk.allowCredentials = (pk.allowCredentials || []).map((c) => ({ ...c, id: b64zuBytes(c.id) }));
  }
  return { publicKey: pk };
}
function credZuJson(c) {
  const r = c.response;
  const antwort = { clientDataJSON: bytesZuB64(r.clientDataJSON) };
  if (r.attestationObject) {
    antwort.attestationObject = bytesZuB64(r.attestationObject);
    if (r.getTransports) antwort.transports = r.getTransports();
  } else {
    antwort.authenticatorData = bytesZuB64(r.authenticatorData);
    antwort.signature = bytesZuB64(r.signature);
    if (r.userHandle) antwort.userHandle = bytesZuB64(r.userHandle);
  }
  return { id: c.id, rawId: bytesZuB64(c.rawId), type: c.type, response: antwort, clientExtensionResults: c.getClientExtensionResults?.() || {}, authenticatorAttachment: c.authenticatorAttachment || undefined };
}

// Freigabe für eine App (OAuth für den MCP-Server): Name und Ziel zeigen, Erlauben oder Ablehnen.
async function zeigeVerbinden() {
  const anfrage = location.search;
  seite.innerHTML = T('<p class="leer">Lädt …</p>');
  let d;
  try { d = await api(T`/api/oauth/anfrage${anfrage}`); } catch (f) {
    seite.innerHTML = T`<div class="mitte"><h1>App verbinden</h1><div class="hinweis schlecht">${esc(f.message)}</div></div>`;
    return;
  }
  seite.innerHTML = T`<div class="mitte"><h1>App verbinden</h1>
    <p><strong>${esc(d.name)}</strong> möchte auf deine Sprachnotizen zugreifen: Listen und Punkte lesen, Punkte abhaken oder wieder öffnen.</p>
    <p class="leise">Weiterleitung danach an <strong>${esc(d.ziel)}</strong>. Trennen jederzeit unter Status → Verbundene Apps.</p>
    <div class="leiste"><button class="btn haupt" data-erlauben="1">Erlauben</button><button class="btn" data-erlauben="0">Ablehnen</button></div></div>`;
  seite.onclick = async (ev) => {
    const b = ev.target.closest('button[data-erlauben]');
    if (!b) return;
    seite.querySelectorAll('button').forEach((x) => { x.disabled = true; });
    try {
      const r = await api('/api/oauth/freigabe', { method: 'POST', body: { anfrage, erlauben: b.dataset.erlauben === '1' } });
      location.href = r.weiter;
    } catch (f) { melde(f.message); seite.querySelectorAll('button').forEach((x) => { x.disabled = false; }); }
  };
  aufraeumen = () => { seite.onclick = null; };
}

async function zeigeAnmelden() {
  const s = await api('/api/sitzung').catch(() => null);
  if (s?.angemeldet) return geheZu('/', true);
  seite.innerHTML = T`<div class="mitte"><h1>Anmelden</h1>
    ${s && !s.eingerichtet ? T('<div class="hinweis warn">Noch kein Passkey eingerichtet. Auf dem Server <code>pebble-webhook enroll</code> ausführen und den Einmal-Link öffnen.</div>') : ''}
    <p class="leise">Anmeldung mit dem Passkey dieses Geräts.</p>
    <p><button class="btn haupt" id="los"${window.PublicKeyCredential ? '' : ' disabled'}>Mit Passkey anmelden</button></p>
    ${window.PublicKeyCredential ? '' : T('<p class="leise">Dieser Browser unterstützt keine Passkeys.</p>')}
    <h2>Neues Gerät?</h2>
    <p class="leise">Auf einem angemeldeten Gerät unter Status → „Weiteres Gerät hinzufügen“ den Code anzeigen lassen.</p>
    <form id="codeform" class="codeform"><label for="code">Code</label>
      <input type="text" id="code" inputmode="numeric" pattern="[0-9]{4}" maxlength="4" autocomplete="one-time-code" placeholder="0000" required>
      <button class="btn" type="submit">Mit Code hinzufügen</button></form></div>`;
  // Höchstens ein Versuch je 5 Sekunden (der Server prüft das auch); der Knopf zählt herunter.
  const knopf = document.querySelector('#codeform button');
  let sperre = null;
  const warte = (sek) => {
    clearInterval(sperre);
    let rest = sek;
    const zeige = () => { knopf.disabled = rest > 0; knopf.textContent = rest > 0 ? T`Nächster Versuch in ${rest} s` : tx('Mit Code hinzufügen'); };
    zeige();
    sperre = setInterval(() => { rest--; zeige(); if (rest <= 0) clearInterval(sperre); }, 1000);
  };
  aufraeumen = () => clearInterval(sperre);
  document.getElementById('codeform').onsubmit = async (ev) => {
    ev.preventDefault();
    knopf.disabled = true;
    try {
      const r = await api('/api/auth/code', { method: 'POST', body: { code: document.getElementById('code').value } });
      geheZu(T`/einrichten#${r.token}`);
    } catch (f) {
      melde(f.message);
      warte(f instanceof ApiFehler && f.code === 'warten' ? Number(f.message.match(/\d+/)?.[0] || 5) : 5);
    }
  };
  document.getElementById('los').onclick = async (ev) => {
    ev.target.disabled = true;
    try {
      const start = await api('/api/auth/anmelden/start', { method: 'POST' });
      const cred = await navigator.credentials.get(optionenAusJson(start.optionen, false));
      await api(T`/api/auth/anmelden/ende?vorgang=${start.vorgang}`, { method: 'POST', body: credZuJson(cred) });
      besuche.anmeldung();
      starteLive();
      let weiter = '/';
      try { weiter = sessionStorage.getItem('nach-anmeldung') || '/'; sessionStorage.removeItem('nach-anmeldung'); } catch { /* Startseite */ }
      geheZu(weiter.startsWith('/verbinden?') ? weiter : '/', true);
    } catch (f) {
      ev.target.disabled = false;
      melde(f.name === 'NotAllowedError' ? tx('Anmeldung abgebrochen') : f.message);
    }
  };
}

async function zeigeEinrichten() {
  const token = location.hash.slice(1);
  seite.innerHTML = T`<h1>Passkey einrichten</h1>
    <p class="leise">Dieses Gerät bekommt einen Passkey. Ein Passwort gibt es nicht.</p>
    <label for="name">Name des Geräts</label><input type="text" id="name" maxlength="40" placeholder="z. B. Handy">
    <div class="leiste"><button class="btn haupt" id="los"${token && window.PublicKeyCredential ? '' : ' disabled'}>Passkey anlegen</button></div>
    ${token ? '' : T('<div class="hinweis schlecht">Der Link enthält keinen Einrichtungs-Code.</div>')}`;
  document.getElementById('los').onclick = async (ev) => {
    ev.target.disabled = true;
    try {
      const start = await api('/api/auth/einrichten/start', { method: 'POST', body: { token, name: document.getElementById('name').value } });
      const cred = await navigator.credentials.create(optionenAusJson(start.optionen, true));
      await api(T`/api/auth/einrichten/ende?vorgang=${start.vorgang}`, { method: 'POST', body: credZuJson(cred) });
      besuche.anmeldung();
      melde(tx('Passkey angelegt'));
      starteLive();
      geheZu('/', true);
    } catch (f) {
      ev.target.disabled = false;
      melde(f.name === 'NotAllowedError' ? tx('Einrichtung abgebrochen') : f.message);
    }
  };
}

// ---------- Menü (Hamburger links): drei Hauptseiten, darunter ihre Unterseiten ----------

// Seite → Bereich im Menü.
const BEREICH = { joplin: 'status', einstellungBearbeiten: 'status', darstellung: 'status', geraetNeu: 'status', tagsVerwalten: 'status', kosten: 'status', papierkorb: 'status', update: 'status', backups: 'status', einstellungen: 'status', einstellungenBearbeiten: 'status', pebble: 'status', gedaechtnis: 'status', gedaechtnisBearbeiten: 'status', liste: 'listen', verschieben: 'listen', listeBearbeiten: 'listen', gruppeBearbeiten: 'listen', eintrag: 'verlauf', planung: 'verlauf', tags: 'verlauf', antwort: 'verlauf', umhaengen: 'verlauf', wortlaut: 'verlauf', einsortieren: 'verlauf', suche: 'verlauf' };
const lokal = (k, v = '') => { try { return localStorage.getItem(k) ?? v; } catch { return v; } };
function maleMenue() {
  const name = aktuell?.name || '';
  // Notiz oder Planung aus einer Liste geöffnet: Bereich Listen.
  const bereich = herkunft && DETAIL.includes(name) ? 'listen' : BEREICH[name] || name;
  // Listen-Gruppen [Name, offene Punkte], neueste zuerst; im Menü die letzten 5 und „Ohne Gruppe“.
  let gruppen = [], zahlen = {};
  try { gruppen = JSON.parse(lokal('listen-gruppen', '[]')).filter(Array.isArray); } catch { /* noch nie geladen */ }
  let neuZahlen = {};
  try { zahlen = JSON.parse(lokal('verlauf-zahlen', 'null')) || {}; neuZahlen = JSON.parse(lokal('verlauf-neu', 'null')) || {}; } catch { /* noch nie geladen */ }
  // Gelb (gedimmt): neu seit zuletzt gesehen, wie im Verlauf markiert.
  const neuZahl = (n) => (n > 0 ? T` <span class="zaehler neu" title="${tx('{0} neu', n)}">${n}</span>` : '');
  gruppen = [...gruppen.filter(([g]) => g !== '-').slice(0, 5), ...gruppen.filter(([g]) => g === '-')];
  let tagZahl;
  try { tagZahl = JSON.parse(lokal('tag-vorrat', 'null'))?.length; } catch { /* noch nie geladen */ }
  const art = lokal('verlauf-art', 'alles'), gruppe = name === 'listen' && aktuell.arg ? decodeURIComponent(aktuell.arg) : '';
  // Zähler rechts an jedem zählbaren Punkt, auch 0 (dann grau).
  const zahl = (n, still) => (typeof n === 'number' ? T` <span class="zaehler${still || !n ? ' still' : ''}">${n}</span>` : '');
  const teile = [
    { id: 'verlauf', name: tx('Verlauf'), href: '/', setze: ['verlauf-art', 'alles'], unter: ARTEN.map(([a, n]) => ({ name: a === 'termin' ? tx('Termine') : n, href: '/', setze: ['verlauf-art', a], an: name === 'verlauf' && art === a, extra: (a === 'fragen' ? '' : neuZahl(neuZahlen[a])) + zahl(zahlen[a] ?? 0, a !== 'fragen') })) },
    { id: 'listen', name: tx('Listen'), href: '/listen', unter: gruppen.map(([g, offen, neu]) => ({ name: gruppenTitel(g), href: gruppenPfad(g), an: !!gruppe && gruppe.toLowerCase() === g.toLowerCase(), extra: neuZahl(neu) + zahl(offen, true) })) },
    { id: 'status', name: tx('Einstellungen'), href: '/status', unter: [
      { name: tx('Status'), href: '/status', an: ['status', 'pebble', 'joplin', 'einstellungen', 'einstellungBearbeiten', 'geraetNeu', 'darstellung'].includes(name), extra: audioFehler ? T` <span class="zaehler fehler" title="${tx('{0} Aufnahmen ohne Eintrag', audioFehler)}">${audioFehler}</span>` : '' },
      { name: tx('Updates'), href: '/status/update', an: name === 'update', extra: neueVersion ? ` ${updateSymbol}` : '' },
      { name: tx('Backups'), href: '/status/backups', an: name === 'backups' },
      { name: tx('Gedächtnis'), href: '/status/gedaechtnis', an: name === 'gedaechtnis' || name === 'gedaechtnisBearbeiten', extra: zahl(offeneVorschlaege) },
      { name: tx('Tags'), href: '/status/tags', an: name === 'tagsVerwalten', extra: zahl(tagZahl, true) },
      { name: tx('KI-Kosten'), href: '/status/kosten', an: name === 'kosten' },
      { name: tx('Papierkorb'), href: '/status/papierkorb', an: name === 'papierkorb', extra: zahl(papierkorbZahl, true) },
    ] },
  ];
  // Verlauf-Filter ohne Einträge (Zähler 0) sind nicht anklickbar: der Verlauf fiele sonst auf „Alles“ zurück.
  const link = (k, x) => (x.leer && !x.an ? T`<span class="${k} gesperrt" aria-disabled="true"><span>${esc(x.name)}</span>${x.extra || ''}</span>` : linkAktiv(k, x));
  const linkAktiv = (k, x) => T`<a class="${k}${x.an ? ' aktiv' : ''}" href="${x.href}"${x.setze ? T` data-setze="${x.setze[0]}" data-wert="${esc(x.setze[1])}"` : ''}${x.an ? ' aria-current="page"' : ''}><span>${esc(x.name)}</span>${x.extra || ''}</a>`;
  nav.innerHTML = T`<div class="menuekopf"><span class="marke">Sprachnotizen</span><button type="button" class="btn klein rund" data-menue-zu aria-label="Menü schließen">×</button></div>
    ${teile.map((t) => T`<div class="menuegruppe">${link('menuehaupt', { ...t, an: bereich === t.id })}${t.unter.map((u) => link('menueunter', u)).join('')}</div>`).join('')}
    <div class="menuegruppe"><button type="button" class="menueunter abmelden" data-menue-abmelden><span>Abmelden</span></button></div>
    <div class="menuefuss"><div class="menuefusszeile">${appVersion ? T`<a href="/status/update">Version ${esc(versionNr(appVersion))}${appKanal ? ` · ${esc(tx(appKanal))}` : ''}</a>` : '<span></span>'}<a class="themeknopf" href="/status/darstellung" title="Theme" aria-label="Theme"><svg viewBox="0 0 24 24" aria-hidden="true"><circle cx="12" cy="12" r="9" fill="none" stroke="currentColor" stroke-width="2"/><path d="M12 3a9 9 0 0 0 0 18z" fill="currentColor"/></svg></a></div></div>`;
  const punkt = menueKnopf.querySelector('.punkt');
  punkt.hidden = !neueVersion && !offeneVorschlaege && !audioFehler;
  punkt.classList.toggle('vorschlag', !neueVersion && !audioFehler);
  punkt.classList.toggle('fehler', !!audioFehler);
  menueKnopf.title = audioFehler ? tx('{0} Aufnahmen ohne Eintrag', audioFehler) : neueVersion ? T`Update ${versionNr(neueVersion)} verfügbar` : offeneVorschlaege ? T`${offeneVorschlaege} Gedächtnis-Vorschläge` : '';
}
menueKnopf.onclick = () => { maleMenue(); menue.showModal(); };
// Breite Bildschirme: Menü fest links sichtbar (nicht modal), ohne Menüknopf; schmal: Klappmenü.
const breit = matchMedia('(min-width: 1100px)');
function menueModus() {
  const fest = breit.matches && !menueKnopf.hidden;
  if (fest && menue.matches(':modal')) menue.close();
  if (fest && !menue.open) { maleMenue(); menue.show(); }
  if (!fest && menue.open && !menue.matches(':modal')) menue.close();
  document.body.classList.toggle('mitmenue', fest);
}
breit.addEventListener('change', menueModus);
// Schließen: ×, Klick neben das Menü (auf den Hintergrund), Esc (Dialog selbst).
menue.addEventListener('click', (e) => { if (menue.matches(':modal') && (e.target === menue || e.target.closest('[data-menue-zu]'))) menue.close(); });
menue.addEventListener('click', async (e) => {
  const b = e.target.closest('[data-menue-abmelden]');
  if (!b || !confirm(tx('Auf diesem Gerät abmelden?'))) return;
  b.disabled = true;
  try { await api('/api/auth/abmelden', { method: 'POST' }); try { localStorage.setItem('angemeldet', '0'); } catch { /* nur Startabkürzung */ } if (menue.open) menue.close(); geheZu('/anmelden', true); } catch (f) { b.disabled = false; melde(f.message); }
});

// Offene Gedächtnis-Vorschläge: Zahl an „Gedächtnis“ im Menü, Punkt am Menüknopf.
let offeneVorschlaege = 0;
const vorschlagZahl = () => (offeneVorschlaege ? ` (${offeneVorschlaege})` : '');
// Neue Version (Hintergrund-Prüfung alle 4 h): Punkt am Menüknopf, Pfeil an „Updates“, Hinweiskarte auf der Statusseite.
let neueVersion = '', papierkorbZahl = 0, audioFehler = 0, appVersion = lokal('app-version'), appKanal = lokal('app-kanal');
const updateSymbol = T('<svg class="updatesymbol" viewBox="0 0 24 24" aria-hidden="true"><circle cx="12" cy="12" r="11" fill="currentColor"/><path d="M12 17V7m-4.5 4.5L12 7l4.5 4.5" fill="none" stroke="var(--flaeche)" stroke-width="2.4" stroke-linecap="round" stroke-linejoin="round"/></svg>');
// Versionsnummer ohne Kanal-Endung: der Kanal steht als Badge daneben, Builds unterscheidet das Datum.
const versionNr = (v) => String(v || '').replace(/-(stable|(beta|nightly)\.\d+)$/, '');
const versionKanal = (v) => (String(v || '').match(/-(stable|beta|nightly)(\.\d+)?$/) || [])[1] || '';
// Build-Zeit einer Nightly (Endung JJJJMMTTHHMM) als Text, sonst ''.
const versionZeit = (v) => {
  const m = String(v || '').match(/-nightly\.(\d{4})(\d\d)(\d\d)(\d\d)(\d\d)$/);
  return m ? new Date(Date.UTC(+m[1], m[2] - 1, +m[3], +m[4], +m[5])).toLocaleString(LOCALE, { day: '2-digit', month: '2-digit', year: 'numeric', hour: '2-digit', minute: '2-digit' }) : '';
};
// Version mit festem Kanal-Badge (stable/beta/nightly) und bei Nightlies der Build-Zeit.
const versionHtml = (v, kanal) => {
  const k = kanal || versionKanal(v), z = versionZeit(v);
  return T`${k ? T`<span class="badge neutral">${esc(tx(k))}</span> ` : ''}<span class="versionnr">${esc(versionNr(v))}</span>${z ? T` <span class="leise">${esc(z)}</span>` : ''}`;
};
// Neue Version seit der letzten Bestätigung (z. B. automatisches Update): Karte über jeder Seite, bis sie bestätigt ist.
// Gilt für alle Geräte gemeinsam (ein Nutzer): der Server merkt sich die bestätigte Version.
let versionBestaetigt = lokal('version-bestaetigt');
function zeigeVersionskarte() {
  const karte = document.getElementById('versionskarte');
  const jetzt = `${appVersion}|${appKanal}`;
  karte.hidden = !appVersion || !versionBestaetigt || versionBestaetigt === jetzt;
  if (karte.hidden) return;
  const [altV, altK] = versionBestaetigt.split('|');
  karte.innerHTML = T`<div class="versionszeile">${updateSymbol}<div><strong>${tx('App aktualisiert')}</strong><div class="versionswechsel"><div>${versionHtml(altV, altK)}</div><div>→ ${versionHtml(appVersion, appKanal)}</div></div><a href="/status/update">${tx('Änderungen ansehen')}</a></div></div>
    <button type="button" class="btn haupt gross" data-version-ok>${tx('Bestätigen')}</button>`;
  karte.querySelector('[data-version-ok]').onclick = async (ev) => {
    ev.currentTarget.disabled = true;
    try {
      const r = await api('/api/version/bestaetigt', { method: 'POST' });
      versionBestaetigt = r.version_bestaetigt;
      try { localStorage.setItem('version-bestaetigt', versionBestaetigt); } catch { /* nur Anzeige */ }
      karte.hidden = true;
    } catch (f) { ev.currentTarget.disabled = false; melde(f.message); }
  };
}

async function ladeZaehler() {
  try {
    const z = await api('/api/zaehler');
    offeneVorschlaege = z.vorschlaege || 0;
    neueVersion = z.update || '';
    papierkorbZahl = z.papierkorb || 0;
    audioFehler = z.audio_unbekannt || 0;
    joplinAktiv = !!z.joplin;
    try { localStorage.setItem('joplin-aktiv', joplinAktiv ? '1' : '0'); } catch { /* nur Anzeige */ }
    if (z.version) { appVersion = z.version; appKanal = z.kanal || ''; try { localStorage.setItem('app-version', appVersion); localStorage.setItem('app-kanal', appKanal); } catch { /* nur Anzeige */ } }
    if (z.version_bestaetigt) { versionBestaetigt = z.version_bestaetigt; try { localStorage.setItem('version-bestaetigt', versionBestaetigt); } catch { /* nur Anzeige */ } }
    zeigeVersionskarte();
  } catch (f) {
    return; // offline: alte Zahl bleibt
  }
  maleMenue();
}

// ---------- Live-Daten (SSE), Verbindung, Start ----------

let quelle = null, ladeTimer = null;
function starteLive() {
  if (quelle) return;
  quelle = new EventSource('/api/ereignisse');
  quelle.onopen = () => { sendeAusgang(); };
  quelle.onmessage = (ev) => {
    if (String(ev.data).startsWith('gedaechtnis') || ev.data === 'version') ladeZaehler();
    clearTimeout(ladeTimer);
    ladeTimer = setTimeout(() => aktuell?.neuLaden?.(), 300);
  };
  quelle.onerror = () => {
    // Abgemeldet oder offline: der Browser versucht es selbst erneut; bei 401 endet die Quelle.
    if (quelle.readyState === EventSource.CLOSED) { quelle = null; setTimeout(() => { if (navigator.onLine && aktuell && !['anmelden', 'einrichten'].includes(aktuell.name)) starteLive(); }, 15000); }
  };
}

// Verbindung: navigator.onLine allein reicht nicht (WLAN ohne Netz), deshalb zählt auch der letzte Abruf.
let netzDa = true, wiederholTimer = null;
function setzeNetz(da) {
  if (netzDa === da) return;
  netzDa = da;
  zeigeVerbindung();
}
function zeigeVerbindung() {
  const offline = !navigator.onLine || !netzDa;
  document.getElementById('offline').hidden = !offline;
  clearInterval(wiederholTimer);
  // Ohne Verbindung regelmäßig prüfen: Vorgemerktes senden und die Ansicht auffrischen.
  if (offline) wiederholTimer = setInterval(async () => { await sendeAusgang(); aktuell?.neuLaden?.(); if (netzDa) starteLive(); }, 20000);
}
window.addEventListener('online', () => { zeigeVerbindung(); sendeAusgang(); aktuell?.neuLaden?.(); starteLive(); });
window.addEventListener('offline', zeigeVerbindung);
document.addEventListener('visibilitychange', () => {
  if (document.hidden) { besuche.aktiv(); return; }
  if (neueHuelle && !document.querySelector('dialog[open], #seite textarea, #seite input:not([type=search]), #seite select')) { location.reload(); return; }
  besuche.pruefe();
  sendeAusgang();
  aktuell?.neuLaden?.();
});

// Service Worker: hält die Hülle dieser Version offline. Eine neue Version installiert er im Hintergrund;
// die Seite lädt dann beim nächsten Seitenwechsel oder Zurückkehren neu (nie mitten in einer Eingabe).
let neueHuelle = false, swPruefung = 0;
if ('serviceWorker' in navigator) {
  const hatteSw = !!navigator.serviceWorker.controller;
  navigator.serviceWorker.addEventListener('controllerchange', () => { if (hatteSw) neueHuelle = true; });
  navigator.serviceWorker.register('/sw').catch(() => {});
  document.addEventListener('visibilitychange', () => {
    if (document.hidden || Date.now() - swPruefung < 10 * 60 * 1000) return;
    swPruefung = Date.now();
    navigator.serviceWorker.getRegistration().then((r) => r?.update()).catch(() => {});
  });
}

(async () => {
  zeigeVerbindung();
  const pfad = location.pathname;
  // Schon einmal angemeldet: sofort aus dem Zwischenspeicher zeigen, die Sitzung prüft der Hintergrund.
  const vorab = lokal('angemeldet') === '1' && pfad !== '/anmelden' && pfad !== '/einrichten' && pfad !== '/ersteinrichtung';
  if (vorab) zeige();
  if (pfad !== '/anmelden' && pfad !== '/einrichten' && pfad !== '/ersteinrichtung') {
    try {
      const s = await api('/api/sitzung');
      try { localStorage.setItem('angemeldet', s.angemeldet ? '1' : '0'); } catch { /* nur Startabkürzung */ }
      if (!s.angemeldet) {
        // Freigabe für eine App (OAuth): nach der Anmeldung dorthin zurück.
        if (pfad === '/verbinden') try { sessionStorage.setItem('nach-anmeldung', pfad + location.search); } catch { /* dann zur Startseite */ }
        return geheZu('/anmelden', true);
      }
      // Zuletzt gesehen vom Server holen, bevor die erste Seite „Neu“ markiert.
      await besuche.pruefe();
      // Neues Gerät ohne eigene Wahl: die zuletzt gewählte Darstellung übernehmen.
      if (!window.darstellung.lies()) api('/api/darstellung').then((d) => { if (d.modus) window.darstellung.setze(d); }).catch(() => {});
      starteLive();
      sendeAusgang();
    } catch (e) {
      if (!(e instanceof NetzFehler)) throw e; // offline: mit Zwischenspeicher weiter
    }
  }
  if (!vorab) zeige();
})();
