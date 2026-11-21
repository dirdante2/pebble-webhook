// pebble-webhook: nimmt Sprachaufnahmen des Pebble Index 01 an, transkribiert sie,
// erstellt Diktate oder Planungen mit Recherche und schiebt fertige Ergebnisse nach Joplin.
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"pebble-webhook/internal/app"
	"pebble-webhook/internal/config"
	"pebble-webhook/internal/joplin"
	"pebble-webhook/internal/openai"
	"pebble-webhook/internal/store"
)

var version = "dev"

const usage = `pebble-webhook <Befehl>

  serve     Dienst starten (Webhook, Verarbeitung, Web-App)
  enroll    Einmal-Link zur Passkey-Einrichtung ausgeben (15 Minuten gültig)
  zugang    Prüfzugang ohne Passkey anlegen: gibt ein Sitzungs-Cookie aus (Name als Argument);
            erscheint in der Web-App unter Geräte und lässt sich dort entfernen
  termine-nachtragen [Tage]
            fertige Notizen der letzten Tage (Standard 30) einmalig auf Termine prüfen
  notizen-einsortieren
            fertige Notizen ohne Liste einmalig in eine Liste legen (KI, sonst „Unsortiert“)
  gedaechtnis-roh [vorschlaege]
            rohe Korrektur-/Rückfrage-Zeilen im aktiven Gedächtnis (bzw. alle offenen Vorschläge) zu Beobachtungen machen
  gedaechtnis-zuruecksetzen
            alle Gedächtnis-Einträge samt Entscheidungen löschen (Beobachtungen bleiben); danach in der Web-App „Jetzt lernen“
  ersteinrichtung
            Pfad des Einrichtungsassistenten mit Einmal-Code ausgeben (nur solange nicht eingerichtet)
  version   Version ausgeben

Start-Werte (LISTEN, DATA_DIR, AUDIO_DIR) kommen aus der Umgebung; außerhalb von systemd wird die
Datei aus PEBBLE_ENV_FILE gelesen (Standard /etc/pebble-webhook/env). Alle übrigen Einstellungen
liegen in der Datenbank und werden in der Web-App gepflegt.`

func main() {
	log.SetFlags(0) // Zeitstempel setzt das Journal
	cmd := ""
	if len(os.Args) > 1 {
		cmd = os.Args[1]
	}
	switch cmd {
	case "serve":
		fail(serve())
	case "enroll":
		fail(enroll())
	case "zugang":
		fail(access())
	case "termine-nachtragen":
		fail(backfill())
	case "notizen-einsortieren":
		fail(sortNotes())
	case "gedaechtnis-roh":
		fail(rawMemory())
	case "gedaechtnis-zuruecksetzen":
		fail(resetMemory())
	case "ersteinrichtung":
		fail(setupLink())
	case "version":
		fmt.Println(version)
	default:
		fmt.Fprintln(os.Stderr, usage)
		os.Exit(2)
	}
}

func fail(err error) {
	if err != nil {
		log.Fatalf("Fehler: %v", err)
	}
}

// openDB liest die Start-Werte und öffnet die Datenbank.
func openDB() (config.Bootstrap, *store.Store, error) {
	envFile := os.Getenv("PEBBLE_ENV_FILE")
	if envFile == "" {
		envFile = "/etc/pebble-webhook/env"
	}
	if err := config.LoadFile(envFile); err != nil && !errors.Is(err, os.ErrNotExist) {
		return config.Bootstrap{}, nil, fmt.Errorf("%s: %w", envFile, err)
	}
	boot := config.LoadBootstrap()
	for _, dir := range []string{boot.DataDir, boot.AudioDir} {
		if st, err := os.Stat(dir); err != nil || !st.IsDir() {
			return boot, nil, fmt.Errorf("Verzeichnis %s fehlt", dir)
		}
	}
	db, err := store.Open(filepath.Join(boot.DataDir, "pebble.db"))
	return boot, db, err
}

func setup() (*app.App, *store.Store, error) {
	boot, db, err := openDB()
	if err != nil {
		return nil, nil, err
	}
	cfg, err := app.LoadSettings(db, boot)
	if err != nil {
		db.Close()
		if errors.Is(err, app.ErrNotConfigured) {
			return nil, nil, fmt.Errorf("noch nicht eingerichtet – Einrichtungsassistent: pebble ersteinrichtung")
		}
		return nil, nil, err
	}
	var notes app.Notes
	if cfg.JoplinURL != "" {
		notes = joplin.New(cfg.JoplinURL, cfg.JoplinEmail, cfg.JoplinPassword)
	}
	a, err := app.New(cfg, db, openai.New(cfg.OpenAIKey, cfg.OpenAIBaseURL), notes)
	if err != nil {
		db.Close()
		return nil, nil, err
	}
	a.Version = version
	return a, db, nil
}

func serve() error {
	boot, db, err := openDB()
	if err != nil {
		return err
	}
	if _, err := app.LoadSettings(db, boot); err != nil {
		// Ohne gültige Einstellungen läuft nur der Einrichtungsassistent.
		return serveSetup(boot, db, err)
	}
	db.Close()
	a, db, err := setup()
	if err != nil {
		return err
	}
	defer db.Close()
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	done := make(chan struct{})
	go func() { a.Run(ctx); close(done) }()

	srv := &http.Server{Addr: a.Cfg.Listen, Handler: a.Handler(), ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout: 2 * time.Minute, IdleTimeout: 2 * time.Minute}
	errc := make(chan error, 1)
	go func() { errc <- srv.ListenAndServe() }()
	log.Printf("pebble-webhook %s hört auf %s", version, a.Cfg.Listen)

	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}
	shut, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	srv.Shutdown(shut)
	select {
	case <-done:
	case <-time.After(20 * time.Second):
	}
	return nil
}

func serveSetup(boot config.Bootstrap, db *store.Store, loadErr error) error {
	defer db.Close()
	code, err := app.SetupCode(db)
	if err != nil {
		return err
	}
	if errors.Is(loadErr, app.ErrNotConfigured) {
		log.Printf("pebble-webhook %s: noch nicht eingerichtet", version)
	} else {
		log.Printf("pebble-webhook %s: Einstellungen ungültig (%v) – bitte im Einrichtungsassistenten korrigieren", version, loadErr)
	}
	log.Printf("Einrichtungsassistent: http://<server>:%s/ersteinrichtung?code=%s", port(boot.Listen), code)
	s := &app.SetupServer{DB: db, Boot: boot, Version: version, LoadErr: loadErr}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	srv := &http.Server{Addr: boot.Listen, Handler: s.Handler(), ReadHeaderTimeout: 10 * time.Second}
	errc := make(chan error, 1)
	go func() { errc <- srv.ListenAndServe() }()
	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}
	shut, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return srv.Shutdown(shut)
}

func port(listen string) string {
	if i := strings.LastIndex(listen, ":"); i >= 0 {
		return listen[i+1:]
	}
	return listen
}

func setupLink() error {
	boot, db, err := openDB()
	if err != nil {
		return err
	}
	defer db.Close()
	if _, err := app.LoadSettings(db, boot); err == nil {
		return fmt.Errorf("schon eingerichtet – Einstellungen in der Web-App unter Status → Einstellungen")
	}
	code, err := app.SetupCode(db)
	if err != nil {
		return err
	}
	fmt.Printf("Einrichtungsassistent (im Browser öffnen):\n\n  http://<server>:%s/ersteinrichtung?code=%s\n", port(boot.Listen), code)
	return nil
}

func access() error {
	name := "Prüfzugang"
	if len(os.Args) > 2 {
		name = os.Args[2]
	}
	a, db, err := setup()
	if err != nil {
		return err
	}
	defer db.Close()
	cookie, err := a.TestAccess(name)
	if err != nil {
		return err
	}
	fmt.Println(cookie)
	return nil
}

func enroll() error {
	a, db, err := setup()
	if err != nil {
		return err
	}
	defer db.Close()
	link, err := a.EnrollLink()
	if err != nil {
		return err
	}
	fmt.Printf("Einmal-Link (15 Minuten gültig, nur einmal nutzbar):\n\n  %s\n\nIm Browser des Geräts öffnen, das den Passkey bekommen soll.\n", link)
	return nil
}

func backfill() error {
	days := 30
	if len(os.Args) > 2 {
		n, err := strconv.Atoi(os.Args[2])
		if err != nil || n < 1 {
			return fmt.Errorf("Tage: positive Zahl erwartet")
		}
		days = n
	}
	a, db, err := setup()
	if err != nil {
		return err
	}
	defer db.Close()
	checked, found, cost, err := a.BackfillAppointments(context.Background(), days)
	fmt.Printf("Geprüft: %d, Termine erkannt: %d, Kosten: %.4f USD\n", checked, found, cost)
	return err
}

func sortNotes() error {
	a, db, err := setup()
	if err != nil {
		return err
	}
	defer db.Close()
	checked, sorted, cost, err := a.BackfillNoteLists(context.Background())
	fmt.Printf("Ohne Liste: %d, einsortiert: %d, Kosten: %.4f USD\n", checked, sorted, cost)
	return err
}

func rawMemory() error {
	a, db, err := setup()
	if err != nil {
		return err
	}
	defer db.Close()
	n, err := a.RawToObservations(len(os.Args) > 2 && os.Args[2] == "vorschlaege")
	fmt.Printf("Zu Beobachtungen gemacht: %d (die KI wertet sie in etwa 10 Minuten aus)\n", n)
	return err
}

func resetMemory() error {
	_, db, err := setup()
	if err != nil {
		return err
	}
	defer db.Close()
	n, err := db.ResetMemory()
	fmt.Printf("Gedächtnis zurückgesetzt: %d Einträge entfernt\n", n)
	return err
}
