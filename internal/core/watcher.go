package core

import (
    "errors"
    "log"
    "os"
    "path/filepath"
    "strings"
    "sync"
    "time"

    "github.com/fsnotify/fsnotify"
    "watcher-pro/internal/config"
)

// Watcher ist der Kern des File-Watching-Systems.
// Verwaltet fsnotify-Integration, Debouncing und Event-Filtering.
type Watcher struct {
    cfg     config.Config        // Konfiguration für Pfade, Filter, etc.
    fsw     *fsnotify.Watcher    // Zugrunde liegende fsnotify-Instanz
    out     chan Event           // Ausgehende Events nach Debouncing
    stop    chan struct{}        // Signal für Shutdown
    mu      sync.Mutex           // Schutz für pending-Map
    pending map[string]Event     // Events warten auf Debounce-Timeout
    tick    *time.Ticker         // Debounce-Timer
}

// New erstellt eine neue Watcher-Instanz mit der gegebenen Konfiguration.
// Normalisiert die Config und initialisiert interne Strukturen.
func New(cfg config.Config) (*Watcher, error) {
    cfg.Normalize()
    w, err := fsnotify.NewWatcher()
    if err != nil { return nil, err }
    return &Watcher{
        cfg: cfg,
        fsw: w,
        out: make(chan Event, 256),
        stop: make(chan struct{}),
        pending: make(map[string]Event),
        tick: time.NewTicker(time.Duration(max(cfg.DebounceMillis, 50)) * time.Millisecond),
    }, nil
}

// Start beginnt das File-Watching für alle konfigurierten Roots.
// Lädt .gitignore-Dateien und startet Hintergrund-Goroutines.
func (w *Watcher) Start() error {
    if len(w.cfg.Roots) == 0 { return errors.New("no roots configured") }
    for _, r := range w.cfg.Roots {
        if err := w.addRecursive(r); err != nil {
            log.Printf("warn addRecursive(%s): %v", r, err)
        }
        w.cfg.LoadGitIgnore(r)
    }
    go w.run()
    go w.flush()
    return nil
}

// Stop beendet das File-Watching und schließt alle Ressourcen.
// Stoppt Ticker, fsnotify-Watcher und schließt Output-Channel.
func (w *Watcher) Stop() {
    close(w.stop)
    w.tick.Stop()
    _ = w.fsw.Close()
    close(w.out)
}

// Events gibt den Read-Only-Channel für ausgehende Events zurück.
func (w *Watcher) Events() <-chan Event { return w.out }

// addRecursive fügt alle Verzeichnisse unter root rekursiv zum Watcher hinzu.
// Filtert bereits zu ignorierende Pfade aus.
func (w *Watcher) addRecursive(root string) error {
    return filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
        if err != nil { return nil }
        if info.IsDir() {
            if w.shouldIgnore(path) { return filepath.SkipDir }
            if err := w.fsw.Add(path); err != nil { log.Printf("watch add %s: %v", path, err) }
        }
        return nil
    })
}

// run ist die Hauptschleife für die Verarbeitung von fsnotify-Events.
// Läuft in eigener Goroutine und verarbeitet Events, Fehler und Stop-Signal.
func (w *Watcher) run() {
    for {
        select {
        case ev, ok := <-w.fsw.Events:
            if !ok { return }
            op := opToString(ev.Op)
            if w.cfg.FollowNewDirs && (ev.Op&fsnotify.Create == fsnotify.Create) {
                if fi, err := os.Stat(ev.Name); err == nil && fi.IsDir() { _ = w.addRecursive(ev.Name) }
            }
            if op == "remove" { w.enqueue(ev.Name, op); continue }
            if w.shouldIgnore(ev.Name) { continue }
            if !w.shouldIncludeExt(ev.Name) { continue }
            w.enqueue(ev.Name, op)
        case err, ok := <-w.fsw.Errors:
            if !ok { return }
            log.Printf("watch error: %v", err)
        case <-w.stop:
            return
        }
    }
}

// enqueue fügt ein Event zur Debounce-Warteschlange hinzu.
// Sammelt Datei-Metadaten und speichert Event in pending-Map.
func (w *Watcher) enqueue(path, op string) {
    w.mu.Lock()
    defer w.mu.Unlock()
    e := Event{Path: path, Op: op}
    if fi, err := os.Stat(path); err == nil {
        e.IsDir = fi.IsDir()
        if !fi.IsDir() { e.Size = fi.Size(); e.MTime = fi.ModTime().Unix() }
    }
    w.pending[path] = e
}

// flush sendet gesammelte Events aus der pending-Map an den Output-Channel.
// Läuft in eigener Goroutine mit Ticker-basiertem Timing.
func (w *Watcher) flush() {
    for {
        select {
        case <-w.tick.C:
            w.mu.Lock()
            tmp := make([]Event, 0, len(w.pending))
            for k, v := range w.pending { tmp = append(tmp, v); delete(w.pending, k) }
            w.mu.Unlock()
            for _, e := range tmp { select { case w.out <- e: default: } }
        case <-w.stop:
            return
        }
    }
}

// shouldIgnore prüft ob ein Pfad basierend auf Ignore-Patterns übersprungen werden soll.
// Unterstützt Glob-Patterns und relative Pfad-Matching.
func (w *Watcher) shouldIgnore(path string) bool {
    rel := path
    if len(w.cfg.Roots) > 0 { if r, err := filepath.Rel(w.cfg.Roots[0], path); err == nil { rel = r } }
    rel = filepath.ToSlash(rel)
    for _, p := range w.cfg.Ignore { if matchPattern(rel, p) { return true } }
    return false
}

// shouldIncludeExt prüft ob eine Dateierweiterung überwacht werden soll.
// Basiert auf IncludeExt-Konfiguration (Whitelist-Ansatz).
func (w *Watcher) shouldIncludeExt(path string) bool {
    if len(w.cfg.IncludeExt) == 0 { return true }
    ext := strings.ToLower(filepath.Ext(path))
    if ext == "" { return false }
    for _, e := range w.cfg.IncludeExt { if e == ext { return true } }
    return false
}

// opToString konvertiert fsnotify-Operationen zu lesbaren Strings.
// Mappt Bitflags auf entsprechende Aktionsnamen.
func opToString(op fsnotify.Op) string {
    switch {
    case op&fsnotify.Create == fsnotify.Create: return "create"
    case op&fsnotify.Write == fsnotify.Write:   return "write"
    case op&fsnotify.Remove == fsnotify.Remove: return "remove"
    case op&fsnotify.Rename == fsnotify.Rename: return "rename"
    case op&fsnotify.Chmod == fsnotify.Chmod:   return "chmod"
    default: return "unknown"
    }
}

// matchPattern prüft ob ein Dateipfad einem Ignore-Pattern entspricht.
// Unterstützt Verzeichnis-Suffixe, Glob-Patterns und einfache Substring-Matching.
func matchPattern(filePath, pattern string) bool {
    filePath = filepath.ToSlash(strings.TrimSpace(filePath))
    pattern = filepath.ToSlash(strings.TrimSpace(pattern))
    if pattern == "" { return false }
    if strings.HasSuffix(pattern, "/") {
        dir := strings.TrimSuffix(pattern, "/")
        for _, p := range strings.Split(filePath, "/") { if p == dir { return true } }
    }
    if strings.ContainsAny(pattern, "*?") {
        if ok, _ := filepath.Match(pattern, filePath); ok { return true }
        if ok, _ := filepath.Match(pattern, filepath.Base(filePath)); ok { return true }
        simp := strings.ReplaceAll(pattern, "**", "*")
        if ok, _ := filepath.Match(simp, filePath); ok { return true }
        return false
    }
    if strings.Contains(filePath, pattern) { return true }
    if filepath.Base(filePath) == pattern { return true }
    return false
}

// max gibt den größeren von zwei Integer-Werten zurück.
func max(a, b int) int { if a>b {return a}; return b }

