package storage

import (
    "context"
    "encoding/hex"
    "crypto/sha1"
    "path/filepath"
    "time"
)

type FileMeta struct {
    Path string
    Ext  string
    Size int64
    MTime int64
}

func (s *Store) UpsertFile(ctx context.Context, fm FileMeta) error {
    _, err := s.db.ExecContext(ctx, `insert into files(path, ext, size, mtime, last_parsed_at)
        values ($1,$2,$3, to_timestamp($4), now())
        on conflict(path) do update set ext = excluded.ext, size = excluded.size, mtime = excluded.mtime, last_parsed_at = now()`,
        filepath.ToSlash(fm.Path), fm.Ext, fm.Size, fm.MTime,
    )
    return err
}

func (s *Store) ClearFactsForFile(ctx context.Context, path string) error {
    p := filepath.ToSlash(path)
    tables := []string{"symbols", "deps", "api_endpoints", "strs", "comments"}
    for _, t := range tables {
        if _, err := s.db.ExecContext(ctx, "delete from "+t+" where path = $1", p); err != nil { return err }
    }
    return nil
}

func (s *Store) InsertSymbol(ctx context.Context, path, kind, name string, lineStart, lineEnd int, meta any) error {
    _, err := s.db.ExecContext(ctx, `insert into symbols(path, kind, name, line_start, line_end, meta) values ($1,$2,$3,$4,$5,$6)`,
        filepath.ToSlash(path), kind, name, lineStart, lineEnd, meta,
    )
    return err
}

func (s *Store) InsertDep(ctx context.Context, path, dep string, line int, meta any) error {
    _, err := s.db.ExecContext(ctx, `insert into deps(path, dep, line, meta) values ($1,$2,$3,$4)`,
        filepath.ToSlash(path), dep, line, meta,
    )
    return err
}

func (s *Store) InsertAPI(ctx context.Context, path, framework, method, route string, line int, meta any) error {
    _, err := s.db.ExecContext(ctx, `insert into api_endpoints(path, framework, method, route, line, meta) values ($1,$2,$3,$4,$5,$6)`,
        filepath.ToSlash(path), framework, method, route, line, meta,
    )
    return err
}

func (s *Store) InsertString(ctx context.Context, path string, line int, text string, meta any) error {
    short := text
    if len(short) > 160 { short = short[:160] }
    h := sha1.Sum([]byte(text))
    hash := hex.EncodeToString(h[:])
    _, err := s.db.ExecContext(ctx, `insert into strs(path, line, hash, text_short, meta) values ($1,$2,$3,$4,$5)`,
        filepath.ToSlash(path), line, hash, short, meta,
    )
    return err
}

func (s *Store) InsertComment(ctx context.Context, path string, line int, text string, meta any) error {
    short := text
    if len(short) > 160 { short = short[:160] }
    _, err := s.db.ExecContext(ctx, `insert into comments(path, line, text_short, text_full, meta) values ($1,$2,$3,$4,$5)`,
        filepath.ToSlash(path), line, short, text, meta,
    )
    return err
}

func (s *Store) InsertRef(ctx context.Context, srcPath string, srcLine int, symbol string, targetPaths any, meta any) error {
    _, err := s.db.ExecContext(ctx, `insert into refs(src_path, src_line, symbol_name, target_paths, meta) values ($1,$2,$3,$4,$5)`,
        filepath.ToSlash(srcPath), srcLine, symbol, targetPaths, meta,
    )
    return err
}

func (s *Store) InsertParseError(ctx context.Context, path, message string, line int) error {
    _, err := s.db.ExecContext(ctx, `insert into parse_errors(path, message, line, created_at) values ($1,$2,$3,$4)`,
        filepath.ToSlash(path), message, line, time.Now().UTC(),
    )
    return err
}

// Blob-Aufteilung
func (s *Store) WriteBlob(ctx context.Context, filePath string, data []byte, chunkSize int) error {
    p := filepath.ToSlash(filePath)
    if _, err := s.db.ExecContext(ctx, `delete from blobs where file_path=$1`, p); err != nil { return err }
    seq := 0
    for off := 0; off < len(data); off += chunkSize {
        end := off + chunkSize
        if end > len(data) { end = len(data) }
        if _, err := s.db.ExecContext(ctx, `insert into blobs(file_path, seq, data) values ($1,$2,$3)`, p, seq, data[off:end]); err != nil {
            return err
        }
        seq++
    }
    return nil
}

// Abfragen
func (s *Store) FactsByFile(ctx context.Context, path string) (map[string]any, error) {
    p := filepath.ToSlash(path)
    out := map[string]any{}
    collect := func(q string, dest *[]map[string]any, args ...any) error {
        rows, err := s.db.QueryContext(ctx, q, args...)
        if err != nil { return err }
        defer rows.Close()
        cols, _ := rows.Columns()
        for rows.Next() {
            vals := make([]any, len(cols))
            ptrs := make([]any, len(cols))
            for i := range vals { ptrs[i] = &vals[i] }
            if err := rows.Scan(ptrs...); err != nil { return err }
            m := map[string]any{}
            for i, c := range cols { m[c] = vals[i] }
            *dest = append(*dest, m)
        }
        return rows.Err()
    }
    var syms, deps, apis, strs, coms []map[string]any
    if err := collect(`select kind,name,line_start,line_end,meta from symbols where path=$1 order by line_start`, &syms, p); err != nil { return nil, err }
    if err := collect(`select dep,line,meta from deps where path=$1 order by line`, &deps, p); err != nil { return nil, err }
    if err := collect(`select framework,method,route,line,meta from api_endpoints where path=$1 order by line`, &apis, p); err != nil { return nil, err }
    if err := collect(`select line,hash,text_short,meta from strs where path=$1 order by line`, &strs, p); err != nil { return nil, err }
    if err := collect(`select line,text_short,meta from comments where path=$1 order by line`, &coms, p); err != nil { return nil, err }
    out["symbols"] = syms
    out["deps"] = deps
    out["api_endpoints"] = apis
    out["strings"] = strs
    out["comments"] = coms
    return out, nil
}

// CommentsByFile gibt Kommentare für eine Datei zurück; wenn full true ist, wird text_full als text zurückgegeben, andernfalls text_short.
func (s *Store) CommentsByFile(ctx context.Context, path string, full bool) ([]map[string]any, error) {
    p := filepath.ToSlash(path)
    sel := "text_short"
    if full { sel = "coalesce(text_full, text_short)" }
    q := `select line, ` + sel + ` as text, meta from comments where path=$1 order by line`
    rows, err := s.db.QueryContext(ctx, q, p)
    if err != nil { return nil, err }
    defer rows.Close()
    out := []map[string]any{}
    for rows.Next() {
        var line int
        var text string
        var meta any
        if err := rows.Scan(&line, &text, &meta); err != nil { return nil, err }
        out = append(out, map[string]any{"line": line, "text": text, "meta": meta})
    }
    return out, rows.Err()
}

func (s *Store) FindSymbols(ctx context.Context, name string, limit int) ([]map[string]any, error) {
    rows, err := s.db.QueryContext(ctx, `select path, kind, name, line_start, line_end from symbols where name = $1 order by path, line_start limit $2`, name, limit)
    if err != nil { return nil, err }
    defer rows.Close()
    out := []map[string]any{}
    for rows.Next() {
        var path, kind, nm string
        var ls, le int
        if err := rows.Scan(&path, &kind, &nm, &ls, &le); err != nil { return nil, err }
        out = append(out, map[string]any{"path": path, "kind": kind, "name": nm, "line_start": ls, "line_end": le})
    }
    return out, rows.Err()
}
