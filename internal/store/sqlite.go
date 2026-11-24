package store

import (
	"database/sql"
	"encoding/hex"
	"fmt"
	"net/url"

	"github.com/ncruces/go-sqlite3"
	"github.com/ncruces/go-sqlite3/driver"
	"github.com/ncruces/go-sqlite3/ext/fts5"
	_ "github.com/ncruces/go-sqlite3/vfs/adiantum"
)

// KeySize: Länge eines Datenbank-Schlüssels (Adiantum, seitenweise Verschlüsselung).
const KeySize = 32

// fileURI baut die URI einer Datenbankdatei; mit Schlüssel über die verschlüsselnde VFS „adiantum“.
// Der Schlüssel selbst steht nur im Ziel von VACUUM INTO in der URI, sonst setzt ihn openSQLite per PRAGMA.
func fileURI(path string, encrypted bool, params url.Values) string {
	if params == nil {
		params = url.Values{}
	}
	if encrypted {
		params.Set("vfs", "adiantum")
	}
	u := url.URL{Scheme: "file", OmitHost: true, Path: path, RawQuery: params.Encode()}
	return u.String()
}

// openSQLite öffnet eine Datenbankdatei: key nil = Klartext, sonst verschlüsselt (len KeySize).
// Pragmas (Schlüssel zuerst) und FTS5 je Verbindung im Init-Callback – Pragmas in der URI liefen vor dem Schlüssel.
func openSQLite(path string, key []byte, readonly bool) (*sql.DB, error) {
	if key != nil && len(key) != KeySize {
		return nil, fmt.Errorf("Datenbank-Schlüssel: %d statt %d Byte", len(key), KeySize)
	}
	params := url.Values{}
	if readonly {
		params.Set("mode", "ro")
	}
	pragmas := "PRAGMA busy_timeout=10000; PRAGMA temp_store=memory;"
	if !readonly {
		pragmas += " PRAGMA journal_mode=WAL; PRAGMA synchronous=FULL;"
	} else {
		pragmas += " PRAGMA query_only=1;"
	}
	hexKey := hex.EncodeToString(key)
	db, err := driver.Open(fileURI(path, key != nil, params), func(c *sqlite3.Conn) error {
		if key != nil {
			if err := c.Exec("PRAGMA hexkey='" + hexKey + "'"); err != nil {
				return err
			}
		}
		if err := c.Exec(pragmas); err != nil {
			return err
		}
		return fts5.Register(c)
	})
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	return db, nil
}

// snapshotURI: Ziel für VACUUM INTO – ohne Schlüssel ausdrücklich Klartext (VFS „os“; die Datei erbt sonst die VFS
// der Quelle), mit Schlüssel verschlüsselt.
func snapshotURI(path string, key []byte) string {
	if key == nil {
		return fileURI(path, false, url.Values{"vfs": {"os"}})
	}
	return fileURI(path, true, url.Values{"hexkey": {hex.EncodeToString(key)}})
}

// OpenReadOnly öffnet eine Datenbankdatei nur lesend (Prüfung hochgeladener Backups, Schema-Abfrage).
func OpenReadOnly(path string, key []byte) (*sql.DB, error) {
	return openSQLite(path, key, true)
}
