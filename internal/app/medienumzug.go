package app

import (
	"os"
	"path/filepath"
)

// Ab 4.0 liegen die Medien eines Nutzers getrennt nach Art: MEDIA_DIR/<id>/audio/<diktat|planung>/JJJJ/MM/… und
// MEDIA_DIR/<id>/bilder/JJJJ/MM/…. Bis 3.x lagen die Aufnahmen direkt unter AUDIO_DIR/<id>/. migrateMedia schiebt
// alles, was noch direkt im Ordner des Nutzers liegt, nach audio/; die Pfade in der Datenbank richtet danach
// sortAudio nach dem Ort der Dateien (auch, wenn der ganze Ordner umgehängt wurde, z. B. auf ein neues MEDIA_DIR).
func (a *App) migrateMedia() {
	root := a.mediaRoot()
	entries, err := os.ReadDir(root)
	if err != nil {
		if !os.IsNotExist(err) {
			logf("Medien-Umzug: %v", err)
		}
		return
	}
	moved, left := 0, 0
	for _, e := range entries {
		if e.Name() == "audio" || e.Name() == "bilder" {
			continue
		}
		m, l := mergeMove(filepath.Join(root, e.Name()), filepath.Join(a.audioRoot(), e.Name()))
		moved, left = moved+m, left+l
	}
	if moved > 0 || left > 0 {
		logf("Medien-Umzug: %d Dateien nach %s verschoben, %d übrig (Ziel belegt oder Fehler)", moved, a.audioRoot(), left)
	}
}

// mergeMove verschiebt src nach dst; liegt dort schon ein Ordner, werden die Inhalte einzeln zusammengeführt.
// Belegte Ziel-Dateien werden nie überschrieben: die Quelle bleibt dann liegen und zählt als übrig.
func mergeMove(src, dst string) (moved, left int) {
	fi, err := os.Lstat(src)
	if err != nil {
		return 0, 1
	}
	if _, err := os.Lstat(dst); os.IsNotExist(err) {
		if err := os.MkdirAll(filepath.Dir(dst), 0o750); err != nil {
			logf("Medien-Umzug: %v", err)
			return 0, 1
		}
		if err := os.Rename(src, dst); err != nil {
			logf("Medien-Umzug: %v", err)
			return 0, 1
		}
		if fi.IsDir() {
			return countFiles(dst), 0
		}
		return 1, 0
	}
	if !fi.IsDir() {
		logf("Medien-Umzug: %s liegt schon in %s, bleibt am alten Ort", filepath.Base(src), filepath.Dir(dst))
		return 0, 1
	}
	entries, err := os.ReadDir(src)
	if err != nil {
		return 0, 1
	}
	for _, e := range entries {
		m, l := mergeMove(filepath.Join(src, e.Name()), filepath.Join(dst, e.Name()))
		moved, left = moved+m, left+l
	}
	os.Remove(src) // nur, wenn leer
	return moved, left
}

func countFiles(root string) (n int) {
	filepath.WalkDir(root, func(_ string, d os.DirEntry, err error) error {
		if err == nil && d.Type().IsRegular() {
			n++
		}
		return nil
	})
	return n
}
