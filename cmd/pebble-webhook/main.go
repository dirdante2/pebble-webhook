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
  aufnahmen-verschluesseln
            Aufnahmen bis 2.x sofort verschlüsselt in den Ordner des Nutzers legen (sonst 24 h nach dem Umzug)
  nutzer-anlegen <Name> [admin]
            Nutzer anlegen (Schlüssel, verschlüsselte Daten); danach mit PEBBLE_NUTZER=<Id> enroll den Passkey-Link
  schluessel-wiederherstellen <Nutzer-Id> <Code>
            Schlüsseldatei eines Nutzers mit seinem Wiederherstellungscode neu schreiben (Restore auf neuer Maschine)
  vorbereiten
            Datenbestand bei gestopptem Dienst auf den Stand dieses Programms bringen (vom Installer vor dem Start
            aufgerufen, ohne Zeitgrenze der Gesundheitsprüfung); --kann: nur prüfen, ob es den Befehl gibt
  verzeichnisse
            Ordner (DATA_DIR, DATA_DIR/users, MEDIA_DIR, BACKUP_DIR, KEY_DIR) anlegen und Besitzer/Rechte richten
            (als root; vor jedem Start durch systemd und beim Installieren); ohne root nur prüfen
  version   Version ausgeben
  schema [DATEI]
            Schema-Stand ausgeben, den dieses Programm kennt – mit DATEI den einer Datenbank (für den Installer)

Befehle für die Daten eines Nutzers (enroll, zugang, termine-nachtragen, notizen-einsortieren, gedaechtnis-…)
gelten für Nutzer 1, mit PEBBLE_NUTZER=<Id> für einen anderen.

Start-Werte (LISTEN, DATA_DIR, MEDIA_DIR, BACKUP_DIR, KEY_DIR) kommen aus der Umgebung; außerhalb von systemd wird die
Datei aus PEBBLE_ENV_FILE gelesen (Standard /etc/pebble-webhook/env). Alle übrigen Einstellungen
liegen in der Datenbank und werden in der Web-App gepflegt.`

func main() {
	log.SetFlags(0) // Zeitstempel setzt das Journal
	syscall.Umask(0o027) // Datenbanken, Schlüssel und Aufnahmen nie für andere lesbar (auch beim Aufruf über die Kommandozeile)
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
	case "aufnahmen-verschluesseln":
		fail(sealAudio())
	case "nutzer-anlegen":
		fail(addUser())
	case "schluessel-wiederherstellen":
		fail(recoverKey())
	case "vorbereiten":
		if len(os.Args) > 2 && os.Args[2] == "--kann" {
			fmt.Println("ja")
			return
		}
		fail(prepare())
	case "verzeichnisse":
		if len(os.Args) > 2 && os.Args[2] == "--kann" {
			fmt.Println("ja")
			return
		}
		fail(dirs())
	case "version":
		fmt.Println(version)
	case "schema":
		if len(os.Args) > 2 {
			v, err := store.DBSchema(os.Args[2])
			fail(err)
			fmt.Println(v)
		} else {
			fmt.Println(store.SchemaVersion)
		}
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

// loadBoot liest die Start-Werte aus der env-Datei (PEBBLE_ENV_FILE, sonst /etc/pebble-webhook/env) und der Umgebung.
func loadBoot() (config.Bootstrap, error) {
	envFile := os.Getenv("PEBBLE_ENV_FILE")
	if envFile == "" {
		envFile = "/etc/pebble-webhook/env"
	}
	if err := config.LoadFile(envFile); err != nil && !errors.Is(err, os.ErrNotExist) {
		return config.Bootstrap{}, fmt.Errorf("%s: %w", envFile, err)
	}
	return config.LoadBootstrap(), nil
}

// dirs richtet als root die Ordner ein (Mounts nach Anlegen oder Wiederherstellen gehören oft root); sonst nur Prüfung.
func dirs() error {
	boot, err := loadBoot()
	if err != nil {
		return err
	}
	if os.Geteuid() != 0 {
		if err := config.CheckDirs(boot); err != nil {
			return err
		}
		fmt.Println("Ordner in Ordnung")
		return nil
	}
	report, err := config.EnsureDirs(boot)
	for _, r := range report {
		log.Printf("Ordner: %s", r)
	}
	return err
}

// openDB liest die Start-Werte, bringt den Datenbestand auf den Stand dieses Programms (Umzug von pebble.db bis 2.x)
// und öffnet system.db.
func openDB() (config.Bootstrap, *store.System, error) {
	boot, err := loadBoot()
	if err != nil {
		return boot, nil, err
	}
	// Alle Ordner da und beschreibbar? Sonst klar abbrechen statt mitten im Betrieb (z. B. Mount gehört root).
	if err := config.CheckDirs(boot); err != nil {
		return boot, nil, err
	}
	if err := app.Prepare(boot); err != nil {
		return boot, nil, err
	}
	db, err := store.OpenSystem(filepath.Join(boot.DataDir, "system.db"))
	return boot, db, err
}

// setup baut das System und öffnet die Daten aller Nutzer.
func setup() (*app.System, error) {
	boot, db, err := openDB()
	if err != nil {
		return nil, err
	}
	cfg, err := app.LoadSettings(db, boot)
	if err != nil {
		db.Close()
		if errors.Is(err, app.ErrNotConfigured) {
			return nil, fmt.Errorf("noch nicht eingerichtet – Einrichtungsassistent: pebble ersteinrichtung")
		}
		return nil, err
	}
	notes := func(c *config.Config) app.Notes {
		if c.JoplinURL == "" {
			return nil
		}
		return joplin.New(c.JoplinURL, c.JoplinEmail, c.JoplinPassword)
	}
	sys, err := app.NewSystem(cfg, db, openai.New(cfg.OpenAIKey, cfg.OpenAIBaseURL), version, notes)
	if err != nil {
		db.Close()
		return nil, err
	}
	if err := sys.OpenUsers(); err != nil {
		db.Close()
		return nil, err
	}
	return sys, nil
}

// userID: Nutzer für Befehle der Kommandozeile (Umgebung PEBBLE_NUTZER, sonst 1).
func userID() int64 {
	if v, err := strconv.ParseInt(os.Getenv("PEBBLE_NUTZER"), 10, 64); err == nil && v > 0 {
		return v
	}
	return 1
}

// userApp: System samt geöffneter App des Nutzers aus userID.
func userApp() (*app.System, *app.App, error) {
	sys, err := setup()
	if err != nil {
		return nil, nil, err
	}
	a := sys.AppOf(userID())
	if a == nil {
		closeAll(sys)
		return nil, nil, fmt.Errorf("Daten von Nutzer %d nicht geöffnet (gesperrt oder Schlüssel fehlt)", userID())
	}
	return sys, a, nil
}

func closeAll(sys *app.System) {
	sys.Close()
	sys.DB.Close()
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
	a, err := setup()
	if err != nil {
		return err
	}
	defer closeAll(a)
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

func serveSetup(boot config.Bootstrap, db *store.System, loadErr error) error {
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

// prepare: Datenbank öffnen (prüft sie und führt alle Migrationsschritte aus) und wieder schließen.
func prepare() error {
	_, db, err := openDB()
	if err != nil {
		return err
	}
	return db.Close()
}

func sealAudio() error {
	sys, a, err := userApp()
	if err != nil {
		return err
	}
	defer closeAll(sys)
	n, err := a.SealLegacyAudio()
	fmt.Printf("Verschlüsselt und verschoben: %d Aufnahmen\n", n)
	return err
}

func addUser() error {
	if len(os.Args) < 3 || strings.TrimSpace(os.Args[2]) == "" {
		return fmt.Errorf("Aufruf: nutzer-anlegen <Name> [admin]")
	}
	role := store.RoleUser
	if len(os.Args) > 3 && os.Args[3] == "admin" {
		role = store.RoleAdmin
	}
	sys, err := setup()
	if err != nil {
		return err
	}
	defer closeAll(sys)
	a, _, err := sys.NewUser(strings.TrimSpace(os.Args[2]), role)
	if err != nil {
		return err
	}
	fmt.Printf("Nutzer %d angelegt (%s). Passkey-Link: PEBBLE_NUTZER=%d pebble enroll – den Wiederherstellungscode zeigt die Web-App nach der Anmeldung.\n", a.UserID, role, a.UserID)
	return nil
}

// recoverKey: Schlüsseldatei eines Nutzers mit seinem Wiederherstellungscode neu schreiben (z. B. nach Restore auf
// einer neuen Maschine; in der Web-App geht es nach der Anmeldung ebenso).
func recoverKey() error {
	if len(os.Args) < 4 {
		return fmt.Errorf("Aufruf: schluessel-wiederherstellen <Nutzer-Id> <Code>")
	}
	uid, err := strconv.ParseInt(os.Args[2], 10, 64)
	if err != nil {
		return fmt.Errorf("Nutzer-Id: Zahl erwartet")
	}
	boot, db, err := openDB()
	if err != nil {
		return err
	}
	defer db.Close()
	return app.RecoverKey(db, boot.KeyDir, uid, os.Args[3])
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
	sys, err := setup()
	if err != nil {
		return err
	}
	defer closeAll(sys)
	cookie, err := sys.TestAccess(userID(), name)
	if err != nil {
		return err
	}
	fmt.Println(cookie)
	return nil
}

func enroll() error {
	sys, err := setup()
	if err != nil {
		return err
	}
	defer closeAll(sys)
	link, err := sys.EnrollLink(userID())
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
	sys, a, err := userApp()
	if err != nil {
		return err
	}
	defer closeAll(sys)
	checked, found, cost, err := a.BackfillAppointments(context.Background(), days)
	fmt.Printf("Geprüft: %d, Termine erkannt: %d, Kosten: %.4f USD\n", checked, found, cost)
	return err
}

func sortNotes() error {
	sys, a, err := userApp()
	if err != nil {
		return err
	}
	defer closeAll(sys)
	checked, sorted, cost, err := a.BackfillNoteLists(context.Background())
	fmt.Printf("Ohne Liste: %d, einsortiert: %d, Kosten: %.4f USD\n", checked, sorted, cost)
	return err
}

func rawMemory() error {
	sys, a, err := userApp()
	if err != nil {
		return err
	}
	defer closeAll(sys)
	n, err := a.RawToObservations(len(os.Args) > 2 && os.Args[2] == "vorschlaege")
	fmt.Printf("Zu Beobachtungen gemacht: %d (die KI wertet sie in etwa 10 Minuten aus)\n", n)
	return err
}

func resetMemory() error {
	sys, a, err := userApp()
	if err != nil {
		return err
	}
	defer closeAll(sys)
	n, err := a.DB.ResetMemory()
	fmt.Printf("Gedächtnis zurückgesetzt: %d Einträge entfernt\n", n)
	return err
}
