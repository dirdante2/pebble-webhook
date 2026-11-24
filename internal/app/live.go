package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	webpush "github.com/SherClockHolmes/webpush-go"

	"pebble-webhook/internal/store"
)

// hub verteilt Änderungsmeldungen an offene SSE-Verbindungen.
type hub struct {
	mu   sync.Mutex
	subs map[chan string]bool
}

func newHub() *hub { return &hub{subs: map[chan string]bool{}} }

func (h *hub) send(msg string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.subs {
		select {
		case ch <- msg:
		default: // langsamer Empfänger: Meldung fällt weg, der Client lädt beim nächsten Ereignis neu
		}
	}
}

func (h *hub) sub() chan string {
	ch := make(chan string, 16)
	h.mu.Lock()
	h.subs[ch] = true
	h.mu.Unlock()
	return ch
}

func (h *hub) unsub(ch chan string) {
	h.mu.Lock()
	delete(h.subs, ch)
	h.mu.Unlock()
}

func (a *App) changed(jobID string) { a.hub.send("eintrag:" + jobID) }

func (a *App) handleEvents(w http.ResponseWriter, r *http.Request) {
	fl, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "kein Streaming", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	fmt.Fprint(w, "retry: 5000\n\n")
	fl.Flush()
	ch := a.hub.sub()
	defer a.hub.unsub(ch)
	keep := time.NewTicker(25 * time.Second)
	defer keep.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case msg := <-ch:
			fmt.Fprintf(w, "data: %s\n\n", msg)
		case <-keep.C:
			fmt.Fprint(w, ": ping\n\n")
		}
		fl.Flush()
	}
}

// --- Web-Push ---

func (a *System) initPush() error {
	pub, err := a.DB.Get("vapid_public")
	if err != nil {
		return err
	}
	priv, _ := a.DB.Get("vapid_private")
	if pub == "" || priv == "" {
		priv, pub, err = webpush.GenerateVAPIDKeys()
		if err != nil {
			return err
		}
		if err := a.DB.Set("vapid_private", priv); err != nil {
			return err
		}
		if err := a.DB.Set("vapid_public", pub); err != nil {
			return err
		}
	}
	a.vapidPub, a.vapidPriv = pub, priv
	return nil
}

// notify schickt eine Benachrichtigung an alle angemeldeten Geräte (im Hintergrund), wenn der Nutzer den Kanal
// (config.Notify…) eingeschaltet hat; channel "" = immer (Test-Push).
func (a *App) notify(channel, title, body, path string) {
	if channel != "" && !a.Cfg.Notify[channel] {
		return
	}
	subs, err := a.DB.PushSubs()
	if err != nil || len(subs) == 0 {
		return
	}
	msg, _ := json.Marshal(map[string]string{"titel": title, "text": body, "pfad": path})
	subject := a.Cfg.VAPIDSubject
	if subject == "" {
		subject = a.Cfg.PublicURL
	}
	go func() {
		for _, s := range subs {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			resp, err := webpush.SendNotificationWithContext(ctx, msg, &webpush.Subscription{Endpoint: s.Endpoint,
				Keys: webpush.Keys{P256dh: s.P256dh, Auth: s.Auth}}, &webpush.Options{Subscriber: subject,
				VAPIDPublicKey: a.vapidPub, VAPIDPrivateKey: a.vapidPriv, TTL: 86400, Urgency: webpush.UrgencyNormal})
			cancel()
			if err != nil {
				logf("Push: %v", err)
				continue
			}
			resp.Body.Close()
			if resp.StatusCode == 404 || resp.StatusCode == 410 {
				a.DB.DeletePushSub(s.Endpoint) // Abo beim Anbieter erloschen
			} else if resp.StatusCode >= 400 {
				logf("Push: Anbieter antwortet mit HTTP %d", resp.StatusCode)
			}
		}
	}()
}

func (a *App) handlePushKey(w http.ResponseWriter, r *http.Request) {
	subs, _ := a.DB.PushSubs()
	writeJSON(w, 200, map[string]any{"schluessel": a.vapidPub, "abos": len(subs)})
}

func (a *App) handlePushSubscribe(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Endpoint string `json:"endpoint"`
		Keys     struct {
			P256dh string `json:"p256dh"`
			Auth   string `json:"auth"`
		} `json:"keys"`
	}
	if err := readJSON(r, &in); err != nil || in.Endpoint == "" || in.Keys.P256dh == "" || in.Keys.Auth == "" {
		writeErr(w, 400, "ungueltig", "Abo unvollständig")
		return
	}
	if err := a.DB.AddPushSub(store.PushSub{Endpoint: in.Endpoint, P256dh: in.Keys.P256dh, Auth: in.Keys.Auth}); err != nil {
		writeErr(w, 500, "speicher", "Abo nicht gespeichert")
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}

func (a *App) handlePushUnsubscribe(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Endpoint string `json:"endpoint"`
	}
	if err := readJSON(r, &in); err != nil || in.Endpoint == "" {
		writeErr(w, 400, "ungueltig", "Abo fehlt")
		return
	}
	a.DB.DeletePushSub(in.Endpoint)
	writeJSON(w, 200, map[string]any{"ok": true})
}

func (a *App) handlePushTest(w http.ResponseWriter, r *http.Request) {
	a.notify("", "Test", a.T("Benachrichtigungen funktionieren."), "/status")
	writeJSON(w, 200, map[string]any{"ok": true})
}
