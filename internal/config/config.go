// Package config verwaltet die Konfigurationsstruktur und -logik für watcher-pro.
// Unterstützt YAML-Konfiguration mit Standardwerten und Umgebungsvariablen.
package config

import (
    "os"
    "path/filepath"
    "strings"
)

// Config enthält alle Konfigurationsparameter für den File-Watcher.
type Config struct {
    Roots          []string `yaml:"roots"`          // Wurzelverzeichnisse zum Überwachen
    Ignore         []string `yaml:"ignore"`         // Zu ignorierende Pfade/Muster
    IncludeExt     []string `yaml:"include_ext"`     // Nur diese Dateierweiterungen überwachen
    DebounceMillis int      `yaml:"debounce_ms"`     // Debounce-Zeit in Millisekunden
    FollowNewDirs  bool     `yaml:"follow_new_dirs"`  // Neu erstellte Verzeichnisse automatisch überwachen

    // Embedded PostgreSQL-Konfiguration
    DBEnable       bool     `yaml:"db_enable"`       // PostgreSQL-Integration aktivieren
    DBPort         int      `yaml:"db_port"`         // PostgreSQL-Port
    DBDataDir      string   `yaml:"db_data_dir"`      // PostgreSQL-Datenverzeichnis
    DBRuntimeDir   string   `yaml:"db_runtime_dir"`   // PostgreSQL-Laufzeitverzeichnis

    // Zugangsdaten/Datenbankname
    DBUser         string   `yaml:"db_user"`
    DBPass         string   `yaml:"db_pass"`
    DBName         string   `yaml:"db_name"`

    // Optionale Netzwerkfreigabe
    DBExpose       bool     `yaml:"db_expose"`
    DBListen       string   `yaml:"db_listen"`        // z.B. 127.0.0.1 oder 0.0.0.0
    DBHBA          []string `yaml:"db_hba"`           // CIDRs, z.B. ["127.0.0.1/32", "192.168.0.0/16"]
    DBAuthMethod   string   `yaml:"db_auth_method"`   // md5 oder scram-sha-256

    // Optional TLS
    DBSSL          bool     `yaml:"db_ssl"`
    DBSSLCert      string   `yaml:"db_ssl_cert"`
    DBSSLKey       string   `yaml:"db_ssl_key"`

    // Optional: DB-Tuning
    DBTempFileLimit      string `yaml:"db_temp_file_limit"`       // z.B. "-1" (unbegrenzt) oder "10GB"
    DBWorkMem            string `yaml:"db_work_mem"`               // z.B. "64MB"
    DBMaintenanceWorkMem string `yaml:"db_maintenance_work_mem"`   // z.B. "256MB"
    DBSharedBuffers      string `yaml:"db_shared_buffers"`         // z.B. "512MB"
    DBMaxWalSize         string `yaml:"db_max_wal_size"`           // z.B. "4GB"
    DBEffectiveCacheSize string `yaml:"db_effective_cache_size"`   // z.B. "1GB"
}

// Default erstellt eine Konfiguration mit sensiblen Standardwerten.
// Enthält gängige Dateierweiterungen und Ignore-Muster.
func Default() Config {
    return Config{
        Roots:          []string{"."},
        Ignore:         []string{".git/", "node_modules/", "dist/", "build/", "tmp/", "*.log"},
        IncludeExt:     []string{".go", ".py", ".js", ".ts", ".tsx", ".java", ".kt", ".c", ".h", ".cs", ".rb", ".php", ".au3", ".sh", ".bash", ".zsh", ".html", ".css", ".xml", ".json", ".md", ".sql"},
        DebounceMillis: 250,
        FollowNewDirs:  true,
        DBEnable:       true,
        DBPort:         54329,
        DBDataDir:      filepath.Join(".pg", "data"),
        DBRuntimeDir:   filepath.Join(".pg", "runtime"),
        DBUser:         "postgres",
        DBPass:         "postgres",
        DBName:         "postgres",
        DBExpose:       false,
        DBListen:       "127.0.0.1",
        DBHBA:          []string{"127.0.0.1/32"},
        DBAuthMethod:   "scram-sha-256",
        DBSSL:          false,
        DBSSLCert:      "",
        DBSSLKey:       "",

        // Tuning (konservativ, optional aktivierbar)
        DBTempFileLimit:      "-1",
        DBWorkMem:            "64MB",
        DBMaintenanceWorkMem: "256MB",
        DBSharedBuffers:      "256MB",
        DBMaxWalSize:         "2GB",
        DBEffectiveCacheSize: "1GB",
    }
}

// Normalize bereinigt und normalisiert die Konfigurationswerte.
// Pfade werden bereinigt und Umgebungsvariablen angewendet.
func (c *Config) Normalize() {
    for i, r := range c.Roots {
        c.Roots[i] = filepath.Clean(r)
    }
    for i, e := range c.IncludeExt {
        e = strings.ToLower(strings.TrimSpace(e))
        if e != "" && !strings.HasPrefix(e, ".") { e = "." + e }
        c.IncludeExt[i] = e
    }
    // Umgebungsvariablen-Überschreibungen zulassen
    if v := os.Getenv("WATCHER_DB_PORT"); v != "" {
        // Parse-Fehler stillschweigend ignorieren
        if p := atoi(v); p > 0 { c.DBPort = p }
    }
    if v := os.Getenv("WATCHER_DB_EXPOSE"); strings.ToLower(v) == "true" { c.DBExpose = true }
    if v := os.Getenv("WATCHER_DB_LISTEN"); v != "" { c.DBListen = v }
    if v := os.Getenv("WATCHER_DB_USER"); v != "" { c.DBUser = v }
    if v := os.Getenv("WATCHER_DB_PASS"); v != "" { c.DBPass = v }
    if v := os.Getenv("WATCHER_DB_NAME"); v != "" { c.DBName = v }
    if v := os.Getenv("WATCHER_DB_SSL"); strings.ToLower(v) == "true" { c.DBSSL = true }
    if v := os.Getenv("WATCHER_DB_SSL_CERT"); v != "" { c.DBSSLCert = v }
    if v := os.Getenv("WATCHER_DB_SSL_KEY"); v != "" { c.DBSSLKey = v }
    if v := os.Getenv("WATCHER_DB_AUTH_METHOD"); v != "" { c.DBAuthMethod = v }
}

// LoadGitIgnore lädt .gitignore-Muster in die Ignore-Liste.
// Erweitert die bestehenden Ignore-Regeln um Git-Patterns.
func (c *Config) LoadGitIgnore(root string) {
    p := filepath.Join(root, ".gitignore")
    b, err := os.ReadFile(p)
    if err != nil { return }
    lines := strings.Split(string(b), "\n")
    for _, ln := range lines {
        ln = strings.TrimSpace(ln)
        if ln == "" || strings.HasPrefix(ln, "#") { continue }
        c.Ignore = append(c.Ignore, ln)
    }
}

// atoi konvertiert einen String zu int ohne Fehlerbehandlung.
// Gibt 0 zurück bei ungültigen Zeichen.
func atoi(s string) int {
    n := 0
    for _, r := range s {
        if r < '0' || r > '9' { return 0 }
        n = n*10 + int(r-'0')
    }
    return n
}
