package app

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"pebble-webhook/internal/config"
	"pebble-webhook/internal/schluessel"
	"pebble-webhook/internal/store"
)

// Umzug auf mehrere Nutzer (3.0): aus DATA_DIR/pebble.db (Klartext, bis 2.x) werden system.db und die verschlüsselte
// users/1/daten.db des ersten Nutzers (Admin). Läuft bei gestopptem Dienst (Installer: „vorbereiten“) oder beim Start.
//  1. pebble.db auf den Endstand bis 2.x bringen, WAL zurückschreiben.
//  2. Im Staging DATA_DIR/.umzug: system.db anlegen – Einstellungen, Systemwerte, Nutzer 1 mit bisheriger WebAuthn-Id,
//     Passkeys, Sitzungen und Einmal-Links (Anmeldung und Prüfzugang gelten weiter), Zugänge als Hash.
//  3. Schlüssel erzeugen (Datei in KEY_DIR, mit Wiederherstellungscode eingewickelt in system.db).
//  4. pebble.db verschlüsselt kopieren (VACUUM INTO), Schema-Schritt „je Nutzer“ (Anmeldung und Systemwerte raus,
//     user_id an jede Tabelle), Zugang der Pebble-App und Code in die verschlüsselte Datenbank.
//  5. Zeilen zählen und vergleichen, dann umbenennen: users/ und system.db an ihren Platz, pebble.db → pebble.db.vor-3.0,
//     an ihre Stelle ein Platzhalter (Schema 1000), damit 2.x nicht still mit leerer Datenbank startet.
// Bricht er ab, bleibt pebble.db unverändert; der nächste Lauf beginnt von vorn.

const legacyNote = "Daten liegen seit 3.0 in system.db und users/<id>/daten.db; die alte Datenbank heißt pebble.db.vor-3.0"

// Prepare bringt den Datenbestand auf den Stand dieses Programms: Umzug von pebble.db, sonst nur Migrationen.
func Prepare(boot config.Bootstrap) error {
	sysPath := filepath.Join(boot.DataDir, "system.db")
	legacy := filepath.Join(boot.DataDir, "pebble.db")
	_, errSys := os.Stat(sysPath)
	_, errLegacy := os.Stat(legacy)
	switch {
	case errSys == nil && errLegacy == nil && !store.IsTombstone(legacy):
		return fmt.Errorf("pebble.db (bis 2.x) und system.db (ab 3.0) liegen zugleich in %s – bitte klären, welcher Stand gilt", boot.DataDir)
	case errSys != nil && errLegacy == nil && !store.IsTombstone(legacy):
		if err := migrateLegacy(boot); err != nil {
			return fmt.Errorf("Umzug auf mehrere Nutzer: %w", err)
		}
	}
	// Migrationen von system.db und allen Nutzern (deren Schlüssel lesbar ist), damit der Start nicht wartet.
	sys, err := store.OpenSystem(sysPath)
	if err != nil {
		return err
	}
	defer sys.Close()
	users, err := sys.Users()
	if err != nil {
		return err
	}
	for _, u := range users {
		dek, err := schluessel.Read(boot.KeyDir, u.ID, u.KeyID)
		if err != nil {
			log.Printf("Nutzer %d: %v – Daten bleiben gesperrt bis zur Wiederherstellung", u.ID, err)
			continue
		}
		db, err := store.OpenUser(UserDB(boot.DataDir, u.ID), dek.DBKey(), u.ID)
		if err != nil {
			return fmt.Errorf("Nutzer %d: %w", u.ID, err)
		}
		db.Close()
	}
	return nil
}

// systemValues: Werte aus pebble.db, die in system.db weiterleben.
var systemValues = []string{"update-releases", "auto-update-letzt", "backup-letzt", "spenden-link", "vapid_public", "vapid_private",
	"ersteinrichtung:code"}

// legacySecrets: frühere Einstellungen, die jetzt Zugänge des Nutzers sind (nicht mehr in den Einstellungen).
var legacySecrets = map[string]bool{"HOOK_TOKEN": true, "HOOK_SIGNING_SECRET": true, "API_TOKEN": true}

func migrateLegacy(boot config.Bootstrap) error {
	start := time.Now()
	legacyPath := filepath.Join(boot.DataDir, "pebble.db")
	old, err := store.OpenLegacy(legacyPath)
	if err != nil {
		return err
	}
	defer old.Close()
	if _, err := old.DB.Exec(`PRAGMA wal_checkpoint(TRUNCATE)`); err != nil {
		return err
	}
	stage := filepath.Join(boot.DataDir, ".umzug")
	if err := os.RemoveAll(stage); err != nil {
		return err
	}
	if err := os.MkdirAll(stage, 0o750); err != nil {
		return err
	}
	sys, err := store.OpenSystem(filepath.Join(stage, "system.db"))
	if err != nil {
		return err
	}
	defer sys.Close()

	// Einstellungen und Systemwerte.
	settings, err := old.Settings()
	if err != nil {
		return err
	}
	keep := map[string]string{}
	for k, v := range settings {
		if !legacySecrets[k] {
			keep[k] = v
		}
	}
	if err := sys.SaveSettings(keep); err != nil {
		return err
	}
	for _, k := range systemValues {
		if v, err := old.Get(k); err != nil {
			return err
		} else if v != "" {
			if err := sys.Set(k, v); err != nil {
				return err
			}
		}
	}

	// Nutzer 1 (Admin) mit der bisherigen WebAuthn-Id: vorhandene Passkeys melden sich weiter an.
	wid, err := old.Get("webauthn_user")
	if err != nil {
		return err
	}
	dek, err := schluessel.New()
	if err != nil {
		return err
	}
	code, err := schluessel.NewCode()
	if err != nil {
		return err
	}
	wrap, err := schluessel.Wrap(code, 1, dek)
	if err != nil {
		return err
	}
	if wid == "" {
		wid = base64.RawURLEncoding.EncodeToString([]byte(store.NewID() + store.NewID()))
	}
	if _, err := sys.AddUser(&store.User{ID: 1, Role: store.RoleAdmin, WebAuthnID: wid, KeyID: dek.ID(), KeyWrap: wrap}); err != nil {
		return err
	}
	if err := copyRows(old, sys, `SELECT id, 1, name, credential, created_at, last_used_at FROM passkeys`,
		`INSERT INTO passkeys (id, user_id, name, credential, created_at, last_used_at) VALUES (?,?,?,?,?,?)`, 6); err != nil {
		return fmt.Errorf("Passkeys: %w", err)
	}
	if err := copyRows(old, sys, `SELECT token_hash, 1, passkey_id, created_at, expires_at FROM sessions`,
		`INSERT INTO sessions (token_hash, user_id, passkey_id, created_at, expires_at) VALUES (?,?,?,?,?)`, 5); err != nil {
		return fmt.Errorf("Sitzungen: %w", err)
	}
	if err := copyRows(old, sys, `SELECT token_hash, 1, expires_at FROM enroll_tokens`,
		`INSERT INTO enroll_tokens (token_hash, user_id, expires_at) VALUES (?,?,?)`, 3); err != nil {
		return fmt.Errorf("Einmal-Links: %w", err)
	}

	// Zugänge: nur Hashes in system.db; die Pebble-App behält Header, Token und Signatur.
	hookToken, hookSecret := settings["HOOK_TOKEN"], settings["HOOK_SIGNING_SECRET"]
	if hookToken == "" || hookSecret == "" {
		hookToken, hookSecret = config.NewSecret(), config.NewSecret()
		log.Printf("Umzug: kein Zugang der Pebble-App gespeichert – neu erzeugt (Pebble-App neu einrichten)")
	}
	tokens := []struct{ kind, token, label string }{{store.TokenHook, hookToken, "Pebble-App"}, {store.TokenAPI, settings["API_TOKEN"], "API-Token"}}
	if ical, err := old.Get("ical_token"); err != nil {
		return err
	} else if ical != "" {
		tokens = append(tokens, struct{ kind, token, label string }{store.TokenICal, ical, "Kalender-Abo"})
	}
	for _, t := range tokens {
		if t.token != "" {
			if err := sys.AddToken(store.UserToken{UserID: 1, Kind: t.kind, Label: t.label}, store.Hash(t.token)); err != nil {
				return err
			}
		}
	}
	// OAuth-Freigaben (MCP) gehören Nutzer 1.
	if raw, err := old.Get(oauthKey); err != nil {
		return err
	} else if raw != "" {
		var d oauthData
		if err := json.Unmarshal([]byte(raw), &d); err != nil {
			return fmt.Errorf("OAuth: %w", err)
		}
		for _, g := range d.Grants {
			g.UserID = 1
		}
		b, _ := json.Marshal(d)
		if err := sys.Set(oauthKey, string(b)); err != nil {
			return err
		}
	}

	// Schlüsseldatei, dann die verschlüsselte Kopie.
	if err := schluessel.Write(boot.KeyDir, 1, dek); err != nil {
		return fmt.Errorf("Schlüsseldatei in %s: %w", boot.KeyDir, err)
	}
	userDir := UserDir(stage, 1)
	if err := os.MkdirAll(userDir, 0o750); err != nil {
		return err
	}
	if err := old.SnapshotTo(filepath.Join(userDir, "daten.db"), dek.DBKey()); err != nil {
		return fmt.Errorf("verschlüsselte Kopie: %w", err)
	}
	db, err := store.OpenUser(filepath.Join(userDir, "daten.db"), dek.DBKey(), 1)
	if err != nil {
		return err
	}
	access, _ := json.Marshal(pebbleAccess{Token: hookToken, Secret: hookSecret})
	for k, v := range map[string]string{pebbleAccessKey: string(access), recoveryPendingKey: code, migratedAtKey: fmt.Sprint(store.Now())} {
		if err == nil {
			err = db.Set(k, v)
		}
	}
	if err == nil {
		err = compareCounts(old, db)
	}
	db.Close()
	if err != nil {
		return err
	}
	if err := sys.Close(); err != nil {
		return err
	}

	// Umbenennen: users/ zuerst, system.db zuletzt (ihr Dasein heißt „umgezogen“).
	dst := filepath.Join(boot.DataDir, "users")
	if err := os.RemoveAll(dst); err != nil { // Rest eines abgebrochenen Laufs (ohne system.db nie in Gebrauch)
		return err
	}
	if err := os.Rename(filepath.Join(stage, "users"), dst); err != nil {
		return err
	}
	for _, f := range []string{"system.db-wal", "system.db-shm"} {
		os.Remove(filepath.Join(stage, f))
	}
	if err := os.Rename(filepath.Join(stage, "system.db"), filepath.Join(boot.DataDir, "system.db")); err != nil {
		return err
	}
	old.Close()
	if err := os.Rename(legacyPath, legacyPath+".vor-3.0"); err != nil {
		return err
	}
	for _, f := range []string{"-wal", "-shm"} {
		os.Remove(legacyPath + f)
	}
	if err := store.Tombstone(legacyPath, legacyNote); err != nil {
		return err
	}
	os.RemoveAll(stage)
	log.Printf("Umzug auf mehrere Nutzer fertig (%s): Nutzer 1 ist Admin, Daten verschlüsselt in %s; alte Datenbank: %s.vor-3.0",
		time.Since(start).Round(time.Millisecond), UserDB(boot.DataDir, 1), legacyPath)
	return nil
}

// migratedAtKey: Zeitpunkt des Umzugs (Aufnahmen bis 2.x werden danach verschlüsselt verschoben).
const migratedAtKey = "umzug_at"

// copyRows kopiert Zeilen aus pebble.db in system.db.
func copyRows(from *store.Store, to *store.System, query, insert string, cols int) error {
	rows, err := from.DB.Query(query)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		vals := make([]any, cols)
		ptrs := make([]any, cols)
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return err
		}
		if _, err := to.DB.Exec(insert, vals...); err != nil {
			return err
		}
	}
	return rows.Err()
}

// compareCounts: jede Tabelle der Nutzerdaten hat nach dem Umzug so viele Zeilen wie vorher.
func compareCounts(old, now *store.Store) error {
	for _, t := range []string{"jobs", "plannings", "lists", "list_items", "list_groups", "memory", "rings", "ring_recordings",
		"push_subs", "joplin_deletes"} {
		var a, b int
		if err := old.DB.QueryRow(`SELECT COUNT(*) FROM ` + t).Scan(&a); err != nil {
			return fmt.Errorf("%s: %w", t, err)
		}
		if err := now.DB.QueryRow(`SELECT COUNT(*) FROM ` + t).Scan(&b); err != nil {
			return fmt.Errorf("%s: %w", t, err)
		}
		if a != b {
			return fmt.Errorf("%s: %d Zeilen vorher, %d nachher", t, a, b)
		}
	}
	return nil
}
