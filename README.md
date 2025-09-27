# Watcher Pro

Ein modularer, robuster File‑Watcher in Go mit eingebetteter PostgreSQL‑Datenbank, Debouncing, Event‑Bus und HTTP‑API (Health, Status, SSE, Facts). Enthält eine schlanke Parser‑Schicht für die wichtigsten Projekt‑Fakten, optimiert für KI/MCP‑Abfragen.

## Highlights

- Rekursives Watching mehrerer Roots, optionales Folgen neuer Ordner
- Ignore‑Regeln inkl. `.gitignore`, Include‑Extensions
- Debouncing/Coalescing gegen Event‑Stürme
- Event‑Bus (Pub/Sub) + HTTP‑API (SSE: `/events`)
- Eingebettetes PostgreSQL (optional exponierbar im LAN)
- Parser: Funktionen, Variablen/Konstanten, Imports/Deps, API‑Endpoints, Strings, Kommentare (voll)
- Fakten persistiert mit Pfaden und Zeilenangaben, kompakt abrufbar

## Schnellstart (Windows PowerShell)

```powershell
cd watcher-pro
# Abhängigkeiten einmal ziehen
go mod tidy

# Ohne DB (schnell):
go run .\cmd\watcher-pro --config .\watcher.yaml -db=false

# Mit DB (empfohlen):
go run .\cmd\watcher-pro --config .\watcher.yaml -db=true
```

Basierend auf `watcher.yaml`. Beispiel‑Endpoints:

- `GET /health` → `{ok:true}`
- `GET /status` → Laufzeitinfos
- `GET /events` → SSE‑Stream (JSON‑Events)
- `GET /db/status`, `GET /db/recent?limit=50`
- `GET /facts/file?path=<Pfad>` → Fakten zu einer Datei (sym/deps/api/strings/comments kurz)
- `GET /facts/symbol?name=<Name>` → Definitionen (Pfad + Zeilen)
- `GET /comments/file?path=<Pfad>&full=true&max_len=500` → Kommentare (voll, optional gekürzt)

## Konfiguration (`watcher.yaml`)

Kerntypen:

```yaml
roots: ["."]
ignore: [".git/", "node_modules/", "dist/", "*.log"]
include_ext: [".go", ".ts", ".js", ".py", ".json", ".md"]
debounce_ms: 250
follow_new_dirs: true

# Embedded Postgres
db_enable: true
db_port: 54330
db_data_dir: .pg/data
db_runtime_dir: .pg/runtime
db_user: postgres
db_pass: postgres
db_name: postgres

# Netzwerk‑Freigabe (optional)
db_expose: true
db_listen: 0.0.0.0
db_hba: ["192.168.50.0/24", "127.0.0.1/32"]
db_auth_method: scram-sha-256
db_ssl: false
db_ssl_cert: ""
db_ssl_key: ""

# Tuning (optional)
db_temp_file_limit: "-1"
db_work_mem: "64MB"
db_maintenance_work_mem: "256MB"
db_shared_buffers: "256MB"
db_max_wal_size: "2GB"
db_effective_cache_size: "1GB"
```

Hinweise:
- Beim ersten DB‑Start benötigt `embedded-postgres` passende Binaries (Internet oder bereits vorhandene unter `.pg/runtime`).
- Firewall: TCP‑Port (`db_port`) für LAN‑Zugriff freigeben.
- Postgres‑Limit: 1 GB pro Spaltenwert (hart). Große Inhalte → Tabelle `blobs` (Chunking) nutzen.

## Datenmodell (Kurzüberblick)

- `events(id, path, op, is_dir, size, mtime, created_at)`
- `files(path PK, ext, size, mtime, last_parsed_at)`
- `symbols(id, path, kind, name, line_start, line_end, meta)`
- `deps(id, path, dep, line, meta)`
- `api_endpoints(id, path, framework, method, route, line, meta)`
- `strs(id, path, line, hash, text_short, meta)`
- `comments(id, path, line, text_short, text_full, meta)`
- `refs(id, src_path, src_line, symbol_name, target_paths, meta)`
- `blobs(file_path, seq, data)`

Kommentare werden voll in `text_full` gespeichert. Abfragen können per `max_len` gekürzt werden.

## Datenbankschema (Erklärung)

Das Schema ist auf „Fakten statt Volltext“ ausgelegt. Jeder Eintrag ist klein, eindeutig referenzierbar (Pfad + Zeile) und schnell abfragbar.

- `events`: Rohereignisse vom Dateisystem
  - `path`: Vollständiger Dateipfad
  - `op`: Operation (create|write|remove|rename|chmod)
  - `is_dir`: Ob es ein Verzeichnis ist
  - `size`/`mtime`: Größe und letzter Änderungszeitpunkt
  - Indexe für schnelle Suche nach `path` und `created_at`

- `files`: Metadaten zu Dateien
  - `path`: Primärschlüssel (eindeutiger Pfad)
  - `ext`: Dateiendung (z. B. `.go`)
  - `size`, `mtime`: Größe und Änderungszeitpunkt
  - `last_parsed_at`: Zeitpunkt der letzten erfolgreichen Analyse

- `symbols`: Funktionen, Variablen, Konstanten
  - `kind`: `function` | `var` | `const`
  - `name`: Symbolname
  - `line_start`/`line_end`: Zeilenbereich in der Quelldatei
  - `meta`: Zusatzinfos (z. B. Go-Receiver)
  - Indexe auf `name` und `path` für schnelle Nachschlagewerke

- `deps`: Abhängigkeiten/Imports
  - `dep`: Importierter Paket-/Modulname
  - `line`: Zeile der Importstelle
  - `meta`: Zusatzinfos (z. B. Import-Art)

- `api_endpoints`: Erkannte API-Endpunkte
  - `framework`: z. B. `net/http`, `mux`, `express`, `fastapi`
  - `method`: HTTP-Methode (z. B. GET, POST)
  - `route`: Pfad/Pattern
  - `line`: Zeile der Definition

- `strs`: String-Literale (leichtgewichtig)
  - `line`: Zeile des Literals
  - `hash`: SHA1-Hash zum Deduplizieren
  - `text_short`: Gekürzte Vorschau (max. ca. 160 Zeichen)

- `comments`: Kommentare (voll)
  - `line`: Zeile des Kommentars
  - `text_short`: Kurze Vorschau (gekürzt)
  - `text_full`: Vollständiger Kommentartext (keine Kürzung in der DB)
  - Hinweis: Ausgabe kann per `max_len` begrenzt werden, ohne die Speicherung zu ändern

- `refs`: Verwendungsstellen (für spätere Ausbaustufe)
  - `src_path`/`src_line`: Herkunftsstelle
  - `symbol_name`: Referenziertes Symbol
  - `target_paths`: Liste der vermuteten Zielpfade (JSON)

- `blobs`: Große Inhalte in Teilen (Chunking)
  - `file_path`: Pfad der Quelldatei
  - `seq`: Chunk-Nummer (0-basiert)
  - `data`: Byteinhalt des Chunks
  - Zweck: Um Postgres-Grenzen pro Feld (~1 GB) sauber zu umgehen

Warum dieses Design?
- Schnelle Abfragen: Viele kleine, indizierte Zeilen statt riesiger Texte
- KI‑freundlich: Fakten mit Pfad+Zeile → gute Erklärbarkeit + Zitate
- Skalierbarkeit: Volltexte nur optional (Kommentare/Blobs), standardmäßig kompakt

## Parser‑Abdeckung v1

- Go (AST): Funktionen, Variablen/Konstanten, Imports, Strings, Kommentare, einfache net/http + gorilla/mux‑Heuristik
- JS/TS (Regex): Funktionen, Variablen, `import`/`require`, Express `app.METHOD('/route')`
- Python (Regex): `def`, `import/from`, FastAPI `@app.get('/route')`

Bewusst schlank, anti‑Überfrachtung: Strings/Kommentare in Facts gekappt, Volltext separat abrufbar.

## Troubleshooting

- `flag provided but not defined`: Flag‑Syntax beachten (`--db=false` oder `-db=false`).
- Falscher Config‑Pfad: Im Projektordner `watcher-pro` → `--config .\watcher.yaml`.
- `missing go.sum entry`: `go mod tidy` ausführen.
- DB startet nicht: Binaries fehlen → Internet zulassen oder `.pg/runtime` befüllen. Port frei?

## Roadmap (für morgen)

- Refs‑Indexierung: Verwendungsstellen je Funktion (Pfad + Zeilen), heuristisch mit Limits
- API‑Erkennung stabilisieren (Go mux‑Chains, Express Router, FastAPI Routen)
- Trimming‑Optionen erweitern (`max_words`, `max_tokens`, `ellipsize=true`)
- Optional: Auto‑Blobs ab N MB, MCP‑Adapter (Tools für Facts/Comments/Refs)

Viel Erfolg beim Testen! Fragen/Notizen einfach hier fortführen – wir verfeinern morgen gezielt.
