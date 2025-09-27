package parser

import (
    "bufio"
    "bytes"
    "regexp"
)

type RegexParser struct{
    Lang string
}

var (
    reJSFunc = regexp.MustCompile(`\bfunction\s+([A-Za-z_][A-Za-z0-9_]*)\s*\(`)
    reJSVar  = regexp.MustCompile(`\b(const|let|var)\s+([A-Za-z_][A-Za-z0-9_]*)\b`)
    reJSImport = regexp.MustCompile(`\bimport\b[^;]*from\s+['\"]([^'\"]+)['\"]|require\(['\"]([^'\"]+)['\"]\)`)
    reExpress = regexp.MustCompile(`\b(app|router)\.(get|post|put|delete|patch|options|head)\s*\(\s*['\"]([^'\"]+)['\"]`)

    rePyFunc = regexp.MustCompile(`^\s*def\s+([A-Za-z_][A-Za-z0-9_]*)\s*\(`)
    rePyImport = regexp.MustCompile(`^\s*(from\s+([A-Za-z0-9_\.]+)\s+import\s+|import\s+([A-Za-z0-9_\.]+))`)
    reFastAPI = regexp.MustCompile(`@(?:app|router)\.(get|post|put|delete|patch)\(\s*['\"]([^'\"]+)['\"]`)
)

func (r RegexParser) Parse(path string, data []byte) (Facts, error) {
    facts := Facts{}
    scanner := bufio.NewScanner(bytes.NewReader(data))
    scanner.Buffer(make([]byte, 0, 1024), 1024*1024)
    line := 0
    for scanner.Scan() {
        line++
        s := scanner.Text()
        switch r.Lang {
        case "js", "ts", "tsx":
            if m := reJSFunc.FindStringSubmatch(s); len(m) > 1 {
                facts.Symbols = append(facts.Symbols, Symbol{Kind:"function", Name:m[1], LineStart:line, LineEnd:line})
            }
            if m := reJSVar.FindStringSubmatch(s); len(m) > 2 {
                facts.Symbols = append(facts.Symbols, Symbol{Kind:"var", Name:m[2], LineStart:line, LineEnd:line})
            }
            if m := reExpress.FindStringSubmatch(s); len(m) > 3 {
                facts.APIs = append(facts.APIs, APIEndpoint{Framework:"express", Method:m[2], Route:m[3], Line:line})
            }
            if m := reJSImport.FindStringSubmatch(s); len(m) > 0 {
                dep := ""
                if len(m) > 1 && m[1] != "" { dep = m[1] } else if len(m) > 2 { dep = m[2] }
                if dep != "" { facts.Deps = append(facts.Deps, Dep{Name:dep, Line:line}) }
            }
            // grundlegende Kommentare und Strings (überspringen, optional)
        case "py":
            if m := rePyFunc.FindStringSubmatch(s); len(m) > 1 {
                facts.Symbols = append(facts.Symbols, Symbol{Kind:"function", Name:m[1], LineStart:line, LineEnd:line})
            }
            if m := rePyImport.FindStringSubmatch(s); len(m) > 2 {
                dep := m[2]
                if dep == "" && len(m) > 3 { dep = m[3] }
                if dep != "" { facts.Deps = append(facts.Deps, Dep{Name:dep, Line:line}) }
            }
            if m := reFastAPI.FindStringSubmatch(s); len(m) > 2 {
                facts.APIs = append(facts.APIs, APIEndpoint{Framework:"fastapi", Method:m[1], Route:m[2], Line:line})
            }
        }
        // sehr einfache String-/Kommentar-Erfassung könnte hier bei Bedarf hinzugefügt werden
    }
    return facts, nil
}
