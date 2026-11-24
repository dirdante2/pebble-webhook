package store

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// System: die Datenbank system.db (Klartext) – Nutzer, Anmeldung, Zugänge und Systemwerte. Inhalte der Nutzer
// liegen nie hier, sondern verschlüsselt in users/<id>/daten.db.
type System struct{ DB *sql.DB }

const systemSchema = `
CREATE TABLE IF NOT EXISTS users (
  id INTEGER PRIMARY KEY,
  name TEXT NOT NULL DEFAULT '',
  role TEXT NOT NULL DEFAULT 'user',
  webauthn_id TEXT NOT NULL UNIQUE,
  key_id TEXT NOT NULL DEFAULT '',
  key_wrap TEXT NOT NULL DEFAULT '',
  locked_at INTEGER NOT NULL DEFAULT 0,
  created_at INTEGER NOT NULL,
  last_login_at INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE IF NOT EXISTS passkeys (
  id TEXT PRIMARY KEY,
  user_id INTEGER NOT NULL,
  name TEXT NOT NULL,
  credential TEXT NOT NULL,
  created_at INTEGER NOT NULL,
  last_used_at INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS passkeys_user ON passkeys(user_id);
CREATE TABLE IF NOT EXISTS sessions (
  token_hash TEXT PRIMARY KEY,
  user_id INTEGER NOT NULL,
  passkey_id TEXT NOT NULL,
  created_at INTEGER NOT NULL,
  expires_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS enroll_tokens (
  token_hash TEXT PRIMARY KEY,
  user_id INTEGER NOT NULL,
  purpose TEXT NOT NULL DEFAULT 'geraet',
  name TEXT NOT NULL DEFAULT '',
  role TEXT NOT NULL DEFAULT '',
  expires_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS user_tokens (
  id TEXT PRIMARY KEY,
  user_id INTEGER NOT NULL,
  kind TEXT NOT NULL,
  token_hash TEXT NOT NULL UNIQUE,
  label TEXT NOT NULL DEFAULT '',
  client_id TEXT NOT NULL DEFAULT '',
  grant_id TEXT NOT NULL DEFAULT '',
  created_at INTEGER NOT NULL,
  last_used_at INTEGER NOT NULL DEFAULT 0,
  expires_at INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS user_tokens_user ON user_tokens(user_id, kind);
CREATE TABLE IF NOT EXISTS kv (k TEXT PRIMARY KEY, v TEXT NOT NULL);
`

// sysMigrations: Schritte für system.db nach demselben Stand wie migrations (Index i hebt von i auf i+1); nil =
// in diesem Schritt ändert sich system.db nicht.
var sysMigrations = map[int]func(*sql.Tx) error{
	1: func(tx *sql.Tx) error { _, err := tx.Exec(systemSchema); return err }, // Stand 2 (3.0.0)
}

// Rollen.
const (
	RoleAdmin = "admin"
	RoleUser  = "user"
)

// Zwecke eines Einmal-Links.
const (
	EnrollDevice = "geraet"    // weiteres Gerät eines Nutzers
	EnrollInvite = "einladung" // neuer Nutzer (vom Admin eingeladen)
)

// Arten von Zugängen je Nutzer (nur der Hash steht in user_tokens).
const (
	TokenHook         = "hook"
	TokenAPI          = "api"
	TokenICal         = "ical"
	TokenOAuthAccess  = "oauth_access"
	TokenOAuthRefresh = "oauth_refresh"
)

// OpenSystem öffnet (oder legt an) system.db und bringt sie auf SchemaVersion.
func OpenSystem(path string) (*System, error) {
	db, err := openSQLite(path, nil, false)
	if err != nil {
		return nil, err
	}
	if err := quickCheck(db); err != nil {
		db.Close()
		return nil, err
	}
	var v int
	if err := db.QueryRow(`PRAGMA user_version`).Scan(&v); err != nil {
		db.Close()
		return nil, err
	}
	if v > SchemaVersion {
		db.Close()
		return nil, fmt.Errorf("system.db hat Schema %d, diese Version kennt nur %d – zurück nur über ein Backup", v, SchemaVersion)
	}
	for ; v < SchemaVersion; v++ {
		if err := migrateStep(db, v, sysMigrations[v]); err != nil {
			db.Close()
			return nil, fmt.Errorf("system.db Schema %d: %w", v+1, err)
		}
	}
	return &System{DB: db}, nil
}

func (s *System) Close() error { return s.DB.Close() }

func (s *System) Get(k string) (string, error) { return kvGet(s.DB, k) }
func (s *System) Set(k, v string) error        { return kvSet(s.DB, k, v) }

// Settings: Einstellungen des Systems (OpenAI, Adresse, Updates …).
func (s *System) Settings() (map[string]string, error)        { return kvSettings(s.DB) }
func (s *System) SaveSettings(values map[string]string) error { return kvSaveSettings(s.DB, values) }

// --- Nutzer ---

type User struct {
	ID          int64
	Name        string
	Role        string
	WebAuthnID  string
	KeyID       string
	KeyWrap     string
	LockedAt    int64
	CreatedAt   int64
	LastLoginAt int64
}

func (u *User) Admin() bool { return u.Role == RoleAdmin }

const userCols = `id, name, role, webauthn_id, key_id, key_wrap, locked_at, created_at, last_login_at`

func scanUser(sc interface{ Scan(...any) error }) (*User, error) {
	var u User
	err := sc.Scan(&u.ID, &u.Name, &u.Role, &u.WebAuthnID, &u.KeyID, &u.KeyWrap, &u.LockedAt, &u.CreatedAt, &u.LastLoginAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &u, err
}

func (s *System) Users() ([]*User, error) {
	rows, err := s.DB.Query(`SELECT ` + userCols + ` FROM users ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*User
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

func (s *System) User(id int64) (*User, error) {
	return scanUser(s.DB.QueryRow(`SELECT `+userCols+` FROM users WHERE id = ?`, id))
}

func (s *System) UserByWebAuthnID(wid string) (*User, error) {
	return scanUser(s.DB.QueryRow(`SELECT `+userCols+` FROM users WHERE webauthn_id = ?`, wid))
}

// AddUser legt einen Nutzer an (id 0 = nächste freie) und liefert die Id.
func (s *System) AddUser(u *User) (int64, error) {
	if u.Role != RoleAdmin {
		u.Role = RoleUser
	}
	var id any
	if u.ID > 0 {
		id = u.ID
	}
	res, err := s.DB.Exec(`INSERT INTO users (id, name, role, webauthn_id, key_id, key_wrap, created_at) VALUES (?,?,?,?,?,?,?)`,
		id, u.Name, u.Role, u.WebAuthnID, u.KeyID, u.KeyWrap, Now())
	if err != nil {
		return 0, err
	}
	u.ID, err = res.LastInsertId()
	return u.ID, err
}

func (s *System) SetUserName(id int64, name string) error {
	_, err := s.DB.Exec(`UPDATE users SET name = ? WHERE id = ?`, name, id)
	return err
}

func (s *System) SetUserRole(id int64, role string) error {
	if role != RoleAdmin && role != RoleUser {
		return fmt.Errorf("Rolle %q unbekannt", role)
	}
	_, err := s.DB.Exec(`UPDATE users SET role = ? WHERE id = ?`, role, id)
	return err
}

func (s *System) SetUserLocked(id int64, locked bool) error {
	at := int64(0)
	if locked {
		at = Now()
	}
	_, err := s.DB.Exec(`UPDATE users SET locked_at = ? WHERE id = ?`, at, id)
	return err
}

func (s *System) SetUserKey(id int64, keyID, wrap string) error {
	_, err := s.DB.Exec(`UPDATE users SET key_id = ?, key_wrap = ? WHERE id = ?`, keyID, wrap, id)
	return err
}

// DeleteUser entfernt den Nutzer samt Anmeldung und Zugängen aus system.db (Dateien löscht der Aufrufer).
func (s *System) DeleteUser(id int64) error {
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, q := range []string{`DELETE FROM sessions WHERE user_id = ?`, `DELETE FROM passkeys WHERE user_id = ?`,
		`DELETE FROM enroll_tokens WHERE user_id = ?`, `DELETE FROM user_tokens WHERE user_id = ?`, `DELETE FROM users WHERE id = ?`} {
		if _, err := tx.Exec(q, id); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// Admins: Zahl der Administratoren (der letzte bleibt geschützt).
func (s *System) Admins() (int, error) {
	var n int
	err := s.DB.QueryRow(`SELECT COUNT(*) FROM users WHERE role = ?`, RoleAdmin).Scan(&n)
	return n, err
}

// --- Anmeldung ---

type Passkey struct {
	ID         string
	UserID     int64
	Name       string
	Credential string // JSON von webauthn.Credential
	CreatedAt  int64
	LastUsedAt int64
}

// Passkeys eines Nutzers.
func (s *System) Passkeys(uid int64) ([]Passkey, error) {
	rows, err := s.DB.Query(`SELECT id, user_id, name, credential, created_at, last_used_at FROM passkeys WHERE user_id = ? ORDER BY created_at`, uid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Passkey
	for rows.Next() {
		var p Passkey
		if err := rows.Scan(&p.ID, &p.UserID, &p.Name, &p.Credential, &p.CreatedAt, &p.LastUsedAt); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// AnyPasskey: gibt es überhaupt ein Gerät (eingerichtet)?
func (s *System) AnyPasskey() (bool, error) {
	var n int
	err := s.DB.QueryRow(`SELECT COUNT(*) FROM passkeys`).Scan(&n)
	return n > 0, err
}

func (s *System) AddPasskey(p Passkey) error {
	_, err := s.DB.Exec(`INSERT INTO passkeys (id, user_id, name, credential, created_at) VALUES (?,?,?,?,?)`,
		p.ID, p.UserID, p.Name, p.Credential, Now())
	return err
}

func (s *System) UpdatePasskey(uid int64, id, credential string) error {
	_, err := s.DB.Exec(`UPDATE passkeys SET credential = ?, last_used_at = ? WHERE id = ? AND user_id = ?`, credential, Now(), id, uid)
	if err == nil {
		s.DB.Exec(`UPDATE users SET last_login_at = ? WHERE id = ?`, Now(), uid)
	}
	return err
}

// DeletePasskey entfernt ein Gerät des Nutzers samt seinen Sitzungen; ErrNotFound, wenn es nicht seins ist.
func (s *System) DeletePasskey(uid int64, id string) error {
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	res, err := tx.Exec(`DELETE FROM passkeys WHERE id = ? AND user_id = ?`, id, uid)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	if _, err := tx.Exec(`DELETE FROM sessions WHERE passkey_id = ? AND user_id = ?`, id, uid); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *System) NewSession(uid int64, passkeyID string, ttl time.Duration) (string, error) {
	token := NewID() + NewID()
	_, err := s.DB.Exec(`INSERT INTO sessions (token_hash, user_id, passkey_id, created_at, expires_at) VALUES (?,?,?,?,?)`,
		Hash(token), uid, passkeyID, Now(), time.Now().Add(ttl).UnixMilli())
	return token, err
}

// Session prüft eine Sitzung und liefert Nutzer und Passkey; verlängert gleitend.
func (s *System) Session(token string, ttl time.Duration) (uid int64, passkeyID string, ok bool) {
	if token == "" {
		return 0, "", false
	}
	var exp int64
	if err := s.DB.QueryRow(`SELECT user_id, passkey_id, expires_at FROM sessions WHERE token_hash = ?`, Hash(token)).
		Scan(&uid, &passkeyID, &exp); err != nil {
		return 0, "", false
	}
	if exp < Now() {
		return 0, "", false
	}
	if next := time.Now().Add(ttl).UnixMilli(); next-exp > int64(time.Hour/time.Millisecond) {
		s.DB.Exec(`UPDATE sessions SET expires_at = ? WHERE token_hash = ?`, next, Hash(token))
	}
	return uid, passkeyID, true
}

func (s *System) DeleteSession(token string) error {
	_, err := s.DB.Exec(`DELETE FROM sessions WHERE token_hash = ?`, Hash(token))
	return err
}

// Enroll: ein gültiger Einmal-Link.
type Enroll struct {
	UserID  int64
	Purpose string
	Name    string
	Role    string
}

// NewEnrollToken: Einmal-Link für ein weiteres Gerät (uid) oder eine Einladung (uid 0, Name und Rolle des neuen Nutzers).
func (s *System) NewEnrollToken(e Enroll, ttl time.Duration) (string, error) {
	s.DB.Exec(`DELETE FROM enroll_tokens WHERE expires_at < ?`, Now())
	if e.Purpose == "" {
		e.Purpose = EnrollDevice
	}
	token := NewID() + NewID()
	_, err := s.DB.Exec(`INSERT INTO enroll_tokens (token_hash, user_id, purpose, name, role, expires_at) VALUES (?,?,?,?,?,?)`,
		Hash(token), e.UserID, e.Purpose, e.Name, e.Role, time.Now().Add(ttl).UnixMilli())
	return token, err
}

func (s *System) EnrollToken(token string) (*Enroll, bool) {
	var e Enroll
	var exp int64
	if err := s.DB.QueryRow(`SELECT user_id, purpose, name, role, expires_at FROM enroll_tokens WHERE token_hash = ?`, Hash(token)).
		Scan(&e.UserID, &e.Purpose, &e.Name, &e.Role, &exp); err != nil || exp < Now() {
		return nil, false
	}
	return &e, true
}

// UseEnrollToken verbraucht einen Einmal-Link; false, wenn er nicht (mehr) gilt.
func (s *System) UseEnrollToken(token string) bool {
	res, err := s.DB.Exec(`DELETE FROM enroll_tokens WHERE token_hash = ? AND expires_at >= ?`, Hash(token), Now())
	if err != nil {
		return false
	}
	n, _ := res.RowsAffected()
	return n == 1
}

// --- Zugänge je Nutzer (Hook, API, Kalender, OAuth) ---

type UserToken struct {
	ID         string
	UserID     int64
	Kind       string
	Label      string
	ClientID   string
	GrantID    string
	CreatedAt  int64
	LastUsedAt int64
	ExpiresAt  int64
}

// AddToken speichert den Hash eines Zugangs (tokenHash = Hash(Klartext)).
func (s *System) AddToken(t UserToken, tokenHash string) error {
	if t.ID == "" {
		t.ID = NewID()
	}
	_, err := s.DB.Exec(`INSERT INTO user_tokens (id, user_id, kind, token_hash, label, client_id, grant_id, created_at, expires_at)
		VALUES (?,?,?,?,?,?,?,?,?)`, t.ID, t.UserID, t.Kind, tokenHash, t.Label, t.ClientID, t.GrantID, Now(), t.ExpiresAt)
	return err
}

// TokenUser löst einen Zugang der Art kind zum Nutzer auf (abgelaufene zählen nicht) und vermerkt die Nutzung.
func (s *System) TokenUser(kind, token string) (*UserToken, bool) {
	if token == "" {
		return nil, false
	}
	var t UserToken
	err := s.DB.QueryRow(`SELECT id, user_id, kind, label, client_id, grant_id, created_at, last_used_at, expires_at
		FROM user_tokens WHERE token_hash = ? AND kind = ?`, Hash(token), kind).
		Scan(&t.ID, &t.UserID, &t.Kind, &t.Label, &t.ClientID, &t.GrantID, &t.CreatedAt, &t.LastUsedAt, &t.ExpiresAt)
	if err != nil || (t.ExpiresAt > 0 && t.ExpiresAt < Now()) {
		return nil, false
	}
	if Now()-t.LastUsedAt > 60_000 {
		s.DB.Exec(`UPDATE user_tokens SET last_used_at = ? WHERE id = ?`, Now(), t.ID)
	}
	return &t, true
}

// Tokens eines Nutzers je Art (ohne Hash).
func (s *System) Tokens(uid int64, kinds ...string) ([]UserToken, error) {
	q := `SELECT id, user_id, kind, label, client_id, grant_id, created_at, last_used_at, expires_at FROM user_tokens WHERE user_id = ?`
	args := []any{uid}
	if len(kinds) > 0 {
		q += ` AND kind IN (?` + strings.Repeat(",?", len(kinds)-1) + `)`
		for _, k := range kinds {
			args = append(args, k)
		}
	}
	rows, err := s.DB.Query(q+` ORDER BY created_at`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []UserToken
	for rows.Next() {
		var t UserToken
		if err := rows.Scan(&t.ID, &t.UserID, &t.Kind, &t.Label, &t.ClientID, &t.GrantID, &t.CreatedAt, &t.LastUsedAt, &t.ExpiresAt); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// DeleteTokens entfernt Zugänge eines Nutzers je Art (z. B. neuer Kalender-Link).
func (s *System) DeleteTokens(uid int64, kind string) error {
	_, err := s.DB.Exec(`DELETE FROM user_tokens WHERE user_id = ? AND kind = ?`, uid, kind)
	return err
}

// DeleteGrant entfernt alle Zugänge einer OAuth-Freigabe des Nutzers.
func (s *System) DeleteGrant(uid int64, grantID string) error {
	_, err := s.DB.Exec(`DELETE FROM user_tokens WHERE user_id = ? AND grant_id = ?`, uid, grantID)
	return err
}

// DeleteTokenByHash entfernt einen einzelnen Zugang (z. B. verbrauchtes OAuth-Erneuerungs-Token).
func (s *System) DeleteTokenByHash(tokenHash string) error {
	_, err := s.DB.Exec(`DELETE FROM user_tokens WHERE token_hash = ?`, tokenHash)
	return err
}

// PurgeExpiredTokens räumt abgelaufene Zugänge auf.
func (s *System) PurgeExpiredTokens() error {
	_, err := s.DB.Exec(`DELETE FROM user_tokens WHERE expires_at > 0 AND expires_at < ?`, Now())
	return err
}

// Snapshot: in sich stimmige Kopie von system.db (Klartext; enthält nur Hashes und öffentliche Schlüssel der Nutzer).
func (s *System) Snapshot(path string) error {
	_, err := s.DB.Exec(`VACUUM INTO ?`, snapshotURI(path, nil))
	return err
}
