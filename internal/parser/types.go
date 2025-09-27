package parser

type Symbol struct {
    Kind string // Funktion | Variable | Konstante
    Name string
    LineStart int
    LineEnd   int
    Meta map[string]any
}

type Dep struct {
    Name string
    Line int
    Meta map[string]any
}

type APIEndpoint struct {
    Framework string
    Method string
    Route  string
    Line   int
    Meta map[string]any
}

type Str struct {
    Line int
    Text string
    Meta map[string]any
}

type Comment struct {
    Line int
    Text string
    Meta map[string]any
}

type Refs struct {
    // optionale zukünftige Verwendung
}

type Facts struct {
    Symbols  []Symbol
    Deps     []Dep
    APIs     []APIEndpoint
    Strings  []Str
    Comments []Comment
}

