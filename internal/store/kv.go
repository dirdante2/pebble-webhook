package store

import (
	"database/sql"
	"errors"
	"strings"
)

// Schlüssel/Wert-Tabelle kv – gleich aufgebaut in system.db (Systemwerte) und daten.db (Werte des Nutzers).

func kvGet(db *sql.DB, k string) (string, error) {
	var v string
	err := db.QueryRow(`SELECT v FROM kv WHERE k = ?`, k).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return v, err
}

func kvSet(db *sql.DB, k, v string) error {
	_, err := db.Exec(`INSERT INTO kv (k, v) VALUES (?, ?) ON CONFLICT(k) DO UPDATE SET v = excluded.v`, k, v)
	return err
}

const settingPrefix = "einstellung:"

// kvSettings liefert alle gespeicherten Einstellungen (Schlüssel wie in der früheren env-Datei).
func kvSettings(db *sql.DB) (map[string]string, error) {
	rows, err := db.Query(`SELECT k, v FROM kv WHERE k LIKE ?`, settingPrefix+"%")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		out[strings.TrimPrefix(k, settingPrefix)] = v
	}
	return out, rows.Err()
}

// kvSaveSettings schreibt Einstellungen in einem Zug; leere Werte werden gespeichert (= nicht gesetzt).
func kvSaveSettings(db *sql.DB, values map[string]string) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for k, v := range values {
		if _, err := tx.Exec(`INSERT INTO kv (k, v) VALUES (?, ?) ON CONFLICT(k) DO UPDATE SET v = excluded.v`, settingPrefix+k, v); err != nil {
			return err
		}
	}
	return tx.Commit()
}
