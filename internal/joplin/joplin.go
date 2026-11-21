// Package joplin schiebt fertige Notizen in eine Richtung auf einen Joplin-Server
// (Sync-Ziel der Clients). Es gibt keinen lokalen Joplin-Client: der Dienst schreibt
// die Elemente im Format, das auch die Clients ablegen. Voraussetzung: E2EE ist aus.
// Gelesen wird aus Joplin nie; eine erneute Übertragung überschreibt die Notiz.
package joplin

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

type Client struct {
	URL, Email, Password string
	HTTP                 *http.Client

	mu      sync.Mutex
	session string
}

func New(url, email, password string) *Client {
	return &Client{URL: strings.TrimRight(url, "/"), Email: email, Password: password,
		HTTP: &http.Client{Timeout: 30 * time.Second}}
}

type Note struct {
	ID            string
	ParentID      string
	Title         string
	Body          string
	IsTodo        bool
	TodoCompleted int64 // ms, 0 = offen
	CreatedAt     int64 // ms
	UpdatedAt     int64 // ms
}

func iso(ms int64) string {
	return time.UnixMilli(ms).UTC().Format("2006-01-02T15:04:05.000Z")
}

func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

// SerializeNote erzeugt das Ablageformat einer Notiz (Titel, Text, Eigenschaften).
func SerializeNote(n Note) string {
	var b strings.Builder
	b.WriteString(oneLine(n.Title))
	b.WriteString("\n\n")
	b.WriteString(strings.TrimRight(n.Body, "\n"))
	b.WriteString("\n\n")
	fmt.Fprintf(&b, "id: %s\nparent_id: %s\ncreated_time: %s\nupdated_time: %s\n", n.ID, n.ParentID, iso(n.CreatedAt), iso(n.UpdatedAt))
	b.WriteString("is_conflict: 0\nlatitude: 0.00000000\nlongitude: 0.00000000\naltitude: 0.0000\nauthor: \nsource_url: \n")
	fmt.Fprintf(&b, "is_todo: %d\ntodo_due: 0\ntodo_completed: %d\n", b2i(n.IsTodo), n.TodoCompleted)
	b.WriteString("source: pebble-webhook\nsource_application: online.pebble-webhook\napplication_data: \norder: 0\n")
	fmt.Fprintf(&b, "user_created_time: %s\nuser_updated_time: %s\n", iso(n.CreatedAt), iso(n.UpdatedAt))
	b.WriteString("encryption_cipher_text: \nencryption_applied: 0\nmarkup_language: 1\nis_shared: 0\nshare_id: \n")
	b.WriteString("conflict_original_id: \nmaster_key_id: \nuser_data: \ndeleted_time: 0\nis_locked: 0\nextracted_resource_ids: \ntype_: 1")
	return b.String()
}

// SerializeFolder erzeugt das Ablageformat eines Notizbuchs.
func SerializeFolder(id, title, parentID string, ms int64) string {
	t := iso(ms)
	return fmt.Sprintf("%s\n\nid: %s\ncreated_time: %s\nupdated_time: %s\nuser_created_time: %s\nuser_updated_time: %s\n"+
		"encryption_cipher_text: \nencryption_applied: 0\nparent_id: %s\nis_shared: 0\nshare_id: \nmaster_key_id: \nicon: \n"+
		"user_data: \ndeleted_time: 0\ntype_: 2", oneLine(title), id, t, t, t, t, parentID)
}

func (c *Client) login() error {
	body, _ := json.Marshal(map[string]string{"email": c.Email, "password": c.Password})
	resp, err := c.HTTP.Post(c.URL+"/api/sessions", "application/json", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("Joplin nicht erreichbar: %w", err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	var out struct {
		ID string `json:"id"`
	}
	if resp.StatusCode != 200 || json.Unmarshal(data, &out) != nil || out.ID == "" {
		return fmt.Errorf("Joplin-Anmeldung abgelehnt (HTTP %d)", resp.StatusCode)
	}
	c.session = out.ID
	return nil
}

func (c *Client) do(method, path string, body []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for attempt := 0; ; attempt++ {
		if c.session == "" {
			if err := c.login(); err != nil {
				return 0, err
			}
		}
		req, err := http.NewRequest(method, c.URL+path, bytes.NewReader(body))
		if err != nil {
			return 0, err
		}
		req.Header.Set("X-API-AUTH", c.session)
		if body != nil {
			req.Header.Set("Content-Type", "application/octet-stream")
		}
		resp, err := c.HTTP.Do(req)
		if err != nil {
			return 0, fmt.Errorf("Joplin nicht erreichbar: %w", err)
		}
		io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
		resp.Body.Close()
		if (resp.StatusCode == 401 || resp.StatusCode == 403) && attempt == 0 {
			c.session = ""
			continue
		}
		return resp.StatusCode, nil
	}
}

func itemPath(id string) string { return "/api/items/root:/" + id + ".md:" }

func (c *Client) put(id, content string) error {
	st, err := c.do("PUT", itemPath(id)+"/content", []byte(content))
	if err != nil {
		return err
	}
	if st != 200 {
		return fmt.Errorf("Joplin hat das Schreiben abgelehnt (HTTP %d)", st)
	}
	return nil
}

// Exists meldet, ob ein Element auf dem Server liegt.
func (c *Client) Exists(id string) (bool, error) {
	st, err := c.do("GET", itemPath(id), nil)
	if err != nil {
		return false, err
	}
	switch st {
	case 200:
		return true, nil
	case 404:
		return false, nil
	}
	return false, fmt.Errorf("Joplin antwortet mit HTTP %d", st)
}

// EnsureFolder legt ein Notizbuch an, wenn es fehlt.
func (c *Client) EnsureFolder(id, title, parentID string) error {
	ok, err := c.Exists(id)
	if err != nil || ok {
		return err
	}
	return c.put(id, SerializeFolder(id, title, parentID, time.Now().UnixMilli()))
}

// RenameFolder schreibt ein Notizbuch mit neuem Titel (gleiche Id = gleiches Notizbuch, die Notizen darin bleiben).
func (c *Client) RenameFolder(id, title, parentID string) error {
	return c.put(id, SerializeFolder(id, title, parentID, time.Now().UnixMilli()))
}

// PutNote legt eine Notiz an oder überschreibt sie (gleiche Id = gleiche Notiz).
func (c *Client) PutNote(n Note) error { return c.put(n.ID, SerializeNote(n)) }

// Delete entfernt ein Element; ein fehlendes gilt als entfernt.
func (c *Client) Delete(id string) error {
	st, err := c.do("DELETE", itemPath(id), nil)
	if err != nil {
		return err
	}
	if st != 200 && st != 404 {
		return fmt.Errorf("Joplin hat das Löschen abgelehnt (HTTP %d)", st)
	}
	return nil
}

// Ping prüft Erreichbarkeit und Anmeldung.
func (c *Client) Ping() error {
	_, err := c.Exists("info.json-probe")
	return err
}

// EnsureTag legt ein Schlagwort an, wenn es fehlt.
func (c *Client) EnsureTag(id, title string) error {
	ok, err := c.Exists(id)
	if err != nil || ok {
		return err
	}
	t := iso(time.Now().UnixMilli())
	return c.put(id, fmt.Sprintf("%s\n\nid: %s\ncreated_time: %s\nupdated_time: %s\nuser_created_time: %s\nuser_updated_time: %s\n"+
		"encryption_cipher_text: \nencryption_applied: 0\nis_shared: 0\nparent_id: \nuser_data: \ntype_: 5", oneLine(title), id, t, t, t, t))
}

// PutNoteTag hängt ein Schlagwort an eine Notiz.
func (c *Client) PutNoteTag(id, noteID, tagID string) error {
	t := iso(time.Now().UnixMilli())
	return c.put(id, fmt.Sprintf("id: %s\nnote_id: %s\ntag_id: %s\ncreated_time: %s\nupdated_time: %s\nuser_created_time: %s\n"+
		"user_updated_time: %s\nencryption_cipher_text: \nencryption_applied: 0\nis_shared: 0\ntype_: 6", id, noteID, tagID, t, t, t, t))
}
