package app

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"syscall"

	"pebble-webhook/internal/config"
	"pebble-webhook/internal/schluessel"
	"pebble-webhook/internal/store"
)

// Geheimnisse des Systems in system.db: API-Schlüssel (Einstellungen der Art secret) und der private Web-Push-Schlüssel.
// Im Backup stehen sie verschlüsselt mit dem Systemschlüssel (KEY_DIR/system.key, nie im Backup).

func systemSecretKeys() []string {
	keys := []string{"vapid_private"}
	for _, f := range config.Fields {
		if f.Kind == "secret" && !f.Personal {
			keys = append(keys, "einstellung:"+f.Key)
		}
	}
	return keys
}

// SealSystemSecrets: für den Installer (Update-Backups), als root aufgerufen – ein neu angelegter Systemschlüssel
// gehört danach dem Besitzer von KEY_DIR (dem Dienst).
func SealSystemSecrets(path, keyDir string) error {
	if err := sealSystemSecrets(path, keyDir); err != nil {
		return err
	}
	if os.Geteuid() == 0 {
		if fi, err := os.Stat(keyDir); err == nil {
			if st, ok := fi.Sys().(*syscall.Stat_t); ok {
				os.Lchown(filepath.Join(keyDir, "system.key"), int(st.Uid), int(st.Gid))
			}
		}
	}
	return nil
}

// sealSystemSecrets verschlüsselt die Geheimnisse in einer Kopie von system.db (dem Backup-Stand).
func sealSystemSecrets(path, keyDir string) error {
	db, err := store.OpenSystem(path)
	if err != nil {
		return err
	}
	defer db.Close()
	// Gibt es schon einen eingewickelten Systemschlüssel, nie einen neuen anlegen (neuer Server vor dem Wiederherstellen).
	bound, _ := db.Get(systemKeyKV)
	key, err := schluessel.SystemKey(keyDir, bound == "")
	if err != nil {
		return err
	}
	// Alte Klartext-Werte dürfen nicht in freien Seiten der Datei liegen bleiben.
	if _, err := db.DB.Exec(`PRAGMA secure_delete = ON`); err != nil {
		return err
	}
	for _, k := range systemSecretKeys() {
		v, err := db.Get(k)
		if err != nil {
			return err
		}
		if v == "" || schluessel.Sealed(v) {
			continue
		}
		sealed, err := schluessel.Seal(key, k, v)
		if err != nil {
			return err
		}
		if err := db.Set(k, sealed); err != nil {
			return err
		}
	}
	_, err = db.DB.Exec(`VACUUM`)
	return err
}

// unsealSystemSecrets: nach dem Einspielen eines Backups die Geheimnisse wieder lesbar machen. Ohne passenden
// Systemschlüssel (anderer Server) werden sie geleert: API-Schlüssel neu eingeben, Web-Push-Schlüssel entsteht neu.
func unsealSystemSecrets(db *store.System, keyDir string) {
	var key []byte
	for _, k := range systemSecretKeys() {
		v, err := db.Get(k)
		if err != nil || !schluessel.Sealed(v) {
			continue
		}
		if key == nil {
			if key, err = schluessel.SystemKey(keyDir, false); err != nil {
				key = []byte{}
			}
		}
		if len(key) == 0 {
			// Neuer Server: bleibt verschlüsselt, bis der Admin den Systemschlüssel mit seinem Code zurückholt.
			logf("Backup: %s verschlüsselt – Systemschlüssel fehlt noch (Wiederherstellungscode)", k)
			continue
		}
		plain, err := schluessel.Open(key, k, v)
		if err != nil {
			logf("Backup: %s passt nicht zum Systemschlüssel – neu eingeben bzw. neu erzeugt", k)
		}
		db.Set(k, plain)
	}
}

// --- Systemschlüssel wiederherstellen ---
// Der Systemschlüssel steht eingewickelt in system.db (kv „systemschluessel“), damit ein Backup auf einem neuen Server
// vollständig zurückkommt: Vorgabe ist die Bindung an den Datenschlüssel des ersten Admins – dessen
// Wiederherstellungscode holt dann auch den Systemschlüssel zurück (ein Code für beides, der häufige Einzelnutzer-Fall).
// Auf Wunsch erzeugt ein Admin einen eigenen Code nur für das System.

const systemKeyKV = "systemschluessel"

// restartAfterKey: Neustart nach dem Wiederherstellen (in Tests ersetzt).
var restartAfterKey = restartSoonFor

type systemKeyInfo struct {
	Art   string `json:"art"` // nutzer | code
	UID   int64  `json:"uid,omitempty"`
	Wrap  string `json:"wrap"`
	KeyID string `json:"key_id"`
}

func (s *System) systemKeyInfo() (systemKeyInfo, bool) {
	var in systemKeyInfo
	v, err := s.DB.Get(systemKeyKV)
	if err != nil || v == "" || json.Unmarshal([]byte(v), &in) != nil {
		return in, false
	}
	return in, true
}

func (s *System) saveSystemKeyInfo(in systemKeyInfo) error {
	b, _ := json.Marshal(in)
	return s.DB.Set(systemKeyKV, string(b))
}

// systemKeyMissing: Die Bindung gibt es, die Datei nicht (Backup auf neuem Server eingespielt).
func (s *System) systemKeyMissing() bool {
	_, ok := s.systemKeyInfo()
	_, err := schluessel.SystemKey(s.Cfg.KeyDir, false)
	return ok && err != nil
}

// bindSystemKey bindet den Systemschlüssel an den Datenschlüssel eines Admins.
func (s *System) bindSystemKey(key []byte, u *store.User) error {
	dek, err := schluessel.Read(s.Cfg.KeyDir, u.ID, u.KeyID)
	if err != nil {
		return err
	}
	wrap, err := schluessel.BindSystemKey(key, dek)
	if err != nil {
		return err
	}
	return s.saveSystemKeyInfo(systemKeyInfo{Art: "nutzer", UID: u.ID, Wrap: wrap, KeyID: schluessel.SystemKeyID(key)})
}

// Wahl aus dem Einrichtungsassistenten: „code“ = mehrere Admins, der Systemschlüssel bekommt einen eigenen Code.
// Er entsteht beim ersten Start mit Nutzer und wartet (verschlüsselt in der DB des ersten Admins) bis zur Anzeige
// auf der Willkommensseite neben dessen Wiederherstellungscode.
const (
	systemKeyChoiceKV    = "systemschluessel_wahl"
	systemCodePendingKey = "systemcode_offen"
)

// applySystemKeyChoice setzt die Wahl „eigener Code“ einmalig um (nach ensureSystemKey).
func (s *System) applySystemKeyChoice() {
	if v, _ := s.DB.Get(systemKeyChoiceKV); v != "code" {
		return
	}
	info, ok := s.systemKeyInfo()
	if !ok {
		return
	}
	if info.Art == "code" {
		s.DB.Set(systemKeyChoiceKV, "")
		return
	}
	key, err := schluessel.SystemKey(s.Cfg.KeyDir, false)
	if err != nil {
		return
	}
	owner, _ := s.app(info.UID)
	if owner == nil {
		logf("Systemschlüssel: eigener Code wartet – Admin %d nicht geöffnet", info.UID)
		return
	}
	code, err := schluessel.NewCode()
	if err != nil {
		return
	}
	wrap, err := schluessel.WrapSystemCode(code, key)
	if err != nil {
		return
	}
	if err := owner.DB.Set(systemCodePendingKey, code); err != nil {
		logf("Systemschlüssel: Code nicht gespeichert: %v", err)
		return
	}
	if err := s.saveSystemKeyInfo(systemKeyInfo{Art: "code", Wrap: wrap, KeyID: schluessel.SystemKeyID(key)}); err != nil {
		owner.DB.Set(systemCodePendingKey, "")
		return
	}
	s.DB.Set(systemKeyChoiceKV, "")
	logf("Systemschlüssel: eigener Wiederherstellungscode erzeugt (Wahl der Einrichtung)")
}

// ensureSystemKey (beim Start, nach Nutzer-Änderungen): Systemschlüssel anlegen, falls es keinen gibt, und
// eingewickelt halten – an einen aktiven Admin, solange kein eigener Code gewählt ist.
func (s *System) ensureSystemKey() {
	info, have := s.systemKeyInfo()
	key, err := schluessel.SystemKey(s.Cfg.KeyDir, !have)
	if err != nil {
		if have {
			logf("Systemschlüssel fehlt – ein Admin holt ihn mit seinem Wiederherstellungscode zurück (Status → Backups)")
		} else {
			logf("Systemschlüssel nicht anlegbar: %v", err)
		}
		return
	}
	if have && info.KeyID == schluessel.SystemKeyID(key) {
		if info.Art == "code" {
			return
		}
		if u, err := s.DB.User(info.UID); err == nil && u.Admin() && u.LockedAt == 0 {
			return
		}
	}
	users, _ := s.DB.Users()
	for _, u := range users {
		if !u.Admin() || u.LockedAt > 0 {
			continue
		}
		if err := s.bindSystemKey(key, u); err == nil {
			logf("Systemschlüssel an den Wiederherstellungscode von Nutzer %d gebunden", u.ID)
			return
		}
	}
}

// restoreSystemKey legt den zurückgeholten Systemschlüssel ab, macht die Geheimnisse lesbar und startet neu.
func (s *System) restoreSystemKey(key []byte) error {
	info, ok := s.systemKeyInfo()
	if !ok || info.KeyID != schluessel.SystemKeyID(key) {
		return errors.New("Systemschlüssel passt nicht")
	}
	if err := schluessel.WriteSystemKey(s.Cfg.KeyDir, key); err != nil {
		return err
	}
	unsealSystemSecrets(s.DB, s.Cfg.KeyDir)
	logf("Systemschlüssel wiederhergestellt – Dienst startet mit den Geheimnissen aus dem Backup neu")
	restartAfterKey("Systemschlüssel wiederhergestellt")
	return nil
}

// recoverSystemKeyWith: nach dem Wiederherstellen eines Nutzers – ist der Systemschlüssel an ihn gebunden und fehlt,
// kommt er mit zurück.
func (s *System) recoverSystemKeyWith(uid int64, dek schluessel.DEK) {
	info, ok := s.systemKeyInfo()
	if !ok || info.Art != "nutzer" || info.UID != uid || !s.systemKeyMissing() {
		return
	}
	key, err := schluessel.UnbindSystemKey(info.Wrap, dek)
	if err != nil {
		logf("Systemschlüssel mit Nutzer %d nicht zurückgeholt: %v", uid, err)
		return
	}
	if err := s.restoreSystemKey(key); err != nil {
		logf("Systemschlüssel: %v", err)
	}
}

// --- Verwaltung (Admin) ---

func (s *System) handleSystemKey(w http.ResponseWriter, r *http.Request) {
	info, ok := s.systemKeyInfo()
	out := map[string]any{"art": info.Art, "fehlt": s.systemKeyMissing(), "eingerichtet": ok, "ich": sessionUID(r)}
	if info.Art == "nutzer" {
		out["uid"] = info.UID
		if u, err := s.DB.User(info.UID); err == nil {
			out["name"] = u.Name
		}
	}
	writeJSON(w, 200, out)
}

// handleSystemKeyCode erzeugt einen eigenen Code nur für den Systemschlüssel (ersetzt die Bindung an einen Admin).
func (s *System) handleSystemKeyCode(w http.ResponseWriter, r *http.Request) {
	key, err := schluessel.SystemKey(s.Cfg.KeyDir, false)
	if err != nil {
		writeErr(w, http.StatusConflict, "fehlt", "Systemschlüssel fehlt – erst wiederherstellen")
		return
	}
	code, err := schluessel.NewCode()
	if err != nil {
		writeErr(w, 500, "zufall", "Code nicht erzeugt")
		return
	}
	wrap, err := schluessel.WrapSystemCode(code, key)
	if err == nil {
		err = s.saveSystemKeyInfo(systemKeyInfo{Art: "code", Wrap: wrap, KeyID: schluessel.SystemKeyID(key)})
	}
	if err != nil {
		writeErr(w, 500, "speicher", "Code nicht gespeichert")
		return
	}
	logf("Systemschlüssel: eigener Wiederherstellungscode erzeugt (Nutzer %d)", sessionUID(r))
	writeJSON(w, 200, map[string]any{"code": code})
}

// handleSystemKeyBind bindet den Systemschlüssel wieder an den Code des angemeldeten Admins.
func (s *System) handleSystemKeyBind(w http.ResponseWriter, r *http.Request) {
	key, err := schluessel.SystemKey(s.Cfg.KeyDir, false)
	if err != nil {
		writeErr(w, http.StatusConflict, "fehlt", "Systemschlüssel fehlt – erst wiederherstellen")
		return
	}
	u, err := s.DB.User(sessionUID(r))
	if err == nil {
		err = s.bindSystemKey(key, u)
	}
	if err != nil {
		writeErr(w, 500, "schluessel", "Bindung nicht möglich (eigene Daten gesperrt?)")
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}

// handleSystemKeyRecover holt den Systemschlüssel mit seinem eigenen Code zurück (neuer Server).
func (s *System) handleSystemKeyRecover(w http.ResponseWriter, r *http.Request) {
	if !s.allow("systemcode:"+s.clientIP(r), 5) {
		writeErr(w, http.StatusTooManyRequests, "limit", "Zu viele Versuche")
		return
	}
	var in struct {
		Code string `json:"code"`
	}
	info, ok := s.systemKeyInfo()
	if readJSON(r, &in) != nil || !ok || info.Art != "code" || !s.systemKeyMissing() {
		writeErr(w, 400, "ungueltig", "Kein Systemschlüssel zum Wiederherstellen")
		return
	}
	key, err := schluessel.UnwrapSystemCode(in.Code, info.Wrap)
	if err != nil {
		writeErr(w, http.StatusForbidden, "code", "Code falsch")
		return
	}
	if err := s.restoreSystemKey(key); err != nil {
		writeErr(w, 500, "schluessel", err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "neustart": true})
}
