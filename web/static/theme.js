// Darstellung: Tag/Nacht-Modus und Farbpalette, je Gerät im Browser gemerkt. Läuft vor dem ersten Zeichnen,
// damit die Seite nicht kurz in der falschen Farbe erscheint. Neue Geräte übernehmen den letzten Stand vom Server (app.js).
(() => {
  // Akzent je Palette: [hell, dunkel]
  const PALETTEN = {
    blau: ['Blau', '#2f6fde', '#6b9cf5'],
    gruen: ['Grün', '#1d7a4f', '#4cc38a'],
    violett: ['Violett', '#6f47c9', '#a98bf0'],
    bernstein: ['Bernstein', '#b35c00', '#f0a640'],
    graphit: ['Graphit', '#3d4654', '#aab4c3'],
  };
  const dunkelMq = matchMedia('(prefers-color-scheme: dark)');
  const gueltigeFarbe = (f) => /^#[0-9a-f]{6}$/i.test(f || '');
  const lies = () => { try { return JSON.parse(localStorage.getItem('darstellung')) || null; } catch { return null; } };
  const rgb = (h) => [1, 3, 5].map((i) => parseInt(h.slice(i, i + 2), 16));
  const hexAus = (r) => `#${r.map((x) => Math.round(Math.max(0, Math.min(255, x))).toString(16).padStart(2, '0')).join('')}`;
  const helligkeit = (h) => { const [r, g, b] = rgb(h).map((x) => { x /= 255; return x <= 0.03928 ? x / 12.92 : ((x + 0.055) / 1.055) ** 2.4; }); return 0.2126 * r + 0.7152 * g + 0.0722 * b; };
  // Eigene Farbe auf dunklem Grund: dunkle Töne aufhellen, damit sie lesbar bleiben.
  const fuerDunkel = (h) => (helligkeit(h) < 0.18 ? hexAus(rgb(h).map((x) => x + (255 - x) * 0.4)) : h);
  function anwenden(d) {
    d = d || lies() || {};
    const root = document.documentElement;
    const modus = d.modus === 'hell' || d.modus === 'dunkel' ? d.modus : 'auto';
    if (modus === 'auto') delete root.dataset.modus; else root.dataset.modus = modus;
    const dunkel = modus === 'dunkel' || (modus === 'auto' && dunkelMq.matches);
    root.style.colorScheme = dunkel ? 'dark' : 'light';
    let akzent;
    if (d.palette === 'eigene' && gueltigeFarbe(d.farbe)) akzent = dunkel ? fuerDunkel(d.farbe) : d.farbe;
    else { const p = PALETTEN[d.palette] || PALETTEN.blau; akzent = dunkel ? p[2] : p[1]; }
    root.style.setProperty('--akzent', akzent);
    root.style.setProperty('--diktat', akzent);
    root.style.setProperty('--akzent-text', helligkeit(akzent) > 0.4 ? '#0e1116' : '#fff');
    const meta = document.querySelector('meta[name=theme-color]');
    if (meta) meta.content = dunkel ? '#14181f' : '#f4f5f7';
  }
  dunkelMq.addEventListener('change', () => anwenden());
  window.darstellung = {
    PALETTEN, lies, anwenden, gueltigeFarbe,
    setze(d) { try { localStorage.setItem('darstellung', JSON.stringify(d)); } catch { /* nur für diese Sitzung */ } anwenden(d); },
  };
  anwenden();
})();
