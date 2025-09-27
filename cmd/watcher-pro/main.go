// Package main stellt die Hauptanwendung für watcher-pro bereit.
// Ein erweiterbarer File-Watcher mit HTTP-API und Event-Bus.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"gopkg.in/yaml.v3"

	"watcher-pro/internal/api"
	"watcher-pro/internal/bus"
	"watcher-pro/internal/config"
	"watcher-pro/internal/core"
	p "watcher-pro/internal/parser"
	"watcher-pro/internal/storage"
)

// main ist der Einstiegspunkt der watcher-pro Anwendung.
// Initialisiert Konfiguration, Event-Bus, Watcher und HTTP-Server.
func main() {
	var (
		cfgPath  string
		paths    string
		httpAddr string
		debounce int
		dbEnable bool
	)
	flag.StringVar(&cfgPath, "config", "", "Pfad zu watcher.yaml")
	flag.StringVar(&paths, "paths", "", "Kommagetrennte Watch-Pfade")
	flag.StringVar(&httpAddr, "http", ":8080", "HTTP Listen Address (SSE unter /events)")
	flag.IntVar(&debounce, "debounce", 0, "Debounce in Millisekunden (override)")
	flag.BoolVar(&dbEnable, "db", true, "Embedded Postgres aktivieren/deaktivieren (override)")
	flag.Parse()

	cfg := config.Default()
	if cfgPath != "" {
		b, err := os.ReadFile(cfgPath)
		if err != nil {
			log.Fatalf("config read: %v", err)
		}
		if err := yaml.Unmarshal(b, &cfg); err != nil {
			log.Fatalf("config parse: %v", err)
		}
	}
	if paths != "" {
		cfg.Roots = nil
		for _, p := range strings.Split(paths, ",") {
			p = strings.TrimSpace(p)
			if p != "" {
				cfg.Roots = append(cfg.Roots, p)
			}
		}
	}
	if debounce > 0 {
		cfg.DebounceMillis = debounce
	}
	cfg.Normalize()
	if err := cfg.Validate(); err != nil {
		log.Fatalf("ungültige Konfiguration: %v", err)
	}

	// Event-Bus für die Pub/Sub-Kommunikation zwischen Komponenten
	eb := bus.New()

	// Embedded PostgreSQL starten (optional via Config)
	var store *storage.Store
	if dbEnable && cfg.DBEnable {
		st, err := storage.StartEmbedded(cfg)
		if err != nil {
			log.Printf("embedded postgres deaktiviert (Start fehlgeschlagen): %v", err)
		} else {
			store = st
			log.Printf("Embedded Postgres läuft auf Port %d (Expose=%v Listen=%s SSL=%v)", cfg.DBPort, cfg.DBExpose, cfg.DBListen, cfg.DBSSL)
		}
	}

	// Parser-Engine registrieren
	eng := p.NewEngine()
	eng.Register(".go", p.GoParser{})
	eng.Register(".js", p.RegexParser{Lang: "js"})
	eng.Register(".ts", p.RegexParser{Lang: "ts"})
	eng.Register(".tsx", p.RegexParser{Lang: "tsx"})
	eng.Register(".py", p.RegexParser{Lang: "py"})

	// Watcher-Kern erstellen und konfigurieren
	w, err := core.New(cfg)
	if err != nil {
		log.Fatal(err)
	}
	if err := w.Start(); err != nil {
		log.Fatal(err)
	}
	log.Printf("Watcher Pro gestartet. Roots=%v Debounce=%dms", cfg.Roots, cfg.DebounceMillis)

	// Event-Pipeline: Watcher-Events an Event-Bus weiterleiten und parsen
	go func() {
		for ev := range w.Events() {
			eb.Publish(ev)
			if store != nil {
				logStoreError("save event", ev.Path, store.SaveEvent(ev))
			}
			// Parsen bei Erstellen/Schreiben für unterstützte Dateierweiterungen
			if !ev.IsDir {
				facts, err := eng.ParseFile(ev.Path)
				if err != nil {
					// Parse-Fehler erfassen, aber weitermachen
					if store != nil {
						logStoreError("insert parse error", ev.Path, store.InsertParseError(context.Background(), ev.Path, err.Error(), 0))
					}
					continue
				}
				// Fakten persistieren
				ctx := context.Background()
				ext := strings.ToLower(filepath.Ext(ev.Path))
				if store != nil {
					logStoreError("upsert file", ev.Path, store.UpsertFile(ctx, storage.FileMeta{Path: ev.Path, Ext: ext, Size: ev.Size, MTime: ev.MTime}))
					logStoreError("clear facts", ev.Path, store.ClearFactsForFile(ctx, ev.Path))
					for _, s := range facts.Symbols {
						logStoreError("insert symbol", ev.Path, store.InsertSymbol(ctx, ev.Path, s.Kind, s.Name, s.LineStart, s.LineEnd, s.Meta))
					}
					for _, d := range facts.Deps {
						logStoreError("insert dep", ev.Path, store.InsertDep(ctx, ev.Path, d.Name, d.Line, d.Meta))
					}
					for _, a := range facts.APIs {
						logStoreError("insert api", ev.Path, store.InsertAPI(ctx, ev.Path, a.Framework, a.Method, a.Route, a.Line, a.Meta))
					}
					for _, st := range facts.Strings {
						logStoreError("insert string", ev.Path, store.InsertString(ctx, ev.Path, st.Line, st.Text, st.Meta))
					}
					for _, cmt := range facts.Comments {
						logStoreError("insert comment", ev.Path, store.InsertComment(ctx, ev.Path, cmt.Line, cmt.Text, cmt.Meta))
					}
				}
			}
		}
	}()

	// HTTP-API-Server für Health-Check, Status und Event-Streaming
	srv := &http.Server{Addr: httpAddr, Handler: api.Router(eb, cfg, store)}
	go func() {
		log.Printf("HTTP unter %s (GET /health, /status, /events)", httpAddr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("http error: %v", err)
		}
	}()

	// Signal-Handler für graceful shutdown
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	<-sig
	log.Printf("Shutdown …")
	_ = srv.Close()
	w.Stop()
	if store != nil {
		store.Close()
	}
	time.Sleep(100 * time.Millisecond)
	fmt.Println("bye")
}

func logStoreError(action, path string, err error) {
	if err == nil {
		return
	}
	log.Printf("storage %s failed for %s: %v", action, path, err)
}
