// Package api stellt die HTTP-REST-API für watcher-pro bereit.
// Bietet Health-Checks, Status-Abfragen und Server-Sent Events.
package api

import (
    "encoding/json"
    "fmt"
    "net/http"
    "time"

    "watcher-pro/internal/bus"
    "watcher-pro/internal/config"
    "watcher-pro/internal/storage"
)

// Router erstellt einen HTTP-Handler mit allen API-Endpunkten.
// Integriert Event-Bus für Live-Event-Streaming über SSE.
func Router(b *bus.Bus, cfg config.Config, store *storage.Store) http.Handler {
    mux := http.NewServeMux()
    // Health-Check-Endpoint für Load Balancer und Monitoring
    mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
        w.Header().Set("Content-Type", "application/json")
        w.WriteHeader(200)
        w.Write([]byte(`{"ok":true}`))
    })
    // Status-Endpoint zeigt Konfiguration und aktuelle Zeit
    mux.HandleFunc("/status", func(w http.ResponseWriter, r *http.Request) {
        w.Header().Set("Content-Type", "application/json")
        resp := map[string]any{
            "ok": true,
            "roots": cfg.Roots,
            "time": time.Now().Unix(),
        }
        _ = json.NewEncoder(w).Encode(resp)
    })
    // DB Status (ohne psql)
    mux.HandleFunc("/db/status", func(w http.ResponseWriter, r *http.Request) {
        w.Header().Set("Content-Type", "application/json")
        if store == nil {
            _ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "reason": "db disabled"})
            return
        }
        _ = json.NewEncoder(w).Encode(map[string]any{
            "ok": true,
            "port": cfg.DBPort,
            "expose": cfg.DBExpose,
            "listen": cfg.DBListen,
        })
    })
    // DB Aktuelle Events
    mux.HandleFunc("/db/recent", func(w http.ResponseWriter, r *http.Request) {
        w.Header().Set("Content-Type", "application/json")
        if store == nil {
            _ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "reason": "db disabled"})
            return
        }
        limit := 50
        if q := r.URL.Query().Get("limit"); q != "" {
            if n := atoi(q); n > 0 && n <= 500 { limit = n }
        }
        rows, err := store.Recent(r.Context(), limit)
        if err != nil {
            w.WriteHeader(500)
            _ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": err.Error()})
            return
        }
        _ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "items": rows})
    })

    // Facts: nach Datei
    mux.HandleFunc("/facts/file", func(w http.ResponseWriter, r *http.Request) {
        w.Header().Set("Content-Type", "application/json")
        if store == nil { _ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "reason": "db disabled"}); return }
        path := r.URL.Query().Get("path")
        if path == "" { w.WriteHeader(400); _ = json.NewEncoder(w).Encode(map[string]string{"error":"path required"}); return }
        facts, err := store.FactsByFile(r.Context(), path)
        if err != nil { w.WriteHeader(500); _ = json.NewEncoder(w).Encode(map[string]any{"ok":false,"error":err.Error()}); return }
        _ = json.NewEncoder(w).Encode(map[string]any{"ok":true, "facts": facts})
    })

    // Kommentare: nach Datei (full optional)
    mux.HandleFunc("/comments/file", func(w http.ResponseWriter, r *http.Request) {
        w.Header().Set("Content-Type", "application/json")
        if store == nil { _ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "reason": "db disabled"}); return }
        path := r.URL.Query().Get("path")
        if path == "" { w.WriteHeader(400); _ = json.NewEncoder(w).Encode(map[string]string{"error":"path required"}); return }
        // Standard: vollständige Kommentare
        full := true
        if raw := r.URL.Query().Get("full"); raw != "" { full = (raw == "true") }
        items, err := store.CommentsByFile(r.Context(), path, full)
        if err != nil { w.WriteHeader(500); _ = json.NewEncoder(w).Encode(map[string]any{"ok":false,"error":err.Error()}); return }
        // optionale max_len zum Kürzen der Ausgabe ohne Storage zu beeinflussen
        if ml := atoi(r.URL.Query().Get("max_len")); ml > 0 {
            for i := range items {
                if txt, ok := items[i]["text"].(string); ok && len(txt) > ml { items[i]["text"] = txt[:ml] }
            }
        }
        _ = json.NewEncoder(w).Encode(map[string]any{"ok":true, "items": items, "full": full})
    })

    // Facts: Symbole nach Name
    mux.HandleFunc("/facts/symbol", func(w http.ResponseWriter, r *http.Request) {
        w.Header().Set("Content-Type", "application/json")
        if store == nil { _ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "reason": "db disabled"}); return }
        name := r.URL.Query().Get("name")
        if name == "" { w.WriteHeader(400); _ = json.NewEncoder(w).Encode(map[string]string{"error":"name required"}); return }
        rows, err := store.FindSymbols(r.Context(), name, 200)
        if err != nil { w.WriteHeader(500); _ = json.NewEncoder(w).Encode(map[string]any{"ok":false,"error":err.Error()}); return }
        _ = json.NewEncoder(w).Encode(map[string]any{"ok":true, "items": rows})
    })
    // Events-Endpoint für Server-Sent Events (Live-Stream)
    mux.HandleFunc("/events", func(w http.ResponseWriter, r *http.Request) {
        w.Header().Set("Content-Type", "text/event-stream")
        w.Header().Set("Cache-Control", "no-cache")
        w.Header().Set("Connection", "keep-alive")
        // HTTP-Flushing für Echtzeit-Streaming erforderlich
        flusher, ok := w.(http.Flusher)
        if !ok { w.WriteHeader(500); return }
        // Event-Bus-Subscription mit 256-Event-Buffer
        ch := b.Subscribe(256)
        defer b.Unsubscribe(ch)
        fmt.Fprintf(w, "event: ready\n")
        fmt.Fprintf(w, "data: {\"ok\":true}\n\n")
        flusher.Flush()
        // Client-Disconnect-Erkennung über Request-Context
        notify := r.Context().Done()
        for {
            select {
            case ev := <-ch:
                b, _ := json.Marshal(ev)
                fmt.Fprintf(w, "data: %s\n\n", string(b))
                flusher.Flush()
            case <-notify:
                return
            }
        }
    })
    return mux
}

// atoi Hilfsfunktion zum Parsen von Limits
func atoi(s string) int {
    n := 0
    for _, r := range s { if r < '0' || r > '9' { return 0 }; n = n*10 + int(r-'0') }
    return n
}
