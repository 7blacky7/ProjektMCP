package parser

import (
    "go/ast"
    "go/parser"
    gop "go/token"
    "strings"
)

type GoParser struct{}

func (GoParser) Parse(path string, data []byte) (Facts, error) {
    fset := gop.NewFileSet()
    file, err := parser.ParseFile(fset, path, data, parser.ParseComments)
    if err != nil { return Facts{}, err }
    facts := Facts{}

    // Importe -> Abhängigkeiten
    for _, imp := range file.Imports {
        v := strings.Trim(imp.Path.Value, "\"")
        facts.Deps = append(facts.Deps, Dep{Name: v, Line: fset.Position(imp.Pos()).Line})
    }

    // Kommentare
    if file.Comments != nil {
        for _, cg := range file.Comments {
            for _, c := range cg.List {
                line := fset.Position(c.Pos()).Line
                txt := strings.TrimPrefix(c.Text, "//")
                txt = strings.Trim(txt, "/* ")
                facts.Comments = append(facts.Comments, Comment{Line: line, Text: txt})
            }
        }
    }

    // Deklarationen -> Funktionen, Variablen, Konstanten
    for _, decl := range file.Decls {
        switch d := decl.(type) {
        case *ast.FuncDecl:
            name := d.Name.Name
            ls := fset.Position(d.Pos()).Line
            le := fset.Position(d.End()).Line
            meta := map[string]any{}
            if d.Recv != nil && len(d.Recv.List) > 0 {
                // Empfänger-Typ
                meta["recv"] = exprString(d.Recv.List[0].Type)
            }
            facts.Symbols = append(facts.Symbols, Symbol{Kind: "function", Name: name, LineStart: ls, LineEnd: le, Meta: meta})
        case *ast.GenDecl:
            switch d.Tok {
            case gop.VAR:
                for _, spec := range d.Specs {
                    vs := spec.(*ast.ValueSpec)
                    for _, nm := range vs.Names {
                        ls := fset.Position(nm.Pos()).Line
                        facts.Symbols = append(facts.Symbols, Symbol{Kind:"var", Name:nm.Name, LineStart: ls, LineEnd: ls})
                    }
                }
            case gop.CONST:
                for _, spec := range d.Specs {
                    vs := spec.(*ast.ValueSpec)
                    for _, nm := range vs.Names {
                        ls := fset.Position(nm.Pos()).Line
                        facts.Symbols = append(facts.Symbols, Symbol{Kind:"const", Name:nm.Name, LineStart: ls, LineEnd: ls})
                    }
                }
            }
        }
    }

    // Zeichenketten (einfacher Literal-Durchgang)
    ast.Inspect(file, func(n ast.Node) bool {
        bl, ok := n.(*ast.BasicLit)
        if !ok || bl.Kind != gop.STRING { return true }
        line := fset.Position(bl.Pos()).Line
        s := strings.Trim(bl.Value, "\"")
        facts.Strings = append(facts.Strings, Str{Line: line, Text: s})
        return true
    })

    // API-Endpunkte (net/http Mux-Stil Heuristiken)
    ast.Inspect(file, func(n ast.Node) bool {
        call, ok := n.(*ast.CallExpr); if !ok { return true }
        switch fun := call.Fun.(type) {
        case *ast.SelectorExpr:
            name := fun.Sel.Name
            if name == "HandleFunc" && len(call.Args) >= 2 {
                // http.HandleFunc(Muster, Handler)
                line := fset.Position(call.Pos()).Line
                route := argString(call.Args[0])
                facts.APIs = append(facts.APIs, APIEndpoint{Framework:"net/http", Method:"*", Route: route, Line: line})
            } else if name == "Methods" || name == "Handle" || name == "HandleFunc" {
                // rudimentäre Mux-Muster
                line := fset.Position(call.Pos()).Line
                route := ""
                if len(call.Args) > 0 { route = argString(call.Args[0]) }
                facts.APIs = append(facts.APIs, APIEndpoint{Framework:"gorilla/mux", Method:name, Route: route, Line: line})
            }
        }
        return true
    })

    return facts, nil
}

func exprString(e ast.Expr) string {
    switch t := e.(type) {
    case *ast.Ident: return t.Name
    case *ast.StarExpr: return "*"+exprString(t.X)
    case *ast.SelectorExpr: return exprString(t.X)+"."+t.Sel.Name
    default: return ""
    }
}

func argString(e ast.Expr) string {
    switch v := e.(type) {
    case *ast.BasicLit:
        return strings.Trim(v.Value, "\"")
    default:
        return ""
    }
}

