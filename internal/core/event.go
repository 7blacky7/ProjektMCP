// Package core enthält die Kernfunktionalität des File-Watchers.
// Definiert Event-Strukturen und Watcher-Logik.
package core

// Event repräsentiert ein Dateisystem-Ereignis.
// Enthält alle relevanten Metadaten für JSON-Serialisierung.
type Event struct {
    Path  string `json:"path"`            // Dateipfad des Ereignisses
    Op    string `json:"op"`              // Operation (create, write, remove, etc.)
    IsDir bool   `json:"isDir"`           // Ist das Ziel ein Verzeichnis
    Size  int64  `json:"size,omitempty"`  // Dateigröße (nur für Dateien)
    MTime int64  `json:"mtime,omitempty"` // Letzte Änderungszeit (Unix-Timestamp)
}

