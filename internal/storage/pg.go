// Package storage implementiert persistente Speicherung von File-Events.
// Nutzt Embedded PostgreSQL für lokale Datenspeicherung ohne externe Abhängigkeiten.
package storage

import (
    "bufio"
    "context"
    "database/sql"
    "fmt"
    "os"
    "path/filepath"
    "strings"
    "time"

    embeddedpostgres "github.com/fergusstrange/embedded-postgres"
    _ "github.com/lib/pq"
    "watcher-pro/internal/config"
    "watcher-pro/internal/core"
)

// Store verwaltet eine eingebettete PostgreSQL-Instanz für Event-Persistierung.
// Kapselt Embedded PostgreSQL und SQL-Datenbankverbindung.
type Store struct {
    cfg   config.Config              // Konfiguration für DB-Parameter
    pg    *embeddedpostgres.EmbeddedPostgres // Embedded PostgreSQL-Instanz
    db    *sql.DB                    // SQL-Datenbankverbindung
}

// StartEmbedded startet eine eingebettete PostgreSQL-Instanz.
// Erstellt notwendige Verzeichnisse, startet DB und führt Migrationen durch.
func StartEmbedded(c config.Config) (*Store, error) {
    if err := os.MkdirAll(c.DBDataDir, 0o755); err != nil { return nil, err }
    if err := os.MkdirAll(c.DBRuntimeDir, 0o755); err != nil { return nil, err }

    pg := embeddedpostgres.NewDatabase(embeddedpostgres.DefaultConfig().
        Port(uint32(c.DBPort)).
        RuntimePath(c.DBRuntimeDir).
        DataPath(c.DBDataDir).
        Database(c.DBName).
        Username(c.DBUser).
        Password(c.DBPass),
    )
    if err := pg.Start(); err != nil {
        return nil, fmt.Errorf("embedded postgres start: %w", err)
    }
    // Verbindung zur Standard-PostgreSQL-Datenbank herstellen
    dsn := fmt.Sprintf("host=127.0.0.1 port=%d user=%s password=%s dbname=%s sslmode=disable", c.DBPort, c.DBUser, c.DBPass, c.DBName)
    db, err := sql.Open("postgres", dsn)
    if err != nil { _ = pg.Stop(); return nil, err }
    if err := db.Ping(); err != nil { _ = pg.Stop(); return nil, err }

    s := &Store{cfg: c, pg: pg, db: db}
    // Optional: Netzwerkfreigabe und TLS anwenden
    if c.DBExpose || c.DBSSL || len(c.DBHBA) > 0 || c.DBListen != "127.0.0.1" {
        changed, needsRestart := s.applyExposure()
        if changed {
            _ = s.db.Close()
            _ = s.pg.Stop()
            if needsRestart {
                if err := s.pg.Start(); err != nil { return nil, fmt.Errorf("postgres restart: %w", err) }
                db2, err := sql.Open("postgres", dsn)
                if err != nil { _ = s.pg.Stop(); return nil, err }
                if err := db2.Ping(); err != nil { _ = s.pg.Stop(); return nil, err }
                s.db = db2
            }
        }
    }
    if err := s.migrate(); err != nil { s.Close(); return nil, err }
    return s, nil
}

// Close schließt Datenbankverbindung und stoppt Embedded PostgreSQL.
// Sollte beim Shutdown der Anwendung aufgerufen werden.
func (s *Store) Close() {
    if s.db != nil { _ = s.db.Close() }
    if s.pg != nil { _ = s.pg.Stop() }
}

// migrate erstellt die notwendigen Datenbanktabellen und Indizes.
// Wird beim ersten Start der Embedded PostgreSQL ausgeführt.
func (s *Store) migrate() error {
    stmts := []string{
        `create table if not exists events (
            id bigserial primary key,
            path text not null,
            op text not null,
            is_dir boolean not null,
            size bigint,
            mtime timestamptz,
            created_at timestamptz not null default now()
        )`,
        `create index if not exists idx_events_created on events(created_at)`,
        `create index if not exists idx_events_path on events(path)`,

        // files
        `create table if not exists files (
            path text primary key,
            ext text,
            size bigint,
            mtime timestamptz,
            last_parsed_at timestamptz
        )`,

        // symbols (functions, vars, consts)
        `create table if not exists symbols (
            id bigserial primary key,
            path text not null,
            kind text not null,
            name text not null,
            line_start int,
            line_end int,
            meta jsonb,
            created_at timestamptz not null default now()
        )`,
        `create index if not exists idx_symbols_name on symbols(name)`,
        `create index if not exists idx_symbols_path on symbols(path)`,

        // dependencies/imports
        `create table if not exists deps (
            id bigserial primary key,
            path text not null,
            dep text not null,
            line int,
            meta jsonb,
            created_at timestamptz not null default now()
        )`,
        `create index if not exists idx_deps_dep on deps(dep)`,
        `create index if not exists idx_deps_path on deps(path)`,

        // api endpoints
        `create table if not exists api_endpoints (
            id bigserial primary key,
            path text not null,
            framework text,
            method text,
            route text,
            line int,
            meta jsonb,
            created_at timestamptz not null default now()
        )`,
        `create index if not exists idx_api_route on api_endpoints(route)`,

        // strings
        `create table if not exists strs (
            id bigserial primary key,
            path text not null,
            line int,
            hash text,
            text_short text,
            meta jsonb,
            created_at timestamptz not null default now()
        )`,
        `create index if not exists idx_strs_hash on strs(hash)`,

        // comments
        `create table if not exists comments (
            id bigserial primary key,
            path text not null,
            line int,
            text_short text,
            meta jsonb,
            created_at timestamptz not null default now()
        )`,
        `create index if not exists idx_comments_path on comments(path)`,

        // ensure full comment storage
        `alter table if exists comments add column if not exists text_full text`,

        // refs (references/usages)
        `create table if not exists refs (
            id bigserial primary key,
            src_path text not null,
            src_line int,
            symbol_name text not null,
            target_paths jsonb,
            meta jsonb,
            created_at timestamptz not null default now()
        )`,
        `create index if not exists idx_refs_symbol on refs(symbol_name)`,

        // blob chunks for large contents
        `create table if not exists blobs (
            file_path text not null,
            seq int not null,
            data bytea not null,
            primary key(file_path, seq)
        )`,
    }
    for _, q := range stmts {
        if _, err := s.db.Exec(q); err != nil { return err }
    }
    return nil
}

// SaveEvent speichert ein File-Event in der Datenbank.
// Konvertiert Unix-Timestamps zu PostgreSQL-Zeitstempeln.
func (s *Store) SaveEvent(ev core.Event) error {
    var mtime *time.Time
    if ev.MTime != 0 { t := time.Unix(ev.MTime, 0).UTC(); mtime = &t }
    _, err := s.db.Exec(`insert into events(path, op, is_dir, size, mtime) values ($1,$2,$3,$4,$5)`,
        filepath.ToSlash(ev.Path), ev.Op, ev.IsDir, nullInt64(ev.Size), mtime,
    )
    return err
}

// Recent gibt die neuesten Events aus der Datenbank zurück.
// Sortiert nach ID (neueste zuerst) mit konfigurierbarem Limit.
func (s *Store) Recent(ctx context.Context, limit int) ([]core.Event, error) {
    rows, err := s.db.QueryContext(ctx, `select path, op, is_dir, coalesce(size,0), coalesce(extract(epoch from mtime)::bigint,0) from events order by id desc limit $1`, limit)
    if err != nil { return nil, err }
    defer rows.Close()
    out := []core.Event{}
    for rows.Next() {
        var e core.Event
        if err := rows.Scan(&e.Path, &e.Op, &e.IsDir, &e.Size, &e.MTime); err != nil { return nil, err }
        out = append(out, e)
    }
    return out, rows.Err()
}

// nullInt64 konvertiert Null-Werte (0) zu SQL NULL für optionale Felder.
func nullInt64(n int64) any { if n==0 { return nil }; return n }

// applyExposure passt pg_hba.conf und Server-Parameter an.
// Gibt (changed, needsRestart) zurück.
func (s *Store) applyExposure() (bool, bool) {
    changed := false
    needsRestart := false

    // pg_hba.conf erweitern
    if len(s.cfg.DBHBA) > 0 {
        path := filepath.Join(s.cfg.DBDataDir, "pg_hba.conf")
        if ch, _ := appendUniqueLines(path, buildHBALines(s.cfg)); ch { changed = true }
    }

    // ALTER SYSTEM Konfigurationen
    set := func(key, val string) {
        _, _ = s.db.Exec(fmt.Sprintf("alter system set %s = '%s'", key, escape(val)))
        changed = true
    }
    if s.cfg.DBListen != "" {
        set("listen_addresses", s.cfg.DBListen)
        needsRestart = true
    }
    set("port", fmt.Sprintf("%d", s.cfg.DBPort))
    if s.cfg.DBSSL {
        set("ssl", "on")
        needsRestart = true
        if s.cfg.DBSSLCert != "" { set("ssl_cert_file", s.cfg.DBSSLCert) }
        if s.cfg.DBSSLKey != "" { set("ssl_key_file", s.cfg.DBSSLKey) }
    }
    // Optional Tuning-Parameter setzen
    setIf := func(key, val string) {
        if strings.TrimSpace(val) == "" { return }
        _, _ = s.db.Exec(fmt.Sprintf("alter system set %s = '%s'", key, escape(val)))
        changed = true
    }
    setIf("temp_file_limit", s.cfg.DBTempFileLimit)
    setIf("work_mem", s.cfg.DBWorkMem)
    setIf("maintenance_work_mem", s.cfg.DBMaintenanceWorkMem)
    setIf("shared_buffers", s.cfg.DBSharedBuffers)
    setIf("max_wal_size", s.cfg.DBMaxWalSize)
    setIf("effective_cache_size", s.cfg.DBEffectiveCacheSize)

    _, _ = s.db.Exec("select pg_reload_conf()")
    return changed, needsRestart
}

func buildHBALines(c config.Config) []string {
    method := c.DBAuthMethod
    if method == "" { method = "scram-sha-256" }
    lines := []string{}
    for _, cidr := range c.DBHBA {
        cidr = strings.TrimSpace(cidr)
        if cidr == "" { continue }
        keyword := "host"
        if c.DBSSL { keyword = "hostssl" }
        lines = append(lines, fmt.Sprintf("%s all all %s %s", keyword, cidr, method))
    }
    return lines
}

func appendUniqueLines(path string, lines []string) (bool, error) {
    _ = os.MkdirAll(filepath.Dir(path), 0o755)
    existing := map[string]bool{}
    var out []string
    if b, err := os.ReadFile(path); err == nil {
        scanner := bufio.NewScanner(strings.NewReader(string(b)))
        for scanner.Scan() {
            ln := scanner.Text()
            out = append(out, ln)
            existing[strings.TrimSpace(ln)] = true
        }
    }
    added := false
    for _, ln := range lines {
        if !existing[strings.TrimSpace(ln)] {
            out = append(out, ln)
            added = true
        }
    }
    if added {
        return true, os.WriteFile(path, []byte(strings.Join(out, "\n")+"\n"), 0o644)
    }
    return false, nil
}

func escape(s string) string { return strings.ReplaceAll(s, "'", "''") }
