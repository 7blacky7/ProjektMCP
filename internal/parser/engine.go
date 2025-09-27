package parser

import (
    "os"
    "path/filepath"
)

type Parser interface {
    Parse(path string, data []byte) (Facts, error)
}

type Engine struct {
    byExt map[string]Parser
}

func NewEngine() *Engine {
    return &Engine{byExt: make(map[string]Parser)}
}

func (e *Engine) Register(ext string, p Parser) { e.byExt[ext] = p }

func (e *Engine) ParseFile(path string) (Facts, error) {
    b, err := os.ReadFile(path)
    if err != nil { return Facts{}, err }
    ext := filepath.Ext(path)
    if p, ok := e.byExt[ext]; ok {
        return p.Parse(path, b)
    }
    return Facts{}, nil
}
