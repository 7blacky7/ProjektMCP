// Package bus implementiert einen einfachen Pub/Sub Event-Bus.
// Ermöglicht lose gekoppelte Kommunikation zwischen Komponenten.
package bus

import (
    "sync"
    "watcher-pro/internal/core"
)

// Bus ist ein einfacher Pub/Sub Event-Bus für File-Watcher Events.
// Thread-sicher mit RWMutex für gleichzeitigen Zugriff.
type Bus struct {
    mu   sync.RWMutex                    // Schutz für gleichzeitige Zugriffe
    subs map[chan core.Event]struct{}   // Aktive Subscriber-Channels
}

// New erstellt eine neue Bus-Instanz mit initialisierter Subscriber-Map.
func New() *Bus {
    return &Bus{subs: make(map[chan core.Event]struct{})}
}

// Subscribe erstellt einen neuen Event-Channel mit gegebener Puffergröße.
// Rückgabe ist der Channel für empfangene Events.
func (b *Bus) Subscribe(buf int) chan core.Event {
    ch := make(chan core.Event, buf)
    b.mu.Lock()
    b.subs[ch] = struct{}{}
    b.mu.Unlock()
    return ch
}

// Unsubscribe entfernt einen Channel aus der Subscriber-Liste.
// Der Channel wird geschlossen und kann nicht mehr verwendet werden.
func (b *Bus) Unsubscribe(ch chan core.Event) {
    b.mu.Lock()
    delete(b.subs, ch)
    close(ch)
    b.mu.Unlock()
}

// Publish sendet ein Event an alle aktiven Subscriber.
// Langsame Consumer werden übersprungen (non-blocking).
func (b *Bus) Publish(ev core.Event) {
    b.mu.RLock()
    for ch := range b.subs {
        select { case ch <- ev: default: /* Event verwerfen bei langsamem Consumer */ }
    }
    b.mu.RUnlock()
}

