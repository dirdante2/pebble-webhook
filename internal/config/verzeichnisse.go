package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

// Ordner des Dienstes: sie können eigene Mounts sein (Proxmox-Mountpoints). Nach dem Anlegen oder einem
// Wiederherstellen gehören Mounts oft root:root mit 0755 – dann kann der Dienst (Nutzer pebble) nicht schreiben.
// EnsureDirs richtet sie als root vor jedem Start (systemd ExecStartPre=+) und beim Installieren; CheckDirs prüft
// beim Start als Dienstnutzer, ob alles schreibbar ist, und bricht sonst mit klarer Meldung ab.

// Dir: ein Ordner mit Soll-Rechten.
type Dir struct {
	Path string
	Mode fs.FileMode // Ordner; Dateien darin bekommen Mode ohne x (0750 → 0640, 0700 → 0600)
}

// Dirs: alle Ordner, in die der Dienst schreibt.
func (b Bootstrap) Dirs() []Dir {
	return []Dir{
		{b.DataDir, 0o750},
		{filepath.Join(b.DataDir, "users"), 0o750},
		{b.MediaDir, 0o750},
		{b.BackupDir, 0o750},
		{b.KeyDir, 0o700},
	}
}

// ServiceUser: Nutzer, unter dem der Dienst läuft.
const ServiceUser = "pebble"

// safeDir: nie Systemordner rekursiv umschreiben (Fehlkonfiguration wie MEDIA_DIR=/ oder /srv).
func safeDir(p string) error {
	if !filepath.IsAbs(p) {
		return fmt.Errorf("%s ist kein absoluter Pfad", p)
	}
	clean := filepath.Clean(p)
	if strings.Count(clean, "/") < 2 {
		return fmt.Errorf("%s liegt zu weit oben im Dateisystem", clean)
	}
	for _, sys := range []string{"/etc/pebble-webhook", "/usr/local", "/var/lib", "/var/log", "/home", "/root"} {
		if clean == sys {
			return fmt.Errorf("%s ist ein Systemordner", clean)
		}
	}
	return nil
}

// EnsureDirs legt die Ordner an und setzt Besitzer (pebble:pebble) und Rechte – den Ordner selbst und alles darin,
// was nicht dem Dienst gehört oder für andere lesbar ist. Symlinks werden nicht verfolgt. Nur als root.
// Liefert je Ordner eine Zeile, was geändert wurde.
func EnsureDirs(b Bootstrap) ([]string, error) {
	u, err := user.Lookup(ServiceUser)
	if err != nil {
		return nil, fmt.Errorf("Nutzer %s: %w", ServiceUser, err)
	}
	uid, _ := strconv.Atoi(u.Uid)
	gid, _ := strconv.Atoi(u.Gid)
	var report []string
	var errs []error
	for _, d := range b.Dirs() {
		if err := safeDir(d.Path); err != nil {
			errs = append(errs, err)
			continue
		}
		created := false
		if _, err := os.Stat(d.Path); errors.Is(err, fs.ErrNotExist) {
			if err := os.MkdirAll(d.Path, d.Mode); err != nil {
				errs = append(errs, err)
				continue
			}
			created = true
		}
		owners, modes := 0, 0
		fileMode := d.Mode &^ 0o111
		err := filepath.WalkDir(d.Path, func(p string, e fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			// Unter DATA_DIR liegen weitere Ordner der Liste (users/) – die richtet ihr eigener Durchlauf.
			if p != d.Path && e.IsDir() {
				for _, o := range b.Dirs() {
					if o.Path != d.Path && filepath.Clean(o.Path) == filepath.Clean(p) {
						return filepath.SkipDir
					}
				}
			}
			fi, err := os.Lstat(p)
			if err != nil {
				return err
			}
			if st, ok := fi.Sys().(*syscall.Stat_t); ok && (int(st.Uid) != uid || int(st.Gid) != gid) {
				if err := os.Lchown(p, uid, gid); err != nil {
					return err
				}
				owners++
			}
			if fi.Mode()&fs.ModeSymlink != 0 {
				return nil
			}
			want := fi.Mode().Perm()
			switch {
			case p == d.Path:
				want = d.Mode
			case fi.IsDir():
				want = want &^ 0o007 // nichts für andere
				if d.Mode == 0o700 {
					want &^= 0o070
				}
			default:
				want = want &^ 0o007
				if d.Mode == 0o700 {
					want = fileMode
				}
			}
			if want != fi.Mode().Perm() {
				if err := os.Chmod(p, want); err != nil {
					return err
				}
				modes++
			}
			return nil
		})
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", d.Path, err))
			continue
		}
		switch {
		case created:
			report = append(report, fmt.Sprintf("%s angelegt", d.Path))
		case owners > 0 || modes > 0:
			report = append(report, fmt.Sprintf("%s: %d Besitzer, %d Rechte korrigiert", d.Path, owners, modes))
		}
	}
	return report, errors.Join(errs...)
}

// CheckDirs prüft als Dienstnutzer, dass jeder Ordner da ist und beschreibbar ist (Probedatei anlegen und löschen).
func CheckDirs(b Bootstrap) error {
	var errs []error
	for _, d := range b.Dirs() {
		st, err := os.Stat(d.Path)
		if err != nil || !st.IsDir() {
			errs = append(errs, fmt.Errorf("Verzeichnis %s fehlt", d.Path))
			continue
		}
		f, err := os.CreateTemp(d.Path, ".schreibprobe-*")
		if err != nil {
			owner := ""
			if s, ok := st.Sys().(*syscall.Stat_t); ok {
				owner = fmt.Sprintf(" (gehört %d:%d, Rechte %v)", s.Uid, s.Gid, st.Mode().Perm())
			}
			errs = append(errs, fmt.Errorf("Verzeichnis %s nicht beschreibbar%s – als root „pebble-webhook verzeichnisse“ ausführen", d.Path, owner))
			continue
		}
		f.Close()
		os.Remove(f.Name())
	}
	return errors.Join(errs...)
}
