// Sprachnotizen – Web-App ohne Build: Verlauf, Detail, Antwortseite, Status, Offline-Warteschlange.

const seite = document.getElementById('seite');
const nav = document.getElementById('nav');
const menue = document.getElementById('menue');
const menueKnopf = document.getElementById('menueknopf');
const neuKnopf = document.getElementById('neuknopf');

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
// Kurz: „21.11, 18:53“ – Punkt am Ende des Datums weg, das Komma trennt schon (Nutzer 22.11.2026).
const kurz = (ms) => `${new Date(ms).toLocaleDateString(LOCALE, { day: '2-digit', month: '2-digit' }).replace(/\.$/, '')}, ${new Date(ms).toLocaleTimeString(LOCALE, { hour: '2-digit', minute: '2-digit' })}`;
// Verlaufskarte: in den letzten 24 Stunden nur die Uhrzeit, ältere mit Datum.
const kartenZeit = (ms) => (Date.now() - ms < 86_400_000 ? uhr(ms) : kurz(ms));
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
  schluesselwort: tx('per Schlüsselwort „Weiter“ fortgesetzt'), entscheidung: tx('vom Entscheidungsmodell einer offenen Planung zugeordnet'), ki: tx('von der KI einer offenen Planung zugeordnet'), neu: tx('als neue Planung angelegt'),
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
    if (antwort.status === 423 && daten?.grund === 'umschluesselung') { if (aktuell?.name !== 'zugangCode') geheZu('/konto/code', true); }
    else if (antwort.status === 423 && aktuell?.name !== 'wiederherstellen') geheZu('/wiederherstellen', true);
    throw new ApiFehler(antwort.status, daten?.code || '', daten?.fehler || T`Fehler ${antwort.status}`);
  }
  return daten;
}

// Kleines, sicheres Markdown für Rechercheergebnisse: erst maskieren, dann wenige Muster.
// Mit medien: eine Zeile [[medium:N]] zeigt dort das Medium N; nicht platzierte Medien stehen am Ende.
function md(text, medien = null) {
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
      aus.push(T`<div class="tabelle"><table><thead><tr>${kopf.map((z) => T`<th>${z}</th>`).join('')}</tr></thead><tbody>${rest.map((r) => T`<tr>${r.map((z) => T`<td>${z}</td>`).join('')}</tr>`).join('')}</tbody></table></div>`);
      tabelle = null;
    }
  };
  const gezeigt = new Set();
  for (const roh of String(text || '').split('\n')) {
    let m;
    if ((m = roh.trim().match(/^\[\[medium:(\d+)\]\]$/))) {
      zu();
      const me = (medien || []).find((x) => x.nr === Number(m[1]));
      if (me && !gezeigt.has(me)) { gezeigt.add(me); aus.push(mediumHtml(me)); }
      continue;
    }
    const z = roh.replace(/\s*\[\[medium:\d+\]\]/g, '').trim();
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
  for (const me of medien || []) if (!gezeigt.has(me)) aus.push(mediumHtml(me));
  return aus.join('');
}

// ---------- Medien: Bild, Trailer, Diagramm (nur Anzeige) ----------

// Bilder kommen erst beim Anzeigen (loading=lazy) und offline höchstens 7 Tage aus dem Speicher (sw.js).
// Trailer: nur das Vorschaubild; das YouTube-Video lädt erst beim Tippen.
function mediumHtml(m) {
  const titel = m.titel || (m.art === 'trailer' ? tx('Trailer') : '');
  const unter = (quelle) => {
    const teile = [titel ? T`<strong>${esc(titel)}</strong>` : '', m.unterschrift ? esc(m.unterschrift) : '', quelle || ''].filter(Boolean);
    return teile.length ? T`<figcaption>${teile.join(' · ')}</figcaption>` : '';
  };
  if (m.fehler) return T`<figure class="medium fehlt"><figcaption>${titel ? T`<strong>${esc(titel)}</strong> · ` : ''}<span class="leise">${esc(m.art === 'trailer' ? tx('Kein Trailer gefunden') : tx('Nicht verfügbar'))}: ${esc(m.fehler)}</span></figcaption></figure>`;
  if (m.art === 'diagramm') return T`<figure class="medium">${diagrammHtml(m)}${unter('')}</figure>`;
  if (m.art === 'trailer' && m.video) {
    return T`<figure class="medium"><button class="trailer" data-video="${esc(m.video)}" aria-label="${esc(tx('Trailer abspielen: {0}', m.suche || titel))}"><img src="https://i.ytimg.com/vi/${esc(m.video)}/hqdefault.jpg" alt="" loading="lazy"><span class="abspielen" aria-hidden="true"></span></button>${unter(T`<a href="https://www.youtube.com/watch?v=${esc(m.video)}" target="_blank" rel="noopener noreferrer">YouTube</a>`)}</figure>`;
  }
  if (m.bild) {
    let quelle = '';
    if (m.quelle) { try { quelle = T`<a href="${esc(m.quelle)}" target="_blank" rel="noopener noreferrer">${esc(new URL(m.quelle).hostname.replace(/^www\./, ''))}</a>`; } catch { /* ohne Quelle */ } }
    else if (m.art === 'bild_erzeugen') quelle = T`<span class="leise">KI-Bild</span>`;
    return T`<figure class="medium"><a class="bild" href="/api/bilder/${esc(m.bild)}" target="_blank"><img src="/api/bilder/${esc(m.bild)}" alt="${esc(titel)}" loading="lazy"></a>${unter(quelle)}</figure>`;
  }
  return T`<figure class="medium wartet"><figcaption>${titel ? T`<strong>${esc(titel)}</strong> · ` : ''}<span class="leise">${esc(m.art === 'trailer' ? tx('Trailer wird gesucht …') : tx('Bild wird geladen …'))}</span></figcaption></figure>`;
}

// Diagramm aus den Daten der KI als SVG (Balken, Linie, Kreis); darunter die Werte zum Nachlesen.
// Farben über Klassen (die CSP erlaubt keine style-Attribute).
function diagrammHtml(m) {
  const d = m.diagramm || {};
  const namen = d.beschriftungen || [], reihen = d.reihen || [];
  const aria = esc(m.titel || tx('Diagramm'));
  if (d.typ === 'ablauf') {
    // Schritte untereinander mit Pfeilen; Text bricht in bis zu zwei Zeilen um.
    const zeilen = (t) => { const w = t.split(/\s+/); const a = []; let z = ''; for (const x of w) { if ((`${z} ${x}`).trim().length > 34 && z) { a.push(z); z = x; } else z = `${z} ${x}`.trim(); } a.push(z); return a.length > 2 ? [a[0], `${a.slice(1).join(' ').slice(0, 33)}…`] : a; };
    const hoehe = 40, abstand = 18;
    const kaesten = namen.map((t, i) => {
      const y = i * (hoehe + abstand), z = zeilen(t);
      const text = z.map((zz, k) => `<text class="schritt" x="160" y="${(y + hoehe / 2 + (k - (z.length - 1) / 2) * 13 + 4).toFixed(1)}" text-anchor="middle">${esc(zz)}</text>`).join('');
      const pfeil = i < namen.length - 1 ? `<path class="pfeil" d="M160 ${y + hoehe + 2} V${y + hoehe + abstand - 4} M154 ${y + hoehe + abstand - 9} L160 ${y + hoehe + abstand - 3} L166 ${y + hoehe + abstand - 9}"/>` : '';
      return `<rect class="kasten k${i % 8}" x="20" y="${y}" width="280" height="${hoehe}" rx="8"/><circle class="nummer" cx="20" cy="${y + hoehe / 2}" r="10"/><text class="nummerzahl" x="20" y="${y + hoehe / 2 + 4}" text-anchor="middle">${i + 1}</text>${text}${pfeil}`;
    }).join('');
    return T`<svg class="diagramm ablauf" viewBox="0 -2 320 ${namen.length * (hoehe + abstand) - abstand + 4}" role="img" aria-label="${aria}">${kaesten}</svg>`;
  }
  if (!namen.length || !reihen.length) return '';
  const zahl = (v) => `${Number(v).toLocaleString(LOCALE, { maximumFractionDigits: 2 })}${d.einheit ? ` ${d.einheit}` : ''}`;
  const kurz = (t, n) => (t.length > n ? `${t.slice(0, n - 1)}…` : t);
  const legende = (eintraege) => T`<ul class="legende">${eintraege.map(([k, t]) => T`<li><i class="${k}"></i>${esc(t)}</li>`).join('')}</ul>`;
  const werte = T`<details class="werte"><summary>Werte</summary><div class="tabelle"><table class="werte"><thead><tr><th></th>${reihen.map((r) => T`<th>${esc(r.name)}</th>`).join('')}</tr></thead>
    <tbody>${namen.map((n, i) => T`<tr><td>${esc(n)}</td>${reihen.map((r) => T`<td>${esc(zahl(r.werte[i]))}</td>`).join('')}</tr>`).join('')}</tbody></table></div></details>`;
  if (d.typ === 'kreis') {
    const w = reihen[0].werte.map((v) => Math.max(0, Number(v) || 0));
    const summe = w.reduce((a, b) => a + b, 0);
    if (!summe) return '';
    let winkel = -Math.PI / 2;
    const stuecke = w.map((v, i) => {
      const anteil = v / summe, a2 = winkel + anteil * 2 * Math.PI;
      const p = (a) => `${(60 + 55 * Math.cos(a)).toFixed(2)} ${(60 + 55 * Math.sin(a)).toFixed(2)}`;
      const pfad = anteil >= 0.9999 ? '<circle cx="60" cy="60" r="55"/>' : `<path d="M60 60 L${p(winkel)} A55 55 0 ${anteil > 0.5 ? 1 : 0} 1 ${p(a2)} Z"/>`;
      winkel = a2;
      return pfad.replace(/^<(\w+)/, `<$1 class="k${i % 8}"`);
    }).join('');
    return T`<svg class="diagramm torte" viewBox="0 0 120 120" role="img" aria-label="${aria}">${stuecke}</svg>
      ${legende(namen.map((n, i) => [`k${i % 8}`, `${n}: ${zahl(w[i])} (${Math.round((w[i] / summe) * 100)} %)`]))}`;
  }
  if (d.typ === 'balken_quer') {
    // Rangfolge: eine Zeile je Beschriftung, Name links, Balken rechts, Wert am Balkenende.
    const zeile = 16 * reihen.length + 8, LB = 104, BB = 320 - LB - 52;
    const maxW = Math.max(...reihen.flatMap((r) => r.werte.map((v) => Math.abs(Number(v) || 0))), 1);
    const inhalt = namen.map((t, i) => {
      const y = i * zeile;
      return `<text class="achse" x="${LB - 6}" y="${y + zeile / 2 + 3}" text-anchor="end">${esc(kurz(t, 18))}</text>${reihen.map((r, k) => {
        const w = (Math.abs(Number(r.werte[i]) || 0) / maxW) * BB, yy = y + 4 + k * 16;
        return `<rect class="s${k + 1}" x="${LB}" y="${yy}" width="${Math.max(1, w).toFixed(1)}" height="13" rx="1.5"/><text class="achse" x="${(LB + w + 4).toFixed(1)}" y="${yy + 10}">${esc(zahl(r.werte[i]))}</text>`;
      }).join('')}`;
    }).join('');
    return T`<svg class="diagramm" viewBox="0 0 320 ${namen.length * zeile}" role="img" aria-label="${aria}">${inhalt}</svg>
      ${reihen.length > 1 ? legende(reihen.map((r, k) => [`s${k + 1}`, r.name])) : ''}${werte}`;
  }
  // Achsen: Balken, gestapelte Balken und Linie teilen Raster und Beschriftung.
  const B = 320, H = 196, L = 44, R = 8, O = 14, U = 34;
  const gestapelt = d.typ === 'gestapelt' && reihen.length > 1;
  const alle = gestapelt ? namen.flatMap((_, i) => [reihen.reduce((a, r) => a + Math.max(0, Number(r.werte[i]) || 0), 0), Math.min(0, ...reihen.map((r) => Number(r.werte[i]) || 0))]) : reihen.flatMap((r) => r.werte.map(Number));
  let lo = Math.min(0, ...alle), hi = Math.max(0, ...alle);
  if (hi === lo) hi = lo + 1;
  const schritt = (() => { const roh = (hi - lo) / 4, e = 10 ** Math.floor(Math.log10(roh)), f = roh / e; return (f <= 1 ? 1 : f <= 2 ? 2 : f <= 5 ? 5 : 10) * e; })();
  lo = Math.floor(lo / schritt) * schritt; hi = Math.ceil(hi / schritt) * schritt;
  const y = (v) => O + (H - O - U) * (1 - (v - lo) / (hi - lo));
  const breite = (B - L - R) / namen.length;
  let gitter = '';
  for (let v = lo; v <= hi + schritt / 2; v += schritt) {
    gitter += `<line class="gitter" x1="${L}" x2="${B - R}" y1="${y(v).toFixed(1)}" y2="${y(v).toFixed(1)}"/><text class="achse" x="${L - 4}" y="${(y(v) + 3).toFixed(1)}" text-anchor="end">${esc(Number(v.toFixed(10)).toLocaleString(LOCALE, { notation: Math.abs(hi) >= 1e5 ? 'compact' : 'standard', maximumFractionDigits: 2 }))}</text>`;
  }
  const achseX = namen.map((t, i) => `<text class="achse" x="${(L + breite * (i + 0.5)).toFixed(1)}" y="${H - U + 13}" text-anchor="middle">${esc(kurz(t, namen.length > 6 ? 6 : 12))}</text>`).join('');
  let marken = '';
  if (d.typ === 'linie') {
    marken = reihen.map((r, k) => {
      const pkt = r.werte.map((v, i) => [L + breite * (i + 0.5), y(Number(v))]);
      return `<polyline class="linie l${k + 1}" points="${pkt.map(([a, b]) => `${a.toFixed(1)},${b.toFixed(1)}`).join(' ')}"/>${pkt.map(([a, b]) => `<circle class="s${k + 1}" cx="${a.toFixed(1)}" cy="${b.toFixed(1)}" r="2.6"/>`).join('')}`;
    }).join('');
  } else if (gestapelt) {
    const bb = breite * 0.6;
    marken = namen.map((_, i) => {
      let basis = 0;
      return reihen.map((r, k) => {
        const v = Math.max(0, Number(r.werte[i]) || 0), y0 = y(basis), y1 = y(basis + v);
        basis += v;
        return `<rect class="s${k + 1}" x="${(L + breite * i + (breite - bb) / 2).toFixed(1)}" y="${y1.toFixed(1)}" width="${bb.toFixed(1)}" height="${Math.max(0, y0 - y1).toFixed(1)}"/>`;
      }).join('');
    }).join('');
  } else {
    const innen = breite * 0.78, bb = innen / reihen.length;
    marken = reihen.map((r, k) => r.werte.map((v, i) => {
      const x = L + breite * i + (breite - innen) / 2 + bb * k, y0 = y(0), y1 = y(Number(v));
      return `<rect class="s${k + 1}" x="${x.toFixed(1)}" y="${Math.min(y0, y1).toFixed(1)}" width="${Math.max(1, bb - 1).toFixed(1)}" height="${Math.max(0.5, Math.abs(y1 - y0)).toFixed(1)}" rx="1.5"/>`;
    }).join('')).join('');
  }
  const einheit = d.einheit ? `<text class="achse" x="2" y="${O - 3}">${esc(d.einheit)}</text>` : '';
  return T`<svg class="diagramm" viewBox="0 0 ${B} ${H}" role="img" aria-label="${aria}">${gitter}${marken}${achseX}${einheit}</svg>
    ${reihen.length > 1 ? legende(reihen.map((r, k) => [`s${k + 1}`, r.name])) : ''}${werte}`;
}

document.addEventListener('click', (e) => {
  const t = e.target.closest('button.trailer[data-video]');
  if (!t) return;
  const id = t.dataset.video;
  if (!/^[A-Za-z0-9_-]{11}$/.test(id)) return;
  const f = document.createElement('iframe');
  f.className = 'trailer';
  f.src = `https://www.youtube-nocookie.com/embed/${id}?autoplay=1&rel=0&hl=${SPRACHE}&cc_lang_pref=${SPRACHE}`;
  f.title = t.getAttribute('aria-label') || 'YouTube';
  f.allow = 'autoplay; encrypted-media; picture-in-picture; fullscreen';
  f.allowFullscreen = true;
  // YouTube verlangt beim Einbetten den Referrer (sonst Fehler 153); die Seite selbst sendet keinen.
  f.referrerPolicy = 'strict-origin-when-cross-origin';
  t.replaceWith(f);
});

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
        } else if (p.typ === 'notiz' || p.typ === 'anweisung') {
          const form = new FormData();
          form.set('client_id', p.client_id);
          form.set('zeit', String(p.zeit));
          if (p.kontext) form.set('kontext', p.kontext);
          if (p.audio) form.set('audio', p.audio, p.typ);
          else form.set('text', p.text);
          await api(p.typ === 'notiz' ? '/api/notizen' : T`/api/eintraege/${p.eintrag}/anweisung`, { method: 'POST', body: form });
          if (p.typ === 'anweisung') { ladeZaehler(); aktuell?.neuLaden?.(); }
        } else if (p.typ === 'notizantwort') {
          await api(T`/api/eintraege/${p.eintrag}/antwort`, { method: 'POST', body: { antworten: p.antworten, zeit: p.zeit } });
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
        } else if (e instanceof ApiFehler && e.status === 409 && p.typ !== 'anweisung') {
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

// ---------- Angemeldeter Nutzer ----------

// Name und Rolle (für „Hallo …“ und die Systemverwaltung nur bei Admins); offline aus dem letzten Stand.
let ich = (() => { try { return JSON.parse(localStorage.getItem('ich')) || {}; } catch { return {}; } })();
const istAdmin = () => ich.rolle === 'admin';
function merkeIch(s) {
  ich = { id: s.id, name: s.name || '', rolle: s.rolle || '' };
  try { localStorage.setItem('ich', JSON.stringify(ich)); } catch { /* nur Anzeige */ }
}
// vergissGeraet: Zwischenspeicher des Nutzers auf diesem Gerät löschen (Abmelden, Nutzerwechsel); das Theme bleibt.
async function vergissGeraet() {
  let theme = null;
  try { theme = localStorage.getItem('darstellung'); localStorage.clear(); if (theme) localStorage.setItem('darstellung', theme); localStorage.setItem('angemeldet', '0'); } catch { /* nichts gespeichert */ }
  ich = {};
  try { (await dbZusage)?.close(); } catch { /* nie geöffnet */ }
  dbZusage = null;
  await new Promise((ok) => { const r = indexedDB.deleteDatabase('sprachnotizen'); r.onsuccess = r.onerror = r.onblocked = () => ok(); });
  try { await caches.delete('bilder'); } catch { /* ohne Cache-API */ }
}

// abmelden: Push-Abo dieses Geräts beenden (sonst kämen Meldungen des Nutzers weiter an), abmelden, Gerät vergessen.
async function abmelden() {
  try {
    const sub = await (await navigator.serviceWorker?.getRegistration())?.pushManager?.getSubscription();
    if (sub) {
      await api('/api/push/abo/entfernen', { method: 'POST', body: { endpoint: sub.endpoint } }).catch(() => {});
      await sub.unsubscribe().catch(() => {});
    }
  } catch { /* ohne Push */ }
  await api('/api/auth/abmelden', { method: 'POST' });
  await vergissGeraet();
}

// nachAnmeldung: Meldet sich ein anderer Nutzer an als zuletzt auf diesem Gerät, erst alles des alten löschen –
// auch vorgemerkte Aktionen, die sonst im Konto des neuen landeten.
async function nachAnmeldung() {
  const s = await api('/api/sitzung');
  if (ich.id && s.id !== ich.id) await vergissGeraet();
  merkeIch(s);
  return s;
}

// ---------- Router ----------

const routen = [
  [/^\/$/, 'verlauf', zeigeVerlauf],
  [/^\/suche$/, 'suche', zeigeSuche],
  [/^\/eintrag\/([0-9a-f]+)$/, 'eintrag', zeigeEintrag],
  [/^\/eintrag\/([0-9a-f]+)\/umhaengen$/, 'umhaengen', zeigeUmhaengen],
  [/^\/eintrag\/([0-9a-f]+)\/wortlaut$/, 'wortlaut', zeigeWortlaut],
  [/^\/eintrag\/([0-9a-f]+)\/anweisung$/, 'anweisung', (id) => eingabeVonAdresse(T`/eintrag/${id}`, () => eingabeAnweisung(id))],
  [/^\/eintrag\/([0-9a-f]+)\/liste$/, 'einsortieren', (id) => zeigeEinsortieren(id, 'eintrag')],
  [/^\/planung\/([0-9a-f]+)\/liste$/, 'einsortieren', (id) => zeigeEinsortieren(id, 'planung')],
  [/^\/planung\/([0-9a-f]+)$/, 'planung', zeigePlanung],
  [/^\/planung\/([0-9a-f]+)\/antwort$/, 'antwort', (id) => eingabeVonAdresse(T`/planung/${id}`, () => eingabeAntwort(id))],
  [/^\/tags\/(eintrag|planung)\/([0-9a-f]+)$/, 'tags', zeigeTags],
  [/^\/listen$/, 'listen', () => zeigeListen('')],
  [/^\/listen\/gruppe\/([^/]+)$/, 'listen', (g) => zeigeListen(decodeURIComponent(g))],
  [/^\/liste\/([0-9a-f]+)$/, 'liste', zeigeListe],
  [/^\/liste\/([0-9a-f]+)\/bearbeiten$/, 'listeBearbeiten', zeigeListeBearbeiten],
  [/^\/gruppe\/([^/]+)\/bearbeiten$/, 'gruppeBearbeiten', (n) => zeigeGruppeBearbeiten(decodeURIComponent(n))],
  [/^\/status$/, 'status', zeigeStatus],
  [/^\/status\/kosten$/, 'kosten', zeigeKosten],
  [/^\/status\/ring$/, 'ring', zeigeRing],
  [/^\/status\/ring\/(\d+)\/bearbeiten$/, 'ringBearbeiten', zeigeRingBearbeiten],
  [/^\/status\/statistik$/, 'statistik', zeigeStatistik],
  [/^\/status\/rueckmeldung$/, 'rueckmeldung', zeigeRueckmeldung],
  [/^\/status\/papierkorb$/, 'papierkorb', zeigePapierkorb],
  [/^\/status\/gedaechtnis$/, 'gedaechtnis', zeigeGedaechtnis],
  [/^\/status\/gedaechtnis\/neu$/, 'gedaechtnisBearbeiten', () => zeigeGedaechtnisBearbeiten('')],
  [/^\/status\/gedaechtnis\/([0-9a-f]+)\/bearbeiten$/, 'gedaechtnisBearbeiten', zeigeGedaechtnisBearbeiten],
  [/^\/status\/gedaechtnis\/begriff\/neu$/, 'gedaechtnisBearbeiten', () => zeigeBegriffBearbeiten('')],
  [/^\/status\/gedaechtnis\/begriff\/([0-9a-f]+)\/bearbeiten$/, 'gedaechtnisBearbeiten', zeigeBegriffBearbeiten],
  [/^\/pebble$/, 'pebble', zeigePebble],
  [/^\/status\/tags$/, 'tagsVerwalten', zeigeTagsVerwalten],
  [/^\/konto\/theme$/, 'darstellung', zeigeDarstellung],
  [/^\/status\/darstellung$/, 'umzug', () => geheZu('/konto/theme', true)],
  [/^\/status\/joplin$/, 'joplin', zeigeJoplin],
  [/^\/konto\/benachrichtigungen$/, 'benachrichtigungen', zeigeBenachrichtigungen],
  [/^\/status\/benachrichtigungen$/, 'umzug', () => geheZu('/konto/benachrichtigungen', true)],
  [/^\/anmelden$/, 'anmelden', zeigeAnmelden],
  [/^\/verbinden$/, 'verbinden', zeigeVerbinden],
  [/^\/ersteinrichtung$/, 'ersteinrichtung', zeigeErsteinrichtung],
  [/^\/status\/einstellungen$/, 'einstellungen', zeigeEinstellungen],
  [/^\/status\/ki$/, 'ki', zeigeKI],
  [/^\/status\/ki\/([a-z]+)$/, 'kiBereich', zeigeKIBereich],
  [/^\/status\/ki\/([a-z]+)\/([a-z0-9-]+)$/, 'kiEintrag', zeigeKIEintrag],
  [/^\/status\/update$/, 'update', () => zeigeUpdate('')],
  [/^\/status\/backups$/, 'backups', () => zeigeUpdate('backups')],
  [/^\/status\/update\/backups$/, 'backups', () => geheZu('/status/backups', true)],
  [/^\/status\/backups\/hochladen$/, 'backupHochladen', zeigeBackupHochladen],
  [/^\/status\/systemschluessel$/, 'systemschluessel', zeigeSystemschluessel],
  [/^\/status\/einstellungen\/bearbeiten$/, 'einstellungenBearbeiten', () => geheZu('/status/einstellungen', true)],
  [/^\/status\/einstellungen\/([A-Z_]+)$/, 'einstellungBearbeiten', zeigeEinstellungBearbeiten],
  [/^\/einrichten$/, 'einrichten', zeigeEinrichten],
  [/^\/api\/v1\/ai\/connect\/openrouter$/, 'kiVerbunden', zeigeKiVerbunden],
  [/^\/willkommen$/, 'willkommen', zeigeWillkommen],
  [/^\/wiederherstellen$/, 'wiederherstellen', zeigeWiederherstellen],
  [/^\/konto$/, 'konto', zeigeKonto],
  [/^\/konto\/code$/, 'zugangCode', zeigeNeuerCode],
  [/^\/konto\/geraet$/, 'geraetNeu', zeigeGeraetNeu],
  [/^\/konto\/verbinden$/, 'mcp', zeigeMcpAnleitung],
  [/^\/konto\/token$/, 'tokenNeu', zeigeTokenNeu],
  [/^\/konto\/mcp$/, 'umzug', () => geheZu('/konto/verbinden', true)],
  [/^\/konto\/kalender$/, 'kalender', zeigeKalender],
  // Frühere Adressen (vor der Seite Benutzer).
  [/^\/status\/(zugang|zugang\/code|geraet|mcp|kalender)$/, 'umzug', () => geheZu({ zugang: '/konto', 'zugang/code': '/konto/code', geraet: '/konto/geraet', mcp: '/konto/verbinden', kalender: '/konto/kalender' }[location.pathname.slice(8)] + location.search, true)],
  [/^\/admin\/nutzer$/, 'nutzer', zeigeNutzer],
  [/^\/admin\/nutzer\/neu$/, 'nutzerNeu', zeigeNutzerNeu],
  [/^\/admin\/nutzer\/einladung\/([0-9a-f]{16})$/, 'einladung', zeigeEinladung],
  [/^\/admin\/nutzer\/(\d+)\/code$/, 'neuEinladen', zeigeNeuEinladen],
  [/^\/admin\/nutzer\/(\d+)\/bearbeiten$/, 'nutzerBearbeiten', zeigeNutzerBearbeiten],
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
// aufnahmeKontext: gerade offene Gruppe bzw. Liste als Text („Gruppe X · Liste L8 Y“); geht mit einer neuen Notiz an die KI.
let aufnahmeKontext = null;
// Suche, aus der die offene Notiz oder Planung kommt (Suchtext), sonst null; suchStand = geladene Treffer und Scrollstand.
let sucheHerkunft = null, suchStand = null;
const DETAIL = ['eintrag', 'planung', 'tags', 'antwort', 'umhaengen', 'einsortieren'];

async function zeige() {
  besuche.aktiv();
  // Vorgemerkte Wisch-Aktionen laufen beim Seitenwechsel sofort: ihr „Rückgängig“ gehört zur alten Ansicht.
  alleVorgemerktenAusfuehren();
  if (neueHuelle) { location.reload(); return; }
  if (aufraeumen) { aufraeumen(); aufraeumen = null; }
  const vorher = aktuell?.name, vorherArg = aktuell?.arg;
  if (vorher === 'verlauf' && verlaufStand) verlaufStand.scroll = window.scrollY;
  if (vorher === 'suche' && suchStand) suchStand.scroll = window.scrollY;
  const pfad = location.pathname;
  for (const [muster, name, fn] of routen) {
    const m = pfad.match(muster);
    if (!m) continue;
    aktuell = { name, arg: m[1] };
    aufnahmeKontext = null;
    if (name === 'verlauf' || name === 'suche') aktuell.zurueck = DETAIL.includes(vorher);
    // Aus der Suche geöffnet: Brotkrumen zurück zur Suche, Suchwörter auf der Detailseite markiert.
    if (name === 'eintrag' || name === 'planung') { if (vorher === 'suche') sucheHerkunft = suchStand?.q || null; else if (!DETAIL.includes(vorher)) sucheHerkunft = null; } else if (!DETAIL.includes(name)) sucheHerkunft = null;
    // Unterseiten (Tags, Antwort …) und der Wechsel Runde → Planung behalten die Herkunft.
    if (name === 'eintrag' || name === 'planung') { if (vorher === 'suche') herkunft = null; else if (vorher === 'liste') herkunft = T`/liste/${vorherArg}`; else if (vorher === 'listen') herkunft = vorherArg ? T`/listen/gruppe/${vorherArg}` : '/listen'; else if (!DETAIL.includes(vorher)) herkunft = null; }
    if (name !== 'verlauf' && !(name === 'suche' && aktuell.zurueck)) window.scrollTo(0, 0);
    sucheMarkieren(name === 'eintrag' || name === 'planung' ? sucheHerkunft : null);
    const offen = name === 'anmelden' || name === 'einrichten' || name === 'ersteinrichtung' || name === 'kiVerbunden';
    menueKnopf.hidden = offen;
    if (menue.open && menue.matches(':modal')) menue.close();
    menueModus();
    suchForm.hidden = offen;
    neuKnopf.hidden = offen;
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
  const eingabe = e.target.closest('[data-eingabe]');
  if (eingabe) { e.preventDefault(); (eingabe.dataset.eingabe === 'anweisung' ? eingabeAnweisung : eingabeAntwort)(eingabe.dataset.id); return; }
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

// Suchwörter auf einer Detailseite markieren (aus der Suche geöffnet). Die Seite zeichnet sich bei Änderungen neu:
// ein Beobachter markiert danach erneut. q = null schaltet ab.
let markBeobachter = null;
function sucheMarkieren(q) {
  markBeobachter?.disconnect();
  markBeobachter = null;
  const woerter = (q || '').split(/\s+/).filter((w) => w.length >= 2).sort((a, b) => b.length - a.length);
  if (!woerter.length) return;
  const muster = new RegExp(`(${woerter.map((w) => w.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')).join('|')})`, 'gi');
  const probe = new RegExp(muster.source, 'i'); // ohne g: test() ohne Zustand
  const markieren = () => {
    markBeobachter.disconnect();
    const gang = document.createTreeWalker(seite, NodeFilter.SHOW_TEXT, {
      acceptNode: (n) => (n.parentElement.closest('mark, button, script, style, textarea, select, .krumen, svg') || !probe.test(n.data) ? NodeFilter.FILTER_REJECT : NodeFilter.FILTER_ACCEPT),
    });
    const knoten = [];
    for (let n = gang.nextNode(); n; n = gang.nextNode()) knoten.push(n);
    for (const n of knoten) {
      const teil = document.createDocumentFragment();
      for (const [i, s] of n.data.split(muster).entries()) {
        if (!s) continue;
        if (i % 2) { const m = document.createElement('mark'); m.textContent = s; teil.append(m); } else teil.append(s);
      }
      n.replaceWith(teil);
    }
    markBeobachter.observe(seite, { childList: true, subtree: true });
  };
  let geplant = false;
  markBeobachter = new MutationObserver(() => { if (!geplant) { geplant = true; requestAnimationFrame(() => { geplant = false; if (markBeobachter) markieren(); }); } });
  markBeobachter.observe(seite, { childList: true, subtree: true });
}

async function zeigeSuche() {
  const q = new URLSearchParams(location.search).get('q') || '';
  if (document.activeElement !== suchFeld) suchFeld.value = q;
  zeigeLeeren();
  if (q.trim().length < 2) {
    seite.innerHTML = T('<h1>Suche</h1><p class="leise">Mindestens zwei Zeichen eingeben. Gesucht wird in Titel, Wortlaut, Auswertung und Tags aller Notizen, Termine und Planungen; alle Wörter müssen vorkommen. „#46“ findet die Nummer.</p>');
    return;
  }
  // Zurück von einem Treffer: geladene Seiten und Scrollstand wiederherstellen statt neu zu suchen.
  const stand = aktuell.zurueck && suchStand?.q === q ? suchStand : null;
  if (!stand) seite.innerHTML = T('<p class="leer">Sucht …</p>');
  // Online gesucht, neueste zuerst; ältere Treffer kommen seitenweise beim Scrollen.
  const holen = (vor, stufe) => api(T`/api/suche?q=${encodeURIComponent(q)}${vor ? T`&vor=${vor}&stufe=${stufe || 1}` : ''}`);
  let d;
  try {
    d = stand ? { ...stand.d, treffer: stand.treffer } : await holen(0);
  } catch (f) {
    seite.innerHTML = T`<p class="leer">${f instanceof NetzFehler ? tx('Suche nur mit Verbindung.') : esc(f.message)}</p>`;
    return;
  }
  const gilt = () => aktuell?.name === 'suche' && (new URLSearchParams(location.search).get('q') || '') === q;
  if (!gilt()) return;
  const zeile = (e) => {
    const [stand, klasse] = standText(e);
    const art = e.termin ? terminArt(e.termin) : MODUS[e.modus] || e.modus;
    // Je Suchwort seine Fundstelle (Titel steht schon markiert oben).
    const funde = (e.fundstellen || (e.fundstelle ? [e.fundstelle] : [])).filter((f) => f.text && f.feld !== 'Titel');
    return T`<a class="treffer" href="${e.typ === 'planung' ? '/planung/' : '/eintrag/'}${e.id}">
      <div class="trefferkopf"><span class="treffertitel">${markiere(e.titel || tx('Ohne Titel'), q)}</span>${e.nr ? T`<span class="nr">#${e.nr}</span>` : ''}</div>
      <div class="trefferinfo"><span class="badge ${e.termin ? 'termin' : esc(e.modus)}">${esc(art)}</span><span class="badge ${klasse}">${esc(stand)}</span><span class="leise">${kurz(e.aufgenommen)}</span></div>
      ${funde.map((f) => T`<div class="fund">${f.feld === 'Wortlaut' ? '' : T`<span class="leise">${esc(tx(f.feld))}:</span> `}${markiere(f.text, q)}</div>`).join('')}</a>`;
  };
  if (!d.treffer.length) {
    seite.innerHTML = T`<h1>Suche</h1><p class="leise">Nichts gefunden für „${esc(q)}“.</p>`;
    return;
  }
  seite.innerHTML = T`<h1>Suche</h1><p class="leise" id="trefferzahl"></p><div class="trefferliste" id="trefferliste"></div><div id="treffermehr"></div>`;
  const liste = document.getElementById('trefferliste'), mehrBox = document.getElementById('treffermehr'), zahl = document.getElementById('trefferzahl');
  let anzahl = 0, laedt = false;
  suchStand = { q, treffer: [], d: null, scroll: 0 };
  const anhaengen = (x) => {
    d = x;
    anzahl += x.treffer.length;
    suchStand.treffer.push(...x.treffer);
    suchStand.d = { mehr: x.mehr, vor_naechste: x.vor_naechste, stufe: x.stufe };
    liste.insertAdjacentHTML('beforeend', x.treffer.map(zeile).join(''));
    zahl.textContent = tx('{0} Treffer für „{1}“, neueste zuerst', `${anzahl}${x.mehr ? '+' : ''}`, q);
    mehrBox.innerHTML = x.mehr ? T('<p class="leer klein">Lädt …</p>') : '';
  };
  const weiter = async () => {
    if (laedt || !d.mehr || !gilt()) return;
    laedt = true;
    try {
      const x = await holen(d.vor_naechste, d.stufe);
      if (gilt()) anhaengen(x);
    } catch (f) {
      if (!(f instanceof NetzFehler)) throw f;
    } finally {
      laedt = false;
    }
    if (d.mehr && mehrBox.getBoundingClientRect().top < window.innerHeight + 400) weiter();
  };
  anhaengen(d);
  if (stand) window.scrollTo(0, stand.scroll);
  const beobachter = new IntersectionObserver((es) => { if (es.some((x) => x.isIntersecting)) weiter(); }, { rootMargin: '400px' });
  beobachter.observe(mehrBox);
  aufraeumen = () => beobachter.disconnect();
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
  // Planungen mit mehreren Runden: Zähler direkt hinter dem Typ-Badge.
  const runde = planung && e.runden > 1 ? T` <span class="zaehler runden" title="${esc(tx('{0} Runden', e.runden))}">${e.runden}</span>` : '';
  // „fertig“ ist der Normalfall und wird nicht angezeigt; rechts oben steht stattdessen Datum und Uhrzeit.
  const stand = e.status === 'fertig' ? '' : T`<span class="badge ${esc(e.status)}">${esc(STATUS[e.status] || e.status)}${schritt}</span> `;
  // Kurze Notizen sagt der Titel schon ganz: die Vorschau käme nur doppelt.
  const vorschau = (e.vorschau || '').length >= 100 ? T`<div class="vorschau">${esc(e.vorschau)}</div>` : '';
  const t = e.termin;
  // Erledigte Karten: nur Kopfzeile und Titel, ausgegraut. Untertext und Haken entfallen – das Grau sagt es schon.
  const zu = istErledigt(e);
  const termin = t && !zu ? T`<div class="terminkarte${t.verpasst ? ' verpasst' : ''}"><span>${t.verpasst ? T('<span class="badge alarm">verpasst</span> ') : ''}${t.bestaetigt ? `${erledigtZeichen} ` : ''}${esc(terminZeit(t))}${t.ort ? ` · ${esc(t.ort)}` : ''}</span></div>` : ''; // Bestätigen per Wischen nach rechts
  return T`<a class="karte${neu}${t?.verpasst ? ' alarmkarte' : ''}${zu ? ' erledigt' : ''}" href="${planung ? '/planung/' : '/eintrag/'}${e.id}" ${kartenDaten(e)}>
    <div class="zeile"><span>${nrHtml(e.nr)}${t ? T`<span class="badge termin">${terminArt(e.termin)}</span>` : T`<span class="badge ${esc(e.modus)}">${esc(MODUS[e.modus] || e.modus)}</span>${runde}`} ${planung && !zu ? planungZeichen(e.planung_status === 'offen') : ''}${e.liste ? T` <span class="badge liste">${esc(listenName(e.liste))}</span>${e.liste.erledigt && !zu ? ` ${erledigtZeichen}` : ''}` : ''}${e.doppelt ? T` <span class="badge doppelt">${e.doppelt.nr ? tx('Doppelt #{0}', e.doppelt.nr) : tx('Doppelt')}</span>` : ''}</span>
      <span class="kartenzeit">${stand}<span class="leise">${kartenZeit(e.aufgenommen)}</span></span></div>
    <div class="titel">${esc(e.titel || tx('Ohne Titel'))}</div>
    ${e.besetzung && !zu ? T`<div class="besetzung klein">${esc(e.besetzung)}</div>` : ''}${zu ? '' : termin}${zu ? '' : vorschau}${!zu && e.tags?.length ? T`<div class="tags">${e.tags.map((t) => T`<span class="tagchip">${esc(t)}</span>`).join('')}</div>` : ''}${fehler}
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
    // Beim Laden steht aktuell noch nicht fest: dann zählt der Pfad (sonst schickt ein 401 die Passkey-Einrichtung zur Anmeldung).
    if (!anmeldung && (['anmelden', 'einrichten', 'ersteinrichtung', 'kiVerbunden'].includes(aktuell?.name) || /^\/(anmelden|einrichten|ersteinrichtung|api\/v1\/ai\/connect\/openrouter)$/.test(location.pathname))) return false;
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

// Tagesgruppe einer Karte im Verlauf: „Heute“, „Gestern“, sonst Wochentag mit Datum (fremdes Jahr mit Jahreszahl).
function tagGruppe(ms) {
  const d = new Date(ms), h = new Date();
  const tage = Math.round((new Date(h.toDateString()) - new Date(d.toDateString())) / 86400000);
  if (tage === 0) return tx('Heute');
  if (tage === 1) return tx('Gestern');
  return d.toLocaleDateString(LOCALE, { weekday: 'long', day: '2-digit', month: '2-digit', ...(d.getFullYear() !== h.getFullYear() ? { year: 'numeric' } : {}) });
}

// ---------- Wischen im Verlauf: rechts = erledigt, links = löschen, mit „Rückgängig“ ----------

// Erledigt: fester Stand der Notiz bzw. des Termins (erledigt_at), Planung geschlossen.
const istErledigt = (e) => !!(e.typ === 'planung' ? e.planung_status === 'geschlossen' : e.termin ? e.termin.bestaetigt : e.erledigt);
const ICON_HAKEN = T('<svg viewBox="0 0 24 24" aria-hidden="true"><path d="M5 12.5l4.5 4.5L19 7.5" fill="none" stroke="currentColor" stroke-width="2.6" stroke-linecap="round" stroke-linejoin="round"/></svg>');
const ICON_MUELL = T('<svg viewBox="0 0 24 24" aria-hidden="true"><path d="M4 7h16M9.5 7V4.5h5V7M6.5 7l1 13h9l1-13M10 11v5.5M14 11v5.5" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/></svg>');
const ICON_NUR_OFFEN = T('<svg viewBox="0 0 24 24" aria-hidden="true"><path d="M5 12.5l4.5 4.5L19 7.5" fill="none" stroke="currentColor" stroke-width="2.4" stroke-linecap="round" stroke-linejoin="round"/><path d="M4 4l16 16" stroke="currentColor" stroke-width="2" stroke-linecap="round"/></svg>');
const ICON_PFEIL_LINKS = T('<svg viewBox="0 0 24 24" aria-hidden="true"><path d="M19 12H5M10 7l-5 5 5 5" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/></svg>');
const ICON_WEITER = T('<svg class="weiter" viewBox="0 0 24 24" aria-hidden="true"><path d="M9 6l6 6-6 6" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/></svg>');
// Herkunft eines Begriffs: von Hand (Person) oder von der KI (Funken; Lernlauf = Funken im Kreis).
const ICON_QUELLE = {
  nutzer: '<path d="M12 12a4 4 0 1 0 0-8 4 4 0 0 0 0 8zM4.5 20a7.5 7.5 0 0 1 15 0" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/>',
  ki: '<path d="M12 3l1.8 5.2L19 10l-5.2 1.8L12 17l-1.8-5.2L5 10l5.2-1.8zM19 16l.8 2.2L22 19l-2.2.8L19 22l-.8-2.2L16 19l2.2-.8z" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linejoin="round"/>',
  lernlauf: '<path d="M20 12a8 8 0 1 1-2.6-5.9M20 4v4h-4" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/><path d="M12 8.5l1 2.5 2.5 1-2.5 1-1 2.5-1-2.5-2.5-1 2.5-1z" fill="currentColor"/>',
};
ICON_QUELLE.korrektur = '<path d="M4 20h4L19 9l-4-4L4 16zM13.5 6.5l4 4" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/>';
ICON_QUELLE.antwort = ICON_QUELLE.anweisung = '<path d="M5 5h14a1 1 0 0 1 1 1v9a1 1 0 0 1-1 1h-7l-4.5 3.5V16H5a1 1 0 0 1-1-1V6a1 1 0 0 1 1-1z" fill="none" stroke="currentColor" stroke-width="2" stroke-linejoin="round"/>';
const quelleIcon = (q) => `<svg class="quelle" viewBox="0 0 24 24" role="img" aria-label="${esc(GED_QUELLE[q] || q)}"><title>${esc(GED_QUELLE[q] || q)}</title>${ICON_QUELLE[q] || ICON_QUELLE.ki}</svg>`;
const ICON_PFEIL_RECHTS = T('<svg viewBox="0 0 24 24" aria-hidden="true"><path d="M5 12h14M14 7l5 5-5 5" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/></svg>');
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

// Neu oben, danach je Tag eine Gruppe (Einträge kommen neueste zuerst).
// fortsetzung: Tagesgruppe, die schon über den angehängten Karten steht (Nachladen) – ihr Kopf entfällt.
function listeHtml(eintraege, fortsetzung) {
  const gruppen = [[tx('Neu'), eintraege.filter(istNeu)]];
  for (const e of eintraege) {
    if (istNeu(e)) continue;
    const name = tagGruppe(e.eingegangen);
    if (gruppen[gruppen.length - 1][0] === name) gruppen[gruppen.length - 1][1].push(e);
    else gruppen.push([name, [e]]);
  }
  const html = gruppen.filter(([, liste]) => liste.length).map(([name, liste], i) => T`${i === 0 && name === fortsetzung ? '' : T`<div class="tag">${name}</div>`}${liste.map(wischKarte).join('')}`).join('');
  return html || (fortsetzung ? '' : T('<p class="leer">Keine Einträge vorhanden.</p>'));
}

async function zeigeAusgang() {
  const box = document.getElementById('ausgang');
  if (!box) return;
  const posten = (await alle('ausgang')).sort((a, b) => a.zeit - b.zeit);
  const name = (p) => (p.typ === 'notiz' ? (p.audio ? tx('Notiz (Aufnahme)') : T`Notiz: ${esc((p.text || '').slice(0, 80))}`) : p.typ === 'anweisung' ? (p.audio ? tx('KI-Anweisung (Aufnahme)') : T`KI-Anweisung: ${esc((p.text || '').slice(0, 80))}`) : p.typ === 'antwort' ? (p.audio ? tx('Antwort (Aufnahme)') : T`Antwort: ${esc((p.text || '').slice(0, 80))}`) : p.typ === 'notizantwort' ? tx('Antwort auf Rückfrage') : p.typ === 'schliessen' ? tx('Planung schließen') : p.typ === 'haken' ? tx('Punkt abhaken') : tx('Erneut starten'));
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
// Tag-Filter: Trichter in der Suchleiste, Dialog mit Mehrfachauswahl (nach Menge sortiert); alle gewählten müssen passen.
// Eine Auswahl für Verlauf und Listen (verlauf-tags). tags wird in place geändert, danach neu(); aus() blendet den Knopf aus.
// daten(vorrat, kombis): vorrat [{name, anzahl}], kombis [{tags, anzahl}] – daraus die Zahlen im Dialog.
function richteTagFilter({ tags, aus = () => false, neu }) {
  let vorrat = [], kombis = [];
  const male = () => {
    tagFilterKnopf.hidden = aus() || (!vorrat.length && !tags.length);
    suchForm.classList.toggle('mitfilter', !tagFilterKnopf.hidden);
    tagFilterKnopf.classList.toggle('an', tags.length > 0);
    const z = tagFilterKnopf.querySelector('.zaehler');
    z.hidden = !tags.length;
    z.textContent = tags.length;
    // Aktiver Filter sichtbar über der Liste, mit Knopf zum sofortigen Zurücksetzen (Element #tagleiste der Seite,
    // nach jedem Neuaufbau der Seite erneut über male()).
    const tl = document.getElementById('tagleiste');
    if (!tl) return;
    tl.hidden = !tags.length;
    tl.innerHTML = tags.length ? T`<span class="tags">${tags.map((t) => T`<span class="tagchip">${esc(t)}</span>`).join('')}</span>
      <button class="btn klein" data-tags-weg="1" aria-label="Tag-Filter zurücksetzen">✕ Zurücksetzen</button>` : '';
    tl.onclick = (ev) => { if (ev.target.closest('[data-tags-weg]')) setze([]); };
  };
  const setze = (liste) => {
    localStorage.setItem('verlauf-tags', JSON.stringify(liste));
    localStorage.removeItem('verlauf-tag');
    tags.splice(0, tags.length, ...liste);
    male();
    neu();
  };
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
      // kombis: [{tags, anzahl}] (früher eine Liste je Eintrag – im Zwischenspeicher kann noch die alte Form liegen).
      const passend = kombis.map((k) => (Array.isArray(k) ? { tags: k, anzahl: 1 } : k)).filter((k) => [...wahlTags].every((t) => k.tags.includes(t)));
      const summe = (l) => l.reduce((z, k) => z + k.anzahl, 0);
      const anzahl = (n) => (wahlTags.size ? summe(passend.filter((k) => k.tags.includes(n))) : gesamt(n));
      dialog.innerHTML = T`<h2>Nach Tags filtern</h2><p class="leise">${wahlTags.size ? T`${summe(passend)} ${summe(passend) === 1 ? tx('Eintrag trägt') : tx('Einträge tragen')} alle gewählten Tags.` : tx('Gezeigt werden Einträge mit allen gewählten Tags.')}</p>
        <div class="tags wahl">${namen.map((n) => {
          const an = wahlTags.has(n), z = anzahl(n), weg = !an && wahlTags.size > 0 && z === 0;
          return T`<button class="tagchip wahl${an ? ' an' : ''}${weg ? ' aus' : ''}" data-tag="${esc(n)}" aria-pressed="${an}"${weg ? ' disabled' : ''}>${esc(n)} <span class="zahl">${z}</span></button>`;
        }).join('')}</div>
        <div class="leiste">${wahlTags.size ? T('<button class="btn" data-aus="1">Alle abwählen</button>') : ''}<button class="btn haupt" data-zu="1">Fertig</button></div>`;
    };
    // Sofort filtern: Auswahl speichern und die Seite hinter dem Dialog neu aufbauen.
    const uebernehmen = () => setze([...wahlTags]);
    dialog.onclick = (e2) => {
      const k = e2.target.closest('button');
      if (e2.target === dialog || k?.dataset.zu) return dialog.close();
      if (k?.dataset.aus) { wahlTags.clear(); uebernehmen(); return dialog.close(); }
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
  return { male, daten(v, k) { vorrat = (v || []).filter((t) => t && t.name); if (k) kombis = k; male(); } };
}

// tagVorratAus: Vorrat und Kombinationen aus Listenpunkten (je Punkt die Tags seiner Notiz bzw. Planung).
function tagVorratAus(punkte) {
  const zahl = new Map(), kombi = new Map();
  for (const p of punkte) {
    if (!p.tags?.length) continue;
    for (const t of p.tags) zahl.set(t, (zahl.get(t) || 0) + 1);
    const k = [...p.tags].sort().join('\n');
    kombi.set(k, (kombi.get(k) || 0) + 1);
  }
  return {
    vorrat: [...zahl].map(([name, anzahl]) => ({ name, anzahl })),
    kombis: [...kombi].map(([k, anzahl]) => ({ tags: k.split('\n'), anzahl })),
  };
}
const passtTags = (p, tags) => !tags.length || tags.every((t) => p.tags?.includes(t));

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
  // Umschalter für alle Reiter: nur offene (Vorgabe) oder auch erledigte Einträge.
  const nurOffen = localStorage.getItem('verlauf-offen') !== '0';
  const offenText = nurOffen ? tx('Nur offene – Erledigte einblenden') : tx('Alle – Erledigte ausblenden');
  seite.innerHTML = T`<div id="ausgang"></div><div class="filterzeile"><nav class="btn-group unter reiter4" id="filter" aria-label="Verlauf filtern"></nav>
    <button class="btn offenknopf${nurOffen ? ' aktiv' : ''}" id="offenknopf" aria-pressed="${nurOffen}" title="${offenText}" aria-label="${offenText}">${ICON_NUR_OFFEN}</button></div>
    <div id="tagleiste" class="tagleiste" hidden></div>
    <div id="termine"></div>
    <div id="liste"><p class="leer">Lädt …</p></div><div id="mehr" class="mitte"></div>
    <div id="wischhinweis" class="wisch-hinweis" hidden><span>${ICON_PFEIL_LINKS}<span class="schlecht">${ICON_MUELL}</span>Löschen</span><span aria-hidden="true">·</span><span>Erledigt<span class="gut">${ICON_HAKEN}</span>${ICON_PFEIL_RECHTS}</span></div>
    <div id="wahlleiste" class="wahlleiste" hidden></div>`;
  document.getElementById('offenknopf').onclick = () => {
    localStorage.setItem('verlauf-offen', nurOffen ? '0' : '1');
    zeigeVerlauf();
  };
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
  // Tag-Filter (Trichter in der Suchleiste): Vorrat und Kombinationen über alle Einträge vom Server.
  const tagFilter = richteTagFilter({ tags, aus: () => art === 'fragen', neu: () => aktuell.neuLaden?.() });
  let gespeichert = [], kombis = [];
  try { gespeichert = JSON.parse(localStorage.getItem('tag-vorrat') || '[]'); } catch (f) { /* leer */ }
  try { kombis = JSON.parse(localStorage.getItem('tag-kombis') || '[]'); } catch (f) { /* leer */ }
  tagFilter.daten(gespeichert, kombis);
  api('/api/tags').then((d) => {
    localStorage.setItem('tag-vorrat', JSON.stringify(d.tags));
    kombis = d.kombis || [];
    try { localStorage.setItem('tag-kombis', JSON.stringify(kombis)); } catch (f) { /* voll */ }
    if (aktuell?.name === 'verlauf') tagFilter.daten(d.tags, kombis);
  }).catch(() => {});
  zeigeAusgang();
  // Zwischenspeicher aus einer früheren Fassung (eine Zeile je Runde) verwerfen.
  for (const e of await alle('verlauf')) if (!e.typ) await loesche('verlauf', e.id);
  // Zurück von einer Detailseite mit gleichem Filter: Liste samt nachgeladenen Seiten und Scrollposition wiederherstellen.
  const stand = aktuell.zurueck && verlaufStand && verlaufStand.art === art && verlaufStand.offen === nurOffen && verlaufStand.tags === tags.join('\n') ? verlaufStand : null;
  // Die Fragen-Ansicht zeigt jede offene Rückfrage, auch an alten Planungen.
  const gezeigt = (e) => !nurOffen || art === 'fragen' || !istErledigt(e);
  // Bis der Server antwortet: der Zwischenspeicher, gekappt auf die jüngsten Einträge.
  let eintraege = stand ? stand.eintraege.filter(gezeigt) : (await alle('verlauf')).filter((e) => passt(e, art, tags) && gezeigt(e)).sort((a, b) => b.eingegangen - a.eingegangen).slice(0, 60);
  // mehrBekannt: der Server hat ältere Seiten; cursor = vor= für die nächste (Zeitpunkt des letzten geladenen Eintrags).
  let mehrBekannt = stand ? stand.mehr : false;
  let cursor = stand ? stand.cursor : 0;
  const merke = () => { verlaufStand = { art, offen: nurOffen, tags: tags.join('\n'), eintraege, mehr: mehrBekannt, cursor, scroll: window.scrollY }; };
  filter.onclick = (ev) => {
    const b = ev.target.closest('button[data-art]');
    if (!b || b.dataset.art === art) return;
    localStorage.setItem('verlauf-art', b.dataset.art);
    zeigeVerlauf();
    maleMenue();
  };
  // seit: zuletzt gesehen, damit der Server auch Neues älter als 24 Stunden liefert.
  const seit = () => (Number.isFinite(besuche.grenze()) ? T`seit=${besuche.grenze()}` : '');
  const abfrage = () => [art === 'alles' ? '' : T`art=${art}`, ...tags.map((t) => T`tag=${encodeURIComponent(t)}`), seit(), nurOffen && art !== 'fragen' ? 'offen=1' : ''].filter(Boolean).join('&');

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
    // Nur offene: ein gerade erledigter Eintrag fällt sofort heraus („Rückgängig“ holt ihn zurück).
    const sicht = eintraege.filter((x) => ausstehend.get(x.id) !== null).map((x) => ausstehend.get(x.id) || x).filter((x) => !nurOffen || art === 'fragen' || !istErledigt(x));
    liste.innerHTML = !sicht.length && nurOffen && art !== 'fragen' ? T('<p class="leer">Keine offenen Einträge.</p>') : art === 'termin' ? terminListeHtml(sicht) : listeHtml(sicht);
    document.getElementById('wischhinweis').hidden = !liste.querySelector('.wisch');
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

  // Endlos nachladen: sobald das Listenende in Sicht kommt, die nächsten 50 vom Server.
  const mehrBox = document.getElementById('mehr');
  let laedtMehr = false;
  const mehrKnopf = (mehr) => { mehrBox.innerHTML = mehr ? T('<p class="leer klein">Lädt …</p>') : ''; };
  const nachladen = async () => {
    if (laedtMehr || !mehrBekannt || !cursor || aktuell?.name !== 'verlauf') return;
    laedtMehr = true;
    try {
      const d = await api(T`/api/verlauf?vor=${cursor}&${abfrage()}`);
      if (aktuell?.name !== 'verlauf') return;
      const da = new Set(eintraege.map((e) => e.id));
      const neue = d.eintraege.filter((e) => !da.has(e.id));
      // Nur die neuen Karten anhängen statt die ganze (lange) Liste neu zu zeichnen; Termine sortieren anders → ganz.
      const letzte = [...eintraege].reverse().find((e) => !istNeu(e) && ausstehend.get(e.id) !== null && (!nurOffen || art === 'fragen' || !istErledigt(ausstehend.get(e.id) || e)));
      eintraege = eintraege.concat(neue);
      for (const e of d.eintraege) await lege('verlauf', e);
      mehrBekannt = d.mehr;
      cursor = d.vor_naechste || 0;
      if (art === 'termin' || !letzte || !liste.querySelector('.wisch')) male();
      else {
        liste.insertAdjacentHTML('beforeend', listeHtml(neue.filter((x) => !nurOffen || art === 'fragen' || !istErledigt(x)), tagGruppe(letzte.eingegangen)));
        maleWahl();
      }
      mehrKnopf(mehrBekannt);
      merke();
    } catch (f) {
      if (!(f instanceof NetzFehler)) throw f;
    } finally {
      laedtMehr = false;
    }
    // Passt die neue Seite ganz in den Bildschirm, gleich weiter.
    if (mehrBekannt && mehrBox.getBoundingClientRect().top < window.innerHeight + 400) nachladen();
  };
  const beobachter = new IntersectionObserver((es) => { if (es.some((x) => x.isIntersecting)) nachladen(); }, { rootMargin: '400px' });
  beobachter.observe(mehrBox);
  const altesAufraeumen = aufraeumen;
  aufraeumen = () => { beobachter.disconnect(); altesAufraeumen(); };
  if (eintraege.length) male();
  if (stand) {
    mehrKnopf(stand.mehr);
    window.scrollTo(0, stand.scroll);
  }
  const laden = async () => {
    try {
      const d = await api(T`/api/verlauf?${abfrage()}`);
      if (d.besuch_pause_minuten) besuche.pause(d.besuch_pause_minuten);
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
      // Zwischenspeicher = erste Seite des Servers; ältere bleiben, Entferntes fällt heraus.
      const alt = (await alle('verlauf')).filter((e) => passt(e, art, tags) && gezeigt(e));
      const grenze = d.vor_naechste || (d.eintraege.length ? d.eintraege[d.eintraege.length - 1].eingegangen : 0);
      const neu = new Set(d.eintraege.map((e) => e.id));
      for (const e of alt) if (!neu.has(e.id) && (e.eingegangen >= grenze || !d.mehr)) await loesche('verlauf', e.id);
      for (const e of d.eintraege) await lege('verlauf', e);
      holeDetails(d.eintraege.slice(0, 30)); // Details für offline nur der jüngsten, nicht eines ganzen Rückstands
      if (aktuell?.name !== 'verlauf') return;
      // Erste Seite frisch vom Server; bereits nachgeladene ältere Einträge bleiben stehen.
      // Nur wirklich nachgeladene Seiten (cursor gesetzt), nicht der Zwischenspeicher vom Start.
      const aeltereSeiten = d.mehr && cursor ? eintraege.filter((e) => !neu.has(e.id) && e.eingegangen < grenze) : [];
      if (!aeltereSeiten.length) { mehrBekannt = d.mehr; cursor = d.vor_naechste || 0; }
      eintraege = d.eintraege.concat(aeltereSeiten);
      const y = window.scrollY;
      male();
      mehrKnopf(mehrBekannt);
      window.scrollTo(0, y);
      merke();
      if (mehrBekannt && mehrBox.getBoundingClientRect().top < window.innerHeight + 400) nachladen();
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
  if (sucheHerkunft) return krumen([[tx('Suche „{0}“', sucheHerkunft), T`/suche?q=${encodeURIComponent(sucheHerkunft)}`], [nr]]);
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
  const planung = `${plan.ergebnisse || plan.medien?.length ? T`<h3>Hintergrund</h3><div class="md">${md(plan.ergebnisse, plan.medien || [])}</div>` : ''}
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
  // Doppelt: diese Punkte standen schon in der Liste (aus einer anderen Notiz oder Planung) – kein zweiter Punkt, Link aufs Original.
  if (e.doppelt?.length) {
    html += T`<div class="hinweis warn doppelt"><strong>Doppelt</strong><ul>${e.doppelt.map((d) => T`<li>${tx('„{0}“ steht schon in {1}:', esc(d.text), esc(d.liste))} ${d.id ? T`<a href="/${d.typ}/${d.id}">#${d.nr || ''} ${esc(d.titel || '')}</a>` : (d.nr ? tx('#{0} (nicht mehr da)', d.nr) : tx('Original nicht mehr da'))}</li>`).join('')}</ul></div>`;
  }
  if (an?.antworten?.length) html += T`<h2>Deine Antworten</h2><dl>${an.antworten.map((x) => T`<dt>${esc(x.frage)}</dt><dd><span class="leise">${x.zeit ? zeit(x.zeit) : tx('Zeit unbekannt')}</span> ${esc(x.antwort)}</dd>`).join('')}</dl>`;
  // Eckdaten zuerst (nur die der Notiz, Listenpunkte tragen keine), darunter getrennt der Listenbezug mit Erledigt-Haken.
  const lp = e.liste?.punkte || [];
  html += block(tx('Eckdaten'), eckdatenHtml(an?.eckdaten));
  if (e.liste) {
    html += block(T`Liste <a href="/liste/${e.liste.id}">${esc(listenName(e.liste))}</a>`, lp.map((p) => T`<div class="lp${p.erledigt ? ' erledigt' : ''}"><button class="haken" data-haken="${p.id}" aria-pressed="${!!p.erledigt}" aria-label="${p.erledigt ? tx('Wieder öffnen') : tx('Abhaken')}: ${esc(p.text)}"><span class="hakenbild" aria-hidden="true"></span></button><div class="lptext"><div>${esc(p.text)}</div></div></div>`).join(''), 'listenbezug');
  }
  // Text: kurze KI-Fassung des Gesagten; ältere Einträge ohne diese Fassung zeigen hier das Transkript.
  html += block(tx('Text'), notizHtml(an?.notiz || e.transkript), 'notiz');
  // Fortsetzung einer früheren Notiz (von der KI erkannt): Verweis darauf.
  if (e.bezug) html += T`<p class="leise bezug">Fortsetzung von <a href="/eintrag/${e.bezug.id}">#${e.bezug.nr} ${esc(e.bezug.titel || '')}</a></p>`;
  if (!an) return html + aufnahme;
  // Details: Zusatzwissen (z. B. Inhalt eines Films), Hinweise, Links. Die Aufnahme steht als eigene Karte am Ende.
  const mehr = `${an.beschreibung || an.medien?.length ? T`<div class="beschreibung md">${md(an.beschreibung, an.medien || [])}</div>` : ''}${an.hinweise?.length ? T`<ul>${an.hinweise.map((h) => T`<li>${md(h).replace(/^<p>|<\/p>$/g, '')}</li>`).join('')}</ul>` : ''}${an.quellen?.length ? T`<h3>Links</h3>${linksHtml(an.quellen)}` : ''}`;
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
  // Freie Anweisung an die KI („bitte in zwei Punkte trennen“): eigene Seite, danach neue Auswertung.
  if (e.modus === 'diktat' && !e.planung && fertig && e.analyse && !offline) haupt.push(T`<button type="button" class="btn" data-eingabe="anweisung" data-id="${e.id}">KI-Anweisung</button>`);
  if (e.status === 'fehler' || e.status === 'wiederholung' || e.status === 'leer') haupt.push(T`<button class="btn${e.status === 'leer' ? '' : ' haupt'}" data-akt="neustart">Erneut starten</button>`);
  if (offen && fertig) haupt.push(T`<button type="button" class="btn haupt" data-eingabe="antwort" data-id="${p.id}">Antworten</button>`);
  // Termin verschieben: im Mehr-Menü (Nutzervorgabe 23.11.2026), nicht als Hauptaktion.
  if (t && fertig && !offline) mehr.push(punkt('b', 'data-akt="verschieben"', tx('Termin verschieben')));
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
  // Neu transkribieren: Aufnahme vorhanden, nichts läuft; bei Planungen nur die letzte Runde.
  if (e.audio === 'da' && !offline && (fertig || e.status === 'fehler') && (e.modus === 'diktat' || (e.modus === 'planung' && (!p || p.letzte_runde)))) {
    aufnahme += T('<div class="leiste"><button type="button" class="btn klein" data-akt="neu-transkribieren">Neu transkribieren</button></div>');
  }
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

  // Anweisungen an die KI: ganz unten, eingeklappt.
  const anw = e.analyse?.anweisungen || [], anwZeit = e.analyse?.anweisungen_zeit || [];
  if (anw.length) html += T`<details class="einklapp anweisungen"><summary>Deine Anweisungen an die KI (${anw.length})</summary><ul>${anw.map((x, k) => T`<li><span class="leise">${anwZeit[k] ? zeit(anwZeit[k]) : tx('Zeit unbekannt')}</span> ${esc(x)}</li>`).join('')}</ul></details>`;

  const details = [];
  if (e.zuordnung) details.push([tx('Zuordnung'), ZUORDNUNG[e.zuordnung] || e.zuordnung]);
  if (e.transkript_quelle) details.push([tx('Transkript von'), { openai: tx('Cloud-Anbieter'), lokal: tx('Whisper (lokal)'), 'openai-ausweich': tx('Cloud-Anbieter (lokal gescheitert)'), pebble: tx('Pebble'), text: tx('getippt'), nutzer: tx('von Hand korrigiert') }[e.transkript_quelle] || e.transkript_quelle]);
  // KI-Anfragen dieses Auftrags aus dem Protokoll: je Schritt Anbieter, Modell, rohe Tokens und Kosten (0 $ bleibt weg).
  // Ältere Aufträge haben kein Protokoll, dort steht nur der gespeicherte Betrag.
  if (e.ki_anfragen?.length) for (const q of e.ki_anfragen) details.push([kiZweck(q), kiVerbrauch(q)]);
  else if (e.kosten_usd > 0) details.push([e.modus === 'diktat' ? tx('Kosten der KI-Auswertung') : tx('Recherchekosten'), `${e.kosten_usd.toFixed(3)} $`]);
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
      } else if (akt === 'neu-transkribieren') {
        if (!confirm(tx('Aufnahme neu transkribieren? Danach wertet die KI den neuen Wortlaut neu aus; von Hand korrigierter Wortlaut geht verloren.'))) return;
        b.disabled = true;
        await api(T`/api/eintraege/${e.id}/neu-transkribieren`, { method: 'POST' });
        melde(tx('Wird neu transkribiert'));
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
  if (offen && !laeuft) haupt.push(T`<button type="button" class="btn${p.fragen ? '' : ' haupt'}" data-eingabe="antwort" data-id="${p.id}">Antworten</button>`);
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

// Nach dem Speichern: je einzeln berichtigtem Wort fragen, ob es in die Begriffsliste der Spracherkennung soll.
// „Nein“ merkt ein neues Wort als nicht erwünscht vor, damit auch die KI es nicht einträgt. Steht das Wort schon in
// der Liste (trotzdem wieder falsch erkannt), macht „Als Favorit“ es zum Favoriten: es geht dann vorn sicher mit.
function frageBegriffe(fragen, zurueck) {
  const offen = new Map(fragen.map((f) => [f.wort, f]));
  const male = () => {
    seite.innerHTML = T`<h1>In die Begriffsliste aufnehmen?</h1>
      <p class="leise">Begriffe der Liste gehen als Hinweis an die Spracherkennung mit, damit sie das Wort künftig gleich richtig schreibt.</p>
      ${[...offen.values()].map((f) => T`<div class="karte gedkarte"><div><span class="badge neutral">${esc(f.wort)}</span>${f.vorhanden ? T` <span class="leise">steht schon in der Liste</span>` : ''}</div><div class="leiste"><button class="btn klein haupt" data-wort="${esc(f.wort)}">${f.vorhanden ? tx('Als Favorit – immer mitsenden') : tx('Hinzufügen')}</button><button class="btn klein" data-wort="${esc(f.wort)}" data-nein="1">Nein</button></div></div>`).join('')}
      <div class="leiste"><a class="btn" href="${zurueck}">Fertig</a></div>`;
  };
  male();
  seite.onclick = async (ev) => {
    const b = ev.target.closest('button[data-wort]');
    if (!b) return;
    const f = offen.get(b.dataset.wort);
    b.disabled = true;
    try {
      // „Nein“ bei einem vorhandenen Begriff ändert nichts (er bleibt in der Liste).
      if (!(b.dataset.nein && f.vorhanden)) await api('/api/gedaechtnis/begriffe', { method: 'POST', body: { begriffe: [f.wort], ablehnen: !!b.dataset.nein } });
      melde(b.dataset.nein ? (f.vorhanden ? tx('Unverändert') : tx('Nicht aufgenommen')) : f.vorhanden ? tx('Favorit – geht immer mit') : tx('Aufgenommen'));
      offen.delete(f.wort);
      if (!offen.size) return geheZu(zurueck, true);
      male();
    } catch (x) { b.disabled = false; melde(x instanceof NetzFehler ? tx('Offline – nur mit Verbindung') : x.message); }
  };
}

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
      if (d.begriffe_frage?.length) return frageBegriffe(d.begriffe_frage, zurueck);
      geheZu(zurueck, true);
    } catch (f) {
      b.disabled = false;
      melde(f instanceof NetzFehler ? tx('Offline – nur mit Verbindung') : f.message);
    }
  };
  aufraeumen = () => { seite.onclick = null; };
}

// ---------- Antwortseite ----------

// starteAufnahme: Mikrofon an, Aufnahme läuft bis stopp() oder maxSek (0 = ohne Grenze); onEnde(blob, sek)
// kommt nach dem Stopp, nicht nach verwerfen(). onTick(sek) etwa fünfmal je Sekunde.
async function starteAufnahme({ maxSek = 0, onTick, onEnde } = {}) {
  const strom = await navigator.mediaDevices.getUserMedia({ audio: true });
  const teile = [];
  let rekorder;
  try { rekorder = new MediaRecorder(strom); } catch (f) { strom.getTracks().forEach((t) => t.stop()); throw f; }
  const start = Date.now();
  let verworfen = false, uhr = null;
  const aus = () => { clearInterval(uhr); strom.getTracks().forEach((t) => t.stop()); };
  rekorder.ondataavailable = (d) => d.data.size && teile.push(d.data);
  rekorder.onstop = () => { aus(); if (!verworfen) onEnde?.(new Blob(teile, { type: rekorder.mimeType || 'audio/webm' }), (Date.now() - start) / 1000); };
  rekorder.start();
  uhr = setInterval(() => {
    const sek = (Date.now() - start) / 1000;
    if (maxSek && sek >= maxSek && rekorder.state === 'recording') rekorder.stop();
    else onTick?.(sek);
  }, 200);
  return {
    get laeuft() { return rekorder.state === 'recording'; },
    stopp() { if (rekorder.state === 'recording') rekorder.stop(); },
    verwerfen() { verworfen = true; if (rekorder.state === 'recording') rekorder.stop(); else aus(); },
  };
}

// ---------- Notiz aufnehmen (Knopf + im Kopf) ----------
// Aufnahme bis 60 s → Ausgang (IndexedDB) → POST /api/notizen; gelöscht wird sie erst nach der Bestätigung.
const NOTIZ_MAX_SEK = 60;
const mmss = (sek) => { const s = Math.max(0, Math.round(sek)); return `${Math.floor(s / 60)}:${String(s % 60).padStart(2, '0')}`; };
// oeffneEingabe: ein Fenster für alle Eingaben (neue Notiz, KI-Anweisung, Antwort auf eine Planung): oben Text, darunter
// der Aufnahmeknopf (bis maxSek, tippen = Start, nochmal = Senden). Mit Text im Feld ist die Aufnahme gesperrt –
// entweder tippen oder sprechen. senden({text} | {audio}) legt den Posten in den Ausgang und liefert true = gesendet.
function oeffneEingabe({ titel, hinweis = '', kontext = '', platzhalter = '', maxText = 4000, maxSek = NOTIZ_MAX_SEK, entwurf = '', senden }) {
  if (document.querySelector('dialog.drawer')) return;
  const d = document.createElement('dialog');
  d.className = 'drawer aufnahmedrawer';
  const umfang = 2 * Math.PI * 46;
  const kann = !!(navigator.mediaDevices?.getUserMedia && window.MediaRecorder);
  d.innerHTML = T`<div class="griff" aria-hidden="true"></div><div class="zeile"><h2>${esc(titel)}</h2><button class="btn klein rund" data-d="zu" aria-label="Schließen">×</button></div>
    ${hinweis ? T`<p class="leise">${esc(hinweis)}</p>` : ''}
    ${kontext ? T`<div class="hinweis kontextzeile" data-kontext><div><div class="leise klein">Aufgenommen in</div><strong>${esc(kontext)}</strong></div><button type="button" class="btn klein rund haupt" data-d="kontext-weg" aria-label="Kontext entfernen">×</button></div>` : ''}
    <textarea class="eingabetext" rows="2" maxlength="${maxText}" placeholder="${esc(platzhalter)}" aria-label="Text"></textarea>
    <div class="eingabeleiste" hidden><button type="button" class="btn haupt" data-d="text">Senden</button></div>
    ${kann ? T`<button type="button" class="aufnahmeknopf" data-d="knopf" aria-label="Aufnahme starten">
        <svg viewBox="0 0 100 100" aria-hidden="true"><circle class="spur" cx="50" cy="50" r="46"/><circle class="fortschritt" cx="50" cy="50" r="46" stroke-dasharray="${umfang}" stroke-dashoffset="${umfang}"/></svg>
        <span class="symbol" aria-hidden="true"><svg viewBox="0 0 24 24"><rect x="9" y="3" width="6" height="11" rx="3" fill="currentColor"/><path d="M5.5 11a6.5 6.5 0 0 0 13 0M12 17.5V21" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"/></svg></span>
      </button>
      <p class="aufnahmezeit" aria-live="polite"><span data-zeit>0:00</span> / ${mmss(maxSek)}</p>
      <p class="leise aufnahmehinweis" data-hinweis>Tippen zum Aufnehmen, nochmal tippen zum Senden.</p>` : T('<p class="hinweis warn">Aufnahme wird von diesem Browser nicht unterstützt – bitte tippen.</p>')}`;
  const feld = d.querySelector('.eingabetext');
  const leiste = d.querySelector('.eingabeleiste');
  const knopf = d.querySelector('.aufnahmeknopf');
  const ring = d.querySelector('.fortschritt');
  const zeitEl = d.querySelector('[data-zeit]');
  const hinweisEl = d.querySelector('[data-hinweis]');
  let lauf = null, startet = false, fertigGesendet = false;
  try { feld.value = entwurf ? localStorage.getItem(entwurf) || '' : ''; } catch { /* nur Komfort */ }
  // Text im Feld: Senden zeigen, Aufnahme sperren; das Feld wächst mit (höchstens etwa 8 Zeilen).
  const pruefe = () => {
    const hatText = !!feld.value.trim();
    leiste.hidden = !hatText;
    if (knopf) knopf.disabled = hatText && !lauf;
    if (hinweisEl && !lauf) hinweisEl.textContent = hatText ? tx('Text senden – oder Feld leeren, um aufzunehmen.') : tx('Tippen zum Aufnehmen, nochmal tippen zum Senden.');
    feld.style.height = 'auto';
    feld.style.height = `${Math.min(feld.scrollHeight, 220)}px`;
  };
  feld.oninput = () => { try { if (entwurf) localStorage.setItem(entwurf, feld.value); } catch { /* nur Komfort */ } pruefe(); };
  const abschicken = async (posten) => {
    fertigGesendet = true;
    let gesendet;
    if (kontext) posten.kontext = kontext;
    try { gesendet = await senden(posten); } catch (f) { fertigGesendet = false; return melde(f.message); }
    try { if (entwurf) localStorage.removeItem(entwurf); } catch { /* nur Komfort */ }
    d.close();
    melde(gesendet ? tx('Gesendet') : tx('Vorgemerkt – wird bei Verbindung gesendet'));
    if (aktuell?.name === 'verlauf') zeigeAusgang();
  };
  const tick = (sek) => {
    ring.style.strokeDashoffset = String(umfang * (1 - Math.min(1, sek / maxSek)));
    zeitEl.textContent = mmss(sek);
  };
  const fertig = async (blob, sek) => {
    lauf = null;
    knopf.classList.remove('laeuft');
    knopf.disabled = true;
    if (!blob.size || sek < 0.5) { d.close(); return melde(tx('Aufnahme zu kurz')); }
    await abschicken({ audio: blob });
  };
  d.onclick = async (ev) => {
    if (ev.target === d) return d.close();
    const b = ev.target.closest('[data-d]');
    if (!b) return;
    if (b.dataset.d === 'zu') return d.close();
    if (b.dataset.d === 'kontext-weg') { kontext = ''; d.querySelector('[data-kontext]')?.remove(); return; }
    if (b.dataset.d === 'text') {
      const text = feld.value.trim();
      if (!text || fertigGesendet) return;
      b.disabled = true;
      return abschicken({ text });
    }
    if (b.dataset.d !== 'knopf' || startet || b.disabled) return;
    if (lauf) return lauf.stopp();
    startet = true;
    try {
      lauf = await starteAufnahme({ maxSek, onTick: tick, onEnde: fertig });
      if (!d.open) return lauf.verwerfen();
      knopf.classList.add('laeuft');
      knopf.setAttribute('aria-label', tx('Aufnahme beenden und senden'));
      feld.disabled = true;
      hinweisEl.textContent = tx('Aufnahme läuft – tippen zum Beenden und Senden.');
    } catch (f) {
      melde(tx('Mikrofon nicht verfügbar'));
    } finally { startet = false; }
  };
  // Schließen ohne Stopp verwirft die laufende Aufnahme; getippter Text bleibt als Entwurf.
  d.onclose = () => { lauf?.verwerfen(); d.remove(); };
  document.body.append(d);
  d.showModal();
  pruefe();
}

// Die drei Eingaben über dasselbe Fenster.
function eingabeNotiz() {
  oeffneEingabe({ titel: tx('Neue Notiz'), kontext: aufnahmeKontext || '', platzhalter: tx('Notiz tippen …'), entwurf: 'entwurf:notiz',
    senden: (x) => merkeVor({ typ: 'notiz', ...x }) });
}
function eingabeAnweisung(eintrag) {
  oeffneEingabe({ titel: tx('KI-Anweisung'), hinweis: tx('Was soll die KI an dieser Notiz ändern? Danach wertet sie sie neu aus.'),
    platzhalter: tx('z. B. In zwei Punkte trennen: Mitarbeiter und Kundenportal'), maxText: 500, entwurf: T`entwurf:anweisung:${eintrag}`,
    senden: (x) => merkeVor({ typ: 'anweisung', eintrag, ...x }) });
}
function eingabeAntwort(planung) {
  oeffneEingabe({ titel: tx('Antworten'), hinweis: tx('Die Antwort wird als neue Runde in die Planung eingearbeitet.'),
    platzhalter: tx('Antworten auf die offenen Fragen, Ergänzungen, Änderungen …'), entwurf: T`entwurf:${planung}`,
    senden: (x) => merkeVor({ typ: 'antwort', planung, ...x }) });
}
neuKnopf.onclick = eingabeNotiz;
// Alte Adressen der Eingabeseiten: Detailseite öffnen, dann das Fenster.
function eingabeVonAdresse(pfad, oeffne) {
  geheZu(pfad, true);
  setTimeout(oeffne, 400);
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
const LISTE_PUNKTE_SCHRITT = 10;

async function zeigeListen(filter) {
  if (filter && filter !== '-') aufnahmeKontext = `Gruppe ${filter}`;
  seite.innerHTML = T('<p class="leer">Lädt …</p>');
  // alle: Punkte sämtlicher Listen, erst geholt, wenn „Vergangene“ aufgeklappt wird (die Übersicht lädt nur aktive Punkte).
  let d = null, vergangeneZeigen = false, alle = null;
  // Ausgeklappt: unter jeder aktiven Liste ihre offenen (und kürzlich abgehakten) Punkte; je Gerät gemerkt.
  let ausgeklappt = false;
  try { ausgeklappt = localStorage.getItem('listen-ausgeklappt') === '1'; } catch { /* Standard: eingeklappt */ }
  // Je Liste, wie viele Punkte gerade gezeigt werden (Vorgabe LISTE_PUNKTE_SCHRITT).
  const gezeigt = new Map();
  // Tag-Filter (gleiche Auswahl wie im Verlauf): nur Listen mit passenden Punkten, diese dann aufgeklappt.
  // Tags kommen je Punkt von seiner Notiz bzw. Planung und werden nicht angezeigt.
  const tags = gewaehlteTags();
  const ladeAlle = async () => {
    try {
      const voll = await api('/api/listen?punkte=1');
      alle = new Map(voll.listen.map((x) => [x.id, x.punkte || []]));
      vorratNeu();
      male();
    } catch (f) { if (!(f instanceof NetzFehler)) melde(f.message); }
  };
  const vorratNeu = () => {
    const punkte = new Map();
    for (const l of d?.listen || []) for (const p of l.punkte || []) punkte.set(p.id, p);
    for (const ps of alle?.values() || []) for (const p of ps) punkte.set(p.id, p);
    const v = tagVorratAus(punkte.values());
    tagFilter.daten(v.vorrat, v.kombis);
  };
  const tagFilter = richteTagFilter({ tags, neu: () => { if (tags.length && !alle) ladeAlle(); male(); } });
  const punktHtml = (p) => T`<div class="lp${p.erledigt ? ' erledigt' : ''}">
    <button class="haken" data-haken="${p.id}" aria-pressed="${!!p.erledigt}" aria-label="${p.erledigt ? tx('Wieder öffnen') : tx('Abhaken')}: ${esc(p.text)}"><span class="hakenbild" aria-hidden="true"></span></button>
    <div class="lptext"><div class="lpzeile">${p.eintrag || p.planung ? T`<a class="lptitel" href="${p.eintrag ? T`/eintrag/${p.eintrag}` : T`/planung/${p.planung}`}">${esc(p.text)}</a>${punktEnde(p)}` : `${esc(p.text)}${punktEnde(p)}`}</div></div></div>`;
  const male = () => {
    if (aktuell?.name !== 'listen' || (aktuell.arg ? decodeURIComponent(aktuell.arg) : '') !== filter) return;
    const zeile = (l) => T`<a class="karte listenkarte" href="/liste/${l.id}"><span class="titel">${listenNr(l)}${esc(l.name)}</span>
        <span class="badge ${l.offen ? 'in_arbeit' : 'fertig'}" title="${tx('{0} von {1} offen', l.offen, l.gesamt)}">${l.offen}/${l.gesamt}</span></a>`;
    const gefiltert = tags.length > 0;
    const aktuellePunkte = (l) => (l.punkte || []).filter((p) => (!p.erledigt || p.erledigt > d.vergangen_vor) && passtTags(p, tags));
    // Je Liste höchstens LISTE_PUNKTE_SCHRITT Punkte, „Weitere laden“ zeigt die nächsten; alle stehen in der Liste selbst.
    const punkteHtml = (l, sichtbar) => {
      if (!sichtbar.length) return '';
      const n = gezeigt.get(l.id) || LISTE_PUNKTE_SCHRITT;
      const rest = sichtbar.length - n;
      return T`<div class="lpgruppe">${sichtbar.slice(0, n).map(punktHtml).join('')}${rest > 0 ? T`<div class="lpmehr"><button class="btn klein" data-mehr="${l.id}">${tx('Weitere {0} laden', Math.min(rest, LISTE_PUNKTE_SCHRITT))}</button></div>` : ''}</div>`;
    };
    const mitPunkten = (l) => {
      const sichtbar = ausgeklappt || gefiltert ? aktuellePunkte(l) : [];
      return zeile(l) + punkteHtml(l, sichtbar);
    };
    // Vergangene Listen sehen aus wie die aktiven: ausgeklappt mit allen (erledigten) Punkten.
    const mitAllenPunkten = (l) => {
      const sichtbar = ausgeklappt || gefiltert ? (alle?.get(l.id) || l.punkte || []).filter((p) => passtTags(p, tags)) : [];
      return zeile(l) + punkteHtml(l, sichtbar);
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
    // Aktiv: Listen mit offenen oder kürzlich abgehakten Punkten (LIST_DONE_ACTIVE_MINUTES) – eine eben ganz erledigte
    // Liste bleibt so lange stehen wie ihre Punkte; ältere ganz erledigte und leere stehen unter „Vergangen“.
    const istAktiv = (l) => l.offen > 0 || (l.punkte || []).some((p) => p.erledigt && p.erledigt > d.vergangen_vor);
    // Filter nach Gruppe (je Gerät gemerkt): '' = alle, '-' = ohne Gruppe, sonst der Gruppenname.
    const namen = [...new Set(d.listen.map((l) => l.projekt).filter(Boolean))];
    const gruppe = filter && filter !== '-' ? namen.find((n) => n.toLowerCase() === filter.toLowerCase()) : '';
    const passt = (l) => !filter || (filter === '-' ? !l.projekt : (l.projekt || '').toLowerCase() === filter.toLowerCase());
    const sichtbar = d.listen.filter(passt);
    // Mit Tag-Filter: aktive Listen mit passendem aktuellem Punkt, vergangene mit irgendeinem passenden (sobald alle Punkte da sind).
    const aktiv = sichtbar.filter((l) => istAktiv(l) && (!gefiltert || aktuellePunkte(l).length > 0));
    const vergangen = sichtbar.filter((l) => !istAktiv(l) && (!gefiltert || (alle?.get(l.id) || []).some((p) => passtTags(p, tags))));
    // Titelzeile: links Stift (Gruppe bearbeiten), rechts großes Symbol zum Aus-/Einklappen der offenen Punkte.
    const klapp = aktiv.length || (vergangeneZeigen && vergangen.length) ? T`<button class="titelknopf gross" data-ausklappen="1" aria-pressed="${ausgeklappt}" aria-label="${ausgeklappt ? tx('Offene Punkte einklappen') : tx('Alle offenen Punkte ausklappen')}" title="${ausgeklappt ? tx('Offene Punkte einklappen') : tx('Alle offenen Punkte ausklappen')}">${ausgeklappt ? EINKLAPPEN : AUSKLAPPEN}</button>` : '';
    const titel = gruppenTitel(gruppe || filter);
    const kopf = filter
      ? T`${krumen([[tx('Listen'), '/listen'], [titel]])}<div class="titelzeile">${gruppe ? T`<a class="titelknopf" href="/gruppe/${encodeURIComponent(gruppe)}/bearbeiten" aria-label="${esc(tx('Gruppe {0} bearbeiten', gruppe))}" title="Gruppe bearbeiten">${STIFT}</a>` : ''}<h1>${esc(titel)}</h1>${klapp}</div>`
      : T`<div class="titelzeile"><h1>Listen</h1>${klapp}</div><p class="leise">Die KI sortiert Notizen selbst als Punkte in Listen, Listen eines größeren Themas in Gruppen. Von Hand über „In Liste“ an der Notiz.</p>`;
    seite.innerHTML = `${kopf}<div id="tagleiste" class="tagleiste" hidden></div>
      ${filter && !sichtbar.length ? T('<p class="leer">Keine Einträge vorhanden.</p>') : !d.listen.length ? T('<p class="leer">Noch keine Liste. Einfach einen Filmtitel oder eine Idee diktieren.</p>') : aktiv.length ? gruppiert(aktiv, mitPunkten) : gefiltert ? T('<p class="leer">Keine offenen Punkte mit diesen Tags.</p>') : T('<p class="leer">Alles erledigt.</p>')}
      ${vergangen.length ? T`<div class="leiste"><button class="btn klein" data-vergangen="1">${vergangeneZeigen ? tx('Vergangene ausblenden') : T`Vergangene Listen anzeigen (${vergangen.length})`}</button></div>
        ${vergangeneZeigen ? T`<h2>Vergangen</h2>${gruppiert(vergangen, mitAllenPunkten)}` : ''}` : ''}`;
    tagFilter.male();
  };
  const laden = async (vorab) => {
    try { d = await apiAbgelegt('/api/listen?punkte=aktiv', vorab); } catch (f) {
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
    vorratNeu();
    male();
    if (tags.length && !alle && !vorab) ladeAlle();
  };
  aktuell.neuLaden = laden;
  seite.onclick = async (ev) => {
    const b = ev.target.closest('button');
    if (!b || !d) return;
    if (b.dataset.vergangen) {
      vergangeneZeigen = !vergangeneZeigen;
      male();
      if (vergangeneZeigen && !alle) await ladeAlle();
      return;
    }
    if (b.dataset.mehr) {
      gezeigt.set(b.dataset.mehr, (gezeigt.get(b.dataset.mehr) || LISTE_PUNKTE_SCHRITT) + LISTE_PUNKTE_SCHRITT);
      male();
      return;
    }
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
  await aendereAbgelegt('/api/listen?punkte=aktiv', (d) => ({ ...d, listen: d.listen.map((x) => (x.id === listeId
    ? { ...x, offen: x.offen + (punkt.erledigt ? -1 : 1), erledigt_am: punkt.erledigt ? Math.max(x.erledigt_am || 0, punkt.erledigt) : x.erledigt_am,
      punkte: x.punkte?.map((y) => (y.id === punkt.id ? { ...y, erledigt: punkt.erledigt } : y)) }
    : x)) }));
}

async function zeigeListe(id) {
  seite.innerHTML = T('<p class="leer">Lädt …</p>');
  let l = null, vergangeneZeigen = false;
  // Tag-Filter (gleiche Auswahl wie im Verlauf): nur Punkte, deren Notiz bzw. Planung alle gewählten Tags trägt.
  const tags = gewaehlteTags();
  const tagFilter = richteTagFilter({ tags, neu: () => male() });
  const punkt = (p) => T`<div class="lp${p.erledigt ? ' erledigt' : ''}">
    <button class="haken" data-haken="${p.id}" aria-pressed="${!!p.erledigt}" aria-label="${p.erledigt ? tx('Wieder öffnen') : tx('Abhaken')}: ${esc(p.text)}"><span class="hakenbild" aria-hidden="true"></span></button>
    <div class="lptext"><div class="lpzeile">${p.eintrag || p.planung ? T`<a class="lptitel" href="${p.eintrag ? T`/eintrag/${p.eintrag}` : T`/planung/${p.planung}`}">${esc(p.text)}</a>${punktEnde(p)}` : `${esc(p.text)}${punktEnde(p)}`}</div></div></div>`;
  const male = () => {
    if (aktuell?.name !== 'liste' || aktuell.arg !== id) return;
    const offen = l.punkte.filter((p) => !p.erledigt), fertig = l.punkte.filter((p) => p.erledigt);
    // Frisch Erledigtes bleibt eine Weile bei den aktiven Punkten, älteres steht eingeklappt unter „Vergangen“.
    const passend = (p) => passtTags(p, tags);
    const kuerzlich = fertig.filter((p) => p.erledigt > l.vergangen_vor && passend(p)), vergangen = fertig.filter((p) => p.erledigt <= l.vergangen_vor && passend(p));
    const aktiv = [...offen.filter(passend), ...kuerzlich];
    seite.innerHTML = T`${krumen([[tx('Listen'), '/listen'], [gruppenTitel(l.projekt), gruppenPfad(l.projekt)], [`${l.nr ? `L${l.nr} ` : ''}${l.name}`]])}
      <h1>${listenNr(l)}${esc(l.name)}</h1><div class="leise">${l.projekt ? T`Gruppe ${esc(l.projekt)} · ` : ''}${offen.length} offen · ${fertig.length} erledigt</div>
      ${l.beschreibung ? T`<p class="leise">${esc(l.beschreibung)}</p>` : ''}<div id="tagleiste" class="tagleiste" hidden></div>
      ${aktiv.length ? aktiv.map(punkt).join('') : T`<p class="leer">${tags.length ? tx('Keine offenen Punkte mit diesen Tags.') : fertig.length ? tx('Alles erledigt.') : tx('Die Liste ist leer.')}</p>`}
      ${vergangen.length ? T`<div class="leiste"><button class="btn klein" data-vergangen="1">${vergangeneZeigen ? tx('Vergangene ausblenden') : T`Vergangene anzeigen (${vergangen.length})`}</button></div>
        ${vergangeneZeigen ? T`<h2>Vergangen</h2>${vergangen.map(punkt).join('')}` : ''}` : ''}
      <div class="leiste">${l.joplin_id ? T`<a class="btn" href="joplin://x-callback-url/openNote?id=${esc(l.joplin_id)}" target="_self">In Joplin öffnen</a>` : ''}<a class="btn" href="/liste/${id}/bearbeiten">Bearbeiten</a><button class="btn gefahr" data-liste-weg="1">Liste löschen</button></div>`;
    const v = tagVorratAus(l.punkte);
    tagFilter.daten(v.vorrat, v.kombis);
  };
  const laden = async (vorab) => {
    const hier = () => aktuell?.name === 'liste' && aktuell.arg === id;
    try {
      l = await apiAbgelegt(T`/api/listen/${id}`, vorab);
      if (aktuell?.name === 'liste' && aktuell.arg === id) aufnahmeKontext = `${l.projekt && l.projekt !== '-' ? `Gruppe ${l.projekt} · ` : ''}Liste ${l.nr ? `L${l.nr} ` : ''}${l.name}`;
      male();
    } catch (f) {
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
    <input type="search" id="zielsuche" placeholder="Liste oder Gruppe suchen …" aria-label="Liste oder Gruppe suchen" autocomplete="off" enterkeyhint="search">
    <div id="ziele"></div>
    <label for="neueliste">Neue Liste</label>
    <div class="zeile"><input type="text" id="neueliste" maxlength="40" placeholder="z. B. Filme"><button class="btn" data-neu="1">Anlegen</button></div>
    <div class="leiste">${e.liste ? T`<a class="btn" href="/liste/${e.liste.id}">Zur Liste „${esc(listenName(e.liste))}“</a>` : ''}<a class="btn" href="${zurueck}">Abbrechen</a></div>`;
  // Ziele je Gruppe zum Aufklappen (Gruppe mit dem jüngsten Punkt zuerst, „Ohne Gruppe“ am Ende); die Suche filtert
  // Listen- und Gruppennamen und klappt Gruppen mit Treffern auf.
  const box = seite.querySelector('#ziele'), suche = seite.querySelector('#zielsuche');
  const gruppen = [];
  for (const z of [...ziele].sort((x, y) => (y.neuester || 0) - (x.neuester || 0))) {
    const g = gruppen.find(([n]) => n === (z.projekt || ''));
    if (g) g[1].push(z); else gruppen.push([z.projekt || '', [z]]);
  }
  gruppen.sort(([x], [y]) => (x === '') - (y === ''));
  const maleZiele = () => {
    const q = suche.value.trim().toLowerCase();
    const html = gruppen.map(([g, ls]) => {
      const gruppeTrifft = q && (g || tx('Ohne Gruppe')).toLowerCase().includes(q);
      const sichtbar = !q || gruppeTrifft ? ls : ls.filter((z) => z.name.toLowerCase().includes(q));
      if (!sichtbar.length) return '';
      return T`<details class="zielgruppe"${q ? ' open' : ''}><summary><span>${esc(g ? g : tx('Ohne Gruppe'))}</span><span class="zaehler still">${sichtbar.length}</span></summary>
        ${sichtbar.map((z) => T`<div class="karte listenkarte"><span class="titel">${listenNr(z)}${esc(z.name)}</span><span class="leise">${z.gesamt} ${z.gesamt === 1 ? tx('Punkt') : tx('Punkte')}</span><button class="btn klein haupt" data-ziel="${z.id}">Hierher</button></div>`).join('')}</details>`;
    }).join('');
    box.innerHTML = html || T('<p class="leise">Keine passende Liste – unten neu anlegen.</p>');
  };
  suche.oninput = maleZiele;
  maleZiele();
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

// Auf-/Zugeklappt der Statuskarten merken: übersteht Nachladen (Aufträge in Arbeit) und Neuladen der Seite.
const AUF_SCHLUESSEL = 'status-aufgeklappt';
const aufLesen = () => { try { return JSON.parse(localStorage.getItem(AUF_SCHLUESSEL)) || {}; } catch { return {}; } };
const aufAttr = (k) => (aufLesen()[k] ? ' open' : '') + ` data-auf="${k}"`;
document.addEventListener('toggle', (ev) => {
  const k = ev.target?.dataset?.auf;
  if (!k) return;
  const m = aufLesen();
  if (ev.target.open) m[k] = 1; else delete m[k];
  try { localStorage.setItem(AUF_SCHLUESSEL, JSON.stringify(m)); } catch { /* nur Komfort */ }
}, true);

// Status → Transkription: Art, bei lokal das Modell mit Zustand, gemessenem Tempo und letztem Fehler.
function transkriptionKarte(t) {
  if (!t) return '';
  const art = esc(t.lokal ? tx('Whisper (dieser Server)') : tx(TRANSKRIPTION[t.modus] || t.modus));
  if (!t.lokal) return T`<dl><dt>Transkription</dt><dd>${art}</dd></dl>`;
  const m = t.modell || {};
  const zustand = m.geladen ? T('<span class="gut">bereit</span>') : m.laedt ? T`${tx('lädt {0} %', m.prozent)}` : m.fehler ? T`<span class="schlecht">${tx('Laden gescheitert')}: ${esc(m.fehler)}</span>` : T('<span class="schlecht">nicht geladen</span>');
  const tempo = m.faktor_gemessen ? tx('1 Min. Aufnahme ≈ {0} s (Mittel aus {1} Läufen)', Math.round(m.faktor_gemessen * 60), m.laeufe) : tx('noch nicht gemessen');
  return T`<details${aufAttr('transkription')} class="karte speicherkarte"><summary><span class="zeile kartentitel"><strong>Transkription</strong><span class="leise">${art}</span></span>
      <span class="zeile"><span>Whisper ${esc(m.name || '')}</span><span>${zustand}</span></span>${t.letzter_fehler ? T`<span class="hinweis schlecht">${tx('Zuletzt gescheitert')} (${esc(zeit(t.letzter_fehler_zeit))}): ${esc(t.letzter_fehler)}</span>` : ''}</summary>
    <div class="speicherdetails"><dl class="systemliste"><dt>${tx('Im RAM')}</dt><dd>${t.im_ram ? esc(tx('seit {0}', zeit(t.im_ram_seit))) : esc(tx('nein'))} <span class="leise">${t.ram_halten_minuten ? esc(tx('hält {0} Min nach der letzten Aufnahme', t.ram_halten_minuten)) : esc(tx('dauerhaft'))}</span></dd><dt>${tx('Tempo')}</dt><dd>${esc(tempo)}</dd><dt>${tx('Kerne')}</dt><dd>${t.kerne}</dd><dt>${tx('Engine')}</dt><dd>${esc(t.engine || tx('fehlt in diesem Build'))}</dd></dl>
    ${istAdmin() ? T('<div class="leiste"><a class="btn klein" href="/status/ki/transkription">Ändern</a></div>') : ''}</div></details>`;
}

async function zeigeStatus() {
  seite.innerHTML = T`${statusReiter('status')}<p class="leer">Lädt …</p>`;
  const laden = async (vorab) => {
    let s, sy;
    try {
      [s, sy] = await Promise.all([apiAbgelegt('/api/status', vorab), apiAbgelegt('/api/status/system', vorab).catch(() => null), vorab || ladeZaehler()]);
    } catch (f) {
      if (vorab) return;
      if (f instanceof NetzFehler) { if (aktuell?.name === 'status') seite.innerHTML = T('<p class="leer">Offline – der Status ist noch nicht gespeichert.</p>'); return; }
      throw f;
    }
    if (aktuell?.name !== 'status') return;
    // Speicher: zugeklappt je Ablage Name, belegt und Balken; aufgeklappt Pfad und frei/gesamt.
    const balken = (p) => T`<meter class="balken${p.knapp ? ' knapp' : ''}" min="0" max="${p.gesamt}" value="${p.gesamt - p.frei}" aria-label="${esc(tx('{0} von {1} belegt', groesse(p.gesamt - p.frei), groesse(p.gesamt)))}"></meter>`;
    // Eigene Ablage: ein Balken über den ganzen Mount – Akzent = eigener Anteil, grau = übrige Nutzer und Sonstiges,
    // zusammen = belegt. SVG-Breiten statt style-Attribute (CSP).
    const balkenEigen = (p) => {
      const belegt = Math.max(0, p.gesamt - p.frei), anteil = (v) => (p.gesamt > 0 ? Math.min(100, (v / p.gesamt) * 100) : 0);
      const eigen = anteil(Math.min(p.belegt || 0, belegt)), alle = anteil(belegt);
      return T`<svg class="balken zweiteilig${p.knapp ? ' knapp' : ''}" viewBox="0 0 100 4" preserveAspectRatio="none" role="img" aria-label="${esc(tx('{0} eigene, {1} von {2} belegt', groesse(p.belegt || 0), groesse(belegt), groesse(p.gesamt)))}"><rect class="rest" width="${alle.toFixed(2)}" height="4"/><rect class="eigen" width="${eigen.toFixed(2)}" height="4"/></svg>`;
    };
    // Aufräumen am Ende, je ein Knopf: Backups außer den 10 neuesten und App-Versionen außer der laufenden.
    // Beide Knöpfe stehen immer da; ohne Löschbares ausgegraut mit Grund (sonst sucht man den fehlenden Knopf).
    const aufraeumen = (x) => (x ? T`<div class="leiste aufraeumen">
      ${x.backups ? T`<button class="btn gefahr" data-aufraeumen="backups" data-n="${x.backups}" data-groesse="${x.backups_groesse}">${tx('{0} alte Backups löschen', x.backups)} · ${groesse(x.backups_groesse)}</button>` : T`<button class="btn" disabled>${tx('Nicht mehr als 10 Backups')}</button>`}
      ${x.versionen ? T`<button class="btn gefahr" data-aufraeumen="versionen" data-n="${x.versionen}" data-groesse="${x.versionen_groesse}">${tx('{0} alte App-Versionen löschen', x.versionen)} · ${groesse(x.versionen_groesse)}</button>` : T`<button class="btn" disabled>${tx('Keine alten App-Versionen')}</button>`}</div>` : '');
    // Zugeklappt eigene Ablagen, App-Versionen, Backups und knappe/unlesbare; aufgeklappt alle mit Pfad, frei/gesamt (nicht bei eigenen: gleich wie beim Mount).
    // Admin: Kreis je Ablage (Daten, Medien) nach Nutzer, darunter die Liste – wer belegt am meisten.
    const nutzerName = (n) => n.name || tx('Id {0}', n.id);
    const kreis = (titel, werte) => {
      const summe = werte.reduce((a, b) => a + b, 0);
      if (!summe) return T`<div class="nutzerkreis"><strong>${esc(titel)}</strong><p class="leise">leer</p></div>`;
      let w = -Math.PI / 2;
      const teile = werte.map((v, i) => {
        if (!v) return '';
        const anteil = v / summe, w2 = w + anteil * 2 * Math.PI;
        const p = (a) => `${(60 + 55 * Math.cos(a)).toFixed(2)} ${(60 + 55 * Math.sin(a)).toFixed(2)}`;
        const form = anteil >= 0.9999 ? `<circle class="k${i % 8}" cx="60" cy="60" r="55"/>` : `<path class="k${i % 8}" d="M60 60 L${p(w)} A55 55 0 ${anteil > 0.5 ? 1 : 0} 1 ${p(w2)} Z"/>`;
        w = w2;
        return form;
      }).join('');
      return T`<div class="nutzerkreis"><strong>${esc(titel)}</strong><svg class="diagramm torte" viewBox="0 0 120 120" role="img" aria-label="${esc(titel)}">${teile}</svg><span class="leise">${groesse(summe)}</span></div>`;
    };
    const nachNutzer = (liste) => {
      if (!liste?.length) return '';
      const prozent = (v, ges) => (ges ? `${Math.round((v / ges) * 100)} %` : '–');
      const gd = liste.reduce((a, n) => a + n.daten, 0), gm = liste.reduce((a, n) => a + n.medien, 0);
      return T`<div class="nachnutzer"><strong>Nach Nutzer</strong>
        <div class="nutzerkreise">${kreis(tx('Daten'), liste.map((n) => n.daten))}${kreis(tx('Medien'), liste.map((n) => n.medien))}</div>
        <div class="tabelle"><table class="werte"><thead><tr><th>Nutzer</th><th>Daten</th><th>Medien</th></tr></thead><tbody>
        ${liste.map((n, i) => T`<tr><td><i class="punkt k${i % 8}"></i>${esc(nutzerName(n))}</td><td>${groesse(n.daten)} <span class="leise">${prozent(n.daten, gd)}</span></td><td>${groesse(n.medien)} <span class="leise">${prozent(n.medien, gm)}</span></td></tr>`).join('')}
        </tbody></table></div></div>`;
    };
    const speicher = (liste, x, nutzer) => { const eigen = liste.filter((p) => !p.art || p.art === 'eigen'), oben = liste.filter((p) => !p.art || p.art === 'eigen' || p.art === 'system' || p.knapp || p.fehler);
      // Kopfzahl: alles, was die App belegt – Summen aller Nutzer (Admin) bzw. die eigenen, dazu App-Versionen und Backups.
      const alleN = liste.filter((p) => p.art === 'alle'), summe = (l) => l.reduce((n, p) => n + (p.belegt || 0), 0);
      const gesamt = summe(alleN.length ? alleN : eigen) + summe(liste.filter((p) => p.art === 'system'));
      return T`<details${aufAttr('speicher')} class="karte speicherkarte"><summary><span class="zeile kartentitel"><strong>Speicher</strong><span class="eingangszahl">${groesse(gesamt)}</span></span><span class="speicherraster">${oben.map((p) => T`<span class="speicherzeile"><span class="zeile"><span>${esc(tx(p.name))}</span>${p.fehler ? T`<span class="schlecht">nicht lesbar</span>` : T`<span>${groesse(p.belegt)}</span>`}</span>
        ${p.fehler ? '' : (p.art === 'eigen' ? balkenEigen(p) : balken(p))}${p.knapp ? (p.aufnahmen ? T('<span class="hinweis schlecht">Knapp – neue Aufnahmen werden abgewiesen.</span>') : T('<span class="hinweis schlecht">Fast voll.</span>')) : ''}</span>`).join('')}</span></summary>
      <div class="speicherdetails speicherraster">${liste.map((p) => T`<div><strong>${esc(tx(p.name))}</strong>
        <div class="pfadzeile leise"><span class="pfad">${esc(p.pfad || '')}</span>${p.fehler || p.art === 'eigen' ? '' : T`<span class="frei">frei ${groesse(p.frei)} von ${groesse(p.gesamt)}</span>`}</div>
        ${p.fehler ? T`<div class="hinweis schlecht">nicht lesbar: ${esc(p.fehler)}</div>` : ''}</div>`).join('')}${nachNutzer(nutzer)}${aufraeumen(x)}</div></details>`; };
    // System: zugeklappt Betriebssystem, CPU- und RAM-Balken; aufgeklappt Maschine, dieser Dienst und die Prozesse mit der höchsten CPU-Last.
    const system = (x) => {
      if (!x) return T('<div class="karte"><strong>System</strong> <span class="schlecht">nicht lesbar</span></div>');
      const c = x.cpu || {}, r = x.ram || {}, d = x.dienst || {}, m = x.system || {};
      const prozent = (v) => (v == null ? '–' : `${v.toLocaleString(LOCALE)} %`);
      const zeitraum = (s) => { if (s == null) return '–'; const t = Math.floor(s / 86400), h = Math.floor(s / 3600) % 24, mi = Math.floor(s / 60) % 60; return t ? tx('{0} T {1} Std', t, h) : h ? tx('{0} Std {1} Min', h, mi) : tx('{0} Min', mi); };
      const gr = (n) => (n == null || n < 0 ? '–' : groesse(n));
      const ramBelegt = r.gesamt > 0 && r.verfuegbar >= 0 ? r.gesamt - r.verfuegbar : null;
      const zeile = (name, wert, balken) => T`<span class="speicherzeile"><span class="zeile"><span>${esc(name)}</span><span>${wert}</span></span>${balken}</span>`;
      const cpuBalken = c.auslastung == null ? '' : T`<meter class="balken${c.auslastung >= 90 ? ' knapp' : ''}" min="0" max="100" value="${c.auslastung}" aria-label="${esc(tx('CPU {0} %', c.auslastung))}"></meter>`;
      const ramBalken = ramBelegt == null ? '' : T`<meter class="balken${ramBelegt / r.gesamt >= 0.9 ? ' knapp' : ''}" min="0" max="${r.gesamt}" value="${ramBelegt}" aria-label="${esc(tx('{0} von {1} belegt', groesse(ramBelegt), groesse(r.gesamt)))}"></meter>`;
      const dl = (paare) => T`<dl class="systemliste">${paare.filter(([, v]) => v != null && v !== '').map(([k, v]) => T`<dt>${esc(k)}</dt><dd>${esc(String(v))}</dd>`).join('')}</dl>`;
      const swap = r.swap_gesamt > 0 ? tx('{0} von {1} belegt', gr(r.swap_gesamt - r.swap_frei), gr(r.swap_gesamt)) : tx('keiner');
      return T`<details${aufAttr('system')} class="karte speicherkarte systemkarte"><summary><span class="zeile kartentitel"><strong>System</strong><span class="leise">${esc(m.os || tx('unbekannt'))}</span></span>
          ${zeile(tx('CPU'), prozent(c.auslastung), cpuBalken)}${zeile(tx('RAM'), ramBelegt == null ? '–' : T`${groesse(ramBelegt)} / ${groesse(r.gesamt)}`, ramBalken)}</summary>
        <div class="speicherdetails">
          <div><strong>${esc(tx('Maschine'))}</strong>${dl([[tx('Betriebssystem'), m.os], [tx('Kernel'), m.kernel], [tx('Architektur'), m.architektur], [tx('Container'), m.container], [tx('Hostname'), m.hostname], [tx('Läuft seit'), zeitraum(m.laufzeit_s)],
            [tx('CPU'), c.modell], [tx('Kerne'), c.kerne], [tx('Last 1/5/15 min'), (c.load || []).join(' / ')], [tx('Swap'), swap], [tx('Prozesse'), x.prozesse_anzahl]])}</div>
          <div><strong>${esc(tx('Dieser Dienst'))}</strong>${dl([[tx('Version'), d.version], [tx('CPU'), prozent(d.cpu)], [tx('RAM (RSS)'), gr(d.rss)], [tx('Go-Heap'), gr(d.heap)], [tx('Threads'), d.threads], [tx('Goroutinen'), d.goroutinen], [tx('Go'), d.go], [tx('PID'), d.pid], [tx('Läuft seit'), zeitraum(d.laufzeit_s)]])}</div>
          ${(x.prozesse || []).length ? T`<div><strong>${esc(tx('Prozesse nach CPU'))}</strong><ul class="prozessliste">${x.prozesse.map((p) => T`<li><span class="pfad">${esc(p.name)} <span class="leise">${p.pid}</span></span><span>${prozent(p.cpu)} · ${gr(p.rss)}</span></li>`).join('')}</ul></div>` : ''}
        </div></details>`;
    };
    const joplin = !s.joplin.aktiv ? tx('ausgeschaltet') : s.joplin.fehler ? T`<strong>Fehler:</strong> ${esc(s.joplin.fehler)}` : T`erreichbar${s.joplin.offene_planungen ? T`, ${s.joplin.offene_planungen} Planung(en) noch nicht übertragen` : ''}`;
    const updateKarte = neueVersion ? T`<a class="karte updatekarte" href="/status/update">${updateSymbol}<span><strong>Update ${esc(versionNr(neueVersion))} verfügbar</strong><br><span class="leise">Änderungen ansehen und installieren</span></span></a>` : '';
    seite.innerHTML = T`${statusReiter('status')}<h1>Status</h1>${updateKarte}
      ${s.fehlgeschlagen ? T`<div class="hinweis schlecht">${s.fehlgeschlagen} Auftrag/Aufträge dauerhaft fehlgeschlagen.</div>` : T('<div class="hinweis gut">Keine fehlgeschlagenen Aufträge.</div>')}
      ${(s.audio_unbekannt || []).length ? T`<h2>Aufnahmen ohne Eintrag <span class="zaehler fehler">${s.audio_unbekannt.length}</span></h2>
        <div class="hinweis schlecht">Im Audio-Ordner liegen Aufnahmen, die die Datenbank nicht kennt – z. B. nach dem Wiederherstellen eines älteren Backups. Importieren legt je Aufnahme einen neuen Eintrag an (Eingang = Zeitpunkt der Datei); er wird neu transkribiert und von der KI ausgewertet. Planungen kommen danach dran: eine Runde ohne Rückfragen, die KI ordnet sie einer Planung zu.</div>
        ${s.audio_unbekannt.map((f) => T`<div class="karte"><div class="zeile"><span><span class="badge fehler">ohne Eintrag</span> <span class="badge ${esc(f.modus)}">${esc(MODUS[f.modus] || f.modus)}</span>${f.web ? T(' <span class="badge">Web-App</span>') : ''} ${zeit(f.zeit)}</span><button class="btn klein" data-import="${esc(f.datei)}">Importieren</button></div>
          <div class="leise">${esc(f.datei)} · ${groesse(f.groesse)}</div></div>`).join('')}
        ${s.audio_unbekannt.length > 1 ? T('<div class="leiste"><button class="btn haupt" data-import-alle="1">Alle importieren</button></div>') : ''}` : ''}
      ${s.joplin.aktiv ? T`<dl><dt>Joplin</dt><dd>${joplin}</dd></dl>` : ''}
      ${s.joplin.aktiv ? T('<div class="leiste"><button class="btn klein" data-a="pruefen">Joplin jetzt prüfen</button></div>') : ''}
      ${transkriptionKarte(s.transkription)}
      ${(s.speicher || []).length ? speicher(s.speicher, s.aufraeumen, s.speicher_nutzer) : ''}
      ${system(sy)}
      ${s.probleme.length ? T`<h2>Aufträge mit Fehlern</h2>${s.fehlgeschlagen ? T('<div class="leiste"><button class="btn gefahr" data-a="fehler-weg">Alle fehlerhaften löschen</button></div>') : ''}${s.probleme.map(karte).join('')}` : ''}
      ${s.statistik ? T`<h2>${esc(tx('Statistik und Rückmeldung'))}</h2>
      <a class="karte" href="/status/statistik"><strong>${esc(tx(istAdmin() ? 'Anonyme Statistik' : 'Ring-Lebensdauer aller Installationen'))}</strong><br><span class="leise">${esc(tx(istAdmin() ? 'Ein- oder ausschalten, Ring-Lebensdauer aller Installationen' : 'Vergleich mit allen Installationen'))}</span></a>
      <a class="karte" href="/status/rueckmeldung"><strong>${esc(tx('Rückmeldung senden'))}</strong><br><span class="leise">${esc(tx('Kommentar oder Fehler an den Entwickler'))}</span></a>` : ''}
      ${spendenLink ? T`<section class="spendenbereich"><p><strong>${esc(tx('Gefällt dir die App?'))}</strong> <span class="leise">${esc(tx('Sie ist kostenlos und quelloffen. Wer mag, unterstützt die Entwicklung mit einer kleinen Spende auf Ko-fi.'))}</span></p>
        ${spendenKnopf()}</section>` : ''}`;
    setzeWortlage(seite);
  };
  aktuell.neuLaden = laden;
  seite.onclick = async (ev) => {
    const b = ev.target.closest('button');
    if (!b) return;
    try {
      if (b.dataset.import || b.dataset.importAlle) {
        b.disabled = true;
        const r = await api('/api/audio-unbekannt/import', { method: 'POST', body: b.dataset.importAlle ? { alle: true } : { datei: b.dataset.import } });
        melde(tx('{0} Aufnahme(n) importiert – werden ausgewertet', r.importiert));
        verlaufStand = null;
        await ladeZaehler();
        return laden();
      }
      if (b.dataset.aufraeumen) {
        const backups = b.dataset.aufraeumen === 'backups', n = b.dataset.n, gr = groesse(Number(b.dataset.groesse));
        if (!confirm(backups ? tx('{0} Backups löschen ({1})? Die 10 neuesten bleiben.', n, gr)
          : tx('{0} zwischengespeicherte App-Versionen löschen ({1})? Die laufende Version bleibt; ältere lassen sich bei Bedarf neu laden.', n, gr))) return;
        b.disabled = true;
        const d = await api(T`/api/speicher/aufraeumen?was=${b.dataset.aufraeumen}`, { method: 'POST' });
        melde(backups ? tx('{0} Backups gelöscht', d.backups) : tx('{0} App-Versionen gelöscht', d.versionen));
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
      }
    } catch (f) { b.disabled = false; melde(f.message); }
  };
  aufraeumen = () => { seite.onclick = null; };
  await laden(true).catch(() => {});
  await laden();
}

// ---------- KI-Kosten ----------


// ---------- KI-Anfragen (Protokoll): Zweck und roher Verbrauch ----------

const KI_ZWECK = { transkription: tx('Transkription'), diktat: tx('KI-Auswertung'), auswertung: tx('KI-Auswertung'), kurztitel: tx('Kurztitel'), recherche: tx('Recherche'), planung: tx('Recherche'),
  bilder: tx('Bild'), zuordnung: tx('Zuordnung'), einsortieren: tx('Einsortieren'), termin: tx('Termin'), trailer: tx('Trailer'), beschreibungen: tx('Beschreibungen'),
  entscheidung: tx('Entscheidung'), lernlauf: tx('Lernlauf'), beobachtungen: tx('Beobachtungen auswerten'), uebertrag: tx('Übertrag (vor dem Protokoll)') };
const kiZweck = (q) => KI_ZWECK[q.zweck] || KI_ZWECK[q.bereich] || q.zweck || q.bereich;
const ganz = (n) => Math.round(n).toLocaleString(LOCALE);
// „OpenAI · gpt-x · 4.210 ein / 380 aus · 2 Suchen · 0,023 $“ – nur was bekannt ist und nicht 0.
function kiVerbrauch(q, ohneAnbieter) {
  const t = ohneAnbieter ? [] : [q.anbieter, q.modell].filter(Boolean);
  if (q.tokens_ein != null || q.tokens_aus != null) t.push(T`${ganz(q.tokens_ein || 0)} ein / ${ganz(q.tokens_aus || 0)} aus`);
  if (q.sekunden != null) t.push(`${ganz(q.sekunden)} s`);
  if (q.suchen) t.push(`${ganz(q.suchen)} ${q.suchen === 1 ? tx('Suche') : tx('Suchen')}`);
  if (q.bilder) t.push(`${ganz(q.bilder)} ${q.bilder === 1 ? tx('Bild') : tx('Bilder')}`);
  if (q.anzahl > 1) t.push(`${ganz(q.anzahl)} ${tx('Anfragen')}`);
  if (q.kosten_usd > 0) t.push(`${q.kosten_usd.toFixed(3)} $`);
  return t.join(' · ');
}

// ---------- Gedächtnis: was die KI über den Nutzer gelernt hat ----------

const GED_REITER = [['vorschlaege', tx('Vorschläge')], ['aktiv', tx('Aktiv')], ['begriffe', tx('Begriffe')], ['geloescht', tx('Gelöscht')]];
const GED_QUELLE = { ki: 'KI', lernlauf: tx('Lernlauf'), korrektur: tx('Korrektur'), antwort: tx('Antwort'), anweisung: tx('Anweisung'), nutzer: tx('von dir') };

async function zeigeGedaechtnis() {
  seite.innerHTML = T`<p class="leer">Lädt …</p>`;
  let wartet = false, g = null, sichtbar = [];
  let reiter = localStorage.getItem('gedaechtnis-reiter');
  const male = () => {
    if (!GED_REITER.some(([r]) => r === reiter)) reiter = g.vorschlaege.length ? 'vorschlaege' : 'aktiv';
    const l = g.lernlauf;
    const lauf = !l ? tx('Noch kein Lernlauf.') : l.fehler ? T`Letzter Lernlauf ${zeit(l.zeit)} fehlgeschlagen: ${esc(l.fehler)}`
      : T`Letzter Lernlauf ${zeit(l.zeit)}: ${l.notizen} Notizen und ${l.planungen} Planungen ausgewertet, ${l.neu} neue Vorschläge${l.kosten_usd != null ? `, ${usd(l.kosten_usd)}` : ''}.`;
    const leer = { vorschlaege: tx('Keine offenen Vorschläge.'), aktiv: tx('Noch nichts bestätigt.'), begriffe: tx('Noch keine Begriffe.'), geloescht: tx('Nichts gelöscht.') }[reiter];
    const hinweis = { vorschlaege: tx('Neues von der KI, aus deinen Korrekturen und Antworten. Es wirkt erst, wenn du es übernimmst.'),
      aktiv: tx('Geht bei jeder Notiz und Planung an die KI mit.'),
      begriffe: T`Namen und Eigenbegriffe, die die Spracherkennung richtig schreiben soll. Sie gehen in dieser Reihenfolge als Hinweis mit; Favoriten (★) stehen vorn. Belegt: ${g.begriffe_zeichen} von ${g.begriffe_zeichen_max} Zeichen.`,
      geloescht: T`Kommt nicht wieder. Die letzten ${g.unerwuenscht} gehen als „nicht erwünscht“ an die KI mit.` }[reiter];
    // Gelöschte Begriffe stehen mit unter „Gelöscht“, erkennbar an der Marke.
    const liste = reiter === 'geloescht' ? [...g.geloescht, ...g.begriffe_geloescht.map((e) => ({ ...e, begriff: true }))].sort((x, y) => y.geaendert - x.geaendert) : g[reiter];
    const anzahl = (r) => (r === 'geloescht' ? g.geloescht.length + g.begriffe_geloescht.length : g[r].length);
    sichtbar = liste;
    // Alle Reiter: kompakte Zeilen (Herkunft als Symbol, Datum rechts), Optionen im Blatt von unten.
    // Begriffe stehen in Sende-Reihenfolge; am Ende, was nicht mehr mitgeht.
    const zeile = (e) => T`<button type="button" class="begriffzeile${e.im_prompt === false ? ' aus' : ''}" data-blatt="${e.id}" aria-haspopup="dialog"><span class="begrifftext"><span>${e.favorit ? T`<span class="stern" title="Favorit">★</span> ` : ''}${e.begriff ? T`<span class="badge neutral">${tx('Begriff')}</span> ` : ''}${esc(e.text)}</span></span><span class="begriffmeta">${quelleIcon(e.quelle)}<span class="begriffdatum">${kurzTag(new Date(e.geaendert).toLocaleDateString('sv'))}</span></span>${ICON_WEITER}</button>`;
    const mit = liste.filter((e) => e.im_prompt !== false), ohne = liste.filter((e) => e.im_prompt === false);
    // Begriffe stehen dicht als eine Liste, die Sätze der anderen Reiter mit etwas Abstand.
    const art = reiter === 'begriffe' ? 'begriffliste' : 'begriffliste luft';
    const zeilen = T`<div class="${art}">${mit.map(zeile).join('')}</div>
      ${ohne.length ? T`<p class="leise begriffrest">Passt nicht mehr – wird nicht mitgesendet (${ohne.length})</p><div class="${art}">${ohne.map(zeile).join('')}</div>` : ''}
      <dialog class="mehrblatt" id="gedblatt" aria-label="Optionen" tabindex="-1"></dialog>`;
    seite.innerHTML = T`<h1>Gedächtnis</h1>
      <p class="leise">Was die KI über dich weiß, damit sie kurze Notizen richtig versteht und einsortiert.</p>
      <div class="leiste"><a class="btn" href="/status/gedaechtnis/neu">Neuer Eintrag</a><a class="btn" href="/status/gedaechtnis/begriff/neu">Neuer Begriff</a><button class="btn" data-g="lernen"${wartet || g.lernlauf_laeuft ? ' disabled' : ''}>${wartet || g.lernlauf_laeuft ? tx('Lernlauf läuft …') : tx('Jetzt lernen')}</button></div>
      <nav class="btn-group unter reiter4" aria-label="Gedächtnis">${GED_REITER.map(([r, n]) => T`<button class="btn${r === reiter ? ' aktiv' : ''}" data-reiter="${r}">${n} <span class="zaehler${r === 'vorschlaege' && g[r].length ? '' : ' still'}">${anzahl(r)}</span></button>`).join('')}</nav>
      <p class="leise">${hinweis}</p>
      ${liste.length ? zeilen : T`<p class="leer">${leer}</p>`}
      <p class="leise">${lauf}${g.naechster_lernlauf ? T` Nächster ab ${zeit(g.naechster_lernlauf)}.` : ''} Ausgewertet werden die letzten ${g.lernlauf_eintraege} Einträge.</p>`;    stapeln();
  };
  // Symbol links vom Datum; bei mehrzeiligem Text oder einem, der bis ans Datum reicht, steht es über dem Datum.
  const stapeln = () => {
    for (const z of seite.querySelectorAll('.begriffzeile')) {
      z.classList.remove('gestapelt');
      const feld = z.querySelector('.begrifftext'), text = feld.firstElementChild;
      const zeilenhoehe = parseFloat(getComputedStyle(feld).lineHeight) || 20;
      if (text.getClientRects().length > 1 || feld.offsetHeight > zeilenhoehe * 1.5 || text.getBoundingClientRect().width >= feld.clientWidth - 6) z.classList.add('gestapelt');
    }
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
    if (b.dataset.blatt) {
      const e = sichtbar.find((x) => x.id === b.dataset.blatt), d = document.getElementById('gedblatt');
      if (!e || !d) return;
      const knopf = (aktion, text, klasse = '') => T`<button type="button" class="mehrpunkt${klasse}" data-g="${aktion}" data-id="${e.id}">${text}</button>`;
      const link = (text) => T`<a class="mehrpunkt" href="/status/gedaechtnis/${reiter === 'begriffe' ? 'begriff/' : ''}${e.id}/bearbeiten">${text}</a>`;
      const punkte = { vorschlaege: () => knopf('uebernehmen', tx('Übernehmen')) + link(tx('Ändern')) + knopf('verwerfen', tx('Verwerfen'), ' gefahr'),
        aktiv: () => link(tx('Bearbeiten')) + knopf('loeschen', tx('Löschen'), ' gefahr'),
        begriffe: () => knopf(e.favorit ? 'kein_favorit' : 'favorit', e.favorit ? tx('Favorit entfernen') : tx('Als Favorit – immer mitsenden')) + link(tx('Bearbeiten')) + knopf('loeschen', tx('Löschen'), ' gefahr'),
        geloescht: () => knopf('wiederherstellen', tx('Wiederherstellen')) }[reiter]();
      d.innerHTML = T`<div class="griff" aria-hidden="true"></div>
        <div class="blattkopf"><strong>${esc(e.text)}</strong><div class="leise">${e.begriff ? T`Begriff · ` : ''}${GED_QUELLE[e.quelle] || esc(e.quelle)} · ${zeit(e.geaendert)}${e.im_prompt === false ? T` · wird nicht mitgesendet` : ''}</div></div>${punkte}`;
      d.showModal(); d.focus();
      return;
    }
    if (!b.dataset.g) return;
    b.disabled = true;
    try {
      if (b.dataset.g === 'lernen') {
        wartet = Date.now();
        await api('/api/gedaechtnis/lernen', { method: 'POST' });
        melde(tx('Lernlauf gestartet'));
      } else {
        if (b.dataset.g === 'loeschen' && !confirm(reiter === 'begriffe' ? tx('Begriff löschen? Die KI trägt ihn nicht wieder ein.') : tx('Eintrag löschen? Die KI schlägt ihn nicht wieder vor.'))) { b.disabled = false; return; }
        await api(T`/api/gedaechtnis/${b.dataset.id}`, { method: 'POST', body: { aktion: b.dataset.g } });
        melde({ uebernehmen: tx('Übernommen'), verwerfen: tx('Verworfen'), loeschen: tx('Gelöscht'), wiederherstellen: tx('Wiederhergestellt'), favorit: tx('Favorit – geht immer mit'), kein_favorit: tx('Kein Favorit mehr') }[b.dataset.g]);
      }
      await laden();
    } catch (f) { if (b.dataset.g === 'lernen') wartet = false; b.disabled = false; melde(f.message); }
  };
  addEventListener('resize', stapeln);
  aufraeumen = () => { seite.onclick = null; removeEventListener('resize', stapeln); };
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

// Eigene Seite zum Anlegen (id leer, mehrere auf einmal) oder Ändern eines Begriffs der Spracherkennung.
async function zeigeBegriffBearbeiten(id) {
  let e = null;
  if (id) {
    const g = await api('/api/gedaechtnis');
    e = g.begriffe.find((x) => x.id === id);
    if (!e) { seite.innerHTML = T('<p class="leer">Begriff nicht gefunden.</p><p class="mitte"><a class="btn" href="/status/gedaechtnis">Zum Gedächtnis</a></p>'); return; }
  }
  seite.innerHTML = T`<p><a class="btn klein" href="/status/gedaechtnis">← Gedächtnis</a></p><h1>${e ? tx('Begriff ändern') : tx('Neue Begriffe')}</h1>
    <p class="leise">${e ? tx('So, wie die Spracherkennung ihn schreiben soll.') : tx('Namen und Eigenbegriffe, so wie die Spracherkennung sie schreiben soll – einer je Zeile oder mit Komma getrennt.')}</p>
    ${e ? T`<input id="gtext" type="text" maxlength="60" value="${esc(e.text)}">` : T`<textarea id="gtext" rows="5"></textarea>`}
    <div class="leiste"><button class="btn haupt" id="gsichern">Speichern</button><a class="btn" href="/status/gedaechtnis">Abbrechen</a></div>`;
  document.getElementById('gsichern').onclick = async (ev) => {
    const text = document.getElementById('gtext').value.trim();
    if (!text) return melde(tx('Text fehlt'));
    ev.target.disabled = true;
    try {
      if (e) await api(T`/api/gedaechtnis/${e.id}`, { method: 'PUT', body: { text } });
      else await api('/api/gedaechtnis/begriffe', { method: 'POST', body: { begriffe: text.split(/[\n,;]+/).map((t) => t.trim()).filter(Boolean) } });
      localStorage.setItem('gedaechtnis-reiter', 'begriffe');
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
// opt (Ring-Auswertung): wert = Format eines Werts, achse = Achse zum Maximum, name = Beschriftung je Säule, titel.
function saeulen(tage, id, wochentag, opt = {}) {
  const B = 340, H = 170, links = 46, unten = 22, oben = 8;
  const plotB = B - links - 4, plotH = H - unten - oben;
  const { oben: max, schritte } = (opt.achse || achse)(Math.max(...tage.map((t) => t.diktat + t.planung)));
  const stellen = max < 0.1 ? 3 : 2;
  const wertText = opt.wert || usd, achsText = opt.wert || ((s) => `${s.toLocaleString(LOCALE, { minimumFractionDigits: stellen, maximumFractionDigits: stellen })} $`);
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
    treffer += T`<rect class="treffer" data-i="${i}" x="${links + i * band}" y="${oben}" width="${band}" height="${plotH + unten}" tabindex="0" role="img" aria-label="${esc(tx('{0}: Notizen {1}, Planungen {2}', opt.name ? opt.name(t) : kurzTag(t.tag, true), wertText(t.diktat), wertText(t.planung)))}"/>`;
    const letzter = i === tage.length - 1 && !wochentag;
    const name = opt.name ? opt.name(t) : wochentag ? new Date(`${t.tag}T12:00:00`).toLocaleDateString(LOCALE, { weekday: 'short' }).replace('.', '') : kurzTag(t.tag, false);
    if ((tage.length - 1 - i) % jede === 0) achseX += T`<text class="achse" x="${letzter ? B - 4 : links + i * band + band / 2}" y="${H - 6}" text-anchor="${letzter ? 'end' : 'middle'}">${esc(name)}</text>`;
  });
  const gitter = schritte.map((s) => T`<line class="gitter" x1="${links}" x2="${B - 4}" y1="${y(s)}" y2="${y(s)}"/><text class="achse" x="${links - 6}" y="${y(s) + 4}" text-anchor="end">${esc(achsText(s))}</text>`).join('');
  return T`<svg class="diagramm" id="${id}" viewBox="0 0 ${B} ${H}" role="group" aria-label="${esc(opt.titel || tx('Kosten je Tag in Dollar'))}">${gitter}${marken}${achseX}${treffer}</svg>`;
}

// ---------- Ring: Lebensdauer des Akkus, Auswertung, Ringwechsel ----------

// Stunden als „9:36 h“ (unter einer Stunde „12 min“, unter einer Minute „42 s“).
const stunden = (s) => {
  s = Math.max(0, Math.round(s || 0));
  if (s < 60) return `${s} s`;
  if (s < 3600) return `${Math.round(s / 60)} min`;
  if (s % 3600 === 0) return `${s / 3600} h`;
  return `${Math.floor(s / 3600)}:${String(Math.floor(s / 60) % 60).padStart(2, '0')} h`;
};
const datumKurz = (ms) => new Date(ms).toLocaleDateString(LOCALE, { day: '2-digit', month: '2-digit', year: 'numeric' });
const monatName = (m) => new Date(`${m}-15T12:00:00`).toLocaleDateString(LOCALE, { month: 'short', year: '2-digit' });
// Version der Pebble-App aus ihrem User-Agent („CoreApp/1.18.0.2-…“ → „1.18.0.2-…“).
const pebbleVersion = (s) => String(s || '').replace(/^CoreApp\//i, '');
const ringName = (r) => (r.hardware_id ? (r.beschreibung ? tx('Ring {0} · {1} · {2}', r.nr, r.hardware_id, r.beschreibung) : tx('Ring {0} · {1}', r.nr, r.hardware_id)) : tx('Ring {0}', r.nr));
// Achse für Sekunden: runde Obergrenze in Minuten bzw. Stunden.
function achseZeit(max) {
  const stufen = [60, 120, 300, 600, 900, 1800, 3600, 7200, 10800, 18000, 36000, 72000];
  const oben = stufen.find((v) => v >= max) || Math.ceil(max / 36000) * 36000;
  return { oben, schritte: [0, oben / 2, oben] };
}
const stufeKlasse = (p) => (p >= 95 ? 'stufe95' : p >= 80 ? 'stufe80' : p >= 60 ? 'stufe60' : '');

// Hinweis auf allen Seiten: erreichte Schwelle des zuletzt benutzten Rings oder Update der Pebble-App (bis bestätigt).
let ringInfo = null; // aus /api/zaehler; beim Start aus dem lokalen Speicher (siehe versionBestaetigt)
// Speicher ab 90 % belegt: eine Karte auf jeder Seite für jeden Nutzer (nur die vollste Ablage).
let speicherVoll = null;
function zeigeSpeicherkarte() {
  const karte = document.getElementById('speicherkarte');
  const v = speicherVoll;
  karte.hidden = !v;
  if (!v) return;
  const voll = v.prozent >= 99;
  karte.classList.toggle('kritisch', voll);
  karte.innerHTML = T`<div><strong>${esc(tx('Speicher fast voll: {0} ({1} %)', tx(v.name), v.prozent))}</strong>
    <p class="leise">${esc(tx('Noch {0} frei von {1}. Ist er voll, können Aufnahmen, Bilder oder Backups nicht mehr gespeichert werden.', groesse(v.frei), groesse(v.gesamt)))}</p></div>
    <a class="btn" href="/status">${esc(tx('Speicher ansehen'))}</a>`;
}

let alteAdressen = [], pebbleTest = null;
function zeigeRingkarte() {
  const karte = document.getElementById('ringkarte');
  const r = ringInfo;
  const hinweise = r?.hinweise || [], upd = r?.app_update, fragen = r?.frage_inaktiv || [], alt = alteAdressen;
  karte.hidden = (!r || (!r.fehler && !hinweise.length && !upd && !fragen.length)) && !alt.length && !pebbleTest;
  if (karte.hidden) return;
  if (r?.fehler) {
    karte.innerHTML = T`<div><strong>${esc(tx('Ring-Auswertung'))}</strong><p class="leise">${esc(tx('Fehler: {0}', r.fehler))}</p></div>`;
    return;
  }
  const teile = [];
  // Test aus der Pebble-App angekommen: steht oben auf allen Seiten, bis der Nutzer bestätigt.
  if (pebbleTest) {
    teile.push(T`<div><strong>${esc(tx('Test von der Pebble-App angekommen'))}</strong>
      <p>${esc(tx('Die Verbindung steht ({0}). Aufnahmen kommen jetzt an.', zeit(pebbleTest.zeit)))}</p>
      ${pebbleTest.app ? T`<p class="leise umbruch">${esc(pebbleVersion(pebbleTest.app))}</p>` : ''}</div>
      <div class="leiste"><button type="button" class="btn haupt" data-pebbletest-ok>${esc(tx('OK'))}</button></div>`);
  }
  // Alte Adresse noch in Benutzung (Pebble-App, Kalender-Abo, MCP): neue Adresse eintragen.
  for (const a of alt) {
    teile.push(T`<div><strong>${esc(tx('Adresse geändert: {0}', tx(a.name)))}</strong>
      <p>${esc(tx('Die frühere Adresse gilt nicht mehr, wurde aber zuletzt {0} noch aufgerufen ({1}×). Bitte dort die neue Adresse eintragen:', zeit(a.zuletzt), a.anzahl))}</p>
      <p class="umbruch"><code>${esc(a.neu)}</code></p></div>
      <div class="leiste"><button type="button" class="btn haupt" data-alt-ok="${esc(a.alt)}">${esc(tx('Erledigt'))}</button></div>`);
  }
  if (upd) {
    teile.push(T`<div><strong>${esc(tx('Pebble-App aktualisiert'))}</strong>
      <p class="umbruch">${esc(pebbleVersion(upd.alt))} → ${esc(pebbleVersion(upd.neu))}</p>
      <p class="leise">${esc(tx('Seit {0} meldet sich die Pebble-App mit einer neuen Version.', datumKurz(upd.zeit)))}</p></div>
      <div class="leiste"><button type="button" class="btn haupt" data-ring-appok>${esc(tx('OK'))}</button></div>`);
  }
  for (const h of hinweise) {
    const voll = h.stufe >= 100;
    teile.push(T`<div><strong>${esc(tx('Ring-Nutzung: {0} % erreicht', h.stufe))}</strong>
      <p class="leise">${esc(ringName(h))}</p>
      <p>${esc(tx('{0} von {1} Aufnahmezeit verbraucht ({2} %).', stunden(h.sekunden), stunden(h.kapazitaet_sekunden), Math.floor(h.prozent)))}</p>
      ${voll ? T`<p class="leise">${esc(tx('Der Akku kann länger halten als angegeben. „+30 min“ erhöht die Kapazität dieses Rings dauerhaft.'))}</p>` : ''}
      <a href="/status/ring?ring=${h.nr}">${esc(tx('Auswertung ansehen'))}</a></div>
      <div class="leiste">${voll ? T`<button type="button" class="btn" data-ring-plus="${h.nr}">${esc(tx('+30 min'))}</button>` : ''}<button type="button" class="btn haupt" data-ring-ok="${h.nr}">${esc(tx('Bestätigen'))}</button></div>`);
  }
  for (const f of fragen) {
    teile.push(T`<div><strong>${esc(tx('{0} inaktiv setzen?', ringName(f)))}</strong>
      <p>${esc(tx('Seit {0} nicht mehr benutzt; {1} ist seitdem im Einsatz.', datumKurz(f.letzte), ringName(f.neuer)))}</p></div>
      <div class="leiste"><button type="button" class="btn haupt" data-ring-inaktiv="${f.nr}">${esc(tx('Inaktiv setzen …'))}</button><button type="button" class="btn" data-ring-nein="${f.nr}">${esc(tx('Nein, aktiv lassen'))}</button></div>`);
  }
  karte.innerHTML = teile.join('');
  karte.querySelectorAll('[data-ring-inaktiv]').forEach((b) => {
    const f = fragen.find((x) => x.nr === Number(b.dataset.ringInaktiv));
    b.onclick = () => ringInaktivFenster(f, async () => { if (aktuell?.neuLaden) aktuell.neuLaden(); });
  });
  const knopf = (sel, fn) => karte.querySelectorAll(sel).forEach((b) => { b.onclick = async () => { b.disabled = true; try { await fn(b); await ladeZaehler(); if (aktuell?.neuLaden) aktuell.neuLaden(); } catch (f) { b.disabled = false; melde(f.message); } }; });
  knopf('[data-ring-ok]', (b) => api('/api/ring/bestaetigt', { method: 'POST', body: { ring: Number(b.dataset.ringOk) } }));
  knopf('[data-ring-nein]', (b) => api(`/api/ring/${b.dataset.ringNein}/frage-abgelehnt`, { method: 'POST' }));
  knopf('[data-ring-appok]', () => api('/api/ring/bestaetigt', { method: 'POST', body: { app_update: true } }));
  knopf('[data-pebbletest-ok]', () => api('/api/pebble/test/ok', { method: 'POST' }));
  knopf('[data-alt-ok]', (b) => api('/api/alte-adressen/ok', { method: 'POST', body: { alt: b.dataset.altOk } }));
  knopf('[data-ring-plus]', (b) => api('/api/ring/bestaetigt', { method: 'POST', body: { plus: true, ring: Number(b.dataset.ringPlus) } }));
}

// Balken ganz unten im Menü: Verbrauch des aktiven Rings; ohne aktiven Ring nur der Link zur Ring-Nutzung.
function ringBalkenHtml() {
  const r = ringInfo?.balken; // aktiver Ring mit der höchsten Nutzung; inaktive stehen nicht in der Seitenleiste
  // Kein aktiver Ring (alle inaktiv oder noch keiner): der Link zur Ring-Nutzung bleibt trotzdem da.
  const an = aktuell?.name === 'ring' ? ' aktiv" aria-current="page' : '';
  const pfeil = T('<svg class="ringpfeil" viewBox="0 0 24 24" aria-hidden="true"><path d="M9 6l6 6-6 6" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/></svg>');
  if (!r) return T`<a class="ringbalken${an}" href="/status/ring"><span class="ringtext"><span>${esc(tx('Nutzung'))}</span><span class="leise">${esc(tx('kein aktiver Ring'))}${pfeil}</span></span></a>`;
  return T`<a class="ringbalken ${stufeKlasse(r.prozent)}${an}" href="/status/ring?ring=${r.nr}"><span class="ringtext"><span>${esc(tx('Nutzung'))}</span><span>${esc(stunden(r.sekunden))}${pfeil}</span></span><span class="balken"><i data-anteil="${Math.min(100, r.prozent)}"></i></span></a>`;
}
// Strich und Beschriftung im Wortbalken an ihre Stelle (Prozent aus data-pos); Beschriftung bleibt im Rahmen.
const setzeWortlage = (wurzel) => wurzel.querySelectorAll('.wortskala [data-pos]').forEach((el) => {
  const p = Math.max(0, Math.min(100, Number(el.dataset.pos)));
  el.style.left = `${p}%`;
  if (el.tagName !== 'I') el.style.transform = `translateX(-${p}%)`; // am Rand bündig statt überstehend
});
const setzeBalken = (wurzel) => wurzel.querySelectorAll('.balken i[data-anteil]').forEach((el) => { el.style.width = `${Math.max(0, Math.min(100, Number(el.dataset.anteil)))}%`; });

// Ring-Verlauf: Aufnahmezeit je Monat, Woche oder Tag als Liniendiagramm (Notizen, Planungen).
const RING_ZEITRAEUME = [['tag', 'Tag'], ['woche', 'Woche'], ['monat', 'Monat']];
// ISO-Kalenderwoche und Montag einer Woche zu „JJJJ-MM-TT“ (Ortszeit, mittags gegen Zeitumstellung).
function isoWoche(iso) {
  const d = new Date(`${iso}T12:00:00`), wt = (d.getDay() + 6) % 7;
  const montag = new Date(d); montag.setDate(d.getDate() - wt);
  const donnerstag = new Date(montag); donnerstag.setDate(montag.getDate() + 3);
  const jan4 = new Date(donnerstag.getFullYear(), 0, 4, 12);
  const kw = 1 + Math.round((donnerstag - jan4) / 604800000 + ((jan4.getDay() + 6) % 7) / 7 - 3 / 7);
  const z = (n) => String(n).padStart(2, '0');
  return { kw, montag: `${montag.getFullYear()}-${z(montag.getMonth() + 1)}-${z(montag.getDate())}` };
}
// Tage zu Zeitraum zusammenfassen; Tage sind lückenlos, daher auch Wochen und Monate.
function ringZeitraum(tage, art) {
  if (art === 'tag') return tage.map((t) => ({ ...t, name: kurzTag(t.tag, false), lang: kurzTag(t.tag, true) }));
  const out = [], by = new Map();
  for (const t of tage) {
    let k, name, lang;
    if (art === 'woche') {
      const w = isoWoche(t.tag);
      k = w.montag; name = tx('KW {0}', w.kw);
      const so = new Date(`${w.montag}T12:00:00`); so.setDate(so.getDate() + 6);
      lang = tx('KW {0} ({1} – {2})', w.kw, kurzTag(w.montag, false), so.toLocaleDateString(LOCALE, { day: '2-digit', month: '2-digit' }));
    } else { k = t.tag.slice(0, 7); name = monatName(k); lang = new Date(`${k}-15T12:00:00`).toLocaleDateString(LOCALE, { month: 'long', year: 'numeric' }); }
    let e = by.get(k);
    if (!e) { e = { tag: k, name, lang, diktat: 0, planung: 0, anzahl: 0 }; by.set(k, e); out.push(e); }
    e.diktat += t.diktat; e.planung += t.planung; e.anzahl += t.anzahl;
  }
  return out;
}
// Liniendiagramm mit zwei Reihen (Notizen s1, Planungen s2); Trefferflächen je Punkt zum Ablesen.
function ringLinien(punkte, id, titel) {
  const B = 340, H = 170, links = 46, unten = 22, oben = 8, rechts = 8;
  const plotB = B - links - rechts, plotH = H - unten - oben, n = punkte.length;
  const { oben: max, schritte } = achseZeit(Math.max(...punkte.map((t) => Math.max(t.diktat, t.planung))));
  const x = (i) => links + (n === 1 ? plotB / 2 : (i * plotB) / (n - 1));
  const y = (v) => oben + plotH - (v / max) * plotH;
  const schrittX = n === 1 ? plotB : plotB / (n - 1);
  const linie = (feld, k) => {
    const pkt = punkte.map((t, i) => `${x(i).toFixed(1)},${y(t[feld]).toFixed(1)}`);
    const kreise = n <= 40 ? punkte.map((t, i) => `<circle class="s${k}" cx="${x(i).toFixed(1)}" cy="${y(t[feld]).toFixed(1)}" r="2.6"/>`).join('') : '';
    return `<polyline class="linie l${k}" points="${pkt.join(' ')}"/>${kreise}`;
  };
  const jede = Math.max(1, Math.ceil(n / 6));
  let achseX = '', treffer = '';
  punkte.forEach((t, i) => {
    const x0 = Math.max(links, x(i) - schrittX / 2), x1 = Math.min(B - rechts, x(i) + schrittX / 2);
    treffer += T`<rect class="treffer" data-i="${i}" x="${x0.toFixed(1)}" y="${oben}" width="${Math.max(1, x1 - x0).toFixed(1)}" height="${plotH + unten}" tabindex="0" role="img" aria-label="${esc(tx('{0}: Notizen {1}, Planungen {2}', t.lang, stunden(t.diktat), stunden(t.planung)))}"/>`;
    if ((n - 1 - i) % jede === 0) {
      const anker = n > 1 && i === n - 1 ? 'end' : n > 1 && i === 0 ? 'start' : 'middle';
      achseX += T`<text class="achse" x="${x(i).toFixed(1)}" y="${H - 6}" text-anchor="${anker}">${esc(t.name)}</text>`;
    }
  });
  const gitter = schritte.map((s) => T`<line class="gitter" x1="${links}" x2="${B - rechts}" y1="${y(s)}" y2="${y(s)}"/><text class="achse" x="${links - 6}" y="${y(s) + 4}" text-anchor="end">${esc(stunden(s))}</text>`).join('');
  return T`<svg class="diagramm" id="${id}" viewBox="0 0 ${B} ${H}" role="group" aria-label="${esc(titel)}">${gitter}${linie('diktat', 1)}${linie('planung', 2)}${achseX}${treffer}</svg>`;
}
function zeigeRingVerlauf(tage) {
  const karte = document.getElementById('rverlauf');
  if (!karte) return;
  let art = 'monat';
  try { art = localStorage.getItem('ring-zeitraum') || 'monat'; } catch { /* nur Komfort */ }
  if (!RING_ZEITRAEUME.some(([k]) => k === art)) art = 'monat';
  const punkte = ringZeitraum(tage, art);
  const titel = { monat: tx('Aufnahmezeit je Monat'), woche: tx('Aufnahmezeit je Woche'), tag: tx('Aufnahmezeit je Tag') }[art];
  karte.innerHTML = T`<div class="btn-group unter" role="group" aria-label="${esc(tx('Zeitraum'))}">${RING_ZEITRAEUME.map(([k, name]) => T`<button type="button" class="btn${k === art ? ' aktiv' : ''}" data-zeitraum="${k}"${k === art ? ' aria-pressed="true"' : ' aria-pressed="false"'}>${esc(tx(name))}</button>`).join('')}</div>
    ${T('<div class="legende"><span><i class="s1"></i>Notizen</span><span><i class="s2"></i>Planungen</span></div>')}
    ${punkte.length ? ringLinien(punkte, 'rverlauf-svg', titel) : T`<p class="leise">${esc(tx('Noch keine Aufnahmen.'))}</p>`}
    <div class="ablesen" id="rverlauf-wert">${esc(tx('Punkt antippen für die Werte.'))}</div>`;
  karte.querySelectorAll('[data-zeitraum]').forEach((b) => {
    b.onclick = () => {
      try { localStorage.setItem('ring-zeitraum', b.dataset.zeitraum); } catch { /* nur Komfort */ }
      zeigeRingVerlauf(tage);
    };
  });
  const svg = document.getElementById('rverlauf-svg'), wert = document.getElementById('rverlauf-wert');
  if (!svg) return;
  const zeige = (ev) => {
    const r = ev.target.closest('.treffer');
    if (!r) return;
    const t = punkte[Number(r.dataset.i)];
    svg.querySelectorAll('.treffer.an').forEach((e) => e.classList.remove('an'));
    r.classList.add('an');
    wert.textContent = tx('{0}: Notizen {1} · Planungen {2} · {3} Aufnahmen', t.lang, stunden(t.diktat), stunden(t.planung), t.anzahl);
  };
  svg.addEventListener('pointerover', zeige);
  svg.addEventListener('click', zeige);
  svg.addEventListener('focusin', zeige);
}

// Meine Daten (immer offen, nur aktive Einträge vom Ring über den Webhook): Zahl, Aufnahmen, Auffälliges (in Arbeit, Fehler), Kreis nach Art.
function eingangsKarte(s) {
  const t = s.auftraege_teile || {}, d = s.audio_dateien || {};
  const kachel = (n, name, art) => T`<div class="zahlkachel ${art}"><strong>${n}</strong><span>${esc(name)}</span></div>`;
  const auffaellig = [s.warteschlange ? kachel(s.warteschlange, tx('in Arbeit'), 'warn') : '', s.fehlgeschlagen ? kachel(s.fehlgeschlagen, tx('fehlgeschlagen'), 'schlecht') : ''].join('');
  const teile = [[t.notizen, tx('Notizen'), 'diktat'], [t.termine, tx('Termine'), 'termin'], [t.runden, tx('Planungsrunden'), 'planung']].filter(([n]) => n > 0);
  const summe = teile.reduce((a, [n]) => a + n, 0);
  // Ring aus Kreisbögen (SVG-Attribute statt Inline-Styles: die CSP erlaubt keine style-Attribute).
  let start = 0;
  const ring = summe ? teile.map(([n, , art]) => {
    const anteil = (n / summe) * 100, bogen = T`<circle class="ring ${art}" r="15.9155" cx="18" cy="18" stroke-dasharray="${anteil} ${100 - anteil}" stroke-dashoffset="${25 - start}"/>`;
    start += anteil;
    return bogen;
  }).join('') : '';
  return T`<div class="karte eingangskarte"><div class="eingangskopf"><span class="zeile kartentitel"><strong>${esc(tx('Meine Daten'))}</strong><span class="eingangszahl">${s.auftraege}</span></span>
      ${d.anzahl ? T`<span class="leise">${d.anzahl} Aufnahmen${d.schnitt_groesse ? T` · Ø ${groesse(d.schnitt_groesse)}` : ''}${d.schnitt_sekunden ? T` · Ø ${dauer(d.schnitt_sekunden)} · zusammen ${dauer(d.gesamt_sekunden)}` : ''}</span>` : ''}
      ${auffaellig ? T`<span class="zahlkacheln">${auffaellig}</span>` : ''}</div>
    <div class="eingangsdetail">${summe ? T`<svg class="ringdiagramm" viewBox="-2 -2 40 40" role="img" aria-label="${esc(teile.map(([n, name]) => `${n} ${name}`).join(', '))}">${ring}</svg>` : ''}
      <ul class="legende">${teile.map(([n, name, art]) => T`<li><span class="punkt-farbe ${art}"></span>${esc(name)}<strong>${n}</strong></li>`).join('')}</ul></div>
    ${wortBlock(s.woerter)}
    <p class="leise">Planungen: ${t.planungen} (${t.runden} Runden) · ${t.audio} mit Aufnahme${t.nur_text ? T` · ${t.nur_text} nur Pebble-Text` : ''}</p></div>`;
}
// Wörter je Aufnahme (letzte Tage): Balken über die volle Breite von Min bis Max, Strich bei Ø.
// Lage als data-pos; setzeWortlage setzt sie per JS (die CSP erlaubt keine style-Attribute).
function wortBlock(w) {
  if (!w) return '';
  const zahl = (v) => Math.round(v).toLocaleString(LOCALE), span = w.max - w.min;
  const pos = span ? ((w.schnitt - w.min) / span) * 100 : 50;
  return T`<div class="wortblock"><div class="zeile"><strong>${esc(tx('Wörter je Aufnahme'))}</strong><span class="leise">${esc(tx('{0} Tage · {1} Transkripte', w.tage, w.anzahl))}</span></div>
    <div class="wortskala" role="img" aria-label="${esc(tx('Wörter je Aufnahme: Min {0}, Ø {1}, Max {2}', zahl(w.min), zahl(w.schnitt), zahl(w.max)))}">
      <span class="wortschnitt" data-pos="${pos}">${esc(tx('Ø {0}', zahl(w.schnitt)))}</span>
      <span class="wortbalken"><i data-pos="${pos}"></i></span>
      <span class="wortgrenzen"><span>${esc(tx('Min {0}', zahl(w.min)))}</span><span>${esc(tx('Max {0}', zahl(w.max)))}</span></span></div></div>`;
}

// Teile des Ring-Balkens (Sekunden ohne Überschneidung, Anteil an der Kapazität); die Karten darunter tragen dieselbe
// Farbe als Streifen links – eine Legende braucht es nicht.
const RING_TEILE = [['vorher', 'Vorher genutzt'], ['diktat', 'Notizen'], ['planung', 'Planungen'], ['fehl', 'Fehlaufnahmen'], ['geloescht', 'Gelöscht']];
function ringTeile(g) {
  const kap = Math.max(1, g.kapazitaet_sekunden);
  let rest = 100;
  return RING_TEILE.map(([k]) => {
    const anteil = Math.min(rest, ((g.anteile?.[k] || 0) / kap) * 100);
    rest -= anteil;
    return anteil > 0 ? T`<i class="t-${k}" data-anteil="${anteil}"></i>` : '';
  }).join('');
}

async function zeigeRing() {
  seite.innerHTML = T`<p class="leer">${esc(tx('Lädt …'))}</p>`;
  const wahl = new URLSearchParams(location.search).get('ring') || '';
  const laden = async () => {
    const [d, st] = await Promise.all([api(`/api/ring${wahl ? `?ring=${encodeURIComponent(wahl)}` : ''}`), api('/api/eingaenge')]);
    if (aktuell?.name !== 'ring') return;
    const g = d.ring, aktiv = !g.ende, p = g.prozent;
    // Kleine Kachel mit Zahl und Anteil an allen Aufnahmen des Rings (Fehlaufnahmen, Gelöschte).
    const kachel = (titel, sek, n, farbe = '') => T`<div class="kachel ${farbe}"><div class="leise">${esc(titel)}</div><div class="zahl">${esc(stunden(sek))}</div><div class="leise">${esc(tx('{0} Aufnahmen', n))}${n ? ` · Ø ${esc(stunden(sek / n))}` : ''}</div></div>`;
    const prog = d.prognose;
    const wann = (v) => (v === 0 ? tx('erreicht') : v == null ? tx('keine Nutzung') : datumKurz(v));
    const verlauf = d.verlauf || [];
    // Vergleich als kompakte Zeilen (mobil ohne breite Tabelle): ID, Zeit, Anteil; darunter Zeitraum, Aufnahmen, Ø je Tag.
    const vergleich = d.ringe.map((r) => T`<a class="karte ringzeile${r.nr === g.nr ? ' aktiv' : ''}" href="/status/ring?ring=${r.nr}">
      <span class="zeile"><strong>${esc(r.hardware_id || tx('Ring {0}', r.nr))}${r.ende ? T` <span class="kimarke">${esc(r.inaktiv_grund ? tx('inaktiv · {0}', ringGrund(r.inaktiv_grund)) : tx('inaktiv'))}</span>` : ''}</strong><span>${esc(stunden(r.sekunden))} · ${Math.floor(r.prozent)} %</span></span>
      <span class="balken ${stufeKlasse(r.prozent)}"><i data-anteil="${Math.min(100, r.prozent)}"></i></span>
      <span class="leise">${esc(tx('seit: {0}', datumKurz(r.start)))} · ${esc(tx('{0} Aufnahmen', r.anzahl))} · ${esc(tx('Ø {0} je Tag', stunden(r.sekunden / r.tage)))}</span></a>`).join('');
    seite.innerHTML = T`<h1>${esc(tx('Ringnutzung'))}</h1>
      ${eingangsKarte(st)}
      ${d.ringe.length > 1 ? T`<nav class="btn-group" aria-label="${esc(tx('Ring wählen'))}">${d.ringe.slice().reverse().map((r) => T`<a class="btn${r.nr === g.nr ? ' aktiv' : ''}" href="/status/ring?ring=${r.nr}"${r.nr === g.nr ? ' aria-current="page"' : ''}>${esc(r.hardware_id || tx('Ring {0}', r.nr))}</a>`).join('')}</nav>` : ''}
      <div class="karte">
        <a class="zeile kartentitel kartenlink" href="/status/ring/${g.nr}/bearbeiten" aria-label="${esc(tx('Ring bearbeiten'))}"><strong>${esc(ringName(g))}${aktiv ? '' : T` <span class="kimarke">${esc(g.inaktiv_grund ? tx('inaktiv · {0}', ringGrund(g.inaktiv_grund)) : tx('inaktiv'))}</span>`}</strong><span class="kipfeil" aria-hidden="true">›</span></a>
        <p class="leise zeile ringmeta"><span class="umbruch">${g.app_agent ? esc(tx('App: {0}', pebbleVersion(g.app_agent))) : ''}</span><span>${esc(tx('seit: {0}', datumKurz(g.start)))}</span></p>
        <span class="balken ringgross mehrteilig" role="img" aria-label="${esc(RING_TEILE.map(([k, name]) => tx('{0}: {1}', tx(name), stunden(g.anteile?.[k] || 0))).join(', '))}">${ringTeile(g)}</span>
        <p><strong>${esc(tx('{0} von {1}', stunden(g.sekunden), stunden(g.kapazitaet_sekunden)))}</strong> · ${Math.floor(p)} %</p>
        ${aktiv && prog ? T`<p class="leise">${esc(tx('Bei gleichbleibender Nutzung (Ø {0} je Tag, letzte {1} Tage): 80 % {2} · 100 % {3}', stunden(prog.sekunden_je_tag), Math.round(prog.basis_tage), wann(prog.datum80), wann(prog.datum100)))}</p>` : ''}
      </div>
      <div class="kacheln paar">${RING_TEILE.filter(([k]) => k !== 'vorher' || g.anteile?.vorher).map(([k, name]) => k === 'vorher'
        ? T`<div class="kachel f-vorher"><div class="leise">${esc(tx(name))}</div><div class="zahl">${esc(stunden(g.anteile.vorher))}</div><div class="leise">${esc(tx('vor der ersten Aufnahme hier'))}</div></div>`
        : kachel(tx(name), g.anteile?.[k] || 0, g.anteile_anzahl?.[k] || 0, 'f-' + k)).join('')}</div>
      <div class="kacheln paar">${kachel(tx('Halten'), g.halten_sekunden || 0, g.halten)}${kachel(tx('Klick + Halten'), g.klick_halten_sekunden || 0, g.klick_halten)}</div>
      <h2>${esc(tx('Aufnahmezeit'))}</h2>
      <div class="karte" id="rverlauf"></div>
      ${d.ringe.length > 1 ? T`<h2>${esc(tx('Ringe im Vergleich'))}</h2>${vergleich}` : ''}
      <p id="ringglobal" hidden><a class="karte kartenlink zeile" href="/status/statistik"><span><strong>${esc(tx('Ring-Lebensdauer aller Installationen'))}</strong><br><span class="leise">${esc(tx('Vergleich mit allen Installationen'))}</span></span><span class="kipfeil" aria-hidden="true">›</span></a></p>
`;
    setzeBalken(seite);
    setzeWortlage(seite);
    zeigeRingVerlauf(verlauf);
    // Link zur globalen Statistik nur, wenn dieser Build sie kennt.
    api('/api/statistik').then((st) => { const el = document.getElementById('ringglobal'); if (el && st.verfuegbar) el.hidden = false; }).catch(() => {});
  };
  aktuell.neuLaden = laden;
  await laden();
}

// Gründe für „inaktiv“ in der Reihenfolge der Auswahl.
const RING_GRUENDE = [['verloren', 'Ring verloren'], ['defekt', 'Ring defekt'], ['batterie', 'Batterie leer'], ['sonstiges', 'Sonstiger Grund']];
const ringGrund = (g) => { const x = RING_GRUENDE.find(([k]) => k === g); return x ? tx(x[1]) : ''; };

// Fenster „Ring inaktiv setzen“: Grund wählen (verloren, defekt, Batterie leer, sonstiger); speichert sofort.
function ringInaktivFenster(g, danach) {
  const dlg = document.createElement('dialog');
  dlg.className = 'formblatt';
  dlg.innerHTML = T`<h2>${esc(tx('Ring inaktiv setzen'))}</h2>
    <p class="leise">${esc(ringName(g))} · ${esc(tx('Warum wird der Ring nicht mehr benutzt?'))}</p>
    <div class="grundliste">${RING_GRUENDE.map(([k, t]) => T`<button type="button" class="mehrpunkt" data-grund="${k}">${esc(tx(t))}</button>`).join('')}</div>
    <div class="leiste"><button type="button" class="btn" data-zu>${esc(tx('Abbrechen'))}</button></div>`;
  document.body.append(dlg);
  const zu = () => { dlg.close(); dlg.remove(); };
  dlg.addEventListener('cancel', () => dlg.remove());
  dlg.addEventListener('click', async (ev) => {
    if (ev.target === dlg || ev.target.closest('[data-zu]')) return zu();
    const b = ev.target.closest('[data-grund]');
    if (!b) return;
    dlg.querySelectorAll('button').forEach((x) => { x.disabled = true; });
    try {
      await api(`/api/ring/${g.nr}/aktiv`, { method: 'PUT', body: { aktiv: false, grund: b.dataset.grund } });
      zu();
      melde(tx('Ring inaktiv: {0}', ringGrund(b.dataset.grund)));
      await ladeZaehler();
      if (danach) await danach();
    } catch (f) { dlg.querySelectorAll('button').forEach((x) => { x.disabled = false; }); melde(f.message); }
  });
  dlg.showModal();
}

// Ring bearbeiten: Beschreibung und Kapazität; aktiv/inaktiv über eigene Knöpfe (speichern sofort).
async function zeigeRingBearbeiten(nr) {
  const d = await api(`/api/ring?ring=${encodeURIComponent(nr)}`);
  const g = d.ring;
  const kapWert = Math.round((g.kapazitaet_sekunden / 3600) * 100) / 100, vorherWert = Math.round((g.vorher_sekunden || 0) / 60);
  seite.innerHTML = T`<p><a class="btn klein" href="/status/ring?ring=${g.nr}">← ${esc(tx('Ring'))}</a></p><h1>${esc(tx('Ring {0} bearbeiten', g.nr))}</h1>
    <dl><dt>${esc(tx('Ring-ID'))}</dt><dd>${esc(g.hardware_id || tx('ohne ID'))}</dd><dt>${esc(tx('Kennung'))}</dt><dd class="umbruch">${esc(g.kennung || tx('noch keine Aufnahme mit Kennung'))}</dd></dl>
    <p class="leise hilfe">${esc(tx('Die Ring-ID sind die letzten drei Zeichen der Kennung; beides kommt vom Ring und ist nicht änderbar.'))}</p>
    <label for="rbeschr">${esc(tx('Beschreibung'))}</label><input id="rbeschr" maxlength="60" autocomplete="off" value="${esc(g.beschreibung || '')}">
    <p class="leise hilfe">${esc(tx('Freier Text, z. B. die Farbe des Rings (höchstens 60 Zeichen).'))}</p>
    <div class="feldpaar gleich"><div><label for="rvorher">${esc(tx('Bereits genutzt (min)'))}</label><input id="rvorher" type="number" inputmode="numeric" min="0" max="60000" step="1" value="${vorherWert}"></div>
      <div><label for="rkap">${esc(tx('Kapazität (h)'))}</label><input id="rkap" type="number" inputmode="decimal" min="1" max="1000" step="0.5" value="${kapWert}"></div></div>
    <p class="leise hilfe">${esc(tx('Bereits genutzt: Aufnahmezeit, bevor der Ring hier aufgenommen hat; zählt zur Nutzung. Kapazität: Stunden Aufnahme, die der Ring hält.'))}</p>
    <div class="leiste"><button class="btn haupt" id="rsichern">${esc(tx('Speichern'))}</button><a class="btn" href="/status/ring?ring=${g.nr}">${esc(tx('Abbrechen'))}</a></div>
    <h2>${esc(tx('Status'))}</h2>
    ${g.ende ? T`<p><strong>${esc(tx('Inaktiv seit {0}', datumKurz(g.ende)))}</strong>${g.inaktiv_grund ? ` · ${esc(ringGrund(g.inaktiv_grund))}` : ''}</p>
      <div class="leiste"><button class="btn" id="raktivieren">${esc(tx('Aktiv setzen'))}</button></div>`
    : T`<p>${esc(tx('Aktiv'))}</p><div class="leiste"><button class="btn" id="rinaktiv">${esc(tx('Inaktiv setzen'))}</button></div>`}
    <p class="leise hilfe">${esc(tx('Inaktive Ringe fehlen in der Seitenleiste und warnen nicht mehr; ihre Nutzung bleibt in der Auswertung.'))}</p>`;
  const beschr = document.getElementById('rbeschr'), kap = document.getElementById('rkap'), vorher = document.getElementById('rvorher');
  const geaendert = () => beschr.value.trim() !== (g.beschreibung || '') || Number(kap.value) !== kapWert || Number(vorher.value || 0) !== vorherWert;
  const zurueck = async () => { geheZu(`/status/ring?ring=${g.nr}`, true); };
  const weiterOhneSpeichern = () => !geaendert() || confirm(tx('Ungespeicherte Änderungen verwerfen?'));
  const inaktiv = document.getElementById('rinaktiv');
  if (inaktiv) inaktiv.onclick = () => { if (weiterOhneSpeichern()) ringInaktivFenster(g, zurueck); };
  const aktivieren = document.getElementById('raktivieren');
  if (aktivieren) aktivieren.onclick = async () => {
    if (!weiterOhneSpeichern() || !confirm(tx('{0} wieder aktiv setzen?', ringName(g)))) return;
    aktivieren.disabled = true;
    try {
      await api(`/api/ring/${g.nr}/aktiv`, { method: 'PUT', body: { aktiv: true } });
      melde(tx('Ring wieder aktiv'));
      await ladeZaehler();
      await zurueck();
    } catch (f) { aktivieren.disabled = false; melde(f.message); }
  };
  document.getElementById('rsichern').onclick = async (ev) => {
    const k = Number(kap.value);
    if (!(k >= 1 && k <= 1000)) return melde(tx('Kapazität 1–1000 Stunden'));
    const v = Number(vorher.value || 0);
    if (!(v >= 0 && v <= 60000)) return melde(tx('Bereits genutzt: 0–60000 Minuten'));
    ev.target.disabled = true;
    try {
      await api(`/api/ring/${g.nr}`, { method: 'PUT', body: { kapazitaet_stunden: k, beschreibung: beschr.value.trim(), vorher_minuten: v } });
      melde(tx('Gespeichert'));
      await ladeZaehler();
      await zurueck();
    } catch (f) { ev.target.disabled = false; melde(f.message); }
  };
}

// ---------- Einstellungen: Ersteinrichtung, Ansicht, Bearbeiten ----------

// Formular aus den Feldern des Servers, nach Gruppen. Geheimnisse: im Assistenten erzeugte sichtbar (zum
// Übertragen in die Pebble-App), sonst Passwortfeld – leer lassen behält den gespeicherten Wert.
// Sprachen heißen in ihrer eigenen Sprache.
const WAHL = { de: 'Deutsch', en: 'English' };
const TRANSKRIPTION = { audio: 'Audio, Transkription in der Web-App', pebble: 'Text der Pebble-App übernehmen' };
function einstellungsFormular(felder, { assistent, whisper, whisperLoeschen = false }) {
  const gruppen = [...new Set(felder.map((f) => f.gruppe))];
  // Jedes Feld in eigenem Rahmen, damit es sich je nach anderen Werten ausblenden lässt.
  const feld = (f) => {
    const art = f.key.startsWith('WHISPER_') ? 'whisperfeld' : CLOUDFELDER.includes(f.key) ? 'cloudfeld' : 'feld';
    return T`<div class="${art}" data-feld="${f.key}">${feldInhalt(f)}</div>`;
  };
  const feldInhalt = (f) => {
    const id = `f-${f.key}`;
    const hilfe = f.hilfe ? T`<p class="leise hilfe">${esc(tx(f.hilfe))}</p>` : '';
    let eingabe;
    if (f.key === 'WHISPER_MODEL' && whisper) {
      eingabe = T`<select id="${id}" name="${f.key}">${whisper.modelle.map((m) => T`<option value="${esc(m.id)}"${m.id === f.wert ? ' selected' : ''}>${esc(m.name)} · ${groesse(m.bytes)}</option>`).join('')}</select>`;
      return T`<label for="${id}">${esc(tx(f.name))}</label>${eingabe}${hilfe}${whisperTabelle(whisper, { loeschen: whisperLoeschen, gewaehlt: f.wert })}`;
    } else if (f.art === 'multi') {
      const an = (f.wert || '').split(',');
      return T`<p class="feldname">${esc(tx(f.name))}</p><div class="mehrfach" data-multi="${f.key}">${f.auswahl.map((c, i) => T`<label class="schalter"><input type="checkbox" id="${id}-${i}" value="${esc(c)}"${an.includes(c) ? ' checked' : ''}> ${esc(wahlText(f, c))}</label>`).join('')}</div>${hilfe}`;
    } else if (f.art === 'choice') {
      eingabe = T`<select id="${id}" name="${f.key}">${f.auswahl.map((c) => T`<option value="${esc(c)}"${c === f.wert ? ' selected' : ''}>${esc(wahlText(f, c))}</option>`).join('')}</select>`;
    } else if (f.art === 'secret') {
      const sichtbar = assistent && f.wert;
      const platz = f.gesperrt ? tx('aus der Umgebung gesetzt') : f.gesetzt ? tx('gesetzt – leer lassen zum Behalten') : '';
      // Aus der Umgebung gesetzt: gilt immer, hier nicht änderbar (ohne name, wird nicht mitgeschickt).
      eingabe = f.gesperrt ? T`<input type="password" id="${id}" placeholder="${platz}" disabled>`
        : T`<input type="${sichtbar ? 'text' : 'password'}" id="${id}" name="${f.key}" value="${esc(f.wert || '')}" placeholder="${platz}" autocomplete="off" spellcheck="false">`;
      if (f.key === 'OPENAI_API_KEY' && assistent) eingabe = T`<div class="mitknopf">${eingabe}<button type="button" class="btn klein" data-pruefen="1">Prüfen</button></div>`;
    } else {
      const modus = f.art === 'int' ? ' inputmode="numeric"' : f.art === 'num' ? ' inputmode="decimal"' : '';
      eingabe = T`<input type="${f.art === 'url' ? 'url' : 'text'}" id="${id}" name="${f.key}" value="${esc(f.wert || '')}"${modus} autocomplete="off" spellcheck="false">`;
    }
    return T`<label for="${id}">${esc(tx(f.name))}${f.pflicht ? ' *' : ''}</label>${eingabe}${hilfe}`;
  };
  const koerper = gruppen.map((g) => T`<fieldset><legend>${esc(tx(g))}</legend>${felder.filter((f) => f.gruppe === g).map(feld).join('')}</fieldset>`).join('');
  return T`<form class="einstellungen" novalidate>${koerper}
    <div class="leiste"><button class="btn haupt" type="submit">Speichern</button></div></form>`;
}

// Anzeige eines Auswahlwerts: eigene Bezeichnung des Felds, Sprachname oder der Wert selbst.
const wahlText = (f, c) => (f.bezeichnungen?.[c] ? tx(f.bezeichnungen[c]) : WAHL[c] || tx(c));

// Whisper-Modelle mit dem, was sie brauchen, und den Mitteln dieses Servers. Tempo: gemessen, sonst Richtwert
// (gemessen auf 4 Kernen, auf die Kerne des Servers umgerechnet) – als Rechenzeit je Minute Aufnahme.
function whisperTabelle(w, { loeschen = false, gewaehlt = '' } = {}) {
  const mb = 1024 * 1024;
  const tempo = (m) => {
    const faktor = m.faktor_gemessen || (m.faktor * 4) / Math.max(1, Math.min(w.kerne, 8));
    const s = Math.max(1, Math.round(faktor * 60));
    return T`${s < 90 ? tx('{0} s', s) : tx('{0} Min', (s / 60).toLocaleString(LOCALE, { maximumFractionDigits: 1 }))}${m.faktor_gemessen ? T` <span class="leise">${tx('gemessen')}</span>` : ''}`;
  };
  const zustand = (m) => {
    if (m.geladen) return T('<span class="badge fertig">geladen</span>');
    if (m.laedt) return T`<span class="badge">${tx('lädt {0} %', m.prozent)}</span>`;
    if (m.fehler) return T`<span class="badge fehler" title="${esc(m.fehler)}">${tx('Fehler beim Laden')}</span>`;
    return '';
  };
  const knapp = (m) => w.ram_verfuegbar > 0 && !m.geladen && m.ram_mb * mb > w.ram_verfuegbar;
  return T`<div class="whisperwahl">
    <p class="leise">${tx('Dieser Server: {0} Kerne, {1} RAM frei von {2}.', w.kerne, groesse(w.ram_verfuegbar), groesse(w.ram_gesamt))}${w.engine ? '' : T` <span class="schlecht">${tx('Dieser Build enthält keine Whisper-Engine.')}</span>`}</p>
    <table class="werte whispertabelle"><thead><tr><th>${tx('Modell')}</th><th>${tx('Größe')}</th><th>${tx('1 Min. Aufnahme ≈')}</th><th>${tx('Güte')}</th></tr></thead><tbody>
    ${w.modelle.map((m) => T`<tr${knapp(m) ? ' class="knapp"' : ''}><td><strong>${esc(m.name)}</strong> ${zustand(m)}${knapp(m) ? T`<br><span class="schlecht">${tx('zu wenig RAM frei')}</span>` : ''}${w.kerne < m.kerne ? T`<br><span class="leise">${tx('empfohlen ab {0} Kernen', m.kerne)}</span>` : ''}</td>
      <td>${groesse(m.bytes)}<br><span class="leise">${m.ram_mb < 1000 ? `${m.ram_mb} MB` : `${(m.ram_mb / 1024).toLocaleString(LOCALE, { maximumFractionDigits: 1 })} GB`} RAM</span></td><td>${tempo(m)}</td><td>${esc(tx(m.deutsch))}</td></tr>
      ${loeschen && m.geladen && m.id !== gewaehlt ? T`<tr class="ohnelinie"><td colspan="4"><button class="btn klein gefahr" type="button" data-whisper-loeschen="${esc(m.id)}">${tx('{0} löschen ({1})', m.name, groesse(m.bytes))}</button></td></tr>` : ''}`).join('')}
    </tbody></table></div>`;
}

// Whisper-Modell nur zeigen, wenn lokal transkribiert wird.
function bindeWhisper(form) {
  const art = form.querySelector('[name=TRANSCRIPTION]'), felder = form.querySelectorAll('.whisperfeld'), cloud = form.querySelectorAll('.cloudfeld');
  if (!art || !(felder.length || cloud.length)) return;
  const zeige = () => {
    for (const feld of felder) feld.hidden = !art.value.startsWith('lokal');
    for (const feld of cloud) feld.hidden = art.value !== 'openai' && art.value !== 'lokal-openai';
  };
  art.addEventListener('change', zeige);
  zeige();
}

// Werte eines Einstellungsformulars; Mehrfachauswahl als Komma-Liste, nichts gewählt = „keine“.
const einstellungsWerte = (form) => ({ ...Object.fromEntries([...form.querySelectorAll('[name]')].map((e) => [e.name, e.value])),
  ...Object.fromEntries([...form.querySelectorAll('[data-multi]')].map((d) => [d.dataset.multi, [...d.querySelectorAll('input:checked')].map((e) => e.value).join(',') || 'keine'])) });

// Nach dem Speichern startet der Dienst neu: warten, bis er wieder antwortet.
async function warteAufNeustart() {
  await new Promise((r) => setTimeout(r, 2000));
  for (let i = 0; i < 30; i++) {
    try { if ((await fetch('/healthz', { cache: 'no-store' })).ok) return true; } catch (f) { /* startet noch */ }
    await new Promise((r) => setTimeout(r, 1000));
  }
  return false;
}

// ---------- Konto verbinden (OAuth, bisher OpenRouter) ----------
// Das Fenster der Freigabe öffnet sofort beim Klick (sonst blockt der Browser es) und bekommt die Adresse, sobald der
// Server den Ablauf angelegt hat. OpenRouter leitet danach auf /api/v1/ai/connect/openrouter; die Seite reicht code und state an das
// Fenster weiter, das die Verbindung begonnen hat (BroadcastChannel, sonst localStorage-Ereignis).
async function kiVerbindungStarten(pfad, body) {
  const fenster = window.open('about:blank', 'ki-verbinden', 'popup,width=520,height=760');
  try {
    const r = await api(pfad, { method: 'POST', body });
    if (fenster && !fenster.closed) fenster.location.href = r.url; else window.open(r.url, '_blank');
    return r;
  } catch (f) { fenster?.close(); throw f; }
}
// Wartet auf den Code: Nachricht der Rücksprung-Seite (gleicher Browser) oder Abholen beim Server (der Rücksprung
// legt ihn dort ab – nötig, wenn das Fenster in einem anderen Speicher läuft, z. B. iOS-Web-App). Ablehnung → Fehler,
// nach 15 Minuten '' (abgelaufen).
function kiVerbindungAbwarten(state) {
  return new Promise((resolve, reject) => {
    const kanal = 'BroadcastChannel' in window ? new BroadcastChannel('ki-verbinden') : null;
    let aus = false;
    const ende = () => { aus = true; kanal?.close(); window.removeEventListener('storage', speicher); clearInterval(uhr); clearTimeout(frist); };
    const fertig = (x) => {
      if (aus || !x || (x.state && x.state !== state)) return;
      if (x.fehler) { ende(); reject(new Error(tx('OpenRouter hat den Zugang nicht freigegeben.'))); return; }
      if (!x.code || x.state !== state) return;
      ende();
      resolve(x.code);
    };
    const speicher = (e) => { if (e.key === 'ki-verbinden' && e.newValue) try { fertig(JSON.parse(e.newValue)); } catch { /* fremder Wert */ } };
    if (kanal) kanal.onmessage = (e) => fertig(e.data);
    window.addEventListener('storage', speicher);
    const uhr = setInterval(async () => {
      try { const r = await api('/api/ki/verbinden/abholen', { method: 'POST', body: { state } }); if (r.code) fertig({ state, code: r.code }); } catch { /* weiter warten */ }
    }, 2500);
    const frist = setTimeout(() => { if (!aus) { ende(); resolve(''); } }, 15 * 60 * 1000);
  });
}
// Status → KI: OpenRouter-Konto verbinden (zugang = vorhandener Zugang, dessen Schlüssel erneuert wird); false = abgebrochen.
async function kiOpenRouterVerbinden(zugang) {
  const st = await kiVerbindungStarten('/api/ki/verbinden/openrouter/start', {});
  const oauthCode = st.von_hand ? (prompt(tx('Code von OpenRouter einfügen')) || '').trim() : await kiVerbindungAbwarten(st.state);
  if (!oauthCode) return false;
  await kiNachSpeichern(await api('/api/ki/verbinden/openrouter/fertig', { method: 'POST', body: { code: oauthCode, state: st.state, zugang } }));
  melde(tx('Mit OpenRouter verbunden'));
  return true;
}

// Rücksprung von OpenRouter: Code weiterreichen, Fenster schließen.
function zeigeKiVerbunden() {
  const q = new URLSearchParams(location.search);
  const code = q.get('code') || '', state = q.get('state') || '';
  const nachricht = code ? { code, state } : { state, fehler: q.get('error') || 'abgelehnt' };
  try { const k = new BroadcastChannel('ki-verbinden'); k.postMessage(nachricht); k.close(); } catch { /* ohne BroadcastChannel */ }
  try { localStorage.setItem('ki-verbinden', JSON.stringify({ ...nachricht, t: Date.now() })); localStorage.removeItem('ki-verbinden'); } catch { /* privat */ }
  // Auch beim Server ablegen: das ursprüngliche Fenster holt ihn dort ab, falls es die Nachricht nicht erreicht.
  if (code && state) api('/api/ki/verbinden/rueckgabe', { method: 'POST', body: { state, code } }).catch(() => {});
  seite.innerHTML = code ? T`<div class="mitte"><h1>${tx('Verbunden')}</h1><p>${tx('Die Freigabe ist angekommen. Dieses Fenster schließt sich gleich; weiter geht es im ursprünglichen Fenster.')}</p></div>`
    : T`<div class="mitte"><h1>${tx('Nicht verbunden')}</h1><p>${tx('OpenRouter hat keine Freigabe geschickt. Fenster schließen und neu verbinden.')}</p></div>`;
  if (code) setTimeout(() => window.close(), 1200);
}

// Ersteinrichtung in Schritten: Fortschrittsleiste oben (immer sichtbar, je Schritt ein Block), nur das Nötigste
// sichtbar, der Rest mit Vorgaben unter „Erweitert“. Gespeichert wird erst im letzten Schritt.
const ER_SCHRITTE = [
  { id: 'start', name: 'Start', titel: 'Willkommen' },
  { id: 'ki', name: 'KI', titel: 'Transkription und KI' },
  { id: 'dienste', name: 'Dienste', titel: 'YouTube und Joplin' },
  { id: 'grenzen', name: 'Grenzwerte', titel: 'Fristen und Grenzwerte' },
  { id: 'updates', name: 'Updates', titel: 'Updates und Backups' },
  { id: 'fertig', name: 'Fertig', titel: 'Zusammenfassung' },
];
const KONFIG_STATISTIK = 'TELEMETRY';
const ER_BEREICHE = ['kurztitel', 'auswertung', 'recherche', 'bilder'];
const ER_TITEL = { kurztitel: 'kurztitel', auswertung: 'auswertung', recherche: 'webrecherche', bilder: 'bilder' };

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
  const F = Object.fromEntries(d.felder.map((f) => [f.key, f]));
  const werte = Object.fromEntries(d.felder.map((f) => [f.key, f.wert ?? '']));
  // Öffentliche Adresse: vorgeschlagen ist die, unter der der Assistent gerade läuft (falls https).
  if (!werte.PUBLIC_URL && location.protocol === 'https:') werte.PUBLIC_URL = location.origin;
  // Sprache: die Seite läuft schon in der gewählten (Browser, ?lang= oder gespeichert); Umschalten lädt neu.
  werte.LANGUAGE = SPRACHE;
  // Whisper: Vorgabe das große Modell (large-v3-turbo), sonst das größte, das in den freien RAM passt.
  if (!d.eingerichtet && d.whisper && F.WHISPER_MODEL && werte.WHISPER_MODEL === F.WHISPER_MODEL.vorgabe) {
    const passt = d.whisper.modelle.filter((m) => !d.whisper.ram_verfuegbar || m.ram_mb * 1024 * 1024 <= d.whisper.ram_verfuegbar);
    werte.WHISPER_MODEL = (passt.find((m) => m.id === 'turbo') || passt[passt.length - 1] || d.whisper.modelle.find((m) => m.id === 'turbo'))?.id || werte.WHISPER_MODEL;
  }
  const ki = { zugaenge: [], wahl: { transkription: { zugang: d.whisper?.engine === false ? 'pebble' : 'whisper' }, ausweich: { zugang: '' } } };
  for (const b of ER_BEREICHE) ki.wahl[b] = { zugang: '', websuche: [] };
  // OpenAI-Schlüssel aus der Umgebung (Docker): OpenAI steht schon als Anbieter bereit.
  const envOpenAI = !!F.OPENAI_API_KEY?.gesperrt;
  let name = '', schritt = 0, weitester = 0, neu = null, fehler = '', schluessel = 'nutzer';
  // Stand nach dem Sprachwechsel (Seite lädt neu) – ohne Geheimnisse.
  try {
    const st = JSON.parse(sessionStorage.getItem('ersteinrichtung') || 'null');
    if (st?.code === code) {
      Object.assign(werte, st.werte, { LANGUAGE: SPRACHE });
      name = st.name || '';
      schluessel = st.schluessel || schluessel;
      if (st.ki) {
        Object.assign(ki, st.ki);
        // Anbieter mit Schlüssel gehen beim Sprachwechsel nicht mit: ihre Einträge fallen weg, neu verbinden.
        const fehlt = Object.values(ki.wahl).map((w) => w.zugang).filter((id) => id && id !== 'whisper' && id !== 'pebble' && !ki.zugaenge.some((z) => z.id === id));
        if (fehlt.length) { [...new Set(fehlt)].forEach((id) => anbieterWeg(id)); setTimeout(() => melde(tx('Anbieter mit Schlüssel bitte nach dem Sprachwechsel neu hinzufügen.')), 500); }
      }
    }
  } catch { /* kein Stand */ }
  sessionStorage.removeItem('ersteinrichtung');
  const geheim = (k) => F[k]?.art === 'secret';
  const art = (z) => kiArt(d, z);
  const zugang = (id) => ki.zugaenge.find((z) => z.id === id);
  const erlaubt = (b) => ki.zugaenge.filter((z) => art(z)?.bereiche?.includes(b));
  const searx = () => ki.zugaenge.filter((z) => art(z)?.api === 'searxng');
  if (envOpenAI && !ki.zugaenge.length) neuerAnbieter({ id: kiNeueID(), name: 'OpenAI', art: 'openai', adresse: '', schluessel: '', env: true });

  // Ein Anbieter kommt dazu: leere Bereiche, die er bedienen kann, übernimmt er mit der Vorgabe seiner Modellauswahl.
  function neuerAnbieter(z) {
    ki.zugaenge.push(z);
    const a = art(z) || {};
    if (a.api === 'searxng') {
      for (const b of ['auswertung', 'recherche']) { const w = ki.wahl[b]; if (w.zugang && !art(zugang(w.zugang))?.suche && !w.websuche.includes(z.id)) w.websuche.push(z.id); }
      return;
    }
    for (const b of ER_BEREICHE) {
      const w = ki.wahl[b];
      if (w.zugang || !a.bereiche?.includes(b)) continue;
      ki.wahl[b] = vorgabe(b, z);
    }
  }
  function vorgabe(slot, z) {
    const a = art(z) || {}, b = slot === 'ausweich' ? 'transkription' : slot;
    const vorschlag = z.vorschlag?.[b] || ''; // OpenRouter: Vorgabe aus dem Katalog, den der Server beim Verbinden abruft
    const e = { zugang: z.id, modell: a.modelle?.[b]?.[0]?.id || vorschlag, websuche: [] };
    if (b === 'auswertung' || b === 'recherche') e.websuche = a.suche ? ['anbieter'] : searx().map((x) => x.id);
    kiPreiseAusKatalog({ zugaenge: ki.zugaenge, arten: d.arten }, b, e);
    return e;
  }
  function anbieterWeg(id) {
    ki.zugaenge = ki.zugaenge.filter((z) => z.id !== id);
    for (const [b, w] of Object.entries(ki.wahl)) {
      if (w.zugang === id) ki.wahl[b] = b === 'transkription' ? { zugang: 'whisper' } : { zugang: '', websuche: [] };
      if (w.websuche) w.websuche = w.websuche.filter((x) => x !== id);
    }
  }

  // --- Bausteine ---
  const hilfe = (f) => (f.hilfe ? T`<p class="leise hilfe">${esc(tx(f.hilfe))}</p>` : '');
  const feld = (k, { label } = {}) => {
    const f = F[k];
    if (!f) return '';
    const id = `f-${k}`, w = werte[k] ?? '';
    const titel = T`<label for="${id}">${esc(label || tx(f.name))}${f.pflicht ? ' *' : ''}</label>`;
    if (f.art === 'choice' && f.auswahl?.length === 2 && f.auswahl.includes('aktiv')) {
      return T`<div class="feld"><label class="schalter"><input type="checkbox" data-schalter="${k}"${w === 'aktiv' ? ' checked' : ''}> ${esc(label || tx(f.name))}</label>${hilfe(f)}</div>`;
    }
    if (f.art === 'choice') return T`<div class="feld">${titel}<select id="${id}" data-w="${k}">${f.auswahl.map((c) => T`<option value="${esc(c)}"${c === w ? ' selected' : ''}>${esc(wahlText(f, c))}</option>`).join('')}</select>${hilfe(f)}</div>`;
    if (f.art === 'secret') {
      const platz = f.gesperrt ? tx('aus der Umgebung gesetzt') : '';
      return T`<div class="feld">${titel}<input type="password" id="${id}" data-w="${k}" value="${esc(w)}" placeholder="${platz}"${f.gesperrt ? ' disabled' : ''} autocomplete="off" spellcheck="false">${hilfe(f)}</div>`;
    }
    const modus = f.art === 'int' ? ' inputmode="numeric"' : f.art === 'num' ? ' inputmode="decimal"' : '';
    return T`<div class="feld">${titel}<input type="${f.art === 'url' ? 'url' : 'text'}" id="${id}" data-w="${k}" value="${esc(w)}"${modus}${f.gesperrt ? ' disabled' : ''} autocomplete="off" spellcheck="false">${hilfe(f)}</div>`;
  };
  const felder = (keys) => keys.map((k) => feld(k)).join('');
  const erweitert = (inhalt, titel = tx('Erweitert')) => (inhalt.trim() ? T`<details class="erweitert"><summary>${esc(titel)}</summary>${inhalt}</details>` : '');

  // Modell, Preise und Websuche eines KI-Eintrags (slot: Bereich oder „ausweich“ für die Transkription).
  function eintrag(slot) {
    const b = slot === 'ausweich' ? 'transkription' : slot;
    const e = ki.wahl[slot], z = zugang(e.zugang), a = art(z);
    if (!z) return '';
    const zahl = (k, n) => T`<label for="ke-${slot}-${k}">${esc(tx(n))} *</label><input type="text" id="ke-${slot}-${k}" data-slot="${slot}" data-k="${k}" inputmode="decimal" value="${e[k] ?? ''}" autocomplete="off">`;
    const katalog = a?.modelle?.[b];
    let html = katalog
      ? T`<label for="ke-${slot}-modell">Modell</label><select id="ke-${slot}-modell" data-slot="${slot}" data-k="modell">${katalog.map((m) => T`<option value="${esc(m.id)}"${m.id === e.modell ? ' selected' : ''}>${esc(tx(m.name))}${kiPreisText(m) ? ` · ${esc(kiPreisText(m))}` : ''}</option>`).join('')}</select>`
      : T`<label for="ke-${slot}-modell">Modell *</label><input type="text" id="ke-${slot}-modell" data-slot="${slot}" data-k="modell" value="${esc(e.modell || '')}" list="ke-${slot}-liste" autocomplete="off" spellcheck="false">
        <datalist id="ke-${slot}-liste">${(z.modelle || []).map((m) => T`<option value="${esc(m)}">`).join('')}</datalist>
        ${z.modelle ? '' : T`<p class="leise hilfe">${tx('Modellvorschläge nach „Prüfen“ beim Anbieter.')}</p>`}`;
    if (a?.cloud && !katalog && !a.preise_optional) {
      if (b === 'transkription') html += zahl('preis_minute', 'Preis (USD je Minute)');
      else if (b === 'bilder') html += zahl('preis_bild', 'Preis (USD je Bild)');
      else html += zahl('preis_ein', 'Preis Eingabe (USD je 1 Mio. Tokens)') + zahl('preis_aus', 'Preis Ausgabe (USD je 1 Mio. Tokens)');
    }
    if (b === 'auswertung' || b === 'recherche') {
      const sx = searx();
      if (a?.suche && !e.websuche?.includes('anbieter')) e.websuche = ['anbieter', ...(e.websuche || [])]; // Suche des Anbieters immer
      html += sx.length ? T`<p class="feldname">${a?.suche ? tx('Zusätzliche Websuche') : tx('Websuche')}</p><div class="mehrfach">
        ${sx.map((x) => T`<label class="schalter"><input type="checkbox" data-slot="${slot}" data-suche="${esc(x.id)}"${e.websuche?.includes(x.id) ? ' checked' : ''}> ${esc(x.name)}</label>`).join('')}</div>` : '';
      html += T`
        ${!a?.suche && !sx.length ? T`<p class="leise hilfe">${tx('Ein lokales Modell sucht nicht selbst im Web; dafür einen SearXNG-Anbieter hinzufügen.')}</p>` : ''}
        ${a?.cloud && !katalog && !a.preise_optional && e.websuche?.includes('anbieter') ? zahl('preis_suche', 'Preis Websuche (USD je Aufruf)') : ''}`;
    }
    return html;
  }

  // --- Schritte ---
  const SEITEN = {
    start: () => T`<p class="leise">Ein paar Angaben, dann ist die App bereit. Alles lässt sich später unter Status → Konfiguration ändern.</p>
      ${feld('LANGUAGE')}
      ${d.eingerichtet ? '' : T`<div class="feld"><label for="f-name">Dein Name *</label><input id="f-name" data-name="1" value="${esc(name)}" autocomplete="given-name" maxlength="40"><p class="leise hilfe">Du bist der erste Nutzer und Admin. Weitere Nutzer lädst du später ein.</p></div>`}
      ${feld('PUBLIC_URL')}
      ${d.eingerichtet ? '' : T`<fieldset><legend>${tx('Verschlüsselung')}</legend>
        <p class="leise">${tx('Notizen, Listen und Aufnahmen liegen immer verschlüsselt, je Nutzer mit eigenem Schlüssel. Ein zweiter Schlüssel schützt die Geheimnisse des Systems (API-Schlüssel, Benachrichtigungen) im Backup.')}</p>
        <div class="wahlliste">
          <label class="wahlzeile${schluessel === 'nutzer' ? ' an' : ''}"><input type="radio" name="er-schluessel" value="nutzer"${schluessel === 'nutzer' ? ' checked' : ''}><span><strong>${tx('Nur ich bin Admin')}</strong><span class="leise">${tx('Ein gemeinsamer Code: dein Wiederherstellungscode stellt auch das System wieder her.')}</span></span></label>
          <label class="wahlzeile${schluessel === 'code' ? ' an' : ''}"><input type="radio" name="er-schluessel" value="code"${schluessel === 'code' ? ' checked' : ''}><span><strong>${tx('Mehrere Admins')}</strong><span class="leise">${tx('Getrennte Schlüssel (Pflicht bei mehreren Admins): das System bekommt einen eigenen Code.')}</span></span></label>
        </div><p class="leise hilfe">${tx('Die Codes siehst du nach dem Anlegen deines Passkeys – einmalig, zum Notieren.')}</p></fieldset>`}
      ${erweitert(felder(['TRUSTED_PROXY']))}`,

    ki: () => {
      // Schon eingerichtet (ungültige Einstellungen reparieren): KI bleibt, wie sie ist – geändert unter Status → KI.
      if (d.eingerichtet) return T`<p class="hinweis">${tx('KI-Anbieter und Transkription bleiben unverändert; ändern unter Status → KI.')}</p>`;
      const t = ki.wahl.transkription;
      const trans = ki.zugaenge.filter((z) => art(z)?.bereiche?.includes('transkription'));
      const wm = d.whisper?.modelle.find((m) => m.id === werte.WHISPER_MODEL);
      const option = (wert, titel, text) => T`<label class="wahlzeile${t.zugang === wert ? ' an' : ''}"><input type="radio" name="er-trans" value="${esc(wert)}"${t.zugang === wert ? ' checked' : ''}><span><strong>${esc(titel)}</strong>${text ? T`<span class="leise">${esc(text)}</span>` : ''}</span></label>`;
      const whisperTeil = t.zugang === 'whisper' && d.whisper ? T`<div class="feld"><label for="f-WHISPER_MODEL">${tx('Whisper-Modell')}</label>
          <div class="mitknopf"><select id="f-WHISPER_MODEL" data-w="WHISPER_MODEL">${d.whisper.modelle.map((m) => T`<option value="${esc(m.id)}"${m.id === werte.WHISPER_MODEL ? ' selected' : ''}>${esc(m.name)} · ${groesse(m.bytes)}</option>`).join('')}</select>
          <button type="button" class="btn klein" data-hilfe="whisper" aria-expanded="false">${tx('Hilfe')}</button></div>
          <p class="leise hilfe">${tx('Größer = genauer, aber langsamer und mehr RAM. Wird nach der Einrichtung geladen.')}</p>
          <div id="er-whisperhilfe" hidden>${whisperTabelle(d.whisper, { gewaehlt: werte.WHISPER_MODEL })}</div></div>
        ${trans.length ? T`<label class="schalter"><input type="checkbox" data-ausweich="1"${ki.wahl.ausweich.zugang ? ' checked' : ''}> ${tx('Scheitert Whisper, übernimmt ein Cloud-Anbieter')}</label>
          ${ki.wahl.ausweich.zugang ? T`<select data-ausweichzugang="1" aria-label="${esc(tx('Ausweich-Anbieter'))}">${trans.map((z) => T`<option value="${esc(z.id)}"${z.id === ki.wahl.ausweich.zugang ? ' selected' : ''}>${esc(z.name)}</option>`).join('')}</select>${eintrag('ausweich')}` : ''}` : ''}
        ${erweitert(felder(['WHISPER_KEEP_MINUTES']))}` : '';
      const textBereich = ER_BEREICHE.some((b) => erlaubt(b).length);
      return T`<fieldset><legend>${tx('KI-Anbieter')}</legend>
        <p class="leise">${tx('Für Titel, Auswertung, Einsortieren und Recherche braucht die App einen KI-Anbieter. Am einfachsten ist OpenRouter: ein Konto für viele Modelle, verbunden ohne API-Schlüssel. Daneben gehen OpenAI, Grok, Gemini und Muse mit API-Schlüssel, ein OpenAI-kompatibler Dienst oder ein lokales Modell. Ohne Anbieter werden Aufnahmen nur in Text umgewandelt.')}</p>
        ${ki.zugaenge.length ? T`<div class="karte kizeilen">${ki.zugaenge.map((z) => T`<div class="kizeile erzeile"><span class="kizeilename">${esc(z.name)}</span><span class="leise">${esc(tx(art(z)?.name || z.art))}</span>
          <button type="button" class="btn klein" data-weg="${esc(z.id)}">${tx('Entfernen')}</button></div>`).join('')}</div>` : ''}
        ${neu ? neuForm() : ki.zugaenge.some((z) => art(z)?.oauth) ? T`<div class="leiste"><button type="button" class="btn" data-neu="1">${tx('Weiteren Anbieter hinzufügen')}</button></div>`
          : T`<div class="leiste"><button type="button" class="btn haupt" data-schnell="1">${tx('Mit OpenRouter verbinden')}</button><button type="button" class="btn" data-neu="1">${tx('Anderen Anbieter hinzufügen')}</button></div>
            <p class="leise hilfe">${tx('Empfohlen: Mit OpenRouter funktionieren alle Funktionen – Auswertung, Websuche, Bilder und Transkription.')}</p>`}
        </fieldset>

        <fieldset><legend>${tx('Transkription')}</legend><p class="leise">${tx('Wer die Aufnahmen in Text umwandelt.')}</p>
        <div class="wahlliste">
          ${d.whisper?.engine === false ? '' : option('whisper', tx('Whisper auf diesem Server'), tx('Die Aufnahme verlässt den Server nicht.') + (wm ? ` ${wm.name}` : ''))}
          ${trans.map((z) => option(z.id, z.name, tx('Cloud-Anbieter'))).join('')}
          ${option('pebble', tx('Aus – Text der Pebble-App übernehmen'), tx('Keine eigene Transkription.'))}
        </div>
        ${whisperTeil}${zugang(t.zugang) ? eintrag('transkription') : ''}</fieldset>
        ${ER_BEREICHE.map((b) => {
          const s = KI_SEITEN[ER_TITEL[b]], w = ki.wahl[b], moeglich = erlaubt(b);
          return T`<fieldset><legend>${esc(tx(s.titel))}</legend><p class="leise">${esc(tx(s.text))}</p>
            ${moeglich.length ? T`<label for="er-b-${b}">${tx('Anbieter')}</label><select id="er-b-${b}" data-bereich="${b}"><option value="">${tx('Aus')}</option>${moeglich.map((z) => T`<option value="${esc(z.id)}"${z.id === w.zugang ? ' selected' : ''}>${esc(z.name)}</option>`).join('')}</select>
            ${eintrag(b)}` : T`<p class="hinweis">${b === 'bilder' ? tx('Füge oben einen Anbieter mit Bildern hinzu (OpenRouter, OpenAI, Grok, Gemini oder Muse).') : tx('Füge oben einen KI-Anbieter hinzu, um diesen Bereich zu nutzen.')}</p>`}</fieldset>`;
        }).join('')}
        ${textBereich ? erweitert(felder([...(ki.zugaenge.some((z) => ['openai', 'xai', 'muse', 'gemini'].includes(z.art)) ? ['OPENAI_PLAN_REASONING_EFFORT'] : []), 'RESEARCH_LIMIT_USD', 'RESEARCH_MAX_OUTPUT_TOKENS'])) : ''}`;
    },

    dienste: () => T`<p class="leise">Beides ist freiwillig und lässt sich später einrichten.</p>
      <fieldset><legend>YouTube</legend>${feld('YOUTUBE_API_KEY', { label: tx('API-Schlüssel (optional)') })}</fieldset>
      <fieldset><legend>Joplin</legend>${feld('JOPLIN_ENABLED', { label: tx('Notizen nach Joplin übertragen') })}
        ${werte.JOPLIN_ENABLED === 'aktiv' ? felder(['JOPLIN_URL', 'JOPLIN_EMAIL', 'JOPLIN_PASSWORD', 'JOPLIN_NOTEBOOK']) : ''}</fieldset>`,

    grenzen: () => T`<p class="leise">Die Vorgaben passen für die meisten.</p>
      <fieldset><legend>Fristen</legend>${felder(['TRASH_RETENTION_DAYS', 'EMPTY_RETENTION_HOURS', 'PLANNING_AUTOCLOSE_DAYS'])}</fieldset>
      ${ki.zugaenge.some((z) => art(z)?.cloud) ? T`<fieldset><legend>${tx('KI-Kosten')}</legend>${felder(['USER_DAILY_LIMIT_USD'])}</fieldset>` : ''}
      ${erweitert(felder(['LIST_DONE_ACTIVE_MINUTES', 'VISIT_GAP_MINUTES', 'NOTE_CONTEXT_MINUTES', 'HISTORY_MIN_ENTRIES', 'MEMORY_TRAIN_ENTRIES', 'MEMORY_REJECTED']), tx('Anzeige und Gedächtnis'))}
      ${erweitert(felder(['TRIGGER_SINGLE', 'TRIGGER_DOUBLE', 'RING_CAPACITY_HOURS', 'MAX_ATTEMPTS', 'MIN_FREE_MB', 'HOOK_MAX_MB', 'HOOK_RATE_PER_MIN']), tx('Pebble-App und Grenzwerte'))}`,

    updates: () => T`<fieldset><legend>Updates</legend>
        ${F.AUTO_UPDATE ? T`${feld('AUTO_UPDATE', { label: tx('Updates automatisch installieren') })}${erweitert(felder(['AUTO_UPDATE_TIME']))}`
          : T`<p class="leise">${tx('Im Container: Updates mit docker compose pull und docker compose up -d.')}</p>`}</fieldset>
      <fieldset><legend>Backups</legend>${felder(['BACKUP_SCHEDULE', 'BACKUP_AUDIO'])}${erweitert(felder(['BACKUP_TIME', 'BACKUP_RETENTION_DAYS']))}</fieldset>`,

    fertig: () => {
      const wahl = (k) => (F[k] ? wahlText(F[k], werte[k]) : '–');
      const t = ki.wahl.transkription;
      const transText = t.zugang === 'whisper' ? `Whisper ${d.whisper?.modelle.find((m) => m.id === werte.WHISPER_MODEL)?.name || ''}` : t.zugang === 'pebble' ? tx('Text der Pebble-App') : zugang(t.zugang)?.name || '–';
      const bereiche = ER_BEREICHE.filter((b) => ki.wahl[b].zugang).map((b) => tx(KI_SEITEN[ER_TITEL[b]].titel));
      const zeile = (n, w, s) => T`<dt>${esc(n)}</dt><dd>${esc(w)} <button type="button" class="linkknopf" data-gehe="${s}">${tx('Ändern')}</button></dd>`;
      return T`<p class="leise">Prüfe kurz deine Angaben. Mit „Einrichten“ wird gespeichert und der Dienst startet neu.</p>
        <dl class="eckdaten">
          ${zeile(tx('Sprache'), wahl('LANGUAGE'), 0)}
          ${d.eingerichtet ? '' : zeile(tx('Name'), name, 0)}
          ${zeile(tx('Adresse'), werte.PUBLIC_URL || '–', 0)}
          ${d.eingerichtet ? '' : zeile(tx('Schlüssel'), schluessel === 'code' ? tx('getrennt (mehrere Admins)') : tx('gemeinsam (ein Admin)'), 0)}
          ${zeile(tx('Transkription'), transText, 1)}
          ${zeile(tx('KI'), ki.zugaenge.length ? `${ki.zugaenge.map((z) => z.name).join(', ')}${bereiche.length ? ` – ${bereiche.join(', ')}` : ''}` : tx('kein Anbieter'), 1)}
          ${zeile('YouTube', werte.YOUTUBE_API_KEY ? tx('Schlüssel gesetzt') : tx('aus'), 2)}
          ${zeile('Joplin', werte.JOPLIN_ENABLED === 'aktiv' ? tx('an') : tx('aus'), 2)}
          ${F.AUTO_UPDATE ? zeile(tx('Updates'), werte.AUTO_UPDATE === 'aktiv' ? tx('automatisch') : tx('von Hand'), 4) : ''}
          ${zeile(tx('Backups'), wahl('BACKUP_SCHEDULE'), 4)}
        </dl>
        ${fehler ? T`<div class="hinweis schlecht">${esc(fehler)}</div>` : ''}
        ${F[KONFIG_STATISTIK] ? T`<label class="schalter erstatistik"><input type="checkbox" data-schalter="${KONFIG_STATISTIK}"${werte[KONFIG_STATISTIK] === 'aktiv' ? ' checked' : ''}> ${tx('Anonyme Statistik')}</label>
          <p class="leise erstatistik">${tx('Gesendet wird nur die gesamte Aufnahmezeit je Ring in Sekunden, ohne Bezug zu dir – damit sich die maximale Laufzeit der Ringe besser berechnen lässt.')}</p>` : ''}`;
    },
  };

  // Formular „Anbieter hinzufügen“: Art, Name, Adresse, Schlüssel; Prüfen fragt die Modelle ab.
  function neuForm() {
    const a = art(neu) || {};
    const schluessel = a.schluessel && a.schluessel !== 'kein';
    const schluesselFeld = schluessel ? T`<label for="er-n-key">${tx('Schlüssel')}${a.schluessel === 'pflicht' && !(neu.art === 'openai' && envOpenAI) && !a.oauth ? ' *' : ''}</label><input id="er-n-key" type="password" data-n="schluessel" value="${esc(neu.schluessel || '')}" placeholder="${neu.art === 'openai' && envOpenAI ? esc(tx('aus der Umgebung gesetzt')) : ''}" autocomplete="off" spellcheck="false">` : '';
    if (a.oauth) {
      return T`<div class="karte erneu">
        <label for="er-n-art">${tx('Art')}</label><select id="er-n-art" data-n="art">${(d.arten || []).map((x) => T`<option value="${esc(x.id)}"${x.id === neu.art ? ' selected' : ''}>${esc(tx(x.name))}</option>`).join('')}</select>
        ${a.text ? T`<p class="leise hilfe">${esc(tx(a.text))}</p>` : ''}
        <p>${tx('Melde dich bei OpenRouter an und gib den Zugang frei. Ein API-Schlüssel ist nicht nötig; Ausgaben begrenzt du dort selbst.')}</p>
        <div class="leiste"><button type="button" class="btn haupt" data-verbinden="1">${tx('Mit OpenRouter verbinden')}</button><button type="button" class="btn" data-abbruch="1">${tx('Abbrechen')}</button></div>
        ${neu.vonHand ? T`<label for="er-n-oacode">${tx('Code von OpenRouter')}</label><div class="mitknopf"><input id="er-n-oacode" data-n="oacode" value="${esc(neu.oacode || '')}" autocomplete="off" spellcheck="false"><button type="button" class="btn" data-oacode="1">${tx('Übernehmen')}</button></div>
          <p class="leise hilfe">${tx('Diese Seite läuft ohne https: OpenRouter zeigt den Code nach der Freigabe an – hier einfügen.')}</p>` : ''}
        <p class="leise hilfe" id="er-n-pruef">${neu.pruef ? esc(neu.pruef) : ''}</p>
        <details class="erweitert"><summary>${tx('Stattdessen API-Schlüssel einfügen')}</summary>${schluesselFeld}
          <div class="leiste"><button type="button" class="btn" data-hinzu="1">${tx('Hinzufügen')}</button></div></details></div>`;
    }
    return T`<div class="karte erneu">
      <label for="er-n-art">${tx('Art')}</label><select id="er-n-art" data-n="art">${(d.arten || []).map((x) => T`<option value="${esc(x.id)}"${x.id === neu.art ? ' selected' : ''}>${esc(tx(x.name))}</option>`).join('')}</select>
      ${a.text ? T`<p class="leise hilfe">${esc(tx(a.text))}</p>` : ''}
      <label for="er-n-name">Name *</label><input id="er-n-name" data-n="name" value="${esc(neu.name)}" autocomplete="off">
      ${a.adresse_fest ? '' : T`<label for="er-n-adresse">${tx('Adresse')}${a.adresse ? '' : ' *'}</label><input id="er-n-adresse" type="url" data-n="adresse" value="${esc(neu.adresse || '')}" placeholder="${esc(a.adresse || (a.api === 'searxng' ? 'http://<server>:8080' : 'https://…/v1'))}" autocomplete="off" spellcheck="false">`}
      ${schluesselFeld}
      <p class="leise hilfe" id="er-n-pruef">${neu.pruef ? esc(neu.pruef) : ''}</p>
      <div class="leiste"><button type="button" class="btn haupt" data-hinzu="1">${tx('Hinzufügen')}</button><button type="button" class="btn" data-pruefen="1">${tx('Prüfen')}</button><button type="button" class="btn" data-abbruch="1">${tx('Abbrechen')}</button></div></div>`;
  }
  const neuerName = (artId) => {
    const basis = tx(d.arten.find((x) => x.id === artId)?.name || artId).replace(/ \(.*\)$/, '');
    let n = basis, i = 2;
    while (ki.zugaenge.some((z) => z.name.toLowerCase() === n.toLowerCase())) n = `${basis} ${i++}`;
    return n;
  };

  // Prüfung eines Schritts vor „Weiter“; liefert die erste Meldung oder ''.
  function pruefe(i) {
    const s = ER_SCHRITTE[i].id;
    if (s === 'start') {
      if (!d.eingerichtet && !name.trim()) return tx('Bitte deinen Namen eingeben');
      try { const u = new URL(werte.PUBLIC_URL); if (u.protocol !== 'https:' && !(u.protocol === 'http:' && ['localhost', '127.0.0.1'].includes(u.hostname))) return tx('Die öffentliche Adresse muss mit https:// beginnen'); } catch { return tx('Bitte die öffentliche Adresse eingeben (https://…)'); }
    }
    if (s === 'ki') {
      if (neu) return tx('Den neuen Anbieter erst hinzufügen oder abbrechen');
      for (const [slot, e] of Object.entries(ki.wahl)) {
        const z = zugang(e.zugang);
        if (!z || (slot === 'ausweich' && ki.wahl.transkription.zugang !== 'whisper')) continue;
        const b = slot === 'ausweich' ? 'transkription' : slot;
        const a = art(z), titel = slot === 'ausweich' ? tx('Ausweich-Anbieter') : tx(b === 'transkription' ? 'Transkription' : KI_SEITEN[ER_TITEL[b]].titel);
        if (!e.modell?.trim()) return tx('{0}: Modell fehlt', titel);
        if (a?.cloud && !a.modelle?.[b] && !a.preise_optional) {
          const preise = b === 'transkription' ? ['preis_minute'] : b === 'bilder' ? ['preis_bild'] : ['preis_ein', 'preis_aus', ...(e.websuche?.includes('anbieter') ? ['preis_suche'] : [])];
          if (preise.some((k) => e[k] == null || Number.isNaN(e[k]))) return tx('{0}: Preise fehlen', titel);
        }
      }
    }
    return '';
  }

  // Einträge der Bereiche aus der Wahl.
  function bereicheAusWahl() {
    const out = {};
    const eintragAus = (id, e) => {
      const { zugang: z, modell, websuche, ...rest } = e;
      const x = { id, zugang: z, modell, ...Object.fromEntries(Object.entries(rest).filter(([k, v]) => k.startsWith('preis_') && v != null)) };
      if (websuche?.length) x.websuche = websuche;
      return x;
    };
    const t = ki.wahl.transkription;
    if (t.zugang === 'whisper') {
      out.transkription = [{ id: 'transkription-1', zugang: 'whisper' }];
      if (ki.wahl.ausweich.zugang) out.transkription.push(eintragAus('transkription-2', ki.wahl.ausweich));
    } else if (t.zugang === 'pebble') out.transkription = [{ id: 'transkription-1', zugang: 'pebble' }];
    else out.transkription = [eintragAus('transkription-1', t)];
    for (const b of ER_BEREICHE) out[b] = ki.wahl[b].zugang ? [eintragAus(`${b}-1`, ki.wahl[b])] : [];
    return out;
  }

  // --- Malen ---
  seite.innerHTML = T`<h1>Ersteinrichtung</h1>
    ${d.eingerichtet ? '' : T`<nav class="btn-group unter" aria-label="Art"><button type="button" class="btn aktiv" data-art="neu" aria-pressed="true">Neu einrichten</button><button type="button" class="btn" data-art="backup" aria-pressed="false">Backup einspielen</button></nav>
      <div id="backupweg" hidden></div>`}
    <div id="neuweg">${d.fehler ? T`<div class="hinweis schlecht">Die gespeicherten Einstellungen sind ungültig: ${esc(d.fehler)}</div>` : ''}
      <nav class="erschritte" aria-label="${esc(tx('Fortschritt'))}"><ol></ol><p class="erstand" aria-live="polite"></p></nav>
      <form class="einstellungen erform" novalidate><div id="erinhalt"></div>
        <div class="leiste erleiste"><button type="button" class="btn" data-zurueck="1">${tx('Zurück')}</button><button type="submit" class="btn haupt"></button></div></form>
      <div id="ergebnis"></div></div>`;
  const leiste = seite.querySelector('.erschritte'), form = seite.querySelector('.erform'), inhalt = document.getElementById('erinhalt');
  // Die Leiste klebt unter dem Kopf der Seite.
  const kopfH = () => { leiste.style.top = `${document.querySelector('.kopf')?.offsetHeight || 0}px`; };
  kopfH();
  window.addEventListener('resize', kopfH);
  const male = () => {
    leiste.querySelector('ol').innerHTML = ER_SCHRITTE.map((s, i) => T`<li><button type="button" class="erblock${i < schritt ? ' erledigt' : ''}${i === schritt ? ' aktiv' : ''}" data-gehe="${i}"${i > weitester ? ' disabled' : ''}${i === schritt ? ' aria-current="step"' : ''} title="${esc(tx(s.titel))}"><span class="erlang">${esc(tx(s.name))}</span><span class="erkurz" aria-hidden="true">${i + 1}</span></button></li>`).join('');
    leiste.querySelector('.erstand').textContent = tx('Schritt {0} von {1}: {2}', schritt + 1, ER_SCHRITTE.length, tx(ER_SCHRITTE[schritt].titel));
    inhalt.innerHTML = T`<h2>${esc(tx(ER_SCHRITTE[schritt].titel))}</h2>${SEITEN[ER_SCHRITTE[schritt].id]()}`;
    form.querySelector('[data-zurueck]').hidden = schritt === 0;
    form.querySelector('[type=submit]').textContent = schritt === ER_SCHRITTE.length - 1 ? tx('Einrichten') : tx('Weiter');
  };
  const gehe = (i) => {
    schritt = Math.max(0, Math.min(ER_SCHRITTE.length - 1, i));
    weitester = Math.max(weitester, schritt);
    fehler = '';
    male();
    window.scrollTo(0, 0);
  };
  male();

  seite.querySelectorAll('[data-art]').forEach((b) => { b.onclick = () => {
    seite.querySelectorAll('[data-art]').forEach((x) => { x.classList.toggle('aktiv', x === b); x.setAttribute('aria-pressed', x === b); });
    document.getElementById('neuweg').hidden = b.dataset.art !== 'neu';
    const weg = document.getElementById('backupweg');
    weg.hidden = b.dataset.art !== 'backup';
    if (!weg.hidden && !weg.innerHTML) einrichtungMitBackup(weg, code);
  }; });

  // Eingaben merken; Umschalter, die andere Felder zeigen, malen den Schritt neu.
  form.addEventListener('input', (ev) => {
    const el = ev.target;
    if (el.dataset.w) werte[el.dataset.w] = el.value;
    else if (el.dataset.name) name = el.value;
    else if (el.dataset.n) neu[el.dataset.n] = el.value;
    else if (el.dataset.slot && el.dataset.k) {
      const e = ki.wahl[el.dataset.slot];
      e[el.dataset.k] = el.dataset.k.startsWith('preis_') ? kiZahl(el.value) : el.value;
    }
  });
  form.addEventListener('change', (ev) => {
    const el = ev.target;
    const dk = { zugaenge: ki.zugaenge, arten: d.arten };
    if (el.dataset.w === 'LANGUAGE') {
      // Seite in der gewählten Sprache neu laden; der Stand (ohne Geheimnisse) kommt mit.
      const ohne = Object.fromEntries(Object.entries(werte).filter(([k]) => !geheim(k)));
      sessionStorage.setItem('ersteinrichtung', JSON.stringify({ code, werte: ohne, name, schluessel, ki: { ...ki, zugaenge: ki.zugaenge.filter((z) => !z.schluessel || z.env) } }));
      const u = new URL(location.href);
      u.searchParams.set('lang', el.value);
      location.replace(u);
    } else if (el.dataset.schalter) {
      werte[el.dataset.schalter] = el.checked ? 'aktiv' : 'inaktiv';
      if (el.dataset.schalter === 'JOPLIN_ENABLED') male();
    } else if (el.dataset.w === 'WHISPER_MODEL') {
      werte.WHISPER_MODEL = el.value;
      const box = document.getElementById('er-whisperhilfe');
      if (box) box.innerHTML = whisperTabelle(d.whisper, { gewaehlt: el.value });
    } else if (el.name === 'er-schluessel') {
      schluessel = el.value;
      male();
    } else if (el.name === 'er-trans') {
      ki.wahl.transkription = el.value === 'whisper' || el.value === 'pebble' ? { zugang: el.value } : vorgabe('transkription', zugang(el.value));
      male();
    } else if (el.dataset.ausweich) {
      const erster = ki.zugaenge.find((z) => art(z)?.bereiche?.includes('transkription'));
      ki.wahl.ausweich = el.checked && erster ? vorgabe('ausweich', erster) : { zugang: '' };
      male();
    } else if (el.dataset.ausweichzugang) {
      ki.wahl.ausweich = vorgabe('ausweich', zugang(el.value));
      male();
    } else if (el.dataset.bereich) {
      const b = el.dataset.bereich;
      ki.wahl[b] = el.value ? vorgabe(b, zugang(el.value)) : { zugang: '', websuche: [] };
      male();
    } else if (el.dataset.suche) {
      const e = ki.wahl[el.dataset.slot];
      e.websuche = [...(e.websuche?.includes('anbieter') ? ['anbieter'] : []), ...[...form.querySelectorAll(T`[data-slot="${el.dataset.slot}"][data-suche]:checked`)].map((c) => c.dataset.suche)];
      kiPreiseAusKatalog(dk, el.dataset.slot === 'ausweich' ? 'transkription' : el.dataset.slot, e);
      male();
    } else if (el.dataset.slot && el.dataset.k === 'modell' && el.tagName === 'SELECT') {
      const e = ki.wahl[el.dataset.slot];
      e.modell = el.value;
      kiPreiseAusKatalog(dk, el.dataset.slot === 'ausweich' ? 'transkription' : el.dataset.slot, e);
      male();
    } else if (el.dataset.n === 'art') {
      neu = { ...neu, art: el.value, name: neuerName(el.value), modelle: null, pruef: '' };
      male();
    }
  });
  form.addEventListener('click', async (ev) => {
    const b = ev.target.closest('button[type=button]');
    if (!b) return;
    if (b.dataset.zurueck) gehe(schritt - 1);
    else if (b.dataset.hilfe) {
      const box = document.getElementById('er-whisperhilfe');
      box.hidden = !box.hidden;
      b.setAttribute('aria-expanded', String(!box.hidden));
    } else if (b.dataset.neu) {
      const frei = (x) => !ki.zugaenge.some((z) => z.art === x.id);
      const a = d.arten.find((x) => x.oauth && frei(x)) || d.arten.find(frei) || d.arten[0];
      neu = { id: kiNeueID(), art: a.id, name: neuerName(a.id), adresse: '', schluessel: '' };
      male();
      document.getElementById('er-n-art')?.focus();
    } else if (b.dataset.abbruch) { neu = null; male(); }
    else if (b.dataset.weg) { anbieterWeg(b.dataset.weg); male(); }
    else if (b.dataset.pruefen) {
      const info = document.getElementById('er-n-pruef');
      b.disabled = true;
      info.textContent = tx('Prüft …');
      try {
        const r = await api('/api/ersteinrichtung/openai', { method: 'POST', body: { code, art: neu.art, base_url: neu.adresse, key: neu.schluessel } });
        neu.modelle = r.modelle || null;
        neu.pruef = r.ok ? (r.modelle ? tx('Erreichbar, {0} Modelle', r.modelle.length) : tx('Erreichbar')) : T`${tx('Fehler')}: ${r.fehler}`;
      } catch (f) { neu.pruef = f.message; }
      male();
    } else if (b.dataset.verbinden || b.dataset.schnell) {
      if (b.dataset.schnell) {
        const a = d.arten.find((x) => x.oauth);
        neu = { id: kiNeueID(), art: a.id, name: neuerName(a.id), adresse: '', schluessel: '' };
      }
      b.disabled = true;
      neu.pruef = tx('Warte auf die Freigabe bei OpenRouter …');
      male();
      try {
        const st = await kiVerbindungStarten('/api/ersteinrichtung/verbinden/openrouter/start', { code, adresse: location.origin });
        if (!neu) return;
        neu.state = st.state;
        if (st.von_hand) { neu.vonHand = true; neu.pruef = ''; male(); return; }
        const oauthCode = await kiVerbindungAbwarten(st.state);
        if (!oauthCode) { if (neu?.state === st.state) { neu.pruef = tx('Verbindung abgelaufen – bitte neu verbinden'); male(); } return; }
        verbindeMitCode(oauthCode, st.state);
      } catch (f) { if (neu) { neu.pruef = f.message; male(); } else melde(f.message); }
    } else if (b.dataset.oacode) {
      if (!neu.oacode?.trim()) { melde(tx('Code fehlt')); return; }
      verbindeMitCode(neu.oacode.trim(), neu.state);
    } else if (b.dataset.hinzu) {
      hinzufuegen();
    }
  });
  // Code der Freigabe gegen den Schlüssel tauschen, Modelle holen und den Anbieter gleich hinzufügen.
  async function verbindeMitCode(oauthCode, state) {
    // Nur, wenn noch derselbe OpenRouter-Anbieter offen ist (nicht abgebrochen, Art nicht gewechselt).
    if (!neu || neu.state !== state || !art(neu)?.oauth) return;
    try {
      const r = await api('/api/ersteinrichtung/verbinden/openrouter/fertig', { method: 'POST', body: { code, oauth_code: oauthCode, state } });
      if (!neu || neu.state !== state) return;
      neu.schluessel = r.key;
      neu.vorschlag = r.vorschlag || {};
      try {
        const m = await api('/api/ersteinrichtung/openai', { method: 'POST', body: { code, art: neu.art, base_url: '', key: r.key } });
        if (m.ok && m.modelle) neu.modelle = m.modelle;
      } catch { /* Modelle nur als Vorschlag */ }
      hinzufuegen();
      melde(tx('Mit OpenRouter verbunden'));
    } catch (f) { if (neu?.state === state) { neu.pruef = f.message; male(); } else melde(f.message); }
  }
  function hinzufuegen() {
    {
      const a = art(neu) || {};
      const name2 = neu.name.trim();
      const meld = !name2 ? tx('Name fehlt') : ki.zugaenge.some((z) => z.name.toLowerCase() === name2.toLowerCase()) ? tx('Name gibt es schon')
        : !a.adresse_fest && !a.adresse && !neu.adresse.trim() ? tx('Adresse fehlt')
          : a.schluessel === 'pflicht' && !neu.schluessel.trim() && !(neu.art === 'openai' && envOpenAI) ? tx('Schlüssel fehlt') : '';
      if (meld) { melde(meld); return; }
      const { pruef, vonHand, oacode, state, ...z } = neu;
      z.name = name2;
      z.adresse = a.adresse_fest ? '' : z.adresse.trim();
      if (neu.art === 'openai' && envOpenAI && !z.schluessel) z.env = true;
      neu = null;
      neuerAnbieter(z);
      male();
    }
  }
  leiste.addEventListener('click', (ev) => {
    const b = ev.target.closest('[data-gehe]');
    if (!b || b.disabled) return;
    const ziel = Number(b.dataset.gehe);
    // Vorwärts nur über geprüfte Schritte.
    for (let i = schritt; i < ziel; i++) { const m = pruefe(i); if (m) { gehe(i); melde(m); return; } }
    gehe(ziel);
  });
  inhalt.addEventListener('click', (ev) => {
    const b = ev.target.closest('[data-gehe]');
    if (b) gehe(Number(b.dataset.gehe));
  });
  form.onsubmit = async (ev) => {
    ev.preventDefault();
    const m = pruefe(schritt);
    if (m) { melde(m); return; }
    if (schritt < ER_SCHRITTE.length - 1) { gehe(schritt + 1); return; }
    for (let i = 0; i < ER_SCHRITTE.length - 1; i++) { const x = pruefe(i); if (x) { gehe(i); melde(x); return; } }
    const knopf = form.querySelector('[type=submit]');
    knopf.disabled = true;
    try {
      const zugaenge = ki.zugaenge.map((z) => ({ id: z.id, name: z.name, art: z.art, adresse: z.adresse, schluessel: z.schluessel || '' }));
      const r = await api('/api/ersteinrichtung', { method: 'POST', body: { code, name: name.trim(), werte, ...(d.eingerichtet ? {} : { zugaenge, bereiche: bereicheAusWahl() }), systemschluessel: schluessel } });
      form.hidden = true;
      leiste.hidden = true;
      const box = document.getElementById('ergebnis');
      box.innerHTML = T`<div class="hinweis gut">Gespeichert. Der Dienst startet neu …</div>`;
      window.scrollTo(0, 0);
      await warteAufNeustart();
      // Direkt weiter zum ersten Passkey (danach zeigt die Willkommensseite den Wiederherstellungscode).
      if (r.weiter) { location.replace(r.weiter); return; }
      box.innerHTML = r.weiter
        ? T`<div class="hinweis gut">Eingerichtet. Jetzt unter der öffentlichen Adresse den ersten Passkey anlegen (Link 15 Minuten gültig).</div><div class="leiste"><a class="btn haupt" href="${esc(r.weiter)}" target="_self">Passkey einrichten</a></div>`
        : T`<div class="hinweis gut">Eingerichtet.</div><div class="leiste"><a class="btn haupt" href="${esc(r.adresse)}/" target="_self">Zur Web-App</a></div>`;
    } catch (f) {
      knopf.disabled = false;
      fehler = f.message;
      male();
    }
  };
  aufraeumen = () => { window.removeEventListener('resize', kopfH); };
}

// Ersteinrichtung mit Backup: Datei hochladen, dann die Wiederherstellungscodes der Nutzer (und ggf. den Code des
// Systemschlüssels) eingeben; der Server prüft sie, legt die Schlüssel ab und spielt das Backup ein.
function einrichtungMitBackup(weg, code) {
  weg.innerHTML = T`<p class="leise">Eine unveränderte Backup-Datei (pebble-….tar.gz) dieses Programms. Daten, Einstellungen und Passkeys kommen zurück; mit den Wiederherstellungscodes öffnen sich die Daten der Nutzer gleich mit.</p>
    <form class="einstellungen" id="ebackup" novalidate><label for="ebdatei">Backup-Datei</label>
      <input type="file" id="ebdatei" accept=".tar.gz,.gz,application/gzip" required>
      <p class="leise" id="ebstand" aria-live="polite"></p>
      <div class="leiste"><button class="btn haupt" type="submit" disabled>Hochladen und prüfen</button></div></form><div id="ebcodes"></div>`;
  const form = document.getElementById('ebackup'), feld = document.getElementById('ebdatei'), stand = document.getElementById('ebstand');
  const knopf = form.querySelector('[type=submit]');
  const muster = /^pebble-[0-9A-Za-z.+-]+-(stable|beta|nightly|lokal)-\d{8}-\d{6}-(update|plan|manuell)(-audio)?\.tar\.gz$/;
  feld.onchange = () => {
    const f = feld.files[0];
    const ok = !!f && muster.test(f.name);
    knopf.disabled = !ok;
    stand.textContent = f ? (ok ? tx('{0} · {1}', f.name, groesse(f.size)) : tx('Der Dateiname passt nicht zu einem Backup dieser App.')) : '';
  };
  form.onsubmit = (ev) => {
    ev.preventDefault();
    const f = feld.files[0];
    knopf.disabled = feld.disabled = true;
    const x = new XMLHttpRequest();
    x.open('POST', T`/api/ersteinrichtung/backup?code=${encodeURIComponent(code)}&name=${encodeURIComponent(f.name)}`);
    x.setRequestHeader('X-App', '1');
    x.setRequestHeader('X-Sprache', SPRACHE);
    x.upload.onprogress = (e) => { if (e.lengthComputable) stand.textContent = tx('Lädt hoch … {0} %', Math.round((e.loaded / e.total) * 100)); };
    x.onload = () => {
      let a = null;
      try { a = JSON.parse(x.responseText); } catch { /* leer */ }
      if (x.status !== 200) { stand.textContent = a?.fehler || tx('Fehler {0}', x.status); knopf.disabled = feld.disabled = false; return; }
      form.hidden = true;
      zeigeCodes(a);
    };
    x.onerror = () => { stand.textContent = tx('Upload abgebrochen'); knopf.disabled = feld.disabled = false; };
    x.send(f);
  };
  const zeigeCodes = (b) => {
    const box = document.getElementById('ebcodes');
    const sysNutzer = b.system?.art === 'nutzer' ? b.nutzer.find((u) => u.id === b.system.uid) : null;
    box.innerHTML = T`<div class="hinweis gut">Backup geprüft: ${esc(b.name)}</div>
      <form class="einstellungen" id="ebrest" novalidate><h2>Wiederherstellungscodes</h2>
      <p class="leise">Je Nutzer sein Code. Wer keinen eingibt, öffnet seine Daten später nach der Anmeldung damit.${sysNutzer ? T` Der Code von ${esc(sysNutzer.name)} stellt auch den Systemschlüssel wieder her (API-Schlüssel, Benachrichtigungen).` : ''}</p>
      ${b.nutzer.map((u) => T`<label for="ebc-${u.id}">${esc(u.name || tx('Nutzer {0}', u.id))} <span class="badge neutral">${esc(tx(ROLLE[u.rolle] || u.rolle))}</span></label>
        <input id="ebc-${u.id}" data-uid="${u.id}" autocomplete="off" placeholder="XXXXX-XXXXX-XXXXX-XXXXX-XXXXXX">`).join('')}
      ${b.system?.art === 'code' ? T`<label for="ebsys">Code des Systemschlüssels</label><input id="ebsys" autocomplete="off" placeholder="XXXXX-XXXXX-XXXXX-XXXXX-XXXXXX">` : ''}
      <h2>Adresse</h2><label for="ebadr">Öffentliche Adresse dieses Servers</label><input id="ebadr" inputmode="url" placeholder="https://name.domain" value="${location.protocol === 'https:' ? esc(location.origin) : ''}">
      <p class="leise hilfe">Leer = die Adresse aus dem Backup. Bei einer neuen Adresse legen alle Nutzer neue Passkeys an: der Admin mit seinem Wiederherstellungscode auf der Anmeldeseite, die anderen lädt er danach neu ein.</p>
      <div class="leiste"><button class="btn haupt" type="submit">Backup einspielen</button></div></form><div id="ebende"></div>`;
    const rf = document.getElementById('ebrest');
    rf.onsubmit = async (ev) => {
      ev.preventDefault();
      const codes = {};
      rf.querySelectorAll('[data-uid]').forEach((i) => { if (i.value.trim()) codes[i.dataset.uid] = i.value.trim(); });
      const k = rf.querySelector('[type=submit]');
      k.disabled = true;
      try {
        const r = await api('/api/ersteinrichtung/backup/wiederherstellen', { method: 'POST', body: { code, name: b.name, codes, systemcode: document.getElementById('ebsys')?.value.trim() || '', adresse: document.getElementById('ebadr').value.trim() } });
        rf.hidden = true;
        const ende = document.getElementById('ebende');
        ende.innerHTML = T`<div class="hinweis gut">Backup wird eingespielt${r.system ? tx(' (mit Systemschlüssel)') : ''}. Der Dienst startet neu …</div>`;
        const da = await warteAufNeustart();
        ende.innerHTML = da ? T`<div class="hinweis gut">Fertig. Unter der öffentlichen Adresse anmelden – bei gleicher Adresse mit dem Passkey von vorher, bei neuer als Admin mit dem Wiederherstellungscode.</div><div class="leiste"><a class="btn haupt" href="/anmelden" target="_self">Zur Anmeldung</a></div>`
          : T`<div class="hinweis warn">Das Einspielen dauert länger (z. B. weil die Version aus dem Backup geladen wird). In einer Minute neu laden.</div><div class="leiste"><a class="btn haupt" href="/anmelden" target="_self">Zur Anmeldung</a></div>`;
      } catch (f) {
        k.disabled = false;
        melde(f.message);
      }
    };
  };
}

// Reiter der Statusseite (Buttongruppe): Übersicht, Pebble-App, Joplin (nur wenn aktiv), Konfiguration, KI (Admins), Theme.
let joplinAktiv = (() => { try { return localStorage.getItem('joplin-aktiv') === '1'; } catch { return false; } })();
const statusReiter = (aktiv) => T`<nav class="btn-group unter reiter4" aria-label="Status">${[['status', '/status', tx('Übersicht')],
  ...(joplinAktiv ? [['joplin', '/status/joplin', tx('Joplin')]] : []), ['einstellungen', '/status/einstellungen', tx('Konfiguration')], ...(istAdmin() ? [['ki', '/status/ki', tx('KI')]] : [])]
  .map(([n, href, text]) => T`<a class="btn${n === aktiv ? ' aktiv' : ''}" href="${href}"${n === aktiv ? ' aria-current="page"' : ''}>${text}</a>`).join('')}</nav>`;

// Reiter der Benutzereinstellungen (Buttongruppe wie unter Status).
const kontoReiter = (aktiv) => T`<nav class="btn-group unter reiter4" aria-label="${esc(tx('Benutzer'))}">${[['konto', '/konto', tx('Übersicht')], ['pebble', '/pebble', tx('Pebble-App')],
  ['benachrichtigungen', '/konto/benachrichtigungen', tx('Push')], ['kalender', '/konto/kalender', tx('Kalender')], ['darstellung', '/konto/theme', tx('Theme')]]
  .map(([n, href, text]) => T`<a class="btn${n === aktiv ? ' aktiv' : ''}" href="${href}"${n === aktiv ? ' aria-current="page"' : ''}>${text}</a>`).join('')}</nav>`;

// Konfiguration: je Wert „?“ (Erklärung darunter ein-/ausklappen) und ✎ (eigene Bearbeitungsseite für diesen Wert).
const STIFT = T('<svg viewBox="0 0 24 24" aria-hidden="true"><path d="M4 20h4L19 9l-4-4L4 16zM14 6l4 4" fill="none" stroke="currentColor" stroke-width="2" stroke-linejoin="round" stroke-linecap="round"/></svg>');
// ladeEinstellungen: eigene Werte, bei Admins dazu die des Systems (je Feld „persoenlich“).
async function ladeEinstellungen() {
  const [eigen, system] = await Promise.all([api('/api/einstellungen/persoenlich'), istAdmin() ? api('/api/einstellungen') : { felder: [] }]);
  return { felder: [...eigen.felder, ...system.felder], whisper: system.whisper };
}
// speichereEinstellungen: persönliche Werte gelten sofort, Werte des Systems nach einem Neustart des Dienstes.
async function speichereEinstellungen(werte, persoenlich) {
  if (persoenlich) {
    await api('/api/einstellungen/persoenlich', { method: 'PUT', body: { werte } });
    melde(tx('Gespeichert'));
    await new Promise((r) => setTimeout(r, 1200)); // Daten des Nutzers werden mit den neuen Werten neu geöffnet
    return;
  }
  await api('/api/einstellungen', { method: 'PUT', body: { werte } });
  melde(tx('Gespeichert – Dienst startet neu'));
  await warteAufNeustart();
}

async function zeigeEinstellungen() {
  seite.innerHTML = T`${statusReiter('einstellungen')}<p class="leer">Lädt …</p>`;
  const d = await ladeEinstellungen();
  if (aktuell?.name !== 'einstellungen') return;
  // Joplin-Zugang wird unter Status → Joplin eingerichtet; hier steht nur der Schalter.
  d.felder = d.felder.filter((f) => (f.gruppe !== 'Joplin' || f.key === 'JOPLIN_ENABLED') && f.gruppe !== 'Benachrichtigungen' && f.gruppe !== 'Statistik' && !Object.values(KI_GRUPPEN).includes(f.gruppe));
  const gruppen = [...new Set(d.felder.map((f) => f.gruppe))];
  const wert = (f) => (f.art === 'multi' ? esc(f.wert === 'keine' ? tx('keine') : (f.wert || '').split(',').map((c) => wahlText(f, c)).join(', ')) : f.art === 'secret' ? (f.gesperrt ? tx('aus der Umgebung') : f.gesetzt ? tx('gesetzt') : T('<span class="leise">leer</span>')) : f.wert ? esc(f.key === 'WHISPER_MODEL' ? d.whisper?.modelle.find((m) => m.id === f.wert)?.name || f.wert : f.art === 'choice' ? wahlText(f, f.wert) : f.wert) : T('<span class="leise">leer</span>'));
  const zeile = (f) => T`<div class="konfzeile">
      <button class="konfhilfe" data-hilfe="${f.key}" aria-expanded="false" aria-controls="hilfe-${f.key}" aria-label="${esc(tx('Erklärung zu {0}', tx(f.name)))}">?</button>
      <span class="konfname">${esc(tx(f.name))}</span>
      ${f.gesperrt ? '<span></span>' : T`<a class="konfedit" href="/status/einstellungen/${f.key}" aria-label="${esc(tx('{0} bearbeiten', tx(f.name)))}">${STIFT}</a>`}
      <span class="konfwert">${wert(f)}</span>
      <p class="konfinfo" id="hilfe-${f.key}" hidden>${esc(f.hilfe ? tx(f.hilfe) : tx('Keine Erklärung hinterlegt.'))}${f.vorgabe ? T` <span class="leise">Vorgabe: ${esc(f.vorgabe)}</span>` : ''}</p></div>`;
  const teil = (persoenlich) => gruppen.filter((g) => d.felder.some((f) => f.gruppe === g && !!f.persoenlich === persoenlich))
    .map((g) => T`<h3>${esc(tx(g))}</h3><div class="karte konfliste">${d.felder.filter((f) => f.gruppe === g && !!f.persoenlich === persoenlich).map(zeile).join('')}</div>`).join('');
  seite.innerHTML = T`${statusReiter('einstellungen')}<h1>Konfiguration</h1>
    <p class="leise">? erklärt einen Wert, ✎ ändert ihn.</p>
    <h2>Persönlich</h2><p class="leise">Gilt nur für dich, sofort nach dem Speichern.</p>${teil(true)}
    ${istAdmin() ? T`<h2>System</h2><p class="leise">Gilt für alle Nutzer; nach dem Speichern startet der Dienst kurz neu. Anbieter und Modelle der KI stehen unter <a href="/status/ki">KI</a>.</p>${teil(false)}` : ''}`;
  seite.onclick = (ev) => {
    const b = ev.target.closest('[data-hilfe]');
    if (!b) return;
    const info = document.getElementById(T`hilfe-${b.dataset.hilfe}`);
    info.hidden = !info.hidden;
    b.setAttribute('aria-expanded', String(!info.hidden));
  };
  aufraeumen = () => { seite.onclick = null; };
}

// ---------- KI: Zugänge (beliebig viele, je mit eigenem Namen) und je Bereich die Reihenfolge der Einträge (Admins) ----------

// Unterseiten: Bereich der Daten, Gruppe der übrigen Einstellungen, Titel, kurze Erklärung.
const KI_SEITEN = {
  transkription: { bereich: 'transkription', gruppe: 'Transkription', titel: 'Transkription', text: 'Wandelt Aufnahmen in Text um.' },
  kurztitel: { bereich: 'kurztitel', titel: 'Kurztitel', text: 'Kurztitel, Zuordnung zu Planungen, Termine, Beschreibungen der Listen. Ein kleines Modell reicht.' },
  auswertung: { bereich: 'auswertung', titel: 'Auswertung & Sortierung', text: 'Notizen auswerten, in Gruppen und Listen einsortieren, Lernlauf. Braucht strukturierte Ausgabe (JSON-Schema).' },
  webrecherche: { bereich: 'recherche', gruppe: 'Webrecherche', titel: 'Webrecherche', text: 'Planungen mit Antwort und Trailer-Suche.' },
  bilder: { bereich: 'bilder', titel: 'Bilder', text: 'Erklärende Bilder zu Notizen und Planungen. Ohne Eintrag erzeugt die KI keine Bilder.' },
  entscheidung: { bereich: 'entscheidung', gruppe: 'Entscheidung', titel: 'Entscheidung', text: 'Wählt schnell aus fester Auswahl: Termin oder Notiz, Inhalt, Liste einer Notiz oder Planung, Fortsetzung einer Planung. Nur Entscheidungsmodelle; unsicher oder ohne Eintrag entscheidet die KI wie bisher.' },
};
const KI_GRUPPEN = { zugaenge: 'KI-Zugänge', transkription: 'Transkription', webrecherche: 'Webrecherche', entscheidung: 'Entscheidung' };
const CLOUDFELDER = [];
// Anbieter-Arten kommen vom Server (config.ProviderTypes): Name, Adresse, Schlüssel, Bereiche, Websuche, Modellauswahl.
const kiArt = (d, z) => d.arten?.find((a) => a.id === z?.art);
const kiErlaubt = (d, bereich) => (d.arten || []).filter((a) => a.bereiche?.includes(bereich)).map((a) => a.id);
const KI_SONDER = { whisper: 'Whisper (dieser Server)', pebble: 'Text der Pebble-App' };
const kiCloud = (d, z) => !!kiArt(d, z)?.cloud;
const kiModelle = (id) => { try { return JSON.parse(localStorage.getItem(T`ki-modelle-${id}`)); } catch { return null; } };
const kiNeueID = () => Math.random().toString(36).slice(2, 10);
const kiZahl = (v) => { const t = String(v ?? '').trim().replace(',', '.'); return t === '' ? null : Number(t); };
const kiUSD = (v) => `${Number(v || 0).toLocaleString(LOCALE, { maximumFractionDigits: 4 })} $`;

async function ladeKI() { return api('/api/ki'); }

async function zeigeKI() {
  seite.innerHTML = T`${statusReiter('ki')}<p class="leer">Lädt …</p>`;
  if (!istAdmin()) { seite.innerHTML = T`${statusReiter('ki')}<div class="hinweis schlecht">Nur für Admins.</div>`; return; }
  const d = await ladeKI();
  if (aktuell?.name !== 'ki') return;
  // Je Bereich eine Karte: Titel, der erste aktive Eintrag, was der Bereich macht; rechts ein großer Pfeil.
  const karte = (seiteName, s) => {
    const liste = (d.bereiche[s.bereich] || []).filter((e) => !e.inaktiv);
    const erster = liste[0];
    const z = erster && d.zugaenge.find((x) => x.id === erster.zugang);
    const name = erster && (z ? z.name : tx(KI_SONDER[erster.zugang] || erster.zugang));
    return T`<a class="karte kibereich" href="/status/ki/${seiteName}"><span class="kibereichtext"><strong>${esc(tx(s.titel))}</strong>
      ${erster ? T`<span><span class="kimarke">${esc(name)}</span> ${esc(erster.modell || '')}${liste.length > 1 ? T` <span class="leise">${tx('+{0} weitere', liste.length - 1)}</span>` : ''}</span>`
        : T`<span class="${['bilder', 'entscheidung'].includes(s.bereich) ? 'leise' : 'schlecht'}">${['bilder', 'entscheidung'].includes(s.bereich) ? tx('Aus') : tx('Kein Anbieter')}</span>`}
      <span class="leise">${esc(tx(s.text))}</span></span><span class="kipfeil" aria-hidden="true">›</span></a>`;
  };
  seite.innerHTML = T`${statusReiter('ki')}<h1>KI</h1>
    <div class="leiste"><a class="btn" href="/status/ki/zugaenge">${tx('Anbieter')} (${d.zugaenge.length})</a></div>
    ${Object.entries(KI_SEITEN).map(([n, s]) => karte(n, s)).join('')}`;
}

// Zugänge: je Zugang eine Zeile; Tippen öffnet ein Fenster zum Bearbeiten, das gleich speichert (Dienst startet neu).
// „Prüfen“ fragt einen gespeicherten Zugang ab und merkt sich seine Modelle im Browser für die Auswahl in den Bereichen.
async function zeigeKIZugaenge(d) {
  const f = Object.fromEntries(d.felder.map((x) => [x.key, x]));
  const host = (u) => { try { return new URL(u).host; } catch { return u || ''; } };
  const zeile = (z) => {
    const m = kiModelle(z.id);
    const art = kiArt(d, z);
    const info = [tx(art?.name || z.art), !art?.adresse_fest && host(z.adresse), m && tx('{0} Modelle', m.modelle.length)].filter(Boolean).join(' · ');
    return T`<button type="button" class="kizeile" data-zugang="${esc(z.id)}"><span class="kizeilename">${esc(z.name)}</span><span class="leise">${esc(info)}</span><span class="kizeilestift" aria-hidden="true">${STIFT}</span></button>`;
  };
  seite.innerHTML = T`${statusReiter('ki')}${krumen([[tx('KI'), '/status/ki'], [tx('Anbieter')]])}<h1>Anbieter</h1>
    <p class="leise">Beliebig viele Anbieter, auch mehrere gleicher Art; jeder mit eigenem Namen. Tippen öffnet den Anbieter.</p>
    ${d.zugaenge.some((z) => kiArt(d, z)?.oauth) ? '' : T`<div class="leiste"><button type="button" class="btn haupt" data-or-verbinden="1">${tx('Mit OpenRouter verbinden')}</button></div>
      <p class="leise">${tx('Empfohlen: Mit OpenRouter funktionieren alle Funktionen – Auswertung, Websuche, Bilder und Transkription.')}</p>`}
    ${d.zugaenge.length ? T`<div class="karte kizeilen">${d.zugaenge.map(zeile).join('')}</div>` : T`<p class="leise">Noch kein Anbieter.</p>`}
    <div class="leiste"><button type="button" class="btn" data-neu="1">Anbieter hinzufügen</button></div>
    ${f.USER_DAILY_LIMIT_USD ? T`<div class="karte konfliste"><div class="konfzeile ohnehilfe"><span class="konfname">${esc(tx(f.USER_DAILY_LIMIT_USD.name))}</span>
      <a class="konfedit" href="/status/einstellungen/USER_DAILY_LIMIT_USD?zurueck=/status/ki/zugaenge" aria-label="${esc(tx('{0} bearbeiten', tx(f.USER_DAILY_LIMIT_USD.name)))}">${STIFT}</a>
      <span class="konfwert">${esc(f.USER_DAILY_LIMIT_USD.wert)} $</span></div></div>` : ''}
    <div class="leiste"><a class="btn" href="/status/ki">Zurück</a></div>`;
  seite.onclick = async (ev) => {
    const v = ev.target.closest('[data-or-verbinden]');
    if (v) {
      v.disabled = true;
      try { if (await kiOpenRouterVerbinden('')) zeigeKIBereich('zugaenge'); else v.disabled = false; } catch (f) { melde(f.message); v.disabled = false; }
      return;
    }
    const b = ev.target.closest('[data-zugang], [data-neu]');
    if (!b) return;
    kiZugangFenster(d, b.dataset.zugang ? d.zugaenge.find((z) => z.id === b.dataset.zugang) : null);
  };
  aufraeumen = () => { seite.onclick = null; };
}

// Ein Fenster öffnet ohne fokussiertes Eingabefeld (sonst gehen Tastatur und Modellauswahl auf, ohne dass jemand
// ins Feld getippt hat). Manche Browser setzen den Fokus nach showModal noch einmal: darum auch in den nächsten Frames.
function kiFensterRuhig(dlg) {
  const ruhe = () => { const a = document.activeElement; if (a && a !== dlg && dlg.contains(a) && a.matches('input, select, textarea')) { a.blur(); dlg.focus({ preventScroll: true }); } };
  dlg.focus({ preventScroll: true });
  ruhe();
  requestAnimationFrame(() => { ruhe(); requestAnimationFrame(ruhe); });
}

// Fenster eines Zugangs: Name, Art, Adresse, Schlüssel; Speichern und Entfernen schreiben sofort.
function kiZugangFenster(d, alt) {
  const z = alt ? { ...alt } : { id: kiNeueID(), name: '', art: 'openai', adresse: '' };
  const dlg = document.createElement('dialog');
  dlg.className = 'tagdialog';
  dlg.tabIndex = -1; // Fokus aufs Fenster, nicht aufs erste Feld (sonst springt die Tastatur auf)
  dlg.autofocus = true;
  const male = () => {
    const m = kiModelle(z.id);
    const art = kiArt(d, z) || {};
    const schluessel = art.schluessel && art.schluessel !== 'kein';
    dlg.innerHTML = T`<form method="dialog" class="einstellungen" novalidate><div class="kikopf"><h2>${esc(alt ? alt.name : tx('Neuer Anbieter'))}</h2><button type="button" class="kizu" data-zu="1" aria-label="${esc(tx('Schließen'))}">×</button></div>
      <label for="kz-name">Name *</label><input type="text" id="kz-name" data-k="name" value="${esc(z.name)}" autocomplete="off">
      <label for="kz-art">Art</label><select id="kz-art" data-k="art">${(d.arten || []).map((a) => T`<option value="${esc(a.id)}"${a.id === z.art ? ' selected' : ''}>${esc(tx(a.name))}</option>`).join('')}</select>
      ${art.text ? T`<p class="leise hilfe">${esc(tx(art.text))}</p>` : ''}
      ${art.adresse_fest ? '' : T`<label for="kz-adresse">Adresse${art.adresse ? '' : ' *'}</label><input id="kz-adresse" type="url" data-k="adresse" value="${esc(z.adresse || '')}" placeholder="${esc(art.adresse || (art.api === 'searxng' ? 'http://<server>:8080' : 'https://…/v1'))}" autocomplete="off" spellcheck="false">`}
      ${art.oauth ? T`<div class="leiste"><button type="button" class="btn haupt" data-verbinden="1">${alt ? tx('Neu mit OpenRouter verbinden') : tx('Mit OpenRouter verbinden')}</button></div>
        <p class="leise hilfe">${tx('Anmelden bei OpenRouter und Zugang freigeben – ohne API-Schlüssel. Oder unten einen Schlüssel einfügen.')}</p>` : ''}
      ${schluessel ? T`<label for="kz-key">Schlüssel${art.schluessel === 'pflicht' && !art.oauth ? ' *' : ''}</label><input id="kz-key" type="password" data-k="schluessel" value="${esc(z.schluessel || '')}" placeholder="${alt?.schluessel_gesetzt && alt.art === z.art ? esc(tx('gesetzt – leer lassen zum Behalten')) : ''}" autocomplete="off" spellcheck="false">` : ''}
      ${alt ? T`<div class="leiste kileiste"><button type="button" class="btn klein" data-pruefen="1">Prüfen</button></div>
        <p class="leise hilfe" id="kz-pruef">${m ? esc(tx('{0} Modelle, geprüft {1}', m.modelle.length, new Date(m.zeit).toLocaleString(LOCALE))) : ''}</p>` : ''}
      <p class="hinweis schlecht" id="kz-fehler" hidden></p>
      <div class="leiste"><button type="submit" class="btn haupt" value="speichern">Speichern</button><button type="button" class="btn" data-zu="1">Abbrechen</button></div>
      ${alt ? T`<div class="leiste"><button type="button" class="btn gefahr" data-weg="1">Anbieter entfernen</button></div>` : ''}</form>`;
  };
  male();
  const schreibe = async (zugaenge) => {
    const fehler = dlg.querySelector('#kz-fehler');
    fehler.hidden = true;
    dlg.querySelectorAll('button').forEach((b) => { b.disabled = true; });
    try {
      await kiNachSpeichern(await api('/api/ki', { method: 'PUT', body: { zugaenge, bereiche: d.bereiche, werte: {} } }));
      dlg.close();
      if (aktuell?.name === 'kiBereich') zeigeKIBereich('zugaenge');
    } catch (f) {
      fehler.textContent = f.message;
      fehler.hidden = false;
      dlg.querySelectorAll('button').forEach((b) => { b.disabled = false; });
    }
  };
  const ohneSchluessel = (x) => ({ id: x.id, name: x.name, art: x.art, adresse: x.adresse });
  dlg.addEventListener('input', (ev) => { if (ev.target.dataset.k) z[ev.target.dataset.k] = ev.target.value; });
  dlg.addEventListener('change', (ev) => { if (ev.target.dataset.k === 'art') { z.art = ev.target.value; male(); } });
  dlg.addEventListener('submit', (ev) => {
    ev.preventDefault();
    const rest = d.zugaenge.map(ohneSchluessel);
    const i = rest.findIndex((x) => x.id === z.id);
    if (i >= 0) rest[i] = z; else rest.push(z);
    schreibe(rest);
  });
  dlg.addEventListener('click', async (ev) => {
    const b = ev.target.closest('button[type=button]');
    if (!b) return;
    if (b.dataset.zu) dlg.close();
    else if (b.dataset.verbinden) {
      const fehler = dlg.querySelector('#kz-fehler');
      fehler.hidden = true;
      b.disabled = true;
      try {
        if (!(await kiOpenRouterVerbinden(alt?.id || ''))) { b.disabled = false; return; }
        dlg.close();
        if (aktuell?.name === 'kiBereich') zeigeKIBereich('zugaenge');
      } catch (f) {
        fehler.textContent = f.message;
        fehler.hidden = false;
        b.disabled = false;
      }
    } else if (b.dataset.weg) {
      if (confirm(tx('Anbieter {0} entfernen? Seine Einträge in den Bereichen fallen weg.', alt.name))) {
        d.bereiche = Object.fromEntries(Object.entries(d.bereiche).map(([k, l]) => [k, l.filter((e) => e.zugang !== alt.id).map((e) => ({ ...e, websuche: (e.websuche || []).filter((x) => x !== alt.id) }))]));
        schreibe(d.zugaenge.filter((x) => x.id !== alt.id).map(ohneSchluessel));
      }
    } else if (b.dataset.pruefen) {
      const info = dlg.querySelector('#kz-pruef');
      b.disabled = true;
      info.textContent = tx('Prüft …');
      try {
        const r = await api(T`/api/ki/pruefen/${alt.id}`, { method: 'POST' });
        if (r.ok && r.modelle) localStorage.setItem(T`ki-modelle-${alt.id}`, JSON.stringify({ modelle: r.modelle, zeit: Date.now() }));
        info.textContent = r.ok ? (r.modelle ? tx('Erreichbar, {0} Modelle', r.modelle.length) : tx('Erreichbar')) : T`${tx('Fehler')}: ${r.fehler}`;
        info.className = T`leise hilfe ${r.ok ? 'gut' : 'schlecht'}`;
      } catch (f) { info.textContent = f.message; }
      b.disabled = false;
    }
  });
  dlg.onclose = () => dlg.remove();
  document.body.append(dlg);
  dlg.showModal();
  kiFensterRuhig(dlg);
}

// Felder eines Eintrags auf seiner Seite: Zugang, Modell (Vorschläge aus dem letzten Prüfen), bei Cloud-Anbietern Preise
// und Monatslimit, bei Auswertung und Recherche die Websuche.
function kiEintragFelder(d, bereich, e) {
  const id = 'ke';
  const z = d.zugaenge.find((x) => x.id === e.zugang);
  const zahl = (k, name) => T`<label for="${id}-${k}">${esc(tx(name))} *</label><input type="text" id="${id}-${k}" data-k="${k}" inputmode="decimal" value="${e[k] ?? ''}" autocomplete="off">`;
  const art = kiArt(d, z);
  const cloud = kiCloud(d, z);
  const preise = cloud && !art?.preise_optional; // ohne: Kosten meldet der Anbieter oder sie bleiben offen
  const suche = bereich === 'recherche' || bereich === 'auswertung';
  const searx = d.zugaenge.filter((x) => kiArt(d, x)?.api === 'searxng');
  return T`    ${art?.modelle?.[bereich] ? T`<label for="${id}-modell">Modell</label><select id="${id}-modell" data-k="modell">${art.modelle[bereich].map((m) => T`<option value="${esc(m.id)}"${m.id === e.modell ? ' selected' : ''}>${esc(tx(m.name))}${kiPreisText(m) ? ` · ${esc(kiPreisText(m))}` : ''}</option>`).join('')}</select>
      ${art.preise_optional ? '' : T`<p class="leise hilfe">${tx('Ungefähre Preise laut Anbieter; beim Wechsel übernommen, unten anpassbar.')}</p>`}`
    : z ? T`${art?.oauth === 'openrouter' ? kiAuswahl(d, bereich, e, suche) : ''}<label for="${id}-modell">${tx('Modell *')}</label><input type="text" id="${id}-modell" data-k="modell" value="${esc(e.modell || '')}" list="${id}-liste" autocomplete="off" spellcheck="false">
      <datalist id="${id}-liste">${(kiModelle(z.id)?.modelle || []).map((m) => T`<option value="${esc(m)}">`).join('')}</datalist>
      ${kiModelle(z.id) ? '' : T`<p class="leise hilfe">${esc(tx('Keine Modelle gemerkt'))} – <a href="/status/ki/zugaenge">${esc(tx('unter Zugänge prüfen'))}</a></p>`}` : ''}
    ${kiAufwandFeld(d, bereich, e, z)}
    ${e.zugang === 'pebble' ? T`<p class="leise hilfe">${tx('Nur als erster Eintrag; fehlt der Text, übernehmen die folgenden.')}</p>` : ''}
    ${preise && bereich === 'transkription' ? zahl('preis_minute', 'Preis (USD je Minute)') : ''}
    ${preise && bereich === 'bilder' ? zahl('preis_bild', 'Preis (USD je Bild)') : ''}
    ${preise && bereich !== 'transkription' && bereich !== 'bilder' ? T`${zahl('preis_ein', 'Preis Eingabe (USD je 1 Mio. Tokens)')}${zahl('preis_aus', 'Preis Ausgabe (USD je 1 Mio. Tokens)')}` : ''}
    ${suche && z && searx.length ? T`<p class="feldname">${art?.suche ? tx('Zusätzliche Websuche') : tx('Websuche')}</p><div class="mehrfach">
      ${searx.map((x) => T`<label class="schalter"><input type="checkbox" data-suche="${esc(x.id)}"${e.websuche?.includes(x.id) ? ' checked' : ''}> ${esc(x.name)}</label>`).join('')}</div>` : ''}
    ${suche && z && !art?.suche && !searx.length ? T`<p class="leise hilfe">${tx('Ein lokales Modell sucht nicht selbst im Web; dafür einen SearXNG-Anbieter anlegen.')}</p>` : ''}
    ${suche && preise && art?.suche ? zahl('preis_suche', 'Preis Websuche (USD je Aufruf)') : ''}
    ${cloud && (preise || art?.meldet_kosten) ? T`<label for="${id}-limit">${tx('Kostenlimit im Monat (USD)')}</label><input type="text" id="${id}-limit" data-k="limit_monat" inputmode="decimal" value="${e.limit_monat || ''}" placeholder="${esc(tx('ohne'))}" autocomplete="off">
      <p class="leise hilfe">${tx('Diesen Monat: {0}. Ist das Limit erreicht, übernimmt der nächste Eintrag.', kiUSD(d.verbrauch[e.id]))}</p>` : ''}`;
}

// Nachdenken je Eintrag (nur Textbereiche): Vorgabe, aus, wenig, mittel, viel. Bei OpenRouter nur für Modelle, die laut
// Katalog nachdenken können; ein Modell außerhalb des Katalogs zeigt das Feld (unbekannt).
const KI_AUFWAND = [['', 'Vorgabe'], ['none', 'Aus'], ['low', 'Wenig'], ['medium', 'Mittel'], ['high', 'Viel']];
function kiAufwandFeld(d, bereich, e, z) {
  // Nur wo der Dienst es mitschickt: Responses-API (OpenAI, Grok, Muse), Gemini, OpenRouter.
  const art = kiArt(d, z);
  if (!z || bereich === 'transkription' || bereich === 'bilder' || !(art?.api === 'responses' || art?.id === 'gemini' || art?.oauth === 'openrouter')) return '';
  const m = art.oauth === 'openrouter' ? d.openrouter_katalog?.bereiche?.[bereich]?.find((x) => x.id === e.modell) : null;
  if (m && !m.denkt) return T`<p class="leise hilfe">${tx('Dieses Modell denkt nicht nach.')}</p>`;
  return T`<label for="ke-aufwand">${tx('Nachdenken')}</label><select id="ke-aufwand" data-k="aufwand">${KI_AUFWAND.map(([v, n]) => T`<option value="${v}"${(e.aufwand || '') === v ? ' selected' : ''}>${esc(tx(n))}</option>`).join('')}</select>
    <p class="leise hilfe">${tx('Mehr Nachdenken: genauer, aber langsamer und teurer. Vorgabe = Einstellung des Dienstes bzw. des Modells.')}</p>`;
}

// Alle Einträge eines Bereichs: gespeicherte und je passendem Zugang, der noch fehlt, ein inaktiver Platzhalter –
// jeder Zugang steht genau einmal in der Liste.
function kiAlleEintraege(d, bereich) {
  const da = (d.bereiche[bereich] || []).filter((e) => d.zugaenge.some((z) => z.id === e.zugang) || KI_SONDER[e.zugang]);
  const moeglich = [...d.zugaenge.filter((z) => kiErlaubt(d, bereich).includes(z.art)).map((z) => z.id), ...(bereich === 'transkription' ? Object.keys(KI_SONDER) : [])];
  return [...da, ...moeglich.filter((id) => !da.some((e) => e.zugang === id)).map((id) => ({ id: kiNeueID(), zugang: id, inaktiv: true, neu: true, websuche: [] }))];
}
// Zu speichern: alles außer Platzhaltern, die inaktiv geblieben sind.
const kiZuSpeichern = (liste) => liste.filter((e) => !(e.neu && e.inaktiv)).map(({ neu, ...e }) => e);

// Modellwahl bei OpenRouter: alle passenden Modelle des Bereichs aus dem Katalog, den der Server bei OpenRouter abruft
// und ablegt (Preise, Kontext, Datenschutz/ZDR). Ohne Suchtext 5 Modelle über die Preisspanne, mit Suchtext bis zu 10
// Treffer – gesucht wird nur in den geladenen Daten. Der Schalter Datenschutz zeigt nur Modelle ohne Datenspeicherung
// und erzwingt das bei den Anfragen. Die Stufe ergibt sich aus dem Preisrang im Bereich.
const KI_STUFEN = ['Günstig', 'Mittel', 'Teuer'];
// Schild für „ohne Datenspeicherung“.
const KI_SCHILD = T('<svg viewBox="0 0 24 24" aria-hidden="true"><path d="M12 3l7 3v5c0 4.5-3 8.3-7 10-4-1.7-7-5.5-7-10V6z" fill="none" stroke="currentColor" stroke-width="2" stroke-linejoin="round"/><path d="M8.5 12l2.5 2.5 4.5-5" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/></svg>');
// Bis zu n Modelle gleichmäßig über die Preisspanne (wie orSpread im Server).
function kiStreuen(liste, n) {
  if (liste.length <= n) return liste;
  return Array.from({ length: n }, (_, i) => liste[Math.floor((i * (liste.length - 1) + (n - 1) / 2) / (n - 1))]);
}
// Wie kiStreuen über die ganze Liste, aber stabil beim Filtern: passt ein gestreutes Modell nicht (z. B. speichert
// Daten), rückt das nächstgelegene passende nach; schon gezeigte passende bleiben. Das gewählte Modell ist immer dabei
// (es ersetzt das nach Preis nächste).
function kiStreuenStabil(alle, passt, n, gewaehlt) {
  const frei = alle.map((m, k) => k).filter((k) => passt(alle[k]));
  if (frei.length <= n) return frei.map((k) => alle[k]);
  const plaetze = kiStreuen(alle.map((m, k) => k), n);
  const genommen = new Set(plaetze.filter((k) => passt(alle[k])));
  const out = plaetze.map((k) => {
    if (passt(alle[k])) return k;
    const naechst = frei.filter((x) => !genommen.has(x)).sort((a, b) => Math.abs(a - k) - Math.abs(b - k))[0];
    genommen.add(naechst);
    return naechst;
  });
  const g = alle.findIndex((m) => m.id === gewaehlt);
  if (g >= 0 && passt(alle[g]) && !out.includes(g)) {
    const i = out.reduce((b, k, j) => (Math.abs(k - g) < Math.abs(out[b] - g) ? j : b), 0);
    out[i] = g;
  }
  return [...new Set(out)].sort((a, b) => a - b).map((k) => alle[k]);
}
// Modelle des Bereichs zur Auswahl: gibt es Bewertungen für den Bereich, nur die bewerteten Modelle (das gewählte
// bleibt dabei, auch ohne Bewertung); sonst der ganze Katalog.
function kiKatalog(d, bereich, gewaehlt) {
  const alle = d.openrouter_katalog?.bereiche?.[bereich] || [];
  if (!d.modell_analyse || !KI_BEWERTUNG[bereich]) return alle;
  const mit = alle.filter((m) => kiWerte(d, bereich, m.id) || m.id === gewaehlt);
  return mit.some((m) => kiWerte(d, bereich, m.id)) ? mit : alle;
}
const kiNurBewertet = (d, bereich) => kiKatalog(d, bereich).length < (d.openrouter_katalog?.bereiche?.[bereich] || []).length;
// Vorgabe beim Öffnen ohne Modell: das mittlere der gestreuten Modelle ('' = kein Katalog).
function kiVorgabe(d, bereich) {
  const s = kiStreuen(kiKatalog(d, bereich), 5);
  return s.length ? s[Math.floor(s.length / 2)].id : '';
}
function kiAuswahl(d, bereich, e, suche) {
  return T`<fieldset class="kiauswahl"><legend class="feldname">${tx('Modellwahl')}</legend>
    <label class="schalter"><input type="checkbox" data-datenschutz="1"${e.datenschutz ? ' checked' : ''}> ${tx('Datenschutz: nur Anbieter ohne Datenspeicherung')}</label>
    ${d.openrouter_katalog ? T`<div class="kisuchzeile"><input type="search" data-ki-suche="1" value="${esc(e.suche || '')}" placeholder="${esc(tx('Modell suchen …'))}" aria-label="${esc(tx('Modell suchen'))}" autocomplete="off" spellcheck="false">
      ${kiFilterKennzahlen(d, bereich).length ? T`<button type="button" class="btn kifilterknopf${kiFilterAktiv(e) ? ' aktiv' : ''}" data-ki-filter="1" aria-expanded="${e.filterOffen ? 'true' : 'false'}" aria-controls="ke-filter" aria-label="${esc(tx('Filter'))}" title="${esc(tx('Filter'))}">${KI_FILTER_ICON}${kiFilterAktiv(e) ? T`<span class="kifilterzahl">${kiFilterAktiv(e)}</span>` : ''}</button>` : ''}</div>
      ${e.filterOffen ? kiFilterFeld(d, bereich, e) : ''}` : ''}
    <div class="kiauswahlliste">${kiAuswahlListe(d, bereich, e)}</div></fieldset>`;
}
// Filter der Modellwahl: je Kennzahl des Bereichs ein Schieber „mindestens n von 5 Punkten“ (0 = alle); Modelle ohne
// gemessenen Wert fallen bei gesetztem Schieber heraus. Nur auf der Seite, wird nicht gespeichert.
const kiFilterKennzahlen = (d, bereich) => (d.modell_analyse && KI_BEWERTUNG[bereich] ? KI_BEWERTUNG[bereich][1].filter((k) => kiRang(d, bereich, k).length) : []);
const kiFilterAktiv = (e) => Object.values(e.filter || {}).filter((n) => n > 0).length;
// Passt ein Modell zu Datenschutz, Filtern und Suchwörtern?
function kiPasst(d, bereich, e, mitSuche) {
  const woerter = mitSuche ? (e.suche || '').toLowerCase().split(/\s+/).filter(Boolean) : [];
  const filter = Object.entries(e.filter || {}).filter(([, n]) => n > 0);
  return (m) => (!e.datenschutz || m.zdr)
    && filter.every(([k, n]) => { const v = kiWerte(d, bereich, m.id)?.[k]; return v != null && kiStufe(d, bereich, k, v) >= n; })
    && woerter.every((w) => T`${m.name} ${m.id}`.toLowerCase().includes(w));
}
const kiFilterTreffer = (d, bereich, e) => tx('{0} Modelle passen', kiKatalog(d, bereich, e.modell).filter(kiPasst(d, bereich, e, true)).length);
const kiFilterWert = (n) => (n > 0 ? tx('mind. {0} von 5', n) : tx('alle'));
function kiFilterFeld(d, bereich, e) {
  return T`<div class="kifilter" id="ke-filter">
    ${kiFilterKennzahlen(d, bereich).map((k) => { const n = e.filter?.[k] || 0; return T`<div class="kifilterzeile">
      <label for="ke-min-${k}"><span class="kipunkteicon">${KI_KENNZAHLEN[k].icon}</span>${tx(KI_KENNZAHLEN[k].name)}</label>
      <input type="range" id="ke-min-${k}" data-ki-min="${k}" min="0" max="5" step="1" value="${n}" aria-valuetext="${esc(kiFilterWert(n))}">
      <output for="ke-min-${k}" data-ki-min-wert="${k}">${kiFilterWert(n)}</output></div>`; }).join('')}
    <div class="kifilterfuss"><strong class="kifiltertreffer" aria-live="polite">${kiFilterTreffer(d, bereich, e)}</strong>
      <button type="button" class="btn" data-ki-filter-weg="1"${kiFilterAktiv(e) ? '' : ' disabled'}>${tx('Zurücksetzen')}</button></div></div>`;
}
function kiAuswahlListe(d, bereich, e) {
  const kat = d.openrouter_katalog;
  if (!kat) return T`<p class="hinweis warn">${tx('Modelle von OpenRouter noch nicht geladen – unter Anbieter „Prüfen“.')}</p>`;
  const alle = kiKatalog(d, bereich, e.modell);
  const rang = new Map(alle.map((m, k) => [m.id, k]));
  const stufe = (m) => (alle.length > 1 ? KI_STUFEN[Math.min(KI_STUFEN.length - 1, Math.floor((rang.get(m.id) * KI_STUFEN.length) / alle.length))] : '');
  const kontext = (n) => (n >= 1e6 ? tx('{0} Mio. Tokens Kontext', (n / 1e6).toLocaleString(LOCALE, { maximumFractionDigits: 1 })) : tx('{0} Tsd. Tokens Kontext', Math.round(n / 1000)));
  const passend = alle.filter(kiPasst(d, bereich, e, false));
  const woerter = (e.suche || '').toLowerCase().split(/\s+/).filter(Boolean);
  const gezeigt = woerter.length ? alle.filter(kiPasst(d, bereich, e, true)).slice(0, 10) : kiStreuenStabil(alle, kiPasst(d, bereich, e, false), 5, e.modell);
  // Karte: Name links, rechts zwei feste Plätze – Schild (ohne Datenspeicherung, sonst leer) und Preisstufe als $ $$ $$$;
  // darunter Preis und Kontext. Das Optionsfeld ist unsichtbar, die gewählte Karte ist hervorgehoben.
  const preisIcon = (m) => {
    const n = KI_STUFEN.indexOf(stufe(m)) + 1;
    return T`<span class="kistufe" title="${esc(tx(stufe(m)))}" aria-label="${esc(tx(stufe(m)))}">${'<b>$</b>'.repeat(n)}${'<i>$</i>'.repeat(KI_STUFEN.length - n)}</span>`;
  };
  const zeile = (m) => T`<label class="kiwahl"><input type="radio" class="kiwahlradio" name="ke-auswahl" data-auswahl="${esc(m.id)}"${m.id === e.modell ? ' checked' : ''}>
      <strong class="kiwahlname">${esc(m.name)}</strong>
      <span class="kiwahlicons">${m.zdr ? T`<span class="kischild" title="${esc(tx('ohne Speicherung'))}" aria-label="${esc(tx('ohne Speicherung'))}">${KI_SCHILD}</span>` : '<span class="kischild"></span>'}${preisIcon(m)}</span>
      <span class="kiwahlinfo leise">${esc(kiPreisText(m))}${m.kontext ? T` · ${esc(kontext(m.kontext))}` : ''}</span>
      ${kiPunkte(d, bereich, m.id)}</label>`;
  const gewaehlt = alle.find((m) => m.id === e.modell);
  return T`${e.datenschutz && e.modell && !gewaehlt?.zdr ? T`<p class="hinweis warn">${tx('Für das gewählte Modell ist keine Datenspeicherung ausgeschlossen – ein Modell unten wählen.')}</p>` : ''}
    ${gezeigt.length ? gezeigt.map(zeile).join('') : T`<p class="leise">${tx('Kein passendes Modell.')}</p>`}
    <p class="leise hilfe">${tx('{0} Modelle, laut OpenRouter vom {1}.', passend.length, new Date(kat.zeit).toLocaleDateString(LOCALE))}${kiNurBewertet(d, bereich) ? ` ${tx('Nur Modelle mit Bewertung.')}` : ''}${woerter.length && gezeigt.length === 10 ? ` ${tx('Nur die ersten 10 Treffer.')}` : ''}</p>
    ${kiPunkteLegende(d, bereich)}`;
}

// Bewertungen von Artificial Analysis je OpenRouter-Modell (Server: modellanalyse.go, Stand höchstens 24 h alt).
// Je Bereich bis zu drei Kennzahlen als 1–5 Punkte: Rang unter allen Modellen des Bereichs, die den Wert haben
// (kein Wert = nicht gemessen, dann fehlt die Kennzahl). Die Zahl steht im Hover; darunter erklärt eine Legende die Symbole.
const KI_ICON = (pfade) => `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">${pfade}</svg>`;
const KI_FILTER_ICON = KI_ICON('<path d="M3 5h18l-7 8.5V19l-4 2v-7.5z"/>');
const KI_KENNZAHLEN = {
  intelligenz: { name: 'Intelligenz', text: 'Wie gut das Modell schwierige Aufgaben löst.', zahl: (v) => tx('Index {0}', kiZ(v, 1)),
    icon: KI_ICON('<path d="M12 5.5A3 3 0 0 0 6.6 4.2 3 3 0 0 0 4 8.5a3.2 3.2 0 0 0 .6 5.6A3 3 0 0 0 7 18.6 3 3 0 0 0 12 19.5z"/><path d="M12 5.5a3 3 0 0 1 5.4-1.3A3 3 0 0 1 20 8.5a3.2 3.2 0 0 1-.6 5.6 3 3 0 0 1-2.4 4.5 3 3 0 0 1-5 .9"/><path d="M12 5.5v14"/><path d="M8.5 9.5c1 0 2 .6 2 2"/><path d="M15.5 13c-1 0-2 .6-2 2"/>') },
  token_je_s: { name: 'Tempo', text: 'Wie schnell der Text erscheint (Token je Sekunde).', zahl: (v) => tx('{0} Token/s', kiZ(v)),
    icon: KI_ICON('<circle cx="15.5" cy="3.8" r="1.8"/><path d="M14 7.5l-3 5.5"/><path d="M11 13l3.5 3-1.2 5"/><path d="M11 13l-3.5 2.2-2.5 3.8"/><path d="M13.6 8.2l3.4 2.6 3-.8"/><path d="M13.6 8.2L9.5 8.6 7.5 11"/><path d="M1.5 9h3"/><path d="M1 13h2.5"/>') },
  erste_antwort_s: { name: 'Antwortzeit', text: 'Wartezeit bis zur ersten Antwort, inklusive Nachdenken – kürzer ist besser.', zahl: (v) => tx('Antwort nach {0} s', kiZ(v, 1)), weniger: true,
    icon: KI_ICON('<circle cx="12" cy="14" r="7"/><path d="M12 14v-3.5"/><path d="M10 2.5h4"/><path d="M12 2.5V7"/><path d="M18.2 7.8l1.5-1.5"/>') },
  entscheidung_index: { name: 'Treffsicherheit', text: 'Wie oft das Modell richtig entscheidet.', zahl: (v) => tx('Index {0}', kiZ(v, 1)),
    icon: KI_ICON('<path d="M12 3v18"/><path d="M5 5h10.5L18 7.5 15.5 10H5z"/><path d="M19 12H8.5L6 14.5 8.5 17H19z"/>') },
  zeit_je_aufgabe_s: { name: 'Antwortzeit', text: 'Zeit je Entscheidung – kürzer ist besser.', zahl: (v) => tx('{0} s je Entscheidung', kiZ(v, 1)), weniger: true,
    icon: KI_ICON('<circle cx="12" cy="14" r="7"/><path d="M12 14v-3.5"/><path d="M10 2.5h4"/><path d="M12 2.5V7"/><path d="M18.2 7.8l1.5-1.5"/>') },
  elo: { name: 'Bildqualität', text: 'Wie oft Menschen das Bild im Vergleich vorziehen (Elo).', zahl: (v) => tx('Elo {0}', kiZ(v)),
    icon: KI_ICON('<path d="M8 21h8"/><path d="M12 17v4"/><path d="M7 4h10v5a5 5 0 0 1-10 0z"/><path d="M17 5h3v2a3 3 0 0 1-3 3"/><path d="M7 5H4v2a3 3 0 0 0 3 3"/>') },
  wer_index: { name: 'Genauigkeit', text: 'Wie wenige Wörter falsch erkannt werden – weniger Fehler ist besser.', zahl: (v) => tx('Fehlerquote {0} %', kiZ(v, 1)), weniger: true,
    icon: KI_ICON('<circle cx="12" cy="12" r="9"/><circle cx="12" cy="12" r="5"/><circle cx="12" cy="12" r="1"/>') },
};
const KI_BEWERTUNG = {
  kurztitel: ['text', ['intelligenz', 'token_je_s', 'erste_antwort_s']], auswertung: ['text', ['intelligenz', 'token_je_s', 'erste_antwort_s']],
  recherche: ['text', ['intelligenz', 'token_je_s', 'erste_antwort_s']], entscheidung: ['entscheidung', ['entscheidung_index', 'zeit_je_aufgabe_s']],
  bilder: ['bild', ['elo']], transkription: ['transkription', ['wer_index']],
};
const kiZ = (v, n = 0) => Number(v).toLocaleString(LOCALE, { maximumFractionDigits: n });
const kiWerte = (d, bereich, id) => d.modell_analyse?.modelle?.[id]?.[KI_BEWERTUNG[bereich]?.[0]];
// Rangliste je Bereich und Kennzahl (gemerkt je geladenem Katalog): sortierte Werte aller Modelle des Bereichs.
const kiRangCache = new WeakMap();
function kiRang(d, bereich, k) {
  let c = kiRangCache.get(d);
  if (!c) kiRangCache.set(d, (c = {}));
  const schluessel = `${bereich}|${k}`;
  if (!c[schluessel]) {
    const werte = kiKatalog(d, bereich).map((m) => kiWerte(d, bereich, m.id)?.[k]).filter((v) => v != null);
    c[schluessel] = werte.sort((a, b) => (KI_KENNZAHLEN[k].weniger ? b - a : a - b)); // aufsteigend nach „besser“
  }
  return c[schluessel];
}
// Punkte 1–5: Anteil der Modelle, die schlechter oder gleich sind.
function kiStufe(d, bereich, k, v) {
  const liste = kiRang(d, bereich, k);
  if (liste.length < 2) return 3;
  const besser = (a, b) => (KI_KENNZAHLEN[k].weniger ? a <= b : a >= b);
  const n = liste.filter((x) => besser(v, x)).length;
  return Math.max(1, Math.min(5, Math.ceil((n / liste.length) * 5)));
}
function kiPunkte(d, bereich, id) {
  const w = kiWerte(d, bereich, id);
  if (!w) return '';
  const teile = KI_BEWERTUNG[bereich][1].filter((k) => w[k] != null).map((k) => {
    const z = KI_KENNZAHLEN[k], n = kiStufe(d, bereich, k, w[k]), titel = `${tx(z.name)}: ${z.zahl(w[k])} – ${tx('{0} von 5', n)}`;
    return T`<span class="kipunkte" title="${esc(titel)}" aria-label="${esc(titel)}"><span class="kipunkteicon">${z.icon}</span><span class="kipunktereihe">${'<i class="an"></i>'.repeat(n)}${'<i></i>'.repeat(5 - n)}</span></span>`;
  });
  return teile.length ? T`<span class="kiwahlwerte">${teile.join('')}</span>` : '';
}
// Kurzer Text des ersten Werts (Bereichsseite, am Eintrag).
function kiBewertungText(d, bereich, id) {
  const w = kiWerte(d, bereich, id);
  const k = w && KI_BEWERTUNG[bereich][1].find((x) => w[x] != null);
  return k ? `${tx(KI_KENNZAHLEN[k].name)} ${tx('{0} von 5', kiStufe(d, bereich, k, w[k]))}` : '';
}
// Legende unter der Liste: je Symbol des Bereichs eine Erklärung, dazu die Quelle (Namensnennung ist Pflicht).
function kiPunkteLegende(d, bereich) {
  const a = d.modell_analyse;
  if (!a || !KI_BEWERTUNG[bereich]) return '';
  return T`<div class="kilegende leise">
    ${KI_BEWERTUNG[bereich][1].map((k) => T`<p><span class="kipunkteicon">${KI_KENNZAHLEN[k].icon}</span><span><strong>${tx(KI_KENNZAHLEN[k].name)}</strong> – ${tx(KI_KENNZAHLEN[k].text)}</span></p>`).join('')}
    <p class="hilfe">${tx('Punkte: Rang unter den Modellen dieses Bereichs (5 = beste). Zahl beim Darüberfahren; ohne Punkte = nicht gemessen.')}</p>
    <p class="hilfe">${tx('Bewertungen: {0}, Stand {1}.', T`<a href="${esc(a.quelle_url)}" target="_blank" rel="noopener">${esc(a.quelle)}</a>`, new Date(a.stand).toLocaleDateString(LOCALE))}</p></div>`;
}

// Preise eines Modells der OpenAI-Auswahl als kurzer Text.
function kiPreisText(m) {
  if (m.preis_bild_mio) return tx('{0} $ je 1 Mio. Bild-Tokens', m.preis_bild_mio);
  if (m.preis_ein && !m.preis_aus && !m.preis_minute && !m.preis_bild) return tx('{0} $ je 1 Mio. Eingabe-Tokens', m.preis_ein);
  if (!m.preis_minute && !m.preis_bild && !m.preis_ein && !m.preis_aus) return '';
  if (m.preis_minute) return tx('ca. {0} $/Min', m.preis_minute);
  if (m.preis_bild) return tx('ca. {0} $/Bild', m.preis_bild);
  return tx('ca. {0} / {1} $ je 1 Mio. Tokens', m.preis_ein, m.preis_aus);
}
// Preise eines Modells der Auswahl in den Eintrag übernehmen (Websuche nur, wenn gewählt).
function kiPreiseAusKatalog(d, bereich, e) {
  const m = kiArt(d, d.zugaenge.find((x) => x.id === e.zugang))?.modelle?.[bereich]?.find((x) => x.id === e.modell);
  if (!m) return;
  for (const k of ['preis_ein', 'preis_aus', 'preis_minute', 'preis_bild']) if (m[k]) e[k] = m[k];
  if (m.preis_suche && e.websuche?.includes('anbieter')) e.preis_suche = m.preis_suche;
}

// Name eines Eintrags (Zugang oder Sondereintrag).
const kiEintragName = (d, e) => { const z = d.zugaenge.find((x) => x.id === e.zugang); return z ? z.name : tx(KI_SONDER[e.zugang] || e.zugang || 'Neuer Eintrag'); };

// Schreibt die Einträge eines Bereichs (ganz) und startet den Dienst neu; Fehler zeigt die aufrufende Seite.
async function kiSpeichereBereich(d, bereich, liste, werte = {}) {
  const r = await api('/api/ki', { method: 'PUT', body: { zugaenge: d.zugaenge.map((z) => ({ id: z.id, name: z.name, art: z.art, adresse: z.adresse })),
    bereiche: { ...d.bereiche, [bereich]: liste }, werte } });
  await kiNachSpeichern(r);
}

// Nach dem Speichern unter KI: Änderungen an KI und Whisper gelten sofort, nur andere brauchen einen Neustart.
async function kiNachSpeichern(r) {
  if (!r?.neustart) { melde(tx('Gespeichert')); return; }
  melde(tx('Gespeichert – Dienst startet neu'));
  await warteAufNeustart();
}

// Seite eines Eintrags (/status/ki/<bereich>/<zugang>): jeder Zugang steht genau einmal je Bereich. Speichern schreibt
// den ganzen Bereich und führt zurück zur Bereichsseite.
async function zeigeKIEintrag(seiteName, zugangId) {
  const s = KI_SEITEN[seiteName];
  seite.innerHTML = T`${statusReiter('ki')}<p class="leer">Lädt …</p>`;
  if (!s || !istAdmin()) { seite.innerHTML = T`${statusReiter('ki')}<div class="hinweis schlecht">Unbekannter Bereich.</div>`; return; }
  const d = await ladeKI();
  if (aktuell?.name !== 'kiEintrag') return;
  const bereich = s.bereich, zurueck = T`/status/ki/${seiteName}`;
  // Reihenfolge wie auf der Bereichsseite: aktive vor inaktiven.
  const roh = kiAlleEintraege(d, bereich);
  const alle = [...roh.filter((x) => !x.inaktiv), ...roh.filter((x) => x.inaktiv)];
  const alt = alle.find((x) => x.zugang === zugangId);
  if (!alt) { seite.innerHTML = T`${statusReiter('ki')}<div class="hinweis schlecht">${tx('Unbekannter Eintrag.')}</div><div class="leiste"><a class="btn" href="${zurueck}">Zurück</a></div>`; return; }
  const e = { ...alt, websuche: [...(alt.websuche || [])] };
  const art = kiArt(d, d.zugaenge.find((x) => x.id === e.zugang));
  // Feste Auswahl ohne passendes Modell: Vorgabe des Bereichs samt Preisen.
  const auswahl = art?.modelle?.[bereich];
  if (auswahl && !auswahl.some((m) => m.id === e.modell)) { e.modell = auswahl[0].id; kiPreiseAusKatalog(d, bereich, e); }
  // Freie Modellwahl: Vorschlag des Anbieters vorbelegen.
  if (!auswahl && !e.modell && art?.oauth === 'openrouter') e.modell = kiVorgabe(d, bereich);
  // Auswertung und Recherche nutzen die Websuche des Anbieters immer, wenn er eine hat (kein Schalter).
  if ((bereich === 'recherche' || bereich === 'auswertung') && art?.suche && !e.websuche.includes('anbieter')) { e.websuche.push('anbieter'); if (auswahl) kiPreiseAusKatalog(d, bereich, e); }
  // Whisper: Modell (mit Tabelle der Mittel, geladene löschen) und RAM-Haltezeit auf derselben Seite.
  const whisper = e.zugang === 'whisper';
  const whisperFelder = () => {
    const tmp = document.createElement('div');
    tmp.innerHTML = einstellungsFormular(d.felder.filter((x) => x.key.startsWith('WHISPER_')), { assistent: false, whisper: d.whisper, whisperLoeschen: true });
    return [...tmp.querySelectorAll('form > fieldset > *:not(legend)')].map((x) => x.outerHTML).join('');
  };
  seite.innerHTML = T`${statusReiter('ki')}${krumen([[tx('KI'), '/status/ki'], [tx(s.titel), zurueck], [kiEintragName(d, alt)]])}<h1>${esc(kiEintragName(d, alt))}</h1>
    <form class="einstellungen" novalidate><div class="kiseiteinhalt"></div>
      <p class="hinweis schlecht" hidden></p>
      <div class="leiste"><button type="submit" class="btn haupt">Speichern</button><a class="btn" href="${zurueck}">Abbrechen</a></div></form>`;
  const form = seite.querySelector('form'), koerper = form.querySelector('.kiseiteinhalt'), fehler = form.querySelector('.hinweis');
  const male = () => { koerper.innerHTML = (whisper ? whisperFelder() : '') + kiEintragFelder(d, bereich, e); };
  male();
  form.addEventListener('input', (ev) => {
    const k = ev.target.dataset.k;
    if (k) e[k] = ['preis_ein', 'preis_aus', 'preis_minute', 'preis_suche', 'preis_bild', 'limit_monat'].includes(k) ? kiZahl(ev.target.value) : ev.target.value;
  });
  form.addEventListener('change', (ev) => {
    const t = ev.target;
    if (t.dataset.suche) { e.websuche = [...(e.websuche.includes('anbieter') ? ['anbieter'] : []), ...[...form.querySelectorAll('[data-suche]:checked')].map((c) => c.dataset.suche)]; male(); }
    else if (t.dataset.auswahl) { e.modell = t.dataset.auswahl; male(); }
    else if (t.dataset.datenschutz) { e.datenschutz = t.checked; male(); }
  });
  // Suche und Filter der Modellwahl: nur Liste und Trefferzahl neu malen (Fokus bleibt), offline in den geladenen Daten.
  const neuListe = () => {
    const l = form.querySelector('.kiauswahlliste');
    if (l) l.innerHTML = kiAuswahlListe(d, bereich, e);
    const z = form.querySelector('.kifiltertreffer');
    if (z) z.textContent = kiFilterTreffer(d, bereich, e);
    return l;
  };
  form.addEventListener('input', (ev) => {
    const t = ev.target;
    if (t.dataset.kiMin) {
      e.filter = { ...(e.filter || {}), [t.dataset.kiMin]: Number(t.value) };
      const w = form.querySelector(`[data-ki-min-wert="${t.dataset.kiMin}"]`);
      if (w) w.textContent = kiFilterWert(Number(t.value));
      t.setAttribute('aria-valuetext', kiFilterWert(Number(t.value)));
      neuListe();
      return;
    }
    if (!t.dataset.kiSuche) return;
    e.suche = t.value;
    const l = neuListe();
    if (l) return;
    else if (t.dataset.k === 'modell') { e.modell = t.value.trim(); if (auswahl) kiPreiseAusKatalog(d, bereich, e); male(); }
  });
  form.addEventListener('click', async (ev) => {
    if (ev.target.closest('[data-ki-filter]')) { e.filterOffen = !e.filterOffen; male(); form.querySelector('[data-ki-filter]')?.focus(); return; }
    if (ev.target.closest('[data-ki-filter-weg]')) { e.filter = {}; male(); return; }
    const b = ev.target.closest('[data-whisper-loeschen]');
    if (!b || !confirm(tx('Modell {0} löschen? Es lässt sich später wieder laden.', b.dataset.whisperLoeschen))) return;
    b.disabled = true;
    try {
      d.whisper = (await api(T`/api/whisper/modelle/${b.dataset.whisperLoeschen}`, { method: 'DELETE' })).whisper || d.whisper;
      melde(tx('Gelöscht'));
      male();
    } catch (f) { b.disabled = false; melde(f.message); }
  });
  form.onsubmit = async (ev) => {
    ev.preventDefault();
    const knopf = form.querySelector('[type=submit]');
    fehler.hidden = true;
    knopf.disabled = true;
    try {
      const { suche: _, filter: _f, filterOffen: _o, ...eintrag } = e;
      await kiSpeichereBereich(d, bereich, kiZuSpeichern(alle.map((x) => (x.id === e.id ? { ...eintrag, neu: false } : x))), whisper ? einstellungsWerte(form) : {});
      geheZu(zurueck, true);
    } catch (f) {
      fehler.textContent = f.message;
      fehler.hidden = false;
      knopf.disabled = false;
    }
  };
}

// Ziehbare Liste: Einträge mit Maus oder Finger verschieben (Finger: am Griff sofort, sonst nach kurzem Halten, damit
// die Seite weiter scrollt), Tastatur: Pfeil hoch/runter. Tippen ohne Ziehen = antippen(li); nach jedem Verschieben
// geaendert(neue Reihenfolge der data-i).
function kiZiehbar(liste, { antippen, geaendert }) {
  let zug = null;
  const ende = () => liste.querySelectorAll('.ziehen').forEach((x) => x.classList.remove('ziehen'));
  const melde = () => geaendert([...liste.children].map((x) => Number(x.dataset.i)));
  liste.addEventListener('pointerdown', (ev) => {
    const li = ev.target.closest('.kireihezeile');
    if (!li || ev.button > 0) return;
    const griff = !!ev.target.closest('.kigriff');
    zug = { li, id: ev.pointerId, x: ev.clientX, y: ev.clientY, aktiv: false, bewegt: false };
    const starte = () => { if (!zug) return; zug.aktiv = true; li.classList.add('ziehen'); try { li.setPointerCapture(zug.id); } catch { /* schon losgelassen */ } };
    if (ev.pointerType === 'mouse' || griff) { if (griff) { ev.preventDefault(); starte(); } } else zug.timer = setTimeout(starte, 250);
  });
  liste.addEventListener('pointermove', (ev) => {
    if (!zug || ev.pointerId !== zug.id) return;
    const weit = Math.hypot(ev.clientX - zug.x, ev.clientY - zug.y) > 6;
    if (!zug.aktiv) {
      if (!weit) return;
      if (ev.pointerType !== 'mouse') { clearTimeout(zug.timer); zug = null; return; } // Finger bewegt sich: Seite scrollt
      zug.aktiv = true; zug.li.classList.add('ziehen'); zug.li.setPointerCapture(zug.id);
    }
    zug.bewegt = true;
    const vor = [...liste.children].find((x) => x !== zug.li && ev.clientY < x.getBoundingClientRect().top + x.offsetHeight / 2);
    if (vor !== zug.li.nextElementSibling) liste.insertBefore(zug.li, vor || null);
  });
  // Während des Ziehens darf der Finger die Seite nicht scrollen.
  liste.addEventListener('touchmove', (ev) => { if (zug?.aktiv) ev.preventDefault(); }, { passive: false });
  // Geöffnet wird erst beim click, nicht schon beim Loslassen: sonst landet der click danach auf der neuen Seite
  // (auf dem Feld unter dem Finger) und öffnet Tastatur und Modellauswahl. Nach einem Zug wird der click verschluckt.
  let nachZug = false;
  const los = (ev) => {
    if (!zug || ev.pointerId !== zug.id) return;
    clearTimeout(zug.timer);
    const { bewegt } = zug;
    zug = null;
    ende();
    if (bewegt) { nachZug = true; setTimeout(() => { nachZug = false; }, 100); melde(); }
  };
  liste.addEventListener('click', (ev) => {
    const li = ev.target.closest('.kireihezeile');
    if (nachZug) { nachZug = false; return; }
    if (li) antippen(li);
  });
  liste.addEventListener('pointerup', los);
  liste.addEventListener('pointercancel', (ev) => { if (zug) { clearTimeout(zug.timer); zug = null; ende(); } });
  liste.addEventListener('contextmenu', (ev) => { if (ev.target.closest('.kireihezeile')) ev.preventDefault(); });
  liste.addEventListener('keydown', (ev) => {
    const li = ev.target.closest('.kireihezeile');
    if (!li) return;
    if (ev.key === 'Enter' || ev.key === ' ') { ev.preventDefault(); antippen(li); return; }
    if (ev.key !== 'ArrowUp' && ev.key !== 'ArrowDown') return;
    ev.preventDefault();
    if (ev.key === 'ArrowUp' && li.previousElementSibling) liste.insertBefore(li, li.previousElementSibling);
    else if (ev.key === 'ArrowDown' && li.nextElementSibling) liste.insertBefore(li.nextElementSibling, li);
    const i = li.dataset.i;
    melde();
    liste.querySelector(T`[data-i="${i}"]`)?.focus();
  });
}

// Bereichsseite: je Eintrag eine Zeile (Tippen öffnet seine Seite), Knopf Reihenfolge, übrige Werte als Zeilen mit ✎.
async function zeigeKIBereich(seiteName) {
  const s = KI_SEITEN[seiteName];
  seite.innerHTML = T`${statusReiter('ki')}<p class="leer">Lädt …</p>`;
  if ((!s && seiteName !== 'zugaenge') || !istAdmin()) { seite.innerHTML = T`${statusReiter('ki')}<div class="hinweis schlecht">Unbekannter Bereich.</div>`; return; }
  const d = await ladeKI();
  if (aktuell?.name !== 'kiBereich') return;
  if (seiteName === 'zugaenge') return zeigeKIZugaenge(d);
  const alle = kiAlleEintraege(d, s.bereich);
  // Reihe: aktive, Trenner „Inaktiv“, inaktive – verschoben wird auf der Seite, gespeichert mit eigenem Knopf.
  const reihe = [...alle.filter((e) => !e.inaktiv), { trenner: true }, ...alle.filter((e) => e.inaktiv)];
  const zeile = (e, i, nr) => {
    const z = d.zugaenge.find((x) => x.id === e.zugang);
    const voll = e.limit_monat && d.verbrauch[e.id] >= e.limit_monat;
    const whisperName = e.zugang === 'whisper' && (d.whisper?.modelle.find((m) => m.id === d.felder.find((x) => x.key === 'WHISPER_MODEL')?.wert)?.name);
    const info = [whisperName || e.modell || (e.neu && KI_SONDER[e.zugang] === undefined ? tx('nicht eingerichtet') : ''), kiArt(d, z)?.oauth === 'openrouter' && e.modell && kiBewertungText(d, s.bereich, e.modell), !e.neu && kiCloud(d, z) && T`${kiUSD(d.verbrauch[e.id])}${e.limit_monat ? T` ${tx('von')} ${kiUSD(e.limit_monat)}` : ''} ${tx('im Monat')}`].filter(Boolean).join(' · ');
    return T`<li class="kireihezeile${nr ? '' : ' kiaus'}" data-i="${i}" tabindex="0" aria-label="${esc(tx('{0} – Enter öffnet, Pfeil hoch/runter verschiebt', kiEintragName(d, e)))}">
      <span class="kigriff" aria-hidden="true">⠿</span><span class="kireihetext"><span class="kizeilename">${nr ? `${nr}. ` : ''}${esc(kiEintragName(d, e))}${voll ? T` <span class="badge fehler">${tx('Limit erreicht')}</span>` : ''}</span><span class="leise">${esc(info)}</span></span>
      <span class="kizeilestift" aria-hidden="true">${STIFT}</span></li>`;
  };
  const maleReihe = () => {
    const t = reihe.findIndex((e) => e.trenner);
    return reihe.map((e, i) => (e.trenner ? T`<li class="kitrenner" data-i="${i}">${tx('Inaktiv')}</li>` : zeile(e, i, i < t ? i + 1 : 0))).join('');
  };
  const felder = d.felder.filter((x) => x.gruppe === KI_GRUPPEN[seiteName] && !x.key.startsWith('WHISPER_'));
  const wert = (f) => esc(f.art === 'choice' ? wahlText(f, f.wert) : f.wert || '–');
  seite.innerHTML = T`${statusReiter('ki')}${krumen([[tx('KI'), '/status/ki'], [tx(s.titel)]])}<h1>${esc(tx(s.titel))}</h1>
    <p class="leise">${esc(tx(s.text))} ${tx('Der erste erledigt es; scheitert er oder ist sein Monatslimit erreicht, übernimmt der nächste. Einträge ziehen, um sie zu ordnen oder unter „Inaktiv“ abzuschalten; tippen öffnet sie.')}</p>
    ${alle.length ? T`<ol class="kireihe" id="ki-reihe">${maleReihe()}</ol>`
      : T`<p class="leise">${tx('Kein passender Anbieter')} – <a href="/status/ki/zugaenge">${tx('Anbieter anlegen')}</a></p>`}
    ${s.bereich === 'entscheidung' && d.entscheidung_fragen ? T`<h3>${tx('Fragen an das Modell')}</h3><div class="karte konfliste">
      <div class="konfzeile ohnehilfe"><span class="konfname">${tx('Typ')}</span><span></span><span class="konfwert">${esc(d.entscheidung_fragen.typ.map((o) => tx(o.name)).join(', '))}</span></div>
      <div class="konfzeile ohnehilfe"><span class="konfname">${tx('Inhalt (bei Notizen)')}</span><span></span><span class="konfwert">${esc(d.entscheidung_fragen.inhalt.map((o) => tx(o.name)).join(', '))}</span></div>
      <div class="konfzeile ohnehilfe"><span class="konfname">${tx('Liste (Notizen, Planungen)')}</span><span></span><span class="konfwert">${tx('Passt eine vorhandene Liste? Welche?')}</span></div>
      <div class="konfzeile ohnehilfe"><span class="konfname">${tx('Planung')}</span><span></span><span class="konfwert">${tx('Setzt die Notiz eine offene Planung fort? Welche?')}</span></div></div>` : ''}
    ${felder.length ? T`<div class="karte konfliste">${felder.map((f) => T`<div class="konfzeile ohnehilfe"><span class="konfname">${esc(tx(f.name))}</span>
      <a class="konfedit" href="/status/einstellungen/${f.key}?zurueck=/status/ki/${seiteName}" aria-label="${esc(tx('{0} bearbeiten', tx(f.name)))}">${STIFT}</a><span class="konfwert">${wert(f)}</span></div>`).join('')}</div>` : ''}
    <div class="leiste"><a class="btn" href="/status/ki">Zurück</a></div>`;
  // Einträge in der Reihenfolge der Seite, inaktiv nach dem Trenner – Grundlage für Speichern und Fenster.
  const stand = () => { const t = reihe.findIndex((e) => e.trenner); return [...reihe.slice(0, t).map((e) => ({ ...e, inaktiv: false })), ...reihe.slice(t + 1).map((e) => ({ ...e, inaktiv: true }))]; };
  // Jede Verschiebung wird sofort gespeichert (ohne Neustart); danach zeigt die Seite den gespeicherten Stand.
  let kette = Promise.resolve();
  const ol = document.getElementById('ki-reihe');
  if (ol) {
    kiZiehbar(ol, {
      antippen: (li) => { const e = reihe[Number(li.dataset.i)]; if (e && !e.trenner) geheZu(T`/status/ki/${seiteName}/${e.zugang}`); },
      geaendert: (folge) => {
        reihe.splice(0, reihe.length, ...folge.map((i) => reihe[i]));
        ol.innerHTML = maleReihe();
        const liste = kiZuSpeichern(stand());
        kette = kette.then(async () => {
          try {
            await kiSpeichereBereich(d, s.bereich, liste);
          } catch (f) {
            melde(f.message);
          }
          if (aktuell?.name === 'kiBereich') await zeigeKIBereich(seiteName); // gespeicherten Stand zeigen (Vorgaben des Servers)
        });
      },
    });
  }
  aufraeumen = () => { seite.onclick = null; };
}

// Joplin einrichten und testen (Reiter nur bei JOPLIN_ENABLED = aktiv).
async function zeigeJoplin() {
  seite.innerHTML = T`${statusReiter('joplin')}<p class="leer">Lädt …</p>`;
  const [d, st] = await Promise.all([api('/api/einstellungen/persoenlich'), api('/api/status').catch(() => null)]);
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
      await speichereEinstellungen(werte(), true);
      geheZu('/status/joplin', true);
    } catch (f) { knopf.disabled = false; melde(f.message); }
  };
  aufraeumen = () => { form.onclick = null; };
}

// Benachrichtigungen: Web-Push auf diesem Gerät ein/aus und je Kanal (persönliche Einstellungen NOTIFY_*).
// Anonyme Statistik: ein/aus (persönlich) und der eigene Anteil am nächsten Monatsbericht.
async function zeigeStatistik() {
  seite.innerHTML = T`<p class="leer">Lädt …</p>`;
  const laden = async () => {
    const st = await api('/api/statistik');
    if (aktuell?.name !== 'statistik') return;
    const zurueck = T`<p><a class="btn klein" href="/status">← ${esc(tx('Status'))}</a></p>`;
    if (!st.verfuegbar) {
      seite.innerHTML = T`${zurueck}<h1>${esc(tx('Anonyme Statistik'))}</h1><p class="leise">${esc(tx('In diesem Build nicht verfügbar; es wird nichts gesendet.'))}</p>`;
      return;
    }
    const daten = st.aktiv ? T`<div id="lebensdauer"><p class="leer">${esc(tx('Lädt …'))}</p></div>`
      : T`<p class="leise">${esc(st.admin ? tx('Die Daten aller Installationen siehst du, sobald diese Installation selbst sendet.') : tx('Diese Installation sendet keine Statistik – die Daten aller Installationen sind deshalb nicht sichtbar.'))}</p>`;
    // Ein- und Ausschalten gilt für die ganze Installation – nur Admins; Nutzer sehen nur die Auswertung.
    seite.innerHTML = st.admin ? T`${zurueck}<h1>${esc(tx('Anonyme Statistik'))}</h1>
      <form class="einstellungen" novalidate><fieldset><label class="schalter"><input type="checkbox" name="aktiv"${st.aktiv ? ' checked' : ''}> ${esc(tx('Anonyme Statistik senden'))}</label>
        <p class="leise hilfe">${esc(tx('Es werden nur anonyme Daten zur Gesamtnutzungszeit der Ringe gesendet.'))} ${esc(tx('Gilt für alle Nutzer dieser Installation.'))}</p></fieldset>
        <div class="leiste"><button class="btn haupt" type="submit">${esc(tx('Speichern'))}</button></div></form>
      <h2>${esc(tx('Ring-Lebensdauer aller Installationen'))}</h2>${daten}`
      : T`${zurueck}<h1>${esc(tx('Ring-Lebensdauer aller Installationen'))}</h1>${daten}`;
    if (st.bericht) seite.insertAdjacentHTML('beforeend', statistikVorschau(st));
    if (st.aktiv) zeigeGlobaleLebensdauer(document.getElementById('lebensdauer'));
    const form = seite.querySelector('form');
    if (form) form.onsubmit = async (ev) => {
      ev.preventDefault();
      const knopf = form.querySelector('[type=submit]');
      knopf.disabled = true;
      try {
        await api('/api/statistik', { method: 'PUT', body: { aktiv: form.aktiv.checked } });
        melde(tx('Gespeichert'));
        await laden();
      } catch (f2) { knopf.disabled = false; melde(f2.message); }
    };
  };
  aktuell.neuLaden = laden;
  await laden();
}

// „Erweitert“: das gesendete JSON mit Kommentaren je Feld (JSON mit // wie JSONC; nur zur Ansicht).
const STATISTIK_KOMMENTARE = {
  app_id: () => tx('zufällige App-ID'),
  ring: () => tx('verschlüsselte Ring-ID'),
};
function jsonMitKommentaren(wert, tiefe = 0, schluessel = '', letzt = true) {
  const ein = '  '.repeat(tiefe), komma = letzt ? '' : ',';
  // Kommentar je Feld in eigener Zeile darüber – bleibt auch auf dem Handy lesbar.
  const kom = (k, v) => { const f = STATISTIK_KOMMENTARE[k]; return f ? T`${ein}<span class="jkommentar">// ${esc(f(v))}</span>\n` : ''; };
  const name = schluessel ? T`<span class="jschluessel">${esc(JSON.stringify(schluessel))}</span>: ` : '';
  if (Array.isArray(wert) || (wert && typeof wert === 'object')) {
    const liste = Array.isArray(wert), teile = liste ? wert.map((v) => ['', v]) : Object.entries(wert);
    const auf = liste ? '[' : '{', zu = liste ? ']' : '}';
    if (!teile.length) return T`${kom(schluessel)}${ein}${name}${auf}${zu}${komma}\n`;
    return T`${kom(schluessel)}${ein}${name}${auf}\n${teile.map(([k, v], i) => jsonMitKommentaren(v, tiefe + 1, k, i === teile.length - 1)).join('')}${ein}${zu}${komma}\n`;
  }
  return T`${kom(schluessel, wert)}${ein}${name}<span class="jwert">${esc(JSON.stringify(wert))}</span>${komma}\n`;
}
function statistikVorschau(st) {
  const ev = st.ereignisse || [];
  return T`<details class="karte erweitert"><summary>${esc(tx('Erweitert'))}</summary>
    <p class="leise">${esc(tx('Wird verschlüsselt gesendet.'))}</p>
    <pre class="jsonc">${jsonMitKommentaren(st.bericht)}${ev.length ? T`\n${ev.map((e) => jsonMitKommentaren(e)).join('\n')}` : ''}</pre></details>`;
}

// Ring-Lebensdauer aller Installationen (anonyme Summen, Server hält sie 6 h): je Status Balken Min–Max, Strich bei Ø.
async function zeigeGlobaleLebensdauer(el) {
  let g;
  try { g = await api('/api/statistik/lebensdauer'); } catch (f) { el.innerHTML = T`<p class="leise">${esc(f.message)}</p>`; return; }
  const NAMEN = { batterie: tx('Batterie leer'), defekt: tx('Ring defekt'), verloren: tx('Ring verloren'), sonstiges: tx('Sonstiger Grund'), aktiv: tx('Noch aktiv') };
  const h = (v) => stunden(v * 3600);
  const l = g.lebensdauer || [];
  if (!l.length) { el.innerHTML = T`<p class="leer">${esc(tx('Noch keine Ringe gemeldet.'))}</p>`; return; }
  const eigene = g.eigene || [];
  el.innerHTML = T`<p class="leise">${esc(tx('Aufnahmezeit je Ring: Balken vom kürzesten bis zum längsten Ring, farbiger Strich beim Durchschnitt, schwarzer Strich = deine Ringe.'))}</p>
    ${l.map((x) => { const span = x.max_h - x.min_h, lage = (v) => span > 0 ? ((v - x.min_h) / span) * 100 : 50, pos = lage(x.schnitt_h), name = NAMEN[x.status] ?? x.status;
      const mein = eigene.filter((e) => e.status === x.status);
      return T`<div class="wortblock"><div class="zeile"><strong>${esc(name)}</strong><span class="leise">${esc(tx('{0} Ringe', x.ringe))}</span></div>
      <div class="wortskala" role="img" aria-label="${esc(tx('{0}: Min {1}, Ø {2}, Max {3}', name, h(x.min_h), h(x.schnitt_h), h(x.max_h)))}">
        <span class="wortschnitt" data-pos="${pos}">${esc(tx('Ø {0}', h(x.schnitt_h)))}</span>
        <span class="wortbalken"><i data-pos="${pos}"></i>${mein.map((e) => T`<i class="eigen" data-pos="${lage(e.stunden)}" title="${esc(tx('Dein Ring {0}: {1}', e.ring, h(e.stunden)))}"></i>`).join('')}</span>
        <span class="wortgrenzen"><span>${esc(tx('Min {0}', h(x.min_h)))}</span><span>${esc(tx('Max {0}', h(x.max_h)))}</span></span></div>
      ${mein.length ? T`<p class="leise klein">${esc(tx('Deine Ringe: {0}', mein.map((e) => h(e.stunden)).join(' · ')))}</p>` : ''}</div>`; }).join('')}
    ${g.stand ? T`<p class="leise">${esc(tx('Stand: {0}', zeit(g.stand)))}</p>` : ''}`;
  setzeWortlage(el);
}

// Rückmeldung an den Entwickler: Kommentar oder Fehler mit App-Version, auf Wunsch mit E-Mail-Adresse.
async function zeigeRueckmeldung() {
  let art = 'kommentar';
  seite.innerHTML = T`<p><a class="btn klein" href="/status">← ${esc(tx('Status'))}</a></p><h1>${esc(tx('Rückmeldung senden'))}</h1>
    <form novalidate id="rform">
      <nav class="btn-group" aria-label="${esc(tx('Art'))}"><button type="button" class="btn aktiv" data-art="kommentar" aria-pressed="true">${esc(tx('Kommentar'))}</button><button type="button" class="btn" data-art="fehler" aria-pressed="false">${esc(tx('Fehler melden'))}</button></nav>
      <label for="rtext">${esc(tx('Text'))}</label><textarea id="rtext" rows="7" maxlength="4000" required></textarea>
      <p class="leise hilfe" id="rhilfe">${esc(tx('Was gefällt dir, was fehlt? Höchstens 4000 Zeichen.'))}</p>
      <label for="rmail">${esc(tx('E-Mail für eine Antwort (optional)'))}</label><input id="rmail" type="email" maxlength="200" autocomplete="email" placeholder="name@beispiel.de">
      <div class="leiste"><button class="btn haupt" type="submit">${esc(tx('Senden'))}</button><a class="btn" href="/status">${esc(tx('Abbrechen'))}</a></div></form>`;
  const form = document.getElementById('rform');
  form.querySelectorAll('[data-art]').forEach((b) => { b.onclick = () => {
    art = b.dataset.art;
    form.querySelectorAll('[data-art]').forEach((x) => { x.classList.toggle('aktiv', x === b); x.setAttribute('aria-pressed', x === b ? 'true' : 'false'); });
    document.getElementById('rhilfe').textContent = art === 'fehler' ? tx('Was hast du gemacht, was ist passiert, was hättest du erwartet? Höchstens 4000 Zeichen.') : tx('Was gefällt dir, was fehlt? Höchstens 4000 Zeichen.');
  }; });
  form.onsubmit = async (ev) => {
    ev.preventDefault();
    const text = document.getElementById('rtext').value.trim();
    if (!text) return melde(tx('Bitte einen Text eingeben'));
    const knopf = form.querySelector('[type=submit]');
    knopf.disabled = true;
    try {
      const email = document.getElementById('rmail').value.trim();
      if (email && !document.getElementById('rmail').checkValidity()) { knopf.disabled = false; return melde(tx('E-Mail-Adresse ungültig')); }
      await api('/api/rueckmeldung', { method: 'POST', body: { art, text, email } });
      melde(tx('Danke! Rückmeldung gesendet'));
      geheZu('/status', true);
    } catch (f) { knopf.disabled = false; melde(f.message); }
  };
}

async function zeigeBenachrichtigungen() {
  seite.innerHTML = T`${kontoReiter('benachrichtigungen')}<p class="leer">Lädt …</p>`;
  const laden = async () => {
    const [d, k] = await Promise.all([api('/api/einstellungen/persoenlich'), api('/api/push/schluessel')]);
    if (aktuell?.name !== 'benachrichtigungen') return;
    const abo = await pushAbo().catch(() => null);
    const pushDa = 'PushManager' in window;
    const kanaele = d.felder.filter((f) => f.gruppe === 'Benachrichtigungen' && (f.key !== 'NOTIFY_UPDATES' || istAdmin()));
    const geraet = !pushDa ? T('<p class="leise">Dieser Browser unterstützt keine Benachrichtigungen. Auf dem iPhone die App erst zum Home-Bildschirm hinzufügen.</p>')
      : T`<p>${abo ? tx('Auf diesem Gerät eingeschaltet.') : tx('Auf diesem Gerät ausgeschaltet.')} <span class="leise">Geräte mit Benachrichtigung: ${k.abos}</span></p>
        <div class="leiste">${abo ? T('<button class="btn" type="button" data-a="push-aus">Ausschalten</button><button class="btn" type="button" data-a="push-test">Test senden</button>') : T('<button class="btn haupt" type="button" data-a="push-an">Einschalten</button>')}</div>`;
    seite.innerHTML = T`${kontoReiter('benachrichtigungen')}<h1>Benachrichtigungen</h1>
      <h2>Dieses Gerät</h2><div class="karte">${geraet}</div>
      <h2>Kanäle</h2><p class="leise">Gilt für alle deine Geräte.</p>
      <form class="einstellungen" novalidate><fieldset>${kanaele.map((f) => T`<label class="schalter"><input type="checkbox" name="${f.key}"${f.wert === 'aktiv' ? ' checked' : ''}> ${esc(tx(f.name))}</label><p class="leise hilfe">${esc(tx(f.hilfe))}</p>`).join('')}</fieldset>
        <div class="leiste"><button class="btn haupt" type="submit">Speichern</button></div></form>`;
    const form = seite.querySelector('form');
    form.onsubmit = async (ev) => {
      ev.preventDefault();
      const knopf = form.querySelector('[type=submit]');
      knopf.disabled = true;
      try {
        const werte = Object.fromEntries([...form.querySelectorAll('input[type=checkbox]')].map((e) => [e.name, e.checked ? 'aktiv' : 'inaktiv']));
        await speichereEinstellungen(werte, true);
        await laden();
      } catch (f) { knopf.disabled = false; melde(f.message); }
    };
  };
  seite.onclick = async (ev) => {
    const b = ev.target.closest('[data-a]');
    if (!b) return;
    b.disabled = true;
    try {
      switch (b.dataset.a) {
        case 'push-an': {
          if ((await Notification.requestPermission()) !== 'granted') { b.disabled = false; return melde(tx('Benachrichtigungen wurden im Browser nicht erlaubt')); }
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
        case 'push-test': await api('/api/push/test', { method: 'POST' }); b.disabled = false; return melde(tx('Test gesendet'));
      }
    } catch (f) { b.disabled = false; melde(f.message); }
  };
  aufraeumen = () => { seite.onclick = null; };
  await laden();
}

// Kalender-Abo: geheimer iCal-Link mit Anleitung für Google, Apple und Outlook.
async function zeigeKalender() {
  seite.innerHTML = T`${kontoReiter('kalender')}<p class="leer">${esc(tx('Lädt …'))}</p>`;
  const laden = async () => {
    const k = await api('/api/kalender');
    if (aktuell?.name !== 'kalender') return;
    const webcal = k.url.replace(/^https?:/, 'webcal:');
    seite.innerHTML = T`${kontoReiter('kalender')}<h1>${esc(tx('Kalender-Abo'))}</h1>
      <p class="leise">${esc(tx('Alle Termine aus den Notizen ({0}) als Kalender zum Abonnieren: neue und verschobene erscheinen von selbst, erledigte mit ✓, gelöschte verschwinden. Nur lesend – Änderungen im Kalender kommen nicht zurück.', k.termine))}</p>
      <div class="karte"><div class="leise">${esc(tx('Dein Link (geheim halten)'))}</div><code class="kopierbar" id="icallink">${esc(k.url)}</code>
        <div class="leiste"><button class="btn haupt" data-k="kopieren">${esc(tx('Link kopieren'))}</button><a class="btn" href="${esc(webcal)}">${esc(tx('In Kalender-App öffnen'))}</a></div></div>
      <h2>${esc(tx('Google Kalender'))}</h2><ol class="anleitung">
        <li>${esc(tx('Link oben kopieren.'))}</li>
        <li>${esc(tx('Am Computer calendar.google.com öffnen (in der Handy-App geht das nicht).'))}</li>
        <li>${esc(tx('Links bei „Weitere Kalender“ auf + → „Per URL“.'))}</li>
        <li>${esc(tx('Link einfügen → „Kalender hinzufügen“. Er erscheint als „{0}“ und danach auch auf dem Handy.', k.name))}</li></ol>
      <p class="leise">${esc(tx('Google holt abonnierte Kalender nur alle paar Stunden (bis zu einem Tag) neu – neue Termine erscheinen dort also mit Verzögerung. Erinnerungen kommen weiter als Benachrichtigung dieser App.'))}</p>
      <h2>${esc(tx('iPhone, Mac, Outlook'))}</h2>
      <p>${esc(tx('„In Kalender-App öffnen“ antippen und das Abo bestätigen – oder in der Kalender-App „Kalenderabo hinzufügen“ wählen und den Link einfügen.'))}</p>
      <h2>${esc(tx('Link ersetzen'))}</h2>
      <p class="leise">${esc(tx('Wer den Link kennt, sieht alle Termine. Ein neuer Link macht den alten sofort ungültig; danach im Kalender neu abonnieren.'))}</p>
      <div class="leiste"><button class="btn gefahr" data-k="neu">${esc(tx('Neuen Link erzeugen'))}</button></div>`;
  };
  aktuell.neuLaden = laden;
  await laden();
  seite.onclick = async (ev) => {
    const b = ev.target.closest('[data-k]');
    if (!b) return;
    if (b.dataset.k === 'kopieren') {
      const text = document.getElementById('icallink').textContent;
      try { await navigator.clipboard.writeText(text); melde(tx('Link kopiert')); } catch { melde(tx('Kopieren nicht möglich – Link markieren und kopieren')); }
      return;
    }
    if (b.dataset.k === 'neu') {
      if (!confirm(tx('Neuen Link erzeugen? Der bisherige Link funktioniert dann nicht mehr.'))) return;
      b.disabled = true;
      try { await api('/api/kalender/neu', { method: 'POST' }); melde(tx('Neuer Link erzeugt')); await laden(); } catch (f) { b.disabled = false; melde(f.message); }
    }
  };
  aufraeumen = () => { seite.onclick = null; };
}

// Anleitung: diesen Dienst als MCP-Server in Claude, ChatGPT oder Claude Code eintragen.
// App verbinden: geführt in einer Seite – erst die App wählen, dann nur deren Schritte; braucht sie ein API-Token,
// wird es hier mit Namen angelegt und gleich in Befehl und Beispiele eingesetzt.
const VERBINDEN_APPS = [['claude', 'Claude'], ['chatgpt', 'ChatGPT'], ['claudecode', 'Claude Code'], ['andere', 'Andere MCP-App']];
function zeigeMcpAnleitung() {
  const wahl = new URLSearchParams(location.search).get('app') || '';
  const mcp = `${location.origin}/api/v1/mcp`, basis = `${location.origin}/api`;
  let token = '';
  const code = (id, text) => T`<div class="karte"><code class="kopierbar" id="${id}">${esc(text)}</code><div class="leiste"><button class="btn klein" data-kopieren="${id}">${esc(tx('Kopieren'))}</button></div></div>`;
  // Token für die gewählte App automatisch mit beschreibendem Namen (App und Datum); umbenennen ist nicht nötig.
  const tokenKnopf = (text) => T`<div class="leiste"><button class="btn haupt" data-tokenfuer="${esc(text)}">${esc(tx('Token erzeugen und einsetzen'))}</button></div>`;
  const fertig = T`<div class="leiste"><a class="btn haupt" href="/konto">${esc(tx('Fertig'))}</a></div>`;
  const schritte = () => {
    const t = token || '<API-Token>';
    const tokenHinweis = token ? T`<div class="hinweis warn">${esc(tx('Das Token steht nur jetzt im Klartext – gleich kopieren. Löschen jederzeit unter Benutzer.'))}</div>` : '';
    const oauth = (liste) => T`<ol class="anleitung">${liste.map((x) => T`<li>${esc(tx(x))}</li>`).join('')}</ol>
      <p class="leise">${esc(tx('Nach der Freigabe steht die App unter Benutzer → Apps und Zugänge.'))}</p>${fertig}`;
    switch (wahl) {
      case 'claude': return T`<div class="leise">${esc(tx('Adresse des MCP-Servers'))}</div>${code('mcpurl', mcp)}${oauth(['Auf claude.ai Einstellungen → Konnektoren öffnen.',
        '„Benutzerdefinierten Konnektor hinzufügen“, Name frei wählen, Adresse oben einfügen. Client-ID und Secret leer lassen.',
        '„Verbinden“: Diese App öffnet sich – mit Passkey anmelden und „Erlauben“ tippen.',
        'Der Konnektor gilt danach auch in der Desktop- und Handy-App; im Chat über das Werkzeug-Menü einschalten.'])}`;
      case 'chatgpt': return T`<div class="leise">${esc(tx('Adresse des MCP-Servers'))}</div>${code('mcpurl', mcp)}${oauth(['Einstellungen → Apps & Konnektoren → Erweitert: Entwicklermodus einschalten.',
        '„Erstellen“, Adresse oben einfügen, Authentifizierung OAuth.', 'Mit Passkey anmelden und „Erlauben“ tippen.'])}`;
      case 'claudecode': return T`<h2>${esc(tx('Mit Browser (empfohlen)'))}</h2>
        <p class="leise">${esc(tx('Im Terminal ausführen; beim ersten Aufruf öffnet sich die Freigabe im Browser.'))}</p>${code('mcpbefehl', `claude mcp add --transport http sprachnotizen ${mcp}`)}
        <h2>${esc(tx('Ohne Browser (Server, SSH)'))}</h2>
        ${token ? T`${tokenHinweis}${code('mcptoken', `claude mcp add --transport http sprachnotizen ${mcp} --header "Authorization: Bearer ${t}"`)}` : T`<p class="leise">${esc(tx('Ein eigenes Token für Claude Code wird angelegt und gleich in den Befehl eingesetzt.'))}</p>${tokenKnopf('Claude Code')}`}${fertig}`;
      case 'andere': return T`<p class="leise">${esc(tx('Jede App mit MCP über Streamable HTTP. Kann sie OAuth, reicht die Adresse – sie fragt dann hier nach Freigabe. Sonst ein eigenes Token als Kopfzeile mitgeben.'))}</p>
        <div class="leise">${esc(tx('Adresse des MCP-Servers'))}</div>${code('mcpurl', mcp)}
        ${token ? T`${tokenHinweis}<div class="leise">${esc(tx('Kopfzeile'))}</div>${code('kopf', `Authorization: Bearer ${t}`)}` : tokenKnopf('MCP-App')}${fertig}`;
      default: return T`<p class="leise">${esc(tx('Welche App willst du verbinden?'))}</p>`;
    }
  };
  const male = () => {
    seite.innerHTML = T`<p><a class="btn klein" href="/konto">← ${esc(tx('Benutzer'))}</a></p><h1>${esc(tx('App verbinden'))}</h1>
      <p class="leise">${esc(tx('KI-Apps und Skripte lesen deine Listen, Notizen und Termine und haken Punkte ab – nur deine Daten.'))}</p>
      <nav class="btn-group unter" aria-label="${esc(tx('App'))}">${VERBINDEN_APPS.map(([k, n]) => T`<a class="btn${k === wahl ? ' aktiv' : ''}" href="/konto/verbinden?app=${k}"${k === wahl ? ' aria-current="page"' : ''}>${esc(tx(n))}</a>`).join('')}</nav>
      ${schritte()}`;
  };
  male();
  seite.onclick = async (ev) => {
    const tk = ev.target.closest('[data-tokenfuer]');
    if (tk) {
      tk.disabled = true;
      const name = `${tk.dataset.tokenfuer} (${new Date().toLocaleDateString()})`;
      try { token = (await api('/api/zugang/api', { method: 'POST', body: { name } })).token; male(); } catch (e) { tk.disabled = false; melde(e.message); }
      return;
    }
    const b = ev.target.closest('[data-kopieren]');
    if (!b) return;
    try { await navigator.clipboard.writeText(document.getElementById(b.dataset.kopieren).textContent); melde(tx('Kopiert')); } catch { melde(tx('Kopieren nicht möglich – Text markieren und kopieren')); }
  };
  aufraeumen = () => { seite.onclick = null; };
}

// Eigenes API-Token für Skripte und Werkzeuge: Name wählen, Token einmal zeigen, dazu Adressen und Beispiele.
function zeigeTokenNeu() {
  const mcp = `${location.origin}/api/v1/mcp`, basis = `${location.origin}/api`;
  const code = (id, text) => T`<div class="karte"><code class="kopierbar" id="${id}">${esc(text)}</code><div class="leiste"><button class="btn klein" data-kopieren="${id}">${esc(tx('Kopieren'))}</button></div></div>`;
  const kopf = T`<p><a class="btn klein" href="/konto">← ${esc(tx('Benutzer'))}</a></p><h1>${esc(tx('Eigenes API-Token'))}</h1>`;
  seite.innerHTML = T`${kopf}<p class="leise">${esc(tx('Für Skripte und Werkzeuge: Listen und Einträge lesen, Punkte abhaken, MCP. Gilt nur für deine Daten; je Skript ein eigenes Token.'))}</p>
    <form class="einstellungen" id="tokenform" novalidate><label for="t-name">${esc(tx('Name des Tokens'))}</label>
      <input id="t-name" name="name" maxlength="40" autocomplete="off" required>
      <p class="leise hilfe">${esc(tx('Woran du es später erkennst, z. B. das Skript oder der Rechner.'))}</p>
      <div class="leiste"><a class="btn" href="/konto">Abbrechen</a><button class="btn haupt" type="submit">${esc(tx('Token erzeugen'))}</button></div></form>`;
  const f = document.getElementById('tokenform');
  f.onsubmit = async (ev) => {
    ev.preventDefault();
    const knopf = f.querySelector('button[type=submit]');
    knopf.disabled = true;
    let r;
    try { r = await api('/api/zugang/api', { method: 'POST', body: { name: f.name.value } }); } catch (e) { knopf.disabled = false; melde(e.message); return; }
    seite.innerHTML = T`${kopf}<div class="hinweis warn">${esc(tx('Das Token steht nur jetzt im Klartext – gleich kopieren. Löschen jederzeit unter Benutzer.'))}</div>
      <div class="leise">${esc(tx('Token „{0}“', r.name))}</div>${code('tok', r.token)}
      <h2>${esc(tx('So wird es benutzt'))}</h2>
      <p class="leise">${esc(tx('Jede Anfrage trägt die Kopfzeile „Authorization: Bearer <Token>“.'))}</p>
      <div class="leise">REST-API</div>${code('basis', basis)}
      ${code('bsp1', `curl -H "Authorization: Bearer ${r.token}" ${basis}/listen`)}
      ${code('bsp2', `curl -X POST -H "Authorization: Bearer ${r.token}" ${basis}/listenpunkte/<id>/erledigt`)}
      <div class="leise">MCP</div>${code('mcpurl', mcp)}
      <div class="leiste"><a class="btn haupt" href="/konto">${esc(tx('Fertig'))}</a></div>`;
  };
  seite.onclick = async (ev) => {
    const b = ev.target.closest('[data-kopieren]');
    if (!b) return;
    try { await navigator.clipboard.writeText(document.getElementById(b.dataset.kopieren).textContent); melde(tx('Kopiert')); } catch { melde(tx('Kopieren nicht möglich – Text markieren und kopieren')); }
  };
  aufraeumen = () => { seite.onclick = null; };
}

// Einen Wert der Konfiguration ändern (eigene Seite, Formular mit nur diesem Feld).
async function zeigeEinstellungBearbeiten(key) {
  // Zurück dorthin, woher man kam (z. B. Updates → Uhrzeit), sonst zur Konfiguration.
  const zurueckParam = new URLSearchParams(location.search).get('zurueck') || '';
  const zurueck = /^\/status\/[a-z/]*$/.test(zurueckParam) ? zurueckParam : '/status/einstellungen';
  seite.innerHTML = T`${statusReiter('einstellungen')}<p class="leer">Lädt …</p>`;
  const d = await ladeEinstellungen();
  if (aktuell?.name !== 'einstellungBearbeiten') return;
  const f = d.felder.find((x) => x.key === key);
  if (!f) { seite.innerHTML = T`${statusReiter('einstellungen')}<div class="hinweis schlecht">Unbekannte Einstellung.</div>`; return; }
  // Transkription und Whisper-Modell gehören zusammen: beide auf einer Seite.
  const felder = key === 'TRANSCRIPTION' || key.startsWith('WHISPER_') ? d.felder.filter((x) => x.key === 'TRANSCRIPTION' || x.key.startsWith('WHISPER_')) : [f];
  seite.innerHTML = T`${statusReiter('einstellungen')}<h1>${esc(tx(felder.length > 1 ? f.gruppe : f.name))}</h1>${felder.length > 1 ? '' : T`<p class="leise">${esc(tx(f.gruppe))}</p>`}
    ${einstellungsFormular(felder, { assistent: false, whisper: d.whisper, whisperLoeschen: true }).replace(/<fieldset><legend>[^<]*<\/legend>/, '<fieldset>')}
    <div class="leiste"><a class="btn" href="${zurueck}">Abbrechen</a></div>`;
  const form = seite.querySelector('form');
  bindeWhisper(form);
  // Geladene, nicht gewählte Whisper-Modelle löschen (Bearbeitungsseite, mit Rückfrage).
  form.onclick = async (ev) => {
    const b = ev.target.closest('[data-whisper-loeschen]');
    if (!b || !confirm(tx('Modell {0} löschen? Es lässt sich später wieder laden.', b.dataset.whisperLoeschen))) return;
    b.disabled = true;
    try {
      await api(T`/api/whisper/modelle/${b.dataset.whisperLoeschen}`, { method: 'DELETE' });
      melde(tx('Gelöscht'));
      zeigeEinstellungBearbeiten(key);
    } catch (f) { b.disabled = false; melde(f.message); }
  };
  form.querySelector('input,select')?.focus();
  form.onsubmit = async (ev) => {
    ev.preventDefault();
    const werte = einstellungsWerte(form);
    if (key === 'PUBLIC_URL' && werte.PUBLIC_URL !== f.wert && !confirm(tx('Die öffentliche Adresse ändert sich: alle Passkeys werden ungültig. Trotzdem speichern?'))) return;
    const knopf = form.querySelector('[type=submit]');
    knopf.disabled = true;
    try {
      await speichereEinstellungen(werte, !!f.persoenlich);
      if (key === 'LANGUAGE') { location.replace(zurueck); return; }
      await ladeZaehler();
      geheZu(zurueck, true);
    } catch (fehler) {
      knopf.disabled = false;
      melde(fehler.message);
    }
  };
}


// ---------- Nutzer: Willkommen, Wiederherstellen, Zugang, Verwaltung (Admin) ----------

// Willkommen: nach der Einrichtung oder dem Umzug auf 3.0 – Name (falls noch leer) und einmalig der Wiederherstellungscode.
async function zeigeWillkommen() {
  seite.innerHTML = T('<p class="leer">Lädt …</p>');
  const d = await api('/api/wiederherstellungscode');
  if (aktuell?.name !== 'willkommen') return;
  if (!d.code) { geheZu('/', true); return; }
  seite.innerHTML = T`<h1>${d.name ? esc(tx('Hallo {0}', d.name)) : esc(tx('Willkommen'))}</h1>
    <p>Deine Notizen, Listen und Aufnahmen liegen verschlüsselt mit einem eigenen Schlüssel. Fehlt dieser Schlüssel einmal (z. B. nach einem Umzug auf einen neuen Server), öffnet nur dieser Code deine Daten wieder.</p>
    <div class="karte"><div class="leise">Dein Wiederherstellungscode</div><code class="kopierbar" id="wcode">${esc(d.code)}</code>
      <div class="leiste"><button class="btn" type="button" data-kopieren>Kopieren</button></div></div>
    ${d.system ? T('<p class="leise">Derselbe Code stellt auch das System aus einem Backup wieder her (API-Schlüssel, Benachrichtigungen). Einen eigenen Code dafür gibt es unter Backups → Systemschlüssel.</p>') : ''}
    ${d.systemcode ? T`<p>Das System hat einen eigenen Schlüssel (mehrere Admins). Er schützt API-Schlüssel und Benachrichtigungen im Backup; dieser Code holt ihn auf einem neuen Server zurück.</p>
      <div class="karte"><div class="leise">Code des Systemschlüssels</div><code class="kopierbar">${esc(d.systemcode)}</code>
      <div class="leiste"><button class="btn" type="button" data-kopieren="system">Kopieren</button></div></div>` : ''}
    <div class="hinweis warn">${d.systemcode ? tx('Schreib beide Codes auf oder leg sie in deinen Passwort-Manager. Sie werden nur jetzt angezeigt.') : tx('Schreib ihn auf oder leg ihn in deinen Passwort-Manager. Er wird nur jetzt angezeigt.')}</div>
    <form class="einstellungen" novalidate>
      ${d.name ? '' : T('<label for="w-name">Dein Name *</label><input id="w-name" name="name" maxlength="40" autocomplete="given-name" required>')}
      <label class="schalter"><input type="checkbox" name="notiert"> ${d.systemcode ? tx('Ich habe beide Codes sicher notiert') : tx('Ich habe den Code sicher notiert')}</label>
      <div class="leiste"><button class="btn haupt" type="submit">Weiter</button></div></form>`;
  seite.querySelectorAll('[data-kopieren]').forEach((b) => { b.onclick = async () => {
    try { await navigator.clipboard.writeText(b.dataset.kopieren === 'system' ? d.systemcode : d.code); melde(tx('Code kopiert')); } catch { melde(tx('Kopieren nicht möglich – Code markieren und kopieren')); }
  }; });
  const form = seite.querySelector('form');
  form.onsubmit = async (ev) => {
    ev.preventDefault();
    if (!form.notiert.checked) { melde(tx('Bitte bestätigen, dass der Code notiert ist')); return; }
    const knopf = form.querySelector('[type=submit]');
    knopf.disabled = true;
    try {
      await api('/api/wiederherstellungscode/notiert', { method: 'POST', body: { name: form.name?.value || '', notiert: true } });
      const s = await api('/api/sitzung');
      merkeIch(s);
      maleMenue();
      geheZu('/', true);
    } catch (f) { knopf.disabled = false; melde(f.message); }
  };
}

// Wiederherstellen: die eigenen Daten sind gesperrt (Schlüsseldatei fehlt) – mit dem Code wieder öffnen.
async function zeigeWiederherstellen() {
  const s = await api('/api/sitzung').catch(() => ({}));
  if (aktuell?.name !== 'wiederherstellen') return;
  if (!s.gesperrt) { geheZu('/', true); return; }
  if (s.gesperrt === 'gesperrt') {
    seite.innerHTML = T`<h1>Zugang gesperrt</h1><div class="hinweis schlecht">Dein Zugang ist vom Admin gesperrt. Bitte wende dich an ihn.</div>
      <div class="leiste"><button class="btn" data-menue-abmelden-seite>Abmelden</button></div>`;
  } else {
    seite.innerHTML = T`<h1>Daten wiederherstellen</h1>
      <p>Der Schlüssel zu deinen Daten fehlt auf diesem Server (z. B. nach dem Wiederherstellen eines Backups auf einem neuen Server). Gib deinen Wiederherstellungscode ein, um sie wieder zu öffnen.</p>
      <form class="einstellungen" novalidate><label for="r-code">Wiederherstellungscode</label>
        <input id="r-code" name="code" autocomplete="off" spellcheck="false" placeholder="XXXXX-XXXXX-XXXXX-XXXXX-XXXXXX">
        <div class="leiste"><button class="btn haupt" type="submit">Öffnen</button></div></form>`;
    const form = seite.querySelector('form');
    form.onsubmit = async (ev) => {
      ev.preventDefault();
      const knopf = form.querySelector('[type=submit]');
      knopf.disabled = true;
      try {
        await api('/api/wiederherstellen', { method: 'POST', body: { code: form.code.value } });
        melde(tx('Daten wieder geöffnet'));
        geheZu('/', true);
      } catch (f) { knopf.disabled = false; melde(f.message); }
    };
  }
  const ab = seite.querySelector('[data-menue-abmelden-seite]');
  if (ab) ab.onclick = async () => { await abmelden().catch(() => vergissGeraet()); geheZu('/anmelden', true); };
}

// Benutzer: alles, was dem angemeldeten Nutzer selbst gehört – Passkeys, verbundene Apps (MCP), Kalender-Abo,
// API-Token und Wiederherstellungscode; Abmelden. Ansicht mit Aktionen, Bearbeiten auf eigenen Seiten.
async function zeigeKonto() {
  seite.innerHTML = T`${kontoReiter('konto')}<p class="leer">Lädt …</p>`;
  const laden = async () => {
    const [g, o, t] = await Promise.all([api('/api/geraete'), api('/api/oauth/apps').catch(() => ({ apps: [] })), api('/api/zugang/api')]);
    if (aktuell?.name !== 'konto') return;
    seite.innerHTML = T`${kontoReiter('konto')}<h1>Benutzer</h1>
      <p class="leise">${esc(tx('Angemeldet als {0} ({1}).', ich.name || '?', tx(ROLLE[ich.rolle] || ich.rolle || '')))}</p>
      <h2>Geräte (Passkeys)</h2>
      ${g.geraete.map((d) => T`<div class="karte"><div class="zeile"><span><strong>${esc(d.name)}</strong>${d.dieses ? T(' <span class="badge neutral">dieses Gerät</span>') : ''}</span>
        ${g.geraete.length > 1 && !d.dieses ? T`<button class="btn klein gefahr" data-geraet="${esc(d.id)}">Entfernen</button>` : ''}</div>
        <div class="leise">angelegt ${zeit(d.angelegt)}${d.zuletzt ? T` · zuletzt angemeldet ${zeit(d.zuletzt)}` : ''}</div></div>`).join('')}
      <div class="leiste"><a class="btn" href="/konto/geraet">Weiteres Gerät hinzufügen</a></div>
      <h2>${esc(tx('Apps und Zugänge'))}</h2>
      <p class="leise">${esc(tx('Je App oder Skript ein eigener Zugang – einzeln trennbar. Gilt nur für deine Daten.'))}</p>
      ${o.apps.map((x) => T`<div class="karte"><div class="zeile"><span><strong>${esc(x.name)}</strong> <span class="badge neutral">${esc(tx('per Freigabe'))}</span></span><button class="btn klein gefahr" data-app="${esc(x.id)}">Trennen</button></div>
        <div class="leise">verbunden ${zeit(x.angelegt)}${x.zuletzt ? T` · zuletzt genutzt ${zeit(x.zuletzt)}` : ''}</div></div>`).join('')}
      ${t.tokens.map((x) => T`<div class="karte"><div class="zeile"><span><strong>${esc(x.name)}</strong> <span class="badge neutral">${esc(tx('API-Token'))}</span></span><button class="btn klein gefahr" data-token="${esc(x.id)}" data-name="${esc(x.name)}">Löschen</button></div>
        <div class="leise">angelegt ${zeit(x.angelegt)}${x.zuletzt ? T` · zuletzt genutzt ${zeit(x.zuletzt)}` : tx(' · noch nie genutzt')}</div></div>`).join('')}
      ${o.apps.length || t.tokens.length ? '' : T`<p class="leise">${esc(tx('Noch keine App verbunden.'))}</p>`}
      <div class="leiste"><a class="btn haupt" href="/konto/verbinden">${esc(tx('App verbinden'))}</a><a class="btn" href="/konto/token">${esc(tx('Eigenes Token anlegen'))}</a></div>
      <h2>Wiederherstellungscode</h2>
      <p class="leise">Öffnet deine verschlüsselten Daten, falls der Schlüssel auf dem Server fehlt. Ein neuer Code verschlüsselt alle Daten neu und macht den alten ungültig.</p>
      <div class="leiste"><a class="btn" href="/konto/code">Neuen Code erzeugen</a></div>
      <div class="leiste"><button class="btn gefahr" data-a="abmelden">Abmelden</button></div>`;
  };
  aktuell.neuLaden = laden;
  await laden();
  seite.onclick = async (ev) => {
    const b = ev.target.closest('button');
    if (!b) return;
    try {
      if (b.dataset.a === 'abmelden') { await abmelden(); return geheZu('/anmelden', true); }
      if (b.dataset.app) {
        if (!confirm(tx('App trennen? Sie verliert sofort den Zugriff und muss neu freigegeben werden.'))) return;
        await api(T`/api/oauth/apps/${b.dataset.app}`, { method: 'DELETE' });
        return laden();
      }
      if (b.dataset.geraet) {
        if (!confirm(tx('Gerät entfernen? Es kann sich danach nicht mehr anmelden.'))) return;
        await api(T`/api/geraete/${b.dataset.geraet}`, { method: 'DELETE' });
        return laden();
      }
      if (b.dataset.token) {
        if (!confirm(tx('Token „{0}“ löschen? Die App oder das Skript damit hat sofort keinen Zugriff mehr.', b.dataset.name))) return;
        await api(T`/api/zugang/api/${b.dataset.token}`, { method: 'DELETE' });
        return laden();
      }
    } catch (f) { b.disabled = false; melde(f.message); }
  };
  aufraeumen = () => { seite.onclick = null; };
}

// Neuer Wiederherstellungscode: erst bestätigen, dann neuer Schlüssel – alle Daten werden neu verschlüsselt
// (Fortschrittsbalken); danach zeigt Willkommen den Code einmal. Unterbrochene Läufe lassen sich fortsetzen.
const UMSCHLUESSEL_PHASE = { vorbereiten: 'Vorbereiten …', medien: 'Aufnahmen und Bilder', datenbank: 'Datenbank', abschluss: 'Abschluss', fertig: 'Fertig' };
async function zeigeNeuerCode() {
  seite.innerHTML = T('<p class="leer">Lädt …</p>');
  let uhr = null;
  aufraeumen = () => clearTimeout(uhr);
  const fortschritt = (st) => {
    const prozent = st.gesamt ? Math.round((st.erledigt / st.gesamt) * 100) : 0;
    seite.innerHTML = T`<h1>Daten werden neu verschlüsselt</h1>
      <p class="leise">Bitte warten. Solange sind deine Daten gesperrt; neue Aufnahmen der Uhr werden abgewiesen.</p>
      <div class="karte"><div class="zeile"><strong>${esc(tx(UMSCHLUESSEL_PHASE[st.phase] || st.phase))}</strong><span>${st.gesamt ? esc(`${prozent} %`) : ''}</span></div>
        <meter class="balken" min="0" max="${st.gesamt || 1}" value="${st.erledigt || 0}" aria-label="${esc(tx('{0} von {1} erledigt', st.erledigt || 0, st.gesamt || 0))}"></meter>
        <div class="leise">${st.gesamt ? esc(tx('{0} von {1} (Dateien und Datenbank)', st.erledigt, st.gesamt)) : ''}${st.unlesbar ? esc(tx(' · {0} nicht lesbar – bleiben unverändert', st.unlesbar)) : ''}</div></div>`;
  };
  const fehler = (st) => {
    seite.innerHTML = T`<h1>Neuverschlüsselung unterbrochen</h1>
      <div class="hinweis schlecht">${esc(st.fehler || tx('Unbekannter Fehler'))}</div>
      <p class="leise">Bisher Erledigtes bleibt; Fortsetzen macht beim nächsten offenen Schritt weiter. Der alte Code gilt bis zum Abschluss.</p>
      <div class="leiste"><button class="btn haupt" data-weiter>Fortsetzen</button></div>`;
    seite.querySelector('[data-weiter]').onclick = async (ev) => {
      ev.target.disabled = true;
      try { await api('/api/wiederherstellungscode/neu/weiter', { method: 'POST' }); beobachte(); } catch (f) { ev.target.disabled = false; melde(f.message); }
    };
  };
  const beobachte = async () => {
    clearTimeout(uhr);
    let st;
    try { st = await api('/api/wiederherstellungscode/neu'); } catch (f) { if (aktuell?.name === 'zugangCode') uhr = setTimeout(beobachte, 2000); return; }
    if (aktuell?.name !== 'zugangCode') return;
    if (st.phase === 'fertig' && !st.laeuft) { geheZu('/willkommen', true); return; }
    if (st.phase === 'fehler') { fehler(st); return; }
    if (st.laeuft) { fortschritt(st); uhr = setTimeout(beobachte, 700); return; }
    bestaetigen();
  };
  const bestaetigen = () => {
    seite.innerHTML = T`<p><a class="btn klein" href="/konto">← ${esc(tx('Benutzer'))}</a></p><h1>Neuer Wiederherstellungscode</h1>
      <p>Deine Daten bekommen einen neuen Schlüssel und werden komplett neu verschlüsselt. Der bisherige Code gilt danach nicht mehr; den neuen siehst du nur einmal.</p>
      <ul class="leise"><li>Dauert je nach Menge der Aufnahmen einige Sekunden bis Minuten; so lange sind deine Daten gesperrt.</li>
        <li>Ältere Backups öffnet weiterhin nur der alte Code. Danach wird gleich ein neues Backup angelegt.</li></ul>
      <div class="leiste"><a class="btn" href="/konto">Abbrechen</a><button class="btn haupt" data-neu>Neu verschlüsseln</button></div>`;
    seite.querySelector('[data-neu]').onclick = async (ev) => {
      ev.target.disabled = true;
      try { await api('/api/wiederherstellungscode/neu', { method: 'POST' }); fortschritt({ phase: 'vorbereiten', erledigt: 0, gesamt: 0 }); uhr = setTimeout(beobachte, 300); } catch (f) { ev.target.disabled = false; melde(f.message); }
    };
  };
  await beobachte();
}

const ROLLE = { admin: 'Admin', user: 'Nutzer' };
const SPERRE = { gesperrt: 'gesperrt', schluessel: 'Schlüssel fehlt', fehler: 'Daten nicht lesbar' };

// Nutzer (Admin): Liste mit Rolle, letzter Anmeldung, Kosten und Speicher – nie Inhalte.
async function zeigeNutzer() {
  seite.innerHTML = T('<p class="leer">Lädt …</p>');
  const [d, ei] = await Promise.all([api('/api/admin/nutzer'), api('/api/admin/einladungen')]);
  if (aktuell?.name !== 'nutzer') return;
  seite.innerHTML = T`<h1>Nutzer</h1>
    <p class="leise">Jeder sieht nur seine eigenen Daten – auch Admins nicht die der anderen. Hier stehen nur Rolle, Anmeldung und Verbrauch.</p>
    ${d.nutzer.map((u) => T`<div class="karte"><div class="zeile"><span><strong>${esc(u.name || tx('(ohne Namen)'))}</strong> <span class="badge neutral">${esc(tx(ROLLE[u.rolle] || u.rolle))}</span>${u.id === d.ich ? T(' <span class="badge neutral">du</span>') : ''}${u.gesperrt ? T` <span class="badge fehler">${esc(tx(SPERRE[u.gesperrt] || u.gesperrt))}</span>` : ''}${u.passkey_fehlt ? T` <span class="badge fehler">Neue Adresse – neu einladen</span>` : ''}</span>
        <a class="konfedit" href="/admin/nutzer/${u.id}/bearbeiten" aria-label="${esc(tx('{0} bearbeiten', u.name))}">${STIFT}</a></div>
      <div class="leise">${u.zuletzt ? T`zuletzt angemeldet ${zeit(u.zuletzt)}` : tx('noch nie angemeldet')} · KI-Kosten 30 Tage: ${u.kosten_30_tage != null ? esc(u.kosten_30_tage.toFixed(2)) + ' $' : '–'} · Daten ${groesse(u.daten_bytes)} · Aufnahmen ${groesse(u.audio_bytes)}</div></div>`).join('')}
    ${ei.einladungen.length ? T`<h2>Offene Einladungen</h2>${ei.einladungen.map((e) => T`<div class="karte"><div class="zeile"><span><strong>${e.name ? esc(e.name) : tx('Neuer Nutzer')}</strong> <span class="badge neutral">${esc(tx(ROLLE[e.rolle] || e.rolle))}</span></span>
        <a class="btn klein" href="/admin/nutzer/einladung/${e.id}">Code anzeigen</a></div>
      <div class="leise">gültig bis ${zeit(e.bis)}</div></div>`).join('')}` : ''}
    <div class="leiste"><a class="btn haupt" href="/admin/nutzer/neu">Nutzer einladen</a></div>`;
}

// Einladen (Admin): nur die Rolle; danach zeigt die Einladung ihren 6-stelligen Code (5 Minuten) und den Link.
// Den Namen gibt der neue Nutzer beim Anlegen seines Passkeys selbst ein.
let letzteEinladung = null; // { id, link } – der Link ist nur direkt nach dem Erzeugen bekannt
async function zeigeNutzerNeu() {
  seite.innerHTML = T`<p><a class="btn klein" href="/admin/nutzer">← Nutzer</a></p><h1>Nutzer einladen</h1>
    <form class="einstellungen" novalidate>
      <label for="n-rolle">Rolle</label><select id="n-rolle" name="rolle"><option value="user">${esc(tx('Nutzer'))}</option><option value="admin">${esc(tx('Admin'))}</option></select>
      <p class="leise hilfe">Admins verwalten zusätzlich Updates, Backups, Systemeinstellungen und Nutzer – die Daten anderer sehen auch sie nicht. Den Namen gibt der neue Nutzer selbst ein.</p>
      <div class="leiste"><a class="btn" href="/admin/nutzer">Abbrechen</a><button class="btn haupt" type="submit">Einladung erzeugen</button></div></form>`;
  const form = seite.querySelector('form');
  form.onsubmit = async (ev) => {
    ev.preventDefault();
    const knopf = form.querySelector('[type=submit]');
    knopf.disabled = true;
    try {
      const r = await api('/api/admin/nutzer/einladung', { method: 'POST', body: { rolle: form.rolle.value } });
      letzteEinladung = { id: r.id, link: r.link };
      geheZu(T`/admin/nutzer/einladung/${r.id}`, true);
    } catch (f) { knopf.disabled = false; melde(f.message); }
  };
}

// Nutzer bearbeiten (Admin): Name, Rolle, Sperre; Löschen mit Bestätigung durch den Namen.
async function zeigeNutzerBearbeiten(id) {
  seite.innerHTML = T('<p class="leer">Lädt …</p>');
  const [u, liste] = await Promise.all([api(`/api/admin/nutzer/${id}`), api('/api/admin/nutzer')]);
  if (aktuell?.name !== 'nutzerBearbeiten') return;
  const selbst = u.id === liste.ich;
  seite.innerHTML = T`<p><a class="btn klein" href="/admin/nutzer">← Nutzer</a></p><h1>${esc(u.name || tx('Nutzer'))}</h1>
    ${u.passkey_fehlt ? T`<div class="hinweis warn">Neue Adresse: ${esc(u.name)} hat noch keinen Passkey dafür.</div><div class="leiste"><a class="btn haupt" href="/admin/nutzer/${u.id}/code">Neu einladen (Code)</a></div>` : ''}
    <form class="einstellungen" novalidate><label for="b-name">Name *</label><input id="b-name" name="name" maxlength="40" value="${esc(u.name)}" required>
      <label for="b-rolle">Rolle</label><select id="b-rolle" name="rolle"><option value="user"${u.rolle === 'user' ? ' selected' : ''}>${esc(tx('Nutzer'))}</option><option value="admin"${u.rolle === 'admin' ? ' selected' : ''}>${esc(tx('Admin'))}</option></select>
      ${selbst ? '' : T`<label class="schalter"><input type="checkbox" name="gesperrt"${u.gesperrt === 'gesperrt' ? ' checked' : ''}> Gesperrt (keine Anmeldung, keine Verarbeitung; Daten bleiben)</label>`}
      <div class="leiste"><a class="btn" href="/admin/nutzer">Abbrechen</a><button class="btn haupt" type="submit">Speichern</button></div></form>
    ${selbst ? T`<a class="karte" href="/konto"><strong>${esc(tx('Deine Benutzereinstellungen'))}</strong><br><span class="leise">${esc(tx('Passkeys, Apps und Zugänge, Pebble-App, Push, Kalender, Theme und Wiederherstellungscode'))}</span></a>` : T`<h2>Löschen</h2><p class="leise">Löscht Daten, Aufnahmen und Schlüssel dieses Nutzers unwiderruflich. Zur Bestätigung den Namen eingeben.</p>
      <form class="einstellungen" id="loeschen" novalidate><label for="l-name">Name</label><input id="l-name" name="bestaetigung" autocomplete="off">
        <div class="leiste"><button class="btn gefahr" type="submit">Endgültig löschen</button></div></form>`}`;
  const form = seite.querySelector('form');
  form.onsubmit = async (ev) => {
    ev.preventDefault();
    const knopf = form.querySelector('[type=submit]');
    knopf.disabled = true;
    const body = { name: form.name.value, rolle: form.rolle.value };
    if (form.gesperrt) body.gesperrt = form.gesperrt.checked;
    try { await api(`/api/admin/nutzer/${id}`, { method: 'PUT', body }); melde(tx('Gespeichert')); geheZu('/admin/nutzer', true); } catch (f) { knopf.disabled = false; melde(f.message); }
  };
  const loeschen = document.getElementById('loeschen');
  if (loeschen) loeschen.onsubmit = async (ev) => {
    ev.preventDefault();
    if (!confirm(tx('{0} endgültig löschen?', u.name))) return;
    try { await api(`/api/admin/nutzer/${id}`, { method: 'DELETE', body: { bestaetigung: loeschen.bestaetigung.value } }); melde(tx('Gelöscht')); geheZu('/admin/nutzer', true); } catch (f) { melde(f.message); }
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
  const KANAL_TEXT = { stable: tx('stable – Langzeit-Versionen'), beta: tx('beta – neue Funktionen, noch nicht ausgereift; zeigt auch stable'), nightly: tx('nightly – jede Änderung, ungetestet; zeigt auch beta und stable') };
  let u = null;
  // Updates und Backups sind eigene Menüpunkte; beide nutzen dieselbe Antwort von /api/update.
  const reiter = teil === 'backups' ? 'backups' : 'updates', meinName = teil === 'backups' ? 'backups' : 'update';
  let alle = false; // „Mehr“: alle gespeicherten Versionen des Kanals (samt mit angebotener Kanäle) statt der letzten 3
  const zeigeLog = (titel, text) => zeigeAenderungen(titel, text, u.alle_aenderungen);
  const laden = async (pruefen) => {
    u = await api(`/api/update${pruefen ? '?pruefen=1' : ''}`);
    if (aktuell?.name !== meinName) return;
    const liste = u.versionen || [];
    const laeuft = /^(requested|running)/.test(u.status || '');
    // Installiert ist eine Version aus einem Kanal, den der gewählte nicht mit anbietet (oder ein eigener Build): die neueste des gewählten Kanals ist der Wechsel.
    const wechsel = liste.length > 0 && !(u.kanal_umfasst || [u.kanal]).includes(u.aktuell_kanal);
    const status = !u.status ? '' : u.status.startsWith('error') ? T`<div class="hinweis schlecht">Letztes Update: ${esc(u.status)}</div>`
      : laeuft ? T`<div class="hinweis warn">Update läuft (${esc(u.status)}) …</div>` : '';
    const zeile = (r) => {
      const neueste = r === liste[0];
      const knopf = u.installierbar && !r.installiert
        ? T`<button class="btn klein${neueste ? ' haupt' : ''}" data-installieren="${esc(r.version)}"${laeuft ? ' disabled' : ''}>Installieren</button>` : '';
      return T`<div class="karte versionzeile"><div class="versionkopf"><span class="versionzeile1"><span class="badge neutral">${esc(tx(r.kanal))}</span><strong>${esc(versionNr(r.version))}</strong></span>
          ${r.installiert ? T('<span class="badge fertig">installiert</span>') : ''}${neueste ? '<span class="badge neutral">latest</span>' : ''}${r.neu ? '<span class="badge in_arbeit">new</span>' : ''}${r.vorhanden && !r.installiert ? T('<span class="badge neutral" title="Liegt lokal vor, Wechsel ohne Download">lokal</span>') : ''}
          <span class="leise">${esc(zeitText(r.veroeffentlicht) || r.datum || '')}</span></div>
        <div class="versionknoepfe"><button class="btn klein" data-log="${esc(r.version)}">Änderungen</button>${knopf}</div></div>`;
    };
    const kopf = T`<h1>${reiter === 'backups' ? tx('Backups') : tx('Updates')}</h1>
      ${status}${u.fehler ? T`<div class="hinweis warn">${esc(u.fehler)}</div>` : ''}`;
    // Neueste Version des gewählten Kanals, die später als die installierte erschienen ist: eigene Karte über „Installiert“.
    const neu = liste.find((r) => r.neu);
    // Docker: kein Installieren aus der App – der Pull-Befehl der neuen Version zum Kopieren.
    // Image-Tag = Version ohne Kanal (eine Version ist in jedem Kanal derselbe Code)
    const tag = (v) => String(v).replace(/-.*$/, '');
    const pull = (v) => `docker pull ${u.docker.image || '<image>'}:${tag(v)}`;
    const pullBlock = u.docker && neu ? T`<code class="kopierbar">${esc(pull(neu.version))}</code><div class="leiste"><button class="btn klein" data-pull="${esc(tag(neu.version))}">Kopieren</button></div>` : '';
    const neuKarte = neu ? T`<div class="karte versionzeile neuekarte"><div class="versionkopf"><span class="leise">${updateSymbol} ${tx('Neue Version verfügbar')}</span><span class="versionzeile1"><span class="badge neutral">${esc(tx(neu.kanal))}</span><strong class="versiongross">${esc(versionNr(neu.version))}</strong></span><span class="leise">${esc(zeitText(neu.veroeffentlicht) || neu.datum || '')}</span></div>
        <div class="versionknoepfe"><button class="btn klein" data-log="${esc(neu.version)}">Änderungen</button>${u.installierbar ? T`<button class="btn klein haupt" data-installieren="${esc(neu.version)}"${laeuft ? ' disabled' : ''}>Installieren</button>` : ''}</div>${pullBlock}</div>` : '';
    const updates = T`${neuKarte}<div class="karte versionzeile aktuell"><div class="versionkopf"><span class="leise">Installiert</span><span class="versionzeile1"><span class="badge neutral">${esc(tx(u.aktuell_kanal || ''))}</span><strong class="versiongross">${esc(versionNr(u.aktuell))}</strong></span>${versionZeit(u.aktuell) ? T`<span class="leise">${esc(versionZeit(u.aktuell))}</span>` : ''}</div>
        <div class="versionknoepfe"><button class="btn klein" data-log="">Änderungen</button></div></div>
      <h2>Kanal</h2>
      <nav class="btn-group unter" aria-label="Kanal">${(u.kanaele || []).map((k) => T`<button class="btn${k === u.kanal ? ' aktiv' : ''}" data-kanal="${k}" aria-pressed="${k === u.kanal}">${k}${u.kanal_neu?.[k] ? T` <span class="kanalneu" title="Neuere Version als die installierte">${updateSymbol}</span>` : ''}</button>`).join('')}</nav>
      <p class="leise">${esc(KANAL_TEXT[u.kanal] || '')}</p>
      ${wechsel ? T`<div class="hinweis warn">${esc(tx('Installiert ist {0} aus {1}. Zum Wechsel auf {2} die neueste Version dort installieren.', versionNr(u.aktuell), tx(u.aktuell_kanal), u.kanal))}</div>` : ''}
      ${liste.length ? (alle ? liste : liste.slice(0, 3)).map(zeile).join('') : u.fehler ? '' : T('<p class="leise">In diesem Kanal gibt es noch keine Version.</p>')}
      ${!alle && liste.length > 3 ? T`<div class="leiste"><button class="btn" data-mehr-versionen="1">Mehr (${liste.length - 3})</button></div>` : ''}
      ${liste.length && !liste.some((r) => r.neu) ? T('<p class="leise">Keine neuere Version als die installierte in diesem Kanal.</p>') : ''}
      ${u.docker ? T('<div class="hinweis">Docker: aktualisiert wird mit einem neuen Image (docker compose pull, dann docker compose up -d). Der Kanal hier wählt nur, welche Versionen angezeigt werden; das Image-Tag bestimmt, was läuft.</div>')
        : u.installierbar ? T('<p class="leise">Vor jedem Wechsel werden Datenbank, Einstellungen, Version und Kanal gesichert. Zurück auf eine ältere Version nur ohne Datenbank-Änderung dazwischen, sonst über ein Backup.</p>') : T('<div class="hinweis">Installieren aus der App braucht die Installation per <code>install.sh</code>; sonst das Skript mit <code>--version</code> ausführen.</div>')}
      ${u.docker ? '' : T`<h2>Automatisches Update</h2>
      <nav class="btn-group unter" aria-label="Automatisches Update">${[[true, tx('An')], [false, tx('Aus')]].map(([an, n]) => T`<button class="btn${u.auto === an ? ' aktiv' : ''}" data-auto="${an ? '1' : '0'}" aria-pressed="${u.auto === an}">${n}</button>`).join('')}</nav>
      <p class="leise">${u.auto ? esc(tx('Täglich um {0} die neueste Version aus {1} installieren, wenn sie neuer ist (vorher Backup).', u.auto_zeit, u.kanal)) : tx('Aus – Updates nur von Hand.')}
        <a class="konfedit" href="/status/einstellungen/AUTO_UPDATE_TIME?zurueck=/status/update" aria-label="${esc(tx('Uhrzeit ändern'))}" title="${esc(tx('Uhrzeit ändern'))}">${STIFT}</a></p>
      ${u.auto_letzt ? T`<p class="leise">${esc(tx('Letzter Lauf: {0}', u.auto_letzt))}</p>` : ''}`}
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
      <p class="leise">${b.plan === 'aus' ? tx('Aus – Backups nur von Hand und vor Updates.') : tx('Backups vor Updates bleiben 30 Tage.')} ${u.docker ? tx('Den Ordner legt die Umgebungsvariable BACKUP_DIR des Containers fest.') : tx('Den Ordner legt der Startwert BACKUP_DIR in /etc/pebble-webhook/env fest; danach install.sh erneut ausführen.')}</p>
      ${letzt ? (letzt[2] === 'ok' ? T`<p class="leise">${esc(tx('Letzter Lauf: {0}', letzt[1]))}</p>` : T`<div class="hinweis schlecht">${esc(tx('Letztes geplantes Backup fehlgeschlagen ({0}): {1}', letzt[1], letzt[3]))}</div>`) : ''}
      <div class="leiste"><button class="btn haupt" data-sichern="1">Jetzt sichern</button>${u.installierbar || u.docker ? T('<a class="btn" href="/status/backups/hochladen">Backup hochladen</a>') : ''}</div>
      <div class="karte konfliste"><div class="konfzeile ohnehilfe"><span class="konfname">Systemschlüssel</span>
        <a class="konfedit" href="/status/systemschluessel" aria-label="${esc(tx('Systemschlüssel ansehen'))}">${STIFT}</a><span class="konfwert">für einen neuen Server</span></div></div>
      <h2>Backups</h2>
      ${(u.backups || []).length ? T`<p class="backupsumme"><strong>${u.backups.length}</strong> Backups · <strong>${groesse(u.backups.reduce((n, st) => n + (st.groesse || 0), 0))}</strong> belegt</p>` : ''}
      ${(u.backups || []).length > 10 ? T`<div class="leiste"><button class="btn gefahr" data-prune="${u.backups.length - 10}" data-groesse="${u.backups.slice(10).reduce((n, st) => n + (st.groesse || 0), 0)}"${laeuft ? ' disabled' : ''}>${tx('{0} Backups löschen, die 10 neuesten bleiben', u.backups.length - 10)}</button></div>` : ''}
      <p class="leise">Jedes Backup enthält Datenbank mit allen Einstellungen, Version und Kanal. Wiederherstellen spielt die Daten ein und installiert die Version aus dem Backup; was seitdem dazukam, fehlt danach. Der jetzige Stand wird vorher gesichert.</p>
      ${u.docker ? T('<div class="hinweis">Docker: Wiederherstellen spielt die Daten ein und startet den Container neu; die Version bleibt die des Images. Ein Backup einer neueren Version braucht erst das Image dieser Version.</div>') : ''}
      ${(u.backups || []).length ? u.backups.map((st) => T`<div class="karte backupzeile"><div class="backupinfo">
          <span class="versionzeile1">${st.kanal ? T`<span class="badge neutral">${esc(tx(st.kanal))}</span>` : ''}<strong>${esc(versionNr(st.version))}</strong></span>
          <span class="leise backupzeit">${esc(new Date(st.zeit).toLocaleString(LOCALE, { day: '2-digit', month: '2-digit', year: new Date(st.zeit).getFullYear() === new Date().getFullYear() ? undefined : '2-digit', hour: '2-digit', minute: '2-digit' }))} · ${esc(ART[st.art] || st.art)}${st.audio ? T` · mit Aufnahmen` : ''}</span></div>
        <div class="backupknoepfe">${st.groesse ? T`<span class="leise backupgroesse">${groesse(st.groesse)}</span>` : ''}
          ${st.alt ? '<span class="symbolplatz"></span>' : T`<a class="btn klein rund symbol" href="/api/backups/${encodeURIComponent(st.name)}" download aria-label="${esc(tx('Herunterladen'))}" title="${esc(tx('Herunterladen'))}">${SYM_LADEN}</a>`}
          ${st.wiederherstellbar ? (u.installierbar || (u.docker && !st.alt) ? T`<button class="btn klein rund symbol" data-restore="${esc(st.name)}" data-version="${esc(versionNr(st.version))}" aria-label="${esc(tx('Wiederherstellen'))}" title="${esc(tx('Wiederherstellen'))}"${laeuft ? ' disabled' : ''}>${SYM_ZURUECK}</button>` : '')
    : T('<span class="leise">unvollständig</span>')}
          <button class="btn klein rund symbol gefahr" data-loeschen="${esc(st.name)}" data-version="${esc(versionNr(st.version))}" aria-label="${esc(tx('Löschen'))}" title="${esc(tx('Löschen'))}"${laeuft ? ' disabled' : ''}>${ICON_MUELL}</button></div></div>`).join('') : T('<p class="leise">Noch keine Backups.</p>')}`;
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
    if (b.dataset.pull) {
      try { await navigator.clipboard.writeText(`docker pull ${u.docker?.image || '<image>'}:${b.dataset.pull}`); melde(tx('Befehl kopiert')); } catch { melde(tx('Kopieren nicht möglich – Befehl markieren und kopieren')); }
      return;
    }
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
      if (b.dataset.loeschen) {
        if (!confirm(tx('Backup von Version {0} löschen? Das lässt sich nicht rückgängig machen.', b.dataset.version))) { b.disabled = false; return; }
        await api(T`/api/backups/${encodeURIComponent(b.dataset.loeschen)}`, { method: 'DELETE' });
        melde(tx('Backup gelöscht'));
        return laden(false);
      }
      if (b.dataset.prune) {
        if (!confirm(tx('{0} Backups löschen ({1})? Die 10 neuesten bleiben.', b.dataset.prune, groesse(Number(b.dataset.groesse))))) { b.disabled = false; return; }
        const d = await api('/api/backups/aufraeumen', { method: 'POST' });
        melde(tx('{0} Backups gelöscht', d.backups));
        return laden(false);
      }
      if (b.dataset.restore) {
        const frage = u.docker
          ? tx('Backup von Version {0} wiederherstellen? Daten und Einstellungen werden ersetzt, der Container startet neu; was seitdem dazukam, fehlt danach. Der jetzige Stand wird vorher gesichert.', b.dataset.version)
          : tx('Backup von Version {0} wiederherstellen? Daten und Einstellungen werden ersetzt, die Version aus dem Backup wird installiert; was seitdem dazukam, fehlt danach. Der jetzige Stand wird vorher gesichert.', b.dataset.version);
        if (!confirm(frage)) { b.disabled = false; return; }
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

// ---------- Systemschlüssel: macht API-Schlüssel und Push-Schlüssel eines Backups auf einem neuen Server wieder lesbar ----------
// Vorgabe: an den Wiederherstellungscode eines Admins gebunden (ein Code für Daten und System); auf Wunsch eigener Code.
async function zeigeSystemschluessel() {
  seite.innerHTML = T('<p class="leer">Lädt …</p>');
  const d = await api('/api/admin/systemschluessel');
  if (aktuell?.name !== 'systemschluessel') return;
  const kopf = T`${krumen([[tx('Backups'), '/status/backups'], [tx('Systemschlüssel')]])}<h1>Systemschlüssel</h1>
    <p class="leise">Ein Backup enthält API-Schlüssel und den Schlüssel für Benachrichtigungen verschlüsselt. Auf einem neuen Server macht der Systemschlüssel sie wieder lesbar; die Daten jedes Nutzers öffnet sein eigener Wiederherstellungscode.</p>`;
  if (d.fehlt) {
    seite.innerHTML = kopf + (d.art === 'code'
      ? T`<div class="hinweis warn">Der Systemschlüssel fehlt auf diesem Server. Mit dem Code des Systemschlüssels zurückholen.</div>
        <form class="einstellungen" id="sysform" novalidate><label for="syscode">Code des Systemschlüssels</label><input id="syscode" autocomplete="off" placeholder="XXXXX-XXXXX-XXXXX-XXXXX-XXXXXX" required>
          <div class="leiste"><button class="btn haupt" type="submit">Wiederherstellen</button></div></form>`
      : T`<div class="hinweis warn">Der Systemschlüssel fehlt auf diesem Server. Er kommt automatisch zurück, sobald ${esc(d.name || tx('der Admin'))} die eigenen Daten mit dem Wiederherstellungscode öffnet.</div>`);
    const f = document.getElementById('sysform');
    if (f) f.onsubmit = async (ev) => {
      ev.preventDefault();
      try { await api('/api/admin/systemschluessel/wiederherstellen', { method: 'POST', body: { code: f.syscode.value } }); melde(tx('Systemschlüssel wiederhergestellt – Dienst startet neu')); setTimeout(() => location.reload(), 4000); } catch (e) { melde(e.message); }
    };
    return;
  }
  const stand = d.art === 'code' ? tx('Eigener Code nur für das System.')
    : d.uid === d.ich ? tx('An deinen Wiederherstellungscode gebunden: ein Code stellt deine Daten und das System wieder her.')
    : tx('An den Wiederherstellungscode von {0} gebunden.', d.name || '?');
  seite.innerHTML = kopf + T`<div class="karte"><p>${esc(stand)}</p></div><div id="syscodeneu"></div>
    <div class="leiste">${d.art !== 'nutzer' || d.uid !== d.ich ? T('<button class="btn" data-sys="binden">An meinen Code binden</button>') : ''}<button class="btn" data-sys="code">Eigenen Code erzeugen</button></div>
    <p class="leise">Ein eigener Code lohnt sich bei mehreren Admins. Für einen einzelnen Nutzer reicht der eigene Wiederherstellungscode.</p>`;
  seite.onclick = async (ev) => {
    const b = ev.target.closest('[data-sys]');
    if (!b) return;
    if (b.dataset.sys === 'code' && !confirm(tx('Eigenen Code erzeugen? Danach stellt nur noch dieser Code das System wieder her.'))) return;
    b.disabled = true;
    try {
      if (b.dataset.sys === 'binden') { await api('/api/admin/systemschluessel/binden', { method: 'POST' }); melde(tx('Gebunden')); return zeigeSystemschluessel(); }
      const r = await api('/api/admin/systemschluessel/code', { method: 'POST' });
      seite.querySelector('.leiste').hidden = true;
      document.getElementById('syscodeneu').innerHTML = T`<div class="hinweis warn">Diesen Code jetzt aufschreiben – er wird nur einmal gezeigt.</div>
        <p class="geraetecode wcode">${esc(r.code)}</p><div class="leiste"><a class="btn haupt" href="/status/backups">Notiert</a></div>`;
    } catch (e) { b.disabled = false; melde(e.message); }
  };
  aufraeumen = () => { seite.onclick = null; };
}

// ---------- Backup hochladen: eine heruntergeladene Backup-Datei einspielen ----------
// Eigene Seite (keine Eingabefelder auf der Backup-Übersicht). Der Server nimmt nur genau die Form des Downloads an
// und spielt sie danach sofort ein, samt der Version aus dem Backup.
async function zeigeBackupHochladen() {
  seite.innerHTML = T`${krumen([[tx('Backups'), '/status/backups'], [tx('Hochladen')]])}<h1>Backup hochladen</h1>
    <p class="leise">Nur eine unveränderte Datei aus „Herunterladen“ (pebble-….tar.gz). Sie wird geprüft, abgelegt und sofort eingespielt: Daten und Einstellungen werden ersetzt, die Version aus dem Backup wird installiert (im Docker-Container bleibt die des Images, er startet neu). Der jetzige Stand wird vorher gesichert.</p>
    <form id="hochladen" class="karte"><label for="backupdatei">Backup-Datei</label>
      <input type="file" id="backupdatei" accept=".tar.gz,.gz,application/gzip" required>
      <p class="leise" id="hochladenstand" aria-live="polite"></p>
      <div class="leiste"><a class="btn" href="/status/backups">Abbrechen</a><button class="btn haupt" type="submit" disabled>Hochladen und einspielen</button></div></form>`;
  const form = document.getElementById('hochladen'), feld = document.getElementById('backupdatei');
  const knopf = form.querySelector('button[type=submit]'), stand = document.getElementById('hochladenstand');
  const muster = /^pebble-[0-9A-Za-z.+-]+-(stable|beta|nightly|lokal)-\d{8}-\d{6}-(update|plan|manuell)(-audio)?\.tar\.gz$/;
  feld.onchange = () => {
    const d = feld.files[0];
    const ok = !!d && muster.test(d.name);
    knopf.disabled = !ok;
    stand.textContent = d ? (ok ? tx('{0} · {1}', d.name, groesse(d.size)) : tx('Der Dateiname passt nicht zu einem Backup dieser App.')) : '';
  };
  form.onsubmit = (ev) => {
    ev.preventDefault();
    const d = feld.files[0];
    if (!d || !confirm(tx('Backup {0} einspielen? Daten und Einstellungen werden ersetzt; was seitdem dazukam, fehlt danach. Der jetzige Stand wird vorher gesichert.', d.name))) return;
    knopf.disabled = true;
    feld.disabled = true;
    // XHR statt fetch: zeigt den Fortschritt beim Hochladen.
    const x = new XMLHttpRequest();
    x.open('POST', T`/api/backups/hochladen?name=${encodeURIComponent(d.name)}`);
    x.setRequestHeader('X-App', '1');
    x.setRequestHeader('X-Sprache', SPRACHE);
    x.upload.onprogress = (e) => { if (e.lengthComputable) stand.textContent = tx('Lädt hoch … {0} %', Math.round((e.loaded / e.total) * 100)); };
    x.onload = () => {
      let antwort = null;
      try { antwort = JSON.parse(x.responseText); } catch { /* leer */ }
      if (x.status === 200) {
        melde(tx('Backup hochgeladen – Wiederherstellen angestoßen'));
        geheZu('/status/backups');
        return;
      }
      stand.textContent = antwort?.fehler || tx('Fehler {0}', x.status);
      knopf.disabled = false;
      feld.disabled = false;
    };
    x.onerror = () => { stand.textContent = tx('Keine Verbindung'); knopf.disabled = false; feld.disabled = false; };
    stand.textContent = tx('Lädt hoch …');
    x.send(d);
  };
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

// Protokoll je Anbieter und Modell zusammengefasst, darunter je Zweck.
function anbieterHtml(anfragen) {
  if (!anfragen.length) return T`<p class="leer">Noch keine Anfragen im Protokoll.</p>`;
  const gruppen = new Map();
  for (const q of anfragen) {
    const name = [q.anbieter, q.modell].filter(Boolean).join(' · ') || kiZweck(q);
    if (!gruppen.has(name)) gruppen.set(name, []);
    gruppen.get(name).push(q);
  }
  const summe = (liste) => {
    const s = { anzahl: 0, suchen: 0, bilder: 0 };
    for (const q of liste) {
      s.anzahl += q.anzahl; s.suchen += q.suchen || 0; s.bilder += q.bilder || 0;
      for (const f of ['tokens_ein', 'tokens_aus', 'sekunden', 'kosten_usd']) if (q[f] != null) s[f] = (s[f] || 0) + q[f];
    }
    return s;
  };
  return [...gruppen].map(([name, liste]) => T`<div class="karte anbieterkarte"><div class="zeile"><strong>${esc(name)}</strong>${summe(liste).kosten_usd > 0 ? T`<span>${usd(summe(liste).kosten_usd)}</span>` : ''}</div>
    <div class="leise">${esc(kiVerbrauch(summe(liste), true).replace(/ · [^·]*\$$/, ''))}</div>
    <details><summary>Wofür</summary><table class="werte"><tbody>${liste.map((q) => T`<tr><td>${esc(kiZweck(q))}</td><td>${esc(kiVerbrauch(q, true))}</td></tr>`).join('')}</tbody></table></details></div>`).join('');
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
    const arten = [[tx('Auswertung der Notizen'), k.arten.auswertung], [tx('Recherche (Planungen)'), k.arten.recherche], [tx('Transkription'), k.arten.transkription], [tx('Kurztitel und Zuordnung'), k.arten.titel_zuordnung]];
    const artMax = Math.max(...arten.map((a) => a[1]), 1e-9);
    const tabelle = (tage) => T`<details><summary>Als Tabelle</summary><table class="werte"><thead><tr><th>Tag</th><th>Notizen</th><th>Aufgaben</th><th>Summe</th></tr></thead><tbody>${tage.map((t) => T`<tr><td>${esc(kurzTag(t.tag, true))}</td><td>${usd(t.diktat)}</td><td>${usd(t.planung)}</td><td>${usd(t.diktat + t.planung)}</td></tr>`).join('')}</tbody></table></details>`;
    seite.innerHTML = T`<h1>KI-Kosten</h1>
      <p class="leise">KI-Kosten je Auftrag, berechnet aus Verbrauch und den Preisen der Einträge unter KI. Zugeordnet nach Eingangstag.</p>
      <h2>Letzte 30 Tage</h2>
      <div class="kacheln">${kachel(tx('Gesamt'), { usd: d.usd + p.usd, anzahl: d.anzahl + p.anzahl, ohne_kosten: d.ohne_kosten + p.ohne_kosten })}${kachel(tx('Notizen'), d)}${kachel(tx('Aufgaben (Planungen)'), p)}</div>
      <h2>Letzte 7 Tage – ${usd(summe(t7, 'diktat') + summe(t7, 'planung'))}</h2>
      <div class="karte">${legende}${saeulen(t7, 'd7', true)}<div class="ablesen" id="d7-wert">Säule antippen für die Werte des Tages.</div>${tabelle(t7)}</div>
      <h2>Verlauf 30 Tage</h2>
      <div class="karte">${legende}${saeulen(k.tage, 'd30', false)}<div class="ablesen" id="d30-wert">Säule antippen für die Werte des Tages.</div>${tabelle(k.tage.filter((t) => t.diktat + t.planung > 0))}</div>
      <h2>Wofür – 30 Tage</h2>
      <div class="karte">${arten.map(([name, v]) => T`<div class="balkenzeile"><span>${esc(name)}</span><span class="balken"><i class="${v > 0 ? '' : 'leer'}" data-anteil="${Math.round((v / artMax) * 100)}"></i></span><span class="wert">${usd(v)}</span></div>`).join('')}
        ${k.entscheidung_usd > 0 ? T`<p class="leise">${tx('Davon Entscheidungsmodell: {0}', usd(k.entscheidung_usd))}</p>` : ''}
        <p class="leise">Kostenlimit je Rechercheauftrag: ${usd(k.limit_usd)}.</p></div>
      <h2>Je Anbieter – 30 Tage</h2>
      <p class="leise">Alle KI-Anfragen aus dem Protokoll, auch Lernlauf und Einsortieren: rohe Tokens und Kosten nach den Preisen, die bei der Anfrage galten.</p>
      ${anbieterHtml(k.anfragen || [])}`;
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
  seite.innerHTML = T`${kontoReiter('pebble')}<p class="leer">Lädt …</p>`;
  let d;
  try { d = await api('/api/pebble'); } catch (f) {
    if (f instanceof NetzFehler) { if (aktuell?.name === 'pebble') seite.innerHTML = T('<p class="leer">Offline – die Einrichtungswerte brauchen eine Verbindung.</p>'); return; }
    throw f;
  }
  if (aktuell?.name !== 'pebble') return;
  seite.innerHTML = kontoReiter('pebble') + pebbleSeite(d);
  seite.onclick = (ev) => { const b = ev.target.closest('button'); if (b) pebbleKlick(b, d); };
  aktuell.neuLaden = zeigePebble; // Test-Event kommt an: „Letzter Test“ aktualisieren
  aufraeumen = () => { seite.onclick = null; };
}
function pebbleSeite(d) {
  const wert = (name, schluessel, geheim) => T`<div class="karte"><div class="zeile"><strong>${esc(name)}</strong>
    <span>${geheim ? T`<button class="btn klein" data-zeigen="${schluessel}">Anzeigen</button> ` : ''}<button class="btn klein" data-kopieren="${schluessel}">Kopieren</button></span></div>
    <div class="link" id="wert-${schluessel}">${geheim ? '••••••••••••••••' : esc(d[schluessel])}</div></div>`;
  return T`<h1>Pebble-App</h1>
    <dl><dt>Halten</dt><dd>${esc(MODUS[d.halten])}</dd><dt>Klick + Halten</dt><dd>${esc(MODUS[d.doppelklick_halten])}</dd>
      <dt>Transkription</dt><dd>${esc(tx(TRANSKRIPTION[d.transkription] || d.transkription))}</dd></dl>
    <h2>Einrichten</h2>
    <p class="leise">In der Pebble-App unter Index 01 → Webhooks, für beide Gesten gleich eintragen.</p>
    ${wert(tx('Webhook-URL'), 'url')}
    <dl><dt>Payload-Modus</dt><dd><strong>Both</strong> (Aufnahme und Transkript)</dd><dt>Größe</dt><dd>bis ${d.max_mb} MB je Aufnahme</dd></dl>
    <p class="leise">Unter „Headers“ ein Paar aus Name und Wert anlegen:</p>
    ${wert(tx('Header-Name'), 'header')}${wert(tx('Header-Wert'), 'token', true)}
    <p class="leise">„Signed requests“ einschalten und dieses Secret eintragen, ohne Leerzeichen:</p>
    ${wert(tx('Signatur-Secret'), 'secret', true)}
    <div class="hinweis${d.letzter_test ? ' gut' : ''}">${d.letzter_test ? esc(tx('Letzter Test angekommen: {0}', zeit(d.letzter_test.zeit))) : esc(tx('Noch kein Test angekommen.'))}</div>
    <ol><li>„Send test event“ – oben erscheint „Letzter Test angekommen“. Ein Test wird kein Eintrag.</li>
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

// ---------- Kurz-Code (6 Ziffern) für ein weiteres Gerät und für Einladungen: derselbe Ablauf ----------
// Der Code gilt nur, solange die Seite offen ist (Lebenszeichen alle 10 s); beim Verlassen wird er gelöscht.

async function codeFeld(el, basis, seitenName, beiEnde) {
  const d = await api(basis, { method: 'POST' });
  if (aktuell?.name !== seitenName) { api(basis, { method: 'DELETE' }).catch(() => {}); return () => {}; }
  let gueltig = true;
  const male = () => {
    const rest = Math.max(0, Math.round((d.bis - Date.now()) / 1000));
    if (rest === 0) gueltig = false;
    el.innerHTML = gueltig ? T`<p class="geraetecode" aria-label="Code ${d.code.split('').join(' ')}">${esc(d.code)}</p>
      <p class="mitte leise">noch ${Math.floor(rest / 60)}:${String(rest % 60).padStart(2, '0')} min · nur solange diese Seite offen ist</p>`
      : T('<div class="hinweis warn">Der Code ist abgelaufen.</div><div class="leiste"><button class="btn haupt" data-code-neu>Neuen Code erzeugen</button></div>');
  };
  male();
  const uhr = setInterval(() => { if (gueltig) male(); }, 1000);
  const halten = setInterval(async () => {
    try { const r = await api(basis + '/halten', { method: 'POST' }); if (!r.gueltig && gueltig) { gueltig = false; if (beiEnde) { stopp(); beiEnde(); return; } male(); melde(tx('Code verbraucht oder abgelaufen')); } } catch { /* offline: verfällt */ }
  }, 10000);
  const weg = () => { fetch(basis, { method: 'DELETE', headers: { 'X-App': '1' }, credentials: 'same-origin', keepalive: true }).catch(() => {}); };
  window.addEventListener('pagehide', weg);
  let innen = () => {};
  const stopp = () => { clearInterval(uhr); clearInterval(halten); window.removeEventListener('pagehide', weg); el.onclick = null; innen(); };
  // Neuer Code ersetzt den alten auf dem Server; kein Löschen, das den neuen treffen könnte.
  el.onclick = async (ev) => {
    if (!ev.target.closest('[data-code-neu]')) return;
    clearInterval(uhr); clearInterval(halten); window.removeEventListener('pagehide', weg);
    innen = await codeFeld(el, basis, seitenName, beiEnde).catch((f) => { melde(f.message); return () => {}; });
  };
  return () => { stopp(); weg(); };
}

async function zeigeGeraetNeu() {
  seite.innerHTML = T`<p><a class="btn klein" href="/konto">← ${esc(tx('Benutzer'))}</a></p><h1>Weiteres Gerät hinzufügen</h1><div id="codefeld"><p class="leer">Lädt …</p></div>
    <ol><li>Auf dem neuen Gerät diese Adresse öffnen: <strong>${esc(location.host)}</strong></li>
      <li>„Mit Code hinzufügen“ antippen und den Code eingeben.</li>
      <li>Namen eingeben und den Passkey anlegen.</li></ol>
    <div class="leiste"><a class="btn" href="/konto">Fertig</a></div>`;
  const stopp = await codeFeld(document.getElementById('codefeld'), '/api/geraete/code', 'geraetNeu');
  aufraeumen = stopp;
}

// Neu einladen (Admin, nach einem Adresswechsel): Code für einen neuen Passkey eines bestehenden Nutzers.
async function zeigeNeuEinladen(id) {
  seite.innerHTML = T('<p class="leer">Lädt …</p>');
  const u = await api(T`/api/admin/nutzer/${id}`);
  if (aktuell?.name !== 'neuEinladen') return;
  seite.innerHTML = T`<p><a class="btn klein" href="/admin/nutzer">← Nutzer</a></p><h1>${esc(u.name)} neu einladen</h1>
    <p class="leise">Kein neuer Nutzer: ${esc(u.name)} legt nur einen Passkey für diese Adresse an. Fehlt sein Schlüssel auf diesem Server, fragt die Seite dabei seinen Wiederherstellungscode ab.</p>
    <div id="codefeld"><p class="leer">Lädt …</p></div>
    <ol><li>Auf dem Gerät von ${esc(u.name)} diese Adresse öffnen: <strong>${esc(location.host)}</strong></li>
      <li>„Mit Code hinzufügen“ antippen und den Code eingeben.</li>
      <li>Passkey anlegen (ggf. mit Wiederherstellungscode).</li></ol>
    <div class="leiste"><a class="btn" href="/admin/nutzer">Fertig</a></div>`;
  aufraeumen = await codeFeld(document.getElementById('codefeld'), T`/api/admin/nutzer/${id}/code`, 'neuEinladen', () => { melde(tx('Code genutzt')); geheZu('/admin/nutzer', true); });
}

// Offene Einladung (Admin): Code für das Gerät des Eingeladenen, Widerrufen.
async function zeigeEinladung(id) {
  seite.innerHTML = T('<p class="leer">Lädt …</p>');
  let e;
  try { e = await api(T`/api/admin/einladungen/${id}`); } catch (f) {
    seite.innerHTML = T`<p><a class="btn klein" href="/admin/nutzer">← Nutzer</a></p><div class="hinweis warn">${esc(f.message)}</div>`;
    return;
  }
  if (aktuell?.name !== 'einladung') return;
  const link = letzteEinladung?.id === id ? letzteEinladung.link : '';
  seite.innerHTML = T`<p><a class="btn klein" href="/admin/nutzer">← Nutzer</a></p><h1>Einladung als ${esc(tx(ROLLE[e.rolle] || e.rolle))}</h1>
    <p class="leise">${e.name ? T`für ${esc(e.name)} · ` : ''}Einladung gültig bis ${zeit(e.bis)} · einmal nutzbar</p>
    <div id="codefeld"><p class="leer">Lädt …</p></div>
    <ol><li>Auf dem neuen Gerät diese Adresse öffnen: <strong>${esc(location.host)}</strong></li>
      <li>„Mit Code hinzufügen“ antippen und den Code eingeben.</li>
      <li>Eigenen Namen eingeben und den Passkey anlegen; der Wiederherstellungscode erscheint danach.</li></ol>
    ${link ? T`<details class="karte"><summary>Stattdessen Link verschicken</summary><code class="kopierbar">${esc(link)}</code>
      <div class="leiste"><button class="btn" data-kopieren>Link kopieren</button></div><p class="leise">Wird der Code genutzt, gilt der Link nicht mehr.</p></details>` : ''}
    <div class="leiste"><a class="btn" href="/admin/nutzer">Fertig</a><button class="btn gefahr" data-widerrufen>Einladung widerrufen</button></div>`;
  // Eingelöst: die Einladung bekommt dabei einen neuen Link (neue Id) – zurück zur Liste.
  const stopp = await codeFeld(document.getElementById('codefeld'), T`/api/admin/einladungen/${id}/code`, 'einladung', async () => {
    try { await api(T`/api/admin/einladungen/${id}`); melde(tx('Code abgelaufen')); zeigeEinladung(id); } catch { melde(tx('Code genutzt – der neue Nutzer richtet jetzt seinen Passkey ein')); geheZu('/admin/nutzer', true); }
  });
  aufraeumen = stopp;
  const kopieren = seite.querySelector('[data-kopieren]');
  if (kopieren) kopieren.onclick = async () => { try { await navigator.clipboard.writeText(link); melde(tx('Link kopiert')); } catch { melde(tx('Kopieren nicht möglich – Link markieren und kopieren')); } };
  seite.querySelector('[data-widerrufen]').onclick = async (ev) => {
    if (!confirm(tx('Einladung widerrufen? Link und Code gelten dann nicht mehr.'))) return;
    ev.target.disabled = true;
    try { stopp(); aufraeumen = null; await api(T`/api/admin/einladungen/${id}`, { method: 'DELETE' }); melde(tx('Einladung widerrufen')); geheZu('/admin/nutzer', true); } catch (f) { ev.target.disabled = false; melde(f.message); }
  };
}

// ---------- Darstellung: Tag/Nacht und Farbpalette (theme.js), je Gerät; der Server merkt sich die letzte Wahl ----------

async function zeigeDarstellung() {
  const D = window.darstellung;
  // Vorschau: die letzten 3 Einträge des Verlaufs, wie sie dort aussehen (nur Ansicht, Klicks führen zum Eintrag).
  let beispiele = null;
  let d = { modus: 'auto', palette: 'blau', ...(D.lies() || {}) };
  const male = () => {
    const eigene = D.gueltigeFarbe(d.farbe) ? d.farbe : '#2f6fde';
    seite.innerHTML = T`${kontoReiter('darstellung')}<h1>Theme</h1>
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
    <p><strong>${esc(d.name)}</strong> <span class="leise">(Name von der App selbst angegeben, nicht geprüft)</span></p>
    <p>Die App bekommt vollen Zugriff auf deine Sprachnotizen:</p>
    <ul><li>alle Notizen, Wortlaute, Termine und Planungen lesen und durchsuchen</li><li>Notizen anlegen (verursacht KI-Kosten)</li><li>Listen ändern, zusammenführen und löschen, Punkte abhaken</li></ul>
    <div class="hinweis warn">Der Zugang geht an <strong>${esc(d.ziel)}</strong>. Nur erlauben, wenn du die Verbindung gerade selbst gestartet hast und die Adresse zu deiner App gehört.</div>
    <p class="leise">Trennen jederzeit unter Benutzer → Apps und Zugänge.</p>
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

// Rücksprung nach der Anmeldung (?weiter=/pfad): nur Pfade dieser Adresse.
const eigenerPfad = (p) => {
  if (typeof p !== 'string' || !p.startsWith('/') || /[\x00-\x20\\]/.test(p)) return false;
  try { return new URL(p, location.origin).origin === location.origin && !p.startsWith('//'); } catch { return false; }
};
const springeWeiter = (p) => (p.startsWith('/verbinden?') ? geheZu(p, true) : location.replace(p));

// Erste Einrichtung: Knopf „Einrichten“ (nur der erste Browser, nur 5 Minuten nach dem Start), sonst Hinweis auf Neustart.
function zeigeEinrichtenKnopf(s) {
  const offen = s.einrichtung === 'offen';
  seite.innerHTML = T`<div class="mitte"><h1>${esc(tx('Einrichtung'))}</h1>
    <p>${esc(s.einrichtung === 'gesperrt' ? tx('Der Dienst läuft im Einrichtungsassistenten.') : s.ersteinrichtung ? tx('Diese Installation ist noch nicht eingerichtet.') : tx('Es gibt noch keinen Passkey für den Admin.'))}</p>
    ${offen ? T`<p class="leise">${esc(tx('Möglich in den ersten 5 Minuten nach dem Start des Dienstes, nur in einem Browser.'))}</p>
      <p><button class="btn haupt" id="einrichten">${esc(tx('Einrichten'))}</button></p>
      <p aria-live="polite"><span class="leise">${esc(tx('Noch möglich für'))}</span> <strong id="einrichtrest"></strong></p>
      <span class="balken"><i id="einrichtbalken"></i></span>`
    : T`<div class="hinweis warn">${esc(s.einrichtung === 'gesperrt' ? tx('Die Einstellungen sind ungültig. Den Link aus dem Log des Dienstes öffnen (pebble ersteinrichtung).') : s.einrichtung === 'vergeben' ? tx('Die Einrichtung wurde schon in einem anderen Browser begonnen. Für einen neuen Versuch den Dienst neu starten.') : tx('Die Einrichtung ist nur in den ersten 5 Minuten nach dem Start möglich. Bitte den Dienst neu starten (z. B. systemctl restart pebble-webhook oder den Container neu starten) und diese Seite neu laden.'))}</div>
      <p><button class="btn" id="neuladen">${esc(tx('Neu laden'))}</button></p>`}</div>`;
  const restEl = document.getElementById('einrichtrest');
  if (restEl) {
    const ende = Date.now() + (s.einrichtung_rest || 0) * 1000, balken = document.getElementById('einrichtbalken');
    const tick = () => {
      if (!restEl.isConnected) return clearInterval(uhr);
      const r = Math.max(0, Math.round((ende - Date.now()) / 1000));
      restEl.textContent = tx('{0} min', `${Math.floor(r / 60)}:${String(r % 60).padStart(2, '0')}`);
      balken.style.width = `${(r / 300) * 100}%`;
      if (r <= 0) { clearInterval(uhr); location.reload(); }
    };
    const uhr = setInterval(tick, 1000);
    tick();
  }
  const nl = document.getElementById('neuladen');
  if (nl) nl.onclick = () => location.reload();
  const k = document.getElementById('einrichten');
  if (k) k.onclick = async () => {
    k.disabled = true;
    try {
      if (s.ersteinrichtung) {
        const r = await api('/api/ersteinrichtung/start', { method: 'POST' });
        location.replace(T`/ersteinrichtung?code=${encodeURIComponent(r.code)}`);
      } else {
        const r = await api('/api/auth/ersteinrichten', { method: 'POST' });
        location.replace(r.weiter);
      }
    } catch (f) { melde(f.message); setTimeout(() => location.reload(), 2500); }
  };
}

async function zeigeAnmelden() {
  const ziel = new URLSearchParams(location.search).get('weiter');
  if (eigenerPfad(ziel)) try { sessionStorage.setItem('nach-anmeldung', ziel); } catch { /* dann zur Startseite */ }
  const s = await api('/api/sitzung').catch(() => null);
  if (s?.angemeldet) return eigenerPfad(ziel) ? springeWeiter(ziel) : geheZu('/', true);
  // Noch nicht eingerichtet (kein Passkey bzw. Einrichtungsassistent): „Einrichten“ nur in den ersten 5 Minuten nach dem Start.
  if (s && (s.ersteinrichtung || !s.eingerichtet) && s.einrichtung) return zeigeEinrichtenKnopf(s);
  seite.innerHTML = T`<div class="mitte"><h1>Anmelden</h1>
    ${s?.neue_adresse ? T`<div class="hinweis warn">Neue Adresse: Passkeys gelten nur für die alte. Als Admin mit deinem Wiederherstellungscode anmelden und einen neuen Passkey anlegen; alle anderen lädt der Admin danach unter Nutzer neu ein.</div>
      <form class="einstellungen" id="codeanmeldung" novalidate><div class="namenfeld"><label for="wcode-an">Wiederherstellungscode (Admin)</label><input id="wcode-an" autocomplete="off" placeholder="XXXXX-XXXXX-XXXXX-XXXXX-XXXXXX" required></div>
        <div class="leiste"><button class="btn haupt" type="submit">Weiter zum Passkey</button></div></form>` : ''}
    <p class="leise">Anmeldung mit dem Passkey dieses Geräts.</p>
    <p><button class="btn haupt" id="los"${window.PublicKeyCredential ? '' : ' disabled'}>Mit Passkey anmelden</button></p>
    ${window.PublicKeyCredential ? '' : T('<p class="leise">Dieser Browser unterstützt keine Passkeys.</p>')}
    <h2>Neues Gerät oder Einladung?</h2>
    <p class="leise">Den 6-stelligen Code eingeben: für ein weiteres eigenes Gerät von einem angemeldeten Gerät (Status → „Weiteres Gerät hinzufügen“), für eine Einladung vom Admin.</p>
    <form id="codeform" class="codeform"><label for="code">Code</label>
      <input type="text" id="code" inputmode="numeric" pattern="[0-9]{6}" maxlength="6" autocomplete="one-time-code" placeholder="000000" required>
      <button class="btn" type="submit">Mit Code hinzufügen</button></form></div>`;
  const codeAn = document.getElementById('codeanmeldung');
  if (codeAn) codeAn.onsubmit = async (ev) => {
    ev.preventDefault();
    const k = codeAn.querySelector('[type=submit]');
    k.disabled = true;
    try { const r = await api('/api/auth/wiederherstellungscode', { method: 'POST', body: { code: document.getElementById('wcode-an').value } }); geheZu(T`/einrichten#${r.token}`); } catch (f) { k.disabled = false; melde(f.message); }
  };
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
      const s = await nachAnmeldung();
      besuche.anmeldung();
      starteLive();
      if (s.code_offen) { geheZu('/willkommen', true); return; }
      let weiter = '/';
      try { weiter = sessionStorage.getItem('nach-anmeldung') || '/'; sessionStorage.removeItem('nach-anmeldung'); } catch { /* Startseite */ }
      if (eigenerPfad(weiter) && weiter !== '/') springeWeiter(weiter);
      else geheZu('/', true);
    } catch (f) {
      ev.target.disabled = false;
      melde(f.name === 'NotAllowedError' ? tx('Anmeldung abgebrochen') : f.message);
    }
  };
}

async function zeigeEinrichten() {
  const token = location.hash.slice(1);
  // Einladung ohne Namen: der neue Nutzer gibt ihn hier selbst ein.
  let art = null;
  if (token) art = await api('/api/auth/einrichten/art', { method: 'POST', body: { token } }).catch(() => null);
  const mitName = art?.art === 'einladung' && !art.name;
  const mitCode = !!art?.code_noetig;
  seite.innerHTML = T`<h1>${mitName ? tx('Willkommen') : tx('Passkey einrichten')}</h1>
    ${mitName ? T`<p class="leise">Du wurdest als ${esc(tx(ROLLE[art.rolle] || art.rolle))} eingeladen. Ein Passwort gibt es nicht – dieses Gerät bekommt einen Passkey.</p>
      <div class="namenfeld"><label for="nutzer">Dein Name</label><input type="text" id="nutzer" maxlength="40" autocomplete="name" placeholder="Vorname" required></div>`
    : T('<p class="leise">Dieses Gerät bekommt einen Passkey. Ein Passwort gibt es nicht.</p>')}
    ${mitCode ? T`<div class="namenfeld"><label for="wcode-e">Dein Wiederherstellungscode</label><input id="wcode-e" autocomplete="off" placeholder="XXXXX-XXXXX-XXXXX-XXXXX-XXXXXX" required>
      <p class="leise hilfe">Deine Daten sind auf diesem Server noch verschlüsselt zu – der Code öffnet sie.</p></div>` : ''}
    <label for="name">Name des Geräts</label><input type="text" id="name" maxlength="40" placeholder="z. B. Handy">
    <div class="leiste"><button class="btn haupt" id="los"${token && window.PublicKeyCredential ? '' : ' disabled'}>Passkey anlegen</button></div>
    ${token ? '' : T('<div class="hinweis schlecht">Der Link enthält keinen Einrichtungs-Code.</div>')}
    ${token && !art ? T('<div class="hinweis schlecht">Der Einrichtungs-Link ist ungültig oder abgelaufen.</div>') : ''}`;
  document.getElementById(mitName ? 'nutzer' : mitCode ? 'wcode-e' : 'name').focus();
  document.getElementById('los').onclick = async (ev) => {
    const nutzer = document.getElementById('nutzer')?.value.trim() || '';
    if (mitName && !nutzer) { melde(tx('Bitte deinen Namen eingeben')); document.getElementById('nutzer').focus(); return; }
    const wcode = document.getElementById('wcode-e')?.value.trim() || '';
    if (mitCode && !wcode) { melde(tx('Bitte deinen Wiederherstellungscode eingeben')); document.getElementById('wcode-e').focus(); return; }
    ev.target.disabled = true;
    try {
      const start = await api('/api/auth/einrichten/start', { method: 'POST', body: { token, name: document.getElementById('name').value, nutzer, wiederherstellungscode: wcode } });
      const cred = await navigator.credentials.create(optionenAusJson(start.optionen, true));
      await api(T`/api/auth/einrichten/ende?vorgang=${start.vorgang}`, { method: 'POST', body: credZuJson(cred) });
      const s = await nachAnmeldung();
      besuche.anmeldung();
      melde(tx('Passkey angelegt'));
      starteLive();
      // Erster Passkey nach der Einrichtung: gleich weiter zum Wiederherstellungscode.
      geheZu(s.code_offen ? '/willkommen' : '/', true);
    } catch (f) {
      ev.target.disabled = false;
      melde(f.name === 'NotAllowedError' ? tx('Einrichtung abgebrochen') : f.message);
    }
  };
}

// ---------- Menü (Hamburger links): drei Hauptseiten, darunter ihre Unterseiten ----------

// Seite → Bereich im Menü.
const BEREICH = { zugang: 'status', zugangCode: 'status', nutzer: 'system', nutzerNeu: 'system', einladung: 'system', neuEinladen: 'system', nutzerBearbeiten: 'system', backupHochladen: 'system', systemschluessel: 'system', kalender: 'status', mcp: 'status', ring: 'status', ringBearbeiten: 'status', joplin: 'status', einstellungBearbeiten: 'status', darstellung: 'status', geraetNeu: 'status', tagsVerwalten: 'status', kosten: 'status', papierkorb: 'status', update: 'system', backups: 'system', einstellungen: 'status', einstellungenBearbeiten: 'status', pebble: 'status', gedaechtnis: 'status', gedaechtnisBearbeiten: 'status', liste: 'listen', verschieben: 'listen', listeBearbeiten: 'listen', gruppeBearbeiten: 'listen', eintrag: 'verlauf', planung: 'verlauf', tags: 'verlauf', antwort: 'verlauf', umhaengen: 'verlauf', wortlaut: 'verlauf', einsortieren: 'verlauf', suche: 'verlauf' };
const lokal = (k, v = '') => { try { return localStorage.getItem(k) ?? v; } catch { return v; } };
let alleGruppenZeigen = false;
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
  // Gruppen ohne offene Punkte rutschen nach unten (je Stufe „Ohne Gruppe“ zuletzt).
  const leer = ([, offen]) => !(offen > 0);
  // Mehr als 5 Gruppen: am Ende „Weitere anzeigen“ klappt die übrigen im Menü auf.
  const benannt = gruppen.filter(([g]) => g !== '-').sort((a, b) => leer(a) - leer(b));
  const weitere = benannt.length - 5;
  gruppen = [...(alleGruppenZeigen ? benannt : benannt.slice(0, 5)), ...gruppen.filter(([g]) => g === '-')]
    .sort((a, b) => leer(a) - leer(b));
  let tagZahl;
  try { tagZahl = JSON.parse(lokal('tag-vorrat', 'null'))?.length; } catch { /* noch nie geladen */ }
  const art = lokal('verlauf-art', 'alles'), gruppe = name === 'listen' && aktuell.arg ? decodeURIComponent(aktuell.arg) : '';
  // Zähler rechts an jedem zählbaren Punkt, auch 0 (dann grau).
  const zahl = (n, still) => (typeof n === 'number' ? T` <span class="zaehler${still || !n ? ' still' : ''}">${n}</span>` : '');
  const teile = [
    { id: 'verlauf', name: tx('Verlauf'), href: '/', setze: ['verlauf-art', 'alles'], unter: ARTEN.map(([a, n]) => ({ name: a === 'termin' ? tx('Termine') : n, href: '/', setze: ['verlauf-art', a], an: name === 'verlauf' && art === a, extra: (a === 'fragen' ? '' : neuZahl(neuZahlen[a])) + zahl(zahlen[a] ?? 0, a !== 'fragen') })) },
    { id: 'listen', name: tx('Listen'), href: '/listen', unter: gruppen.map(([g, offen, neu]) => ({ name: gruppenTitel(g), href: gruppenPfad(g), an: !!gruppe && gruppe.toLowerCase() === g.toLowerCase(), extra: neuZahl(neu) + zahl(offen, true) })).concat(weitere > 0 ? [{ knopf: 'data-menue-gruppen', name: alleGruppenZeigen ? tx('Weniger anzeigen') : tx('Weitere anzeigen ({0})', weitere) }] : []) },
    { id: 'status', name: tx('Einstellungen'), href: '/status', unter: [
      { name: tx('Status'), href: '/status', an: ['status', 'joplin', 'einstellungen', 'einstellungBearbeiten'].includes(name), extra: audioFehler ? T` <span class="zaehler fehler" title="${tx('{0} Aufnahmen ohne Eintrag', audioFehler)}">${audioFehler}</span>` : '' },
      { name: tx('Gedächtnis'), href: '/status/gedaechtnis', an: name === 'gedaechtnis' || name === 'gedaechtnisBearbeiten', extra: zahl(offeneVorschlaege) },
      { name: tx('Tags'), href: '/status/tags', an: name === 'tagsVerwalten', extra: zahl(tagZahl, true) },
      { name: tx('KI-Kosten'), href: '/status/kosten', an: name === 'kosten' },
      { name: tx('Papierkorb'), href: '/status/papierkorb', an: name === 'papierkorb', extra: zahl(papierkorbZahl, true) },
    ] },
    // System: was für alle Nutzer gilt, nur für Admins (eigene Gruppe unter den Einstellungen).
    ...(istAdmin() ? [{ id: 'system', name: tx('System'), href: '/status/update', unter: [
      { name: tx('Updates'), href: '/status/update', an: name === 'update', extra: neueVersion ? ` ${updateSymbol}` : '' },
      { name: tx('Backups'), href: '/status/backups', an: name === 'backups' || name === 'backupHochladen' },
      { name: tx('Nutzer'), href: '/admin/nutzer', an: ['nutzer', 'nutzerNeu', 'nutzerBearbeiten', 'einladung', 'neuEinladen'].includes(name) },
    ] }] : []),
  ];
  // Verlauf-Filter ohne Einträge (Zähler 0) sind nicht anklickbar: der Verlauf fiele sonst auf „Alles“ zurück.
  const link = (k, x) => (x.knopf ? T`<button type="button" class="${k} mehr" ${x.knopf}><span>${esc(x.name)}</span></button>` : x.leer && !x.an ? T`<span class="${k} gesperrt" aria-disabled="true"><span>${esc(x.name)}</span>${x.extra || ''}</span>` : linkAktiv(k, x));
  const linkAktiv = (k, x) => T`<a class="${k}${x.an ? ' aktiv' : ''}" href="${x.href}"${x.setze ? T` data-setze="${x.setze[0]}" data-wert="${esc(x.setze[1])}"` : ''}${x.an ? ' aria-current="page"' : ''}><span>${esc(x.name)}</span>${x.extra || ''}</a>`;
  // Fuß: Nutzung, Name (führt zu den Benutzereinstellungen) mit Abmelden rechts, Version und Spenden.
  const nutzerSeiten = ['pebble', 'konto', 'zugangCode', 'geraetNeu', 'mcp', 'tokenNeu', 'kalender', 'benachrichtigungen', 'darstellung'];
  const nutzerAktiv = nutzerSeiten.includes(name);
  nav.innerHTML = T`${teile.map((t) => T`<div class="menuegruppe">${link('menuehaupt', { ...t, an: bereich === t.id })}${t.unter.map((u) => link('menueunter', u)).join('')}</div>`).join('')}
    <div class="menuefuss">${ringBalkenHtml()}
      <div class="nutzerzeile"><a class="nutzerknopf${nutzerAktiv ? ' aktiv' : ''}" href="/konto"${nutzerAktiv ? ' aria-current="page"' : ''}>${ZAHNRAD}<span>${esc(ich.name || tx('Benutzer'))}</span></a>
        <button type="button" class="abmeldeknopf" data-menue-abmelden title="${esc(tx('Abmelden'))}" aria-label="${esc(tx('Abmelden'))}">${ABMELDEN}</button></div>
      <div class="fusszeile">${appVersion ? T`<a class="fussversion" href="/status/update">V ${esc(versionNr(appVersion))}${appKanal ? ` · ${esc(tx(appKanal))}` : ''}</a>` : '<span></span>'}
        ${spendenLink ? T`<a class="fussspende" href="${esc(spendenLink)}" data-kofi>${HERZ}<span>${esc(tx('Spenden'))}</span></a>` : ''}</div></div>`;
  setzeBalken(nav);
  const punkt = menueKnopf.querySelector('.punkt');
  punkt.hidden = !(neueVersion && istAdmin()) && !offeneVorschlaege && !audioFehler;
  punkt.classList.toggle('vorschlag', !neueVersion && !audioFehler);
  punkt.classList.toggle('fehler', !!audioFehler);
  menueKnopf.title = audioFehler ? tx('{0} Aufnahmen ohne Eintrag', audioFehler) : neueVersion ? T`Update ${versionNr(neueVersion)} verfügbar` : offeneVorschlaege ? T`${offeneVorschlaege} Gedächtnis-Vorschläge` : '';
}
const ABMELDEN = T('<svg viewBox="0 0 24 24" aria-hidden="true"><path d="M15 4h3a2 2 0 0 1 2 2v12a2 2 0 0 1-2 2h-3M10 16l-4-4 4-4M6 12h10" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/></svg>');
const ZAHNRAD = T('<svg viewBox="0 0 24 24" aria-hidden="true"><circle cx="12" cy="12" r="3" fill="none" stroke="currentColor" stroke-width="2"/><path d="M19.4 15a1.7 1.7 0 0 0 .3 1.8l.1.1a2 2 0 1 1-2.8 2.8l-.1-.1a1.7 1.7 0 0 0-1.8-.3 1.7 1.7 0 0 0-1 1.5V21a2 2 0 1 1-4 0v-.1a1.7 1.7 0 0 0-1.1-1.5 1.7 1.7 0 0 0-1.8.3l-.1.1a2 2 0 1 1-2.8-2.8l.1-.1a1.7 1.7 0 0 0 .3-1.8 1.7 1.7 0 0 0-1.5-1H3a2 2 0 1 1 0-4h.1a1.7 1.7 0 0 0 1.5-1.1 1.7 1.7 0 0 0-.3-1.8l-.1-.1a2 2 0 1 1 2.8-2.8l.1.1a1.7 1.7 0 0 0 1.8.3H9a1.7 1.7 0 0 0 1-1.5V3a2 2 0 1 1 4 0v.1a1.7 1.7 0 0 0 1 1.5 1.7 1.7 0 0 0 1.8-.3l.1-.1a2 2 0 1 1 2.8 2.8l-.1.1a1.7 1.7 0 0 0-.3 1.8V9a1.7 1.7 0 0 0 1.5 1H21a2 2 0 1 1 0 4h-.1a1.7 1.7 0 0 0-1.5 1z" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linejoin="round"/></svg>');
const HERZ = T('<svg viewBox="0 0 24 24" aria-hidden="true"><path d="M12 20s-7-4.4-7-10a4 4 0 0 1 7-2.6A4 4 0 0 1 19 10c0 5.6-7 10-7 10z" fill="none" stroke="currentColor" stroke-width="2" stroke-linejoin="round"/></svg>');
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
// Schließen: Klick neben das Menü (auf den Hintergrund), Esc (Dialog selbst).
menue.addEventListener('click', (e) => { if (menue.matches(':modal') && e.target === menue) menue.close(); });
menue.addEventListener('click', (e) => { if (e.target.closest('[data-menue-gruppen]')) { alleGruppenZeigen = !alleGruppenZeigen; maleMenue(); } });
menue.addEventListener('click', async (e) => {
  const b = e.target.closest('[data-menue-abmelden]');
  if (!b || !confirm(tx('Auf diesem Gerät abmelden?'))) return;
  b.disabled = true;
  try { await abmelden(); if (menue.open) menue.close(); geheZu('/anmelden', true); } catch (f) { b.disabled = false; melde(f.message); }
});

// Offene Gedächtnis-Vorschläge: Zahl an „Gedächtnis“ im Menü, Punkt am Menüknopf.
let offeneVorschlaege = 0;
const vorschlagZahl = () => (offeneVorschlaege ? ` (${offeneVorschlaege})` : '');
// Neue Version (Hintergrund-Prüfung alle 4 h): Punkt am Menüknopf, Pfeil an „Updates“, Hinweiskarte auf der Statusseite.
let neueVersion = '', papierkorbZahl = 0, audioFehler = 0, appVersion = lokal('app-version'), appKanal = lokal('app-kanal');
// Spenden-Link (Ko-fi) aus FUNDING.yml der Update-Quelle; leer = kein Knopf. Nur unaufdringlich: Menüfuß und ganz unten auf Status.
let spendenLink = lokal('spenden-link', '');
// Knopf im Stil des Ko-fi-Widgets, ohne dessen Skript (fremder Code liefe sonst mit den Rechten der App).
// Tippen öffnet das Ko-fi-Widget (iframe) in einem Fenster; der Link selbst bleibt als Ausweg (neuer Tab).
const spendenKnopf = () => T`<a class="kofiknopf" href="${esc(spendenLink)}" target="_blank" rel="noopener" data-kofi><img src="/kofi-cup.png" alt="" width="24" height="19">${esc(tx('Unterstütze mich'))}</a>`;
const updateSymbol = T('<svg class="updatesymbol" viewBox="0 0 24 24" aria-hidden="true"><circle cx="12" cy="12" r="11" fill="currentColor"/><path d="M12 17V7m-4.5 4.5L12 7l4.5 4.5" fill="none" stroke="var(--flaeche)" stroke-width="2.4" stroke-linecap="round" stroke-linejoin="round"/></svg>');
// Versionsnummer ohne Kanal-Endung: der Kanal steht als Badge daneben, Builds unterscheidet das Datum.
const versionNr = (v) => String(v || '').replace(/-(stable|beta|nightly)(\.\d+)?$/, '');
const versionKanal = (v) => (String(v || '').match(/-(stable|beta|nightly)(\.\d+)?$/) || [])[1] || '';
// Build-Zeit einer Nightly (Endung JJJJMMTTHHMM) als Text, sonst ''.
// Veröffentlichung (RFC 3339) als Text.
const zeitText = (iso) => (iso ? new Date(iso).toLocaleString(LOCALE, { day: '2-digit', month: '2-digit', year: 'numeric', hour: '2-digit', minute: '2-digit' }) : '');
const versionZeit = (v) => {
  const m = String(v || '').match(/-nightly\.(\d{4})(\d\d)(\d\d)(\d\d)(\d\d)$/);
  return m ? new Date(Date.UTC(+m[1], m[2] - 1, +m[3], +m[4], +m[5])).toLocaleString(LOCALE, { day: '2-digit', month: '2-digit', year: 'numeric', hour: '2-digit', minute: '2-digit' }) : '';
};
// Version mit festem Kanal-Badge (stable/beta/nightly) und bei Nightlies der Build-Zeit.
const versionHtml = (v, kanal) => {
  const k = kanal || versionKanal(v), z = versionZeit(v);
  return T`${k ? T`<span class="badge neutral">${esc(tx(k))}</span> ` : ''}<span class="versionnr">${esc(versionNr(v))}</span>${z ? T` <span class="leise">${esc(z)}</span>` : ''}`;
};
// Änderungen einer Version als Blatt (Update-Seite, Karte „App aktualisiert“).
function zeigeKofi() {
  document.querySelector('dialog.kofiblatt')?.remove();
  const d = document.createElement('dialog');
  d.className = 'mehrblatt kofiblatt';
  d.tabIndex = -1;
  d.innerHTML = T`<div class="griff" aria-hidden="true"></div>
    <iframe class="kofiframe" src="${esc(spendenLink)}/?hidefeed=true&amp;widget=true&amp;embed=true&amp;preview=true" height="712" title="Ko-fi"></iframe>
    <div class="leiste"><a class="btn" href="${esc(spendenLink)}" target="_blank" rel="noopener">${esc(tx('In neuem Tab öffnen'))}</a><button class="btn" data-zu="1">${esc(tx('Schließen'))}</button></div>`;
  d.onclick = (ev) => { if (ev.target === d || ev.target.closest('[data-zu]')) d.close(); };
  d.onclose = () => d.remove();
  document.body.append(d);
  d.showModal();
  d.focus();
}
document.addEventListener('click', (ev) => {
  const a = ev.target.closest('a[data-kofi]');
  if (!a || !spendenLink || ev.ctrlKey || ev.metaKey || ev.shiftKey) return;
  ev.preventDefault();
  if (menue.open && menue.matches(':modal')) menue.close();
  zeigeKofi();
});

// abschnitte (optional): mehrere Versionen [{version, datum, text}], neueste oben – statt text.
function zeigeAenderungen(titel, text, alleUrl, abschnitte) {
  document.querySelector('dialog.logblatt')?.remove();
  const d = document.createElement('dialog');
  d.className = 'mehrblatt logblatt';
  d.tabIndex = -1;
  const inhalt = abschnitte?.length
    ? abschnitte.map((a) => T`<h3 class="versionskopf">${esc(a.version)}${a.datum ? T` <span class="leise">${esc(a.datum)}</span>` : ''}</h3>${aenderungenHtml(a.text)}`).join('')
    : text ? aenderungenHtml(text) : T('<p class="leise">Keine Änderungsliste zu dieser Version.</p>');
  d.innerHTML = T`<div class="griff" aria-hidden="true"></div><h2>${esc(titel)}</h2>
    ${inhalt}
    ${alleUrl ? T`<p><a href="${esc(alleUrl)}" target="_blank" rel="noopener">Alle Änderungen auf GitHub</a></p>` : ''}
    <div class="leiste"><button class="btn" data-zu="1">Schließen</button></div>`;
  d.onclick = (ev) => { if (ev.target === d || ev.target.closest('[data-zu]')) d.close(); };
  d.onclose = () => d.remove();
  document.body.append(d);
  d.showModal();
  d.focus();
  d.scrollTop = 0; // showModal fokussiert sonst den Link ganz unten und scrollt dorthin
}

// Neue Version seit der letzten Bestätigung (z. B. automatisches Update): Karte über jeder Seite, bis sie bestätigt ist.
// Gilt für alle Geräte gemeinsam (ein Nutzer): der Server merkt sich die bestätigte Version.
let versionBestaetigt = lokal('version-bestaetigt');
try { ringInfo = JSON.parse(lokal('ring-info', 'null')); } catch { /* noch nie geladen */ }
function zeigeVersionskarte() {
  const karte = document.getElementById('versionskarte');
  const jetzt = `${appVersion}|${appKanal}`;
  karte.hidden = !appVersion || !versionBestaetigt || versionBestaetigt === jetzt;
  if (karte.hidden) return;
  const [altV, altK] = versionBestaetigt.split('|');
  // Alte und neue Version nebeneinander, darunter zwei gleich große Knöpfe nebeneinander.
  karte.innerHTML = T`<div class="versionszeile">${updateSymbol}<div><strong>${tx('App aktualisiert')}</strong><div class="versionswechsel"><span>${versionHtml(altV, altK)}</span><span aria-hidden="true">→</span><span>${versionHtml(appVersion, appKanal)}</span></div></div></div>
    <div class="versionsknoepfe"><button type="button" class="btn" data-version-log>${tx('Änderungen ansehen')}</button><button type="button" class="btn haupt" data-version-ok>${tx('Bestätigen')}</button></div>`;
  karte.querySelector('[data-version-log]').onclick = async (ev) => {
    ev.currentTarget.disabled = true;
    try {
      // Alle Änderungen seit der zuletzt bestätigten Version, auch über übersprungene Zwischenstände hinweg.
      const u = await api(`/api/update?seit=${encodeURIComponent(altV)}`);
      zeigeAenderungen(`${versionNr(altV)} → ${versionNr(appVersion)}`, u.aktuell_aenderungen, u.alle_aenderungen, u.seit_aenderungen);
    } catch (f) { melde(f.message); }
    ev.currentTarget.disabled = false;
  };
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
    // menue=1: Zahlen des Seitenmenüs (Verlauf-Filter mit Tag-Filter, Neu seit zuletzt gesehen, Listen-Gruppen).
    const grenze = besuche.grenze();
    const z = await api(`/api/zaehler?${['menue=1', Number.isFinite(grenze) ? T`seit=${grenze}` : '', ...gewaehlteTags().map((t) => T`tag=${encodeURIComponent(t)}`)].filter(Boolean).join('&')}`);
    if (z.menue) {
      try {
        localStorage.setItem('verlauf-zahlen', JSON.stringify(z.menue.zahlen));
        localStorage.setItem('verlauf-neu', JSON.stringify(z.menue.neu));
        localStorage.setItem('listen-gruppen', JSON.stringify(z.menue.gruppen));
      } catch { /* nur Anzeige */ }
    }
    offeneVorschlaege = z.vorschlaege || 0;
    neueVersion = z.update || '';
    papierkorbZahl = z.papierkorb || 0;
    audioFehler = z.audio_unbekannt || 0;
    joplinAktiv = !!z.joplin;
    try { localStorage.setItem('joplin-aktiv', joplinAktiv ? '1' : '0'); } catch { /* nur Anzeige */ }
    if (z.version) { appVersion = z.version; appKanal = z.kanal || ''; try { localStorage.setItem('app-version', appVersion); localStorage.setItem('app-kanal', appKanal); } catch { /* nur Anzeige */ } }
    if (z.version_bestaetigt) { versionBestaetigt = z.version_bestaetigt; try { localStorage.setItem('version-bestaetigt', versionBestaetigt); } catch { /* nur Anzeige */ } }
    speicherVoll = z.speicher_voll || null;
    zeigeSpeicherkarte();
    ringInfo = z.ring || null;
    alteAdressen = z.alte_adressen || [];
    pebbleTest = z.pebble_test || null;
    try { localStorage.setItem('ring-info', JSON.stringify(ringInfo)); } catch { /* nur Anzeige */ }
    spendenLink = /^https:\/\/ko-fi\.com\/[A-Za-z0-9_]+$/.test(z.spenden || '') ? z.spenden : '';
    try { localStorage.setItem('spenden-link', spendenLink); } catch { /* nur Anzeige */ }
    zeigeVersionskarte();
    zeigeRingkarte();
  } catch (f) {
    return; // offline: alte Zahl bleibt
  }
  maleMenue();
}

// ---------- Live-Daten (SSE), Verbindung, Start ----------

let quelle = null, ladeTimer = null, zaehlerTimer = null;
function starteLive() {
  if (quelle) return;
  quelle = new EventSource('/api/ereignisse');
  quelle.onopen = () => { sendeAusgang(); };
  quelle.onmessage = (ev) => {
    // Jede Änderung (erledigt, gelöscht, Liste/Gruppe neu …) kann die Zahlen im Menü ändern: gebündelt neu laden.
    clearTimeout(zaehlerTimer);
    zaehlerTimer = setTimeout(ladeZaehler, 800);
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
  const ohneSitzung = ['/anmelden', '/einrichten', '/ersteinrichtung', '/api/v1/ai/connect/openrouter'].includes(pfad);
  const vorab = lokal('angemeldet') === '1' && !ohneSitzung;
  if (vorab) zeige();
  if (!ohneSitzung) {
    try {
      const s = await api('/api/sitzung');
      // Anderer Nutzer als zuletzt auf diesem Gerät: dessen Zwischenspeicher (Einträge, Vorgemerktes) verwerfen.
      if (s.angemeldet && ich.id && s.id !== ich.id) { await vergissGeraet(); location.reload(); return; }
      try { localStorage.setItem('angemeldet', s.angemeldet ? '1' : '0'); } catch { /* nur Startabkürzung */ }
      if (s.angemeldet) merkeIch(s);
      if (!s.angemeldet) {
        // Freigabe für eine App (OAuth): nach der Anmeldung dorthin zurück.
        if (pfad === '/verbinden') try { sessionStorage.setItem('nach-anmeldung', pfad + location.search); } catch { /* dann zur Startseite */ }
        return geheZu('/anmelden', true);
      }
      if (s.gesperrt === 'umschluesselung') return geheZu('/konto/code', true);
      if (s.gesperrt) return geheZu('/wiederherstellen', true);
      if (s.code_offen && pfad !== '/willkommen') return geheZu('/willkommen', true);
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
