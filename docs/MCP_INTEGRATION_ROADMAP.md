# Watcher Pro - MCP Integrationsfahrplan (Hosted Service)

## Vision und Ziele
- Bereitstellung einer gehosteten Watcher-Pro-Plattform, die MCP-kompatible Tools ueber HTTPS anbietet.
- Verwaltung von Projekt-Onboarding, API-Schluesseln und Zusammenarbeit ueber die Watcher-Pro-Webanwendung.
- Bereitstellung nahezu echtzeitfaehiger Code-Intelligence fuer Claude und andere MCP-Clients, inklusive sicherer Wiederherstellung bei Offline-Clients.

## Plattformrollen
- Server: Watcher-Pro-Backend (Web-App, API, MCP-Gateway, Datenbank, Hintergrundprozesse).
- Client: MCP-WatcherPro-Agent, der auf Entwicklerrechnern laeuft und Projektzustand synchronisiert.

## Zielarchitektur
```
Entwicklerrechner               Watcher Pro Cloud
-------------------             -----------------
MCP-WatcherPro-Client  -->  HTTPS/API-Gateway  -->  Application Services
Dateiwatcher + Diff           Auth + Rate Limits       Tool Orchestrator
Lokale Queue + Retry          TLS-Terminierung         Event Worker
                              Audit Logging            PostgreSQL + Object Storage
```

### Kerndatenfluss
1. Nutzer registriert sich auf der Watcher-Pro-Webseite (E-Mail/Passwort oder Social Login) und erstellt einen Workspace.
2. Backend erzeugt einen API-Schluessel (Key-ID + Secret), der fuer den Workspace gilt; bei einem einzelnen Key kann der Nutzer ihn rotieren oder spaeter weitere Keys anlegen.
3. Nutzer konfiguriert MCP-WatcherPro mit Server-URL, API-Schluessel.
4. Optional nutzt der Nutzer das Client-MCP-Tool `pro_init`, um das Projekt zu initialisieren oder nach einer neuen Session wieder zu betreten; das Projektverzeichnis wird ueber das Tool zugewiesen und nicht dauerhaft in einer Config hinterlegt. Wenn nur ein API-Schluessel existiert, gilt er fuer alle initialisierten Projekte; ueber Tools wie `project.switch` waehlt der Nutzer, welches Projekt gerade aktiv ist.
5. Client ueberwacht Datei-Create/Update/Delete-Events, berechnet Diffs und uebertraegt sie an die Ingest-API stets als Commits innerhalb des eigenen Commit-Systems. Der Webserver signalisiert dem Client, nach jeder Aenderung einen Commit abzusetzen, damit jede Aenderung rueckgaengig gemacht werden kann; langfristig kann eine lokale KI diesen Schritt serverseitig uebernehmen.
6. Server validiert den Key, persistiert die Aenderung, aktualisiert Parse-Fakten und publiziert Events an Tool-Indizes.
7. Claude (oder ein anderer MCP-Client) verbindet sich mit dem gehosteten MCP-Gateway unter Nutzung desselben API-Schluessels und ruft Tools wie `project.info` oder `code.search` auf.
8. Antworten kombinieren den aktuellen Datenbankzustand mit lokal gepufferten Aenderungen (wenn der Client online ist) oder serverseitigen Edits (wenn offline).

## Komponentenverantwortung

### Webanwendung und Identity
- Registrierung und Login mit passwortloser E-Mail sowie optionalen OAuth-Anbietern (Google, GitHub etc.).
- Workspace-Verwaltung, Projektuebersicht, Rollenvergabe, Audit-Trail.
- Lebenszyklus fuer API-Schluessel: Erstellen, Umbenennen, Rotieren, Widerrufen, Rate-Limits pro Key.
- API-Key-Sharing-Regeln: IP- oder Hostname-Whitelists pflegen, erlaubte Nutzeranzahl pro Key definieren und ungewoehnliche Verwendungen im Webinterface freigeben oder sperren.

### MCP-Gateway-Service
- Stellt JSON-RPC-2.0-Endpunkte ueber HTTPS und WebSocket (`/mcp/jsonrpc`, `/mcp/ws`) bereit.
- Authentifiziert jede Anfrage via `Authorization: Bearer <key_id>:<secret>`.
- Ordnet MCP-Tool-Aufrufe internen Services zu (Projektmetadaten, Dateiinhalte, Suche, Symbole, Referenzen).
- Streamt grosse Antworten und erzwingt Groessen- sowie Zeitlimits pro Methode.

### Ingest-Service (Client-Sync)
- Empfaengt gebatchte Dateideltas vom MCP-WatcherPro ueber HTTPS mit idempotenten Request-IDs.
- Persistiert Dateiversionen, Change-Logs und stoesst Hintergrund-Parsings an.
- Unterstuetzt Offline-Retry: Client puffert Events lokal und spielt sie bei Rueckkehr der Verbindung ab.
- Beim ersten Sync prueft der Server, ob lokale Dateien fehlen; Nutzer entscheidet, ob der Server den aktuellen Projektstand lokal ausrollen oder lokale Dateien unveraendert lassen soll (mit Hinweis auf Auswirkungen auf das gesamte Projekt).
- Divergierende Staende fuehren zu Rueckfragen: Nutzer kann Serverzustand uebernehmen, einen lokalen Stand als separaten Branch sichern oder einen spaeteren Merge planen; alternative Branches werden serverseitig gespeichert und bei Bedarf neu geparst.
- Server-first-Sync: Aenderungen werden zuerst als Commits in der Datenbank verbucht, danach erhalten Clients Delta-Auftraege zur Synchronisation.

### MCP-WatcherPro-Client
- Laeuft headless mit minimaler UI; Nutzer weist lokales Projektverzeichnis und Ziel-Workspace-ID zu.
- Ueberwacht das Dateisystem mit nativen Watchern, wendet Ignore-Regeln an und erzeugt gehashte Snapshots.
- Meldet Diffs, Loeschungen und Metadaten an den Webserver; Clients empfangen priorisierte Server-Deltas und spiegeln damit vorrangig den Serverzustand.
- Stellt lokalen Status (verbunden, letzter Sync, Fehler) bereit.

### Datenbank und Storage
- PostgreSQL-Schema fuer Users, Workspaces, Projects, Files, Symbols, References, Change Logs, API Keys, Audit-Eintraege sowie registrierte Client-Identitaeten (Hostname, Fingerprint).
- Optionales Object Storage (S3-kompatibel) fuer grosse Binaerartefakte oder Historienarchive.
- Hintergrund-Worker bauen Indizes neu und pflegen abgeleitete Tabellen fuer schnelle MCP-Abfragen.

## Sicherheit und Compliance
- Zwingendes TLS; Reverse Proxy (Caddy, Nginx, Traefik) uebernimmt Zertifikate, HSTS und Request-Logging.
- API-Schluessel als gehashte Secrets (argon2id oder bcrypt) mit Prefix-Anzeige im UI.
- Optionale IP-Allowlists und Key-Scopes (`files:read`, `symbols:read`, `ingest:write`).
- Rate-Limiting pro Key und global zur Missbrauchsvermeidung.
- Missbrauchserkennung fuer geteilte Keys: unbekannte IPs/Hosts automatisch blocken, Nutzer benachrichtigen und Freigabe-Workflows im Portal anbieten.
- Detailliertes Audit-Logging fuer Logins, Key-Rotation, Ingest-Events und MCP-Calls.

## Fahrplan-Phasen

### Phase 0 - Fundament (Woche 1-2)
- PostgreSQL-Schema fuer Identity, Projects, Files, `api_keys`, `audit_logs` aufsetzen.
- Web-Auth (E-Mail + Passwort) und grundlegende UI fuer Projektliste und aktuellen API-Schluessel implementieren.
- Endpunkt fuer Key-Rotation und Widerruf bereitstellen; Audit-Events erfassen.

### Phase 1 - MCP-Gateway-MVP (Woche 3-4)
- Binary `cmd/watcher-gateway` mit HTTPS-Unterstuetzung und JSON-RPC-Handling bauen.
- Kern-Tools implementieren: `project.info`, `files.read`, `files.search`.
- Request-Attribution, Rate-Limiting und strukturierte Logs hinzufuegen.
- Referenzkonfiguration fuer Claude Desktop und Smoke-Tests bereitstellen.

### Phase 2 - Client-Sync-Alpha (Woche 5-6)
- MCP-WatcherPro-Client (Go) mit Watcher, Diffing, Batching und Retry-Queue bauen.
- Ingest-API (`/ingest/events`, `/ingest/status`) mit Signaturvalidierung erstellen.
- Change Logs persistieren und Hintergrund-Parsing zum Aktualisieren der Fakten anstossen.
- Entwickler-CLI-Kommandos fuer Projektregistrierung und Konnektivitaetstests bereitstellen.

### Phase 3 - Tool-Erweiterung und Zusammenarbeit (Woche 7-9)
- `code.search`, `symbols.search`, `symbols.refs` sowie Diff-History-Endpunkte ergaenzen.
- Projektberechtigungen und Multi-Key-Unterstuetzung (Team, Umgebung) implementieren.
- Konflikt- und Branch-Workflow fuer geteilte Keys: automatische Sicherungs-Branches, Undo/Redo und Benachrichtigungen bei abweichenden Staenden.
- Projektstatus-Dashboard zeigen (Sync-Health, Queues, letzte Edits).
- Grundlage fuer serverseitige Edits legen, indem strukturierte Changesets mit Autor-Metadaten gespeichert werden.

### Phase 4 - Produktionsreife (Woche 10-12)
- OpenID-Connect-Provider (Google, GitHub) integrieren.
- Observability ausbauen: Prometheus-Metriken, Grafana-Dashboards, Alert-Regeln.
- Infrastruktur harden (WAF, Backup-Strategie, Disaster-Recovery-Runbooks).
- Lasttests fuer Ingest und MCP-Queries ausfuehren; Indizes und Caching optimieren.

## Zukuenftiger Backlog (Post-MVP)
- Serverseitige KI-Agenten, die Edits mit Drittanbieter-API-Schluesseln ausfuehren und als Commit-Objekte speichern.
- Bidirektionaler Sync: Serversignale loesen Client-Updates mit Konfliktloesung und Undo-Stacks aus.
- Umfangreiche Web-IDE mit Inline-Diff-Viewer, Timeline und manuellem Freigabe-Workflow.
- Billing, Nutzungsanalytics und Marketplace-Integrationen.

## Lieferobjekte
- `cmd/watcher-gateway` (HTTPS-MCP-Service) inklusive Container-Image.
- Webportal (Go + Templates oder SPA) fuer Identity, Projects und API-Key-Management.
- `internal/mcp` Tool-Adapter, geteilt vom Gateway und kuenftigen lokalen CLIs.
- MCP-WatcherPro-Client-Binaries fuer Windows, macOS, Linux.
- Infrastrukturskripte (Terraform/Ansible oder Helm) fuer den Server-Rollout.
- Dokumentation: Onboarding-Guide, API-Referenz, Client-Konfiguration, Runbooks.

## Naechste Schritte
1. Hosting-Stack festlegen (Cloud-Provider, Reverse Proxy, CI/CD-Pipeline).
2. Datenbankschema fuer Workspaces, Projects, Files, Change Logs, API Keys designen.
3. HTTPS-Gateway mit statischer Key-Validierung und `project.info` Methode prototypen.
4. Ingest-Vertrag definieren (JSON-Schema, Batching-Regeln, Retry-Semantik).
5. UX-Mockups fuer initiales Projektdashboard und API-Key-Verwaltung erstellen.












