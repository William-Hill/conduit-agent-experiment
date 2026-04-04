package ingest

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strings"
)

// Symbol represents a single extracted Go symbol.
type Symbol struct {
	Name      string `json:"name"`
	Kind      string `json:"kind"`      // "func", "type", "interface", "method", "const", "var"
	Package   string `json:"package"`
	File      string `json:"file"`      // relative path
	Line      int    `json:"line"`
	Signature string `json:"signature"`
	Doc       string `json:"doc"`
	Exported  bool   `json:"exported"`
	Receiver  string `json:"receiver"`  // for methods only
}

// SymbolIndex is a collection of symbols with lookup maps.
type SymbolIndex struct {
	Symbols   []Symbol            `json:"symbols"`
	ByPackage map[string][]Symbol `json:"by_package"`
	ByKind    map[string][]Symbol `json:"by_kind"`
	ByFile    map[string][]Symbol `json:"by_file"`
}

// IndexOption configures BuildSymbolIndex behavior.
type IndexOption func(*indexConfig)

type indexConfig struct {
	includeTests bool
}

// WithTests includes _test.go files in the symbol index.
func WithTests() IndexOption {
	return func(c *indexConfig) { c.includeTests = true }
}

// ExtractSymbols parses a single Go file and returns all symbols found.
// The File field is not populated — the caller should set it.
func ExtractSymbols(filePath string) ([]Symbol, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, filePath, nil, parser.ParseComments)
	if err != nil {
		return nil, err
	}

	pkg := file.Name.Name
	var symbols []Symbol

	ast.Inspect(file, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.FuncDecl:
			s := Symbol{
				Name:     node.Name.Name,
				Package:  pkg,
				Line:     fset.Position(node.Pos()).Line,
				Exported: token.IsExported(node.Name.Name),
			}
			if node.Doc != nil {
				s.Doc = strings.TrimSpace(node.Doc.Text())
			}
			if node.Recv != nil && len(node.Recv.List) > 0 {
				s.Kind = "method"
				s.Receiver = receiverTypeName(node.Recv.List[0].Type)
				s.Signature = fmt.Sprintf("func (%s) %s(%s)%s",
					s.Receiver, s.Name,
					formatFieldList(node.Type.Params),
					formatResults(node.Type.Results))
			} else {
				s.Kind = "func"
				s.Signature = fmt.Sprintf("func %s(%s)%s",
					s.Name,
					formatFieldList(node.Type.Params),
					formatResults(node.Type.Results))
			}
			symbols = append(symbols, s)

		case *ast.GenDecl:
			switch node.Tok {
			case token.TYPE:
				for _, spec := range node.Specs {
					ts := spec.(*ast.TypeSpec)
					s := Symbol{
						Name:     ts.Name.Name,
						Package:  pkg,
						Line:     fset.Position(ts.Pos()).Line,
						Exported: token.IsExported(ts.Name.Name),
					}
					if ts.Doc != nil {
						s.Doc = strings.TrimSpace(ts.Doc.Text())
					} else if node.Doc != nil && len(node.Specs) == 1 {
						s.Doc = strings.TrimSpace(node.Doc.Text())
					}
					if _, ok := ts.Type.(*ast.InterfaceType); ok {
						s.Kind = "interface"
						s.Signature = fmt.Sprintf("type %s interface", ts.Name.Name)
					} else {
						s.Kind = "type"
						s.Signature = fmt.Sprintf("type %s %s", ts.Name.Name, exprString(ts.Type))
					}
					symbols = append(symbols, s)
				}
			case token.CONST, token.VAR:
				kind := "const"
				if node.Tok == token.VAR {
					kind = "var"
				}
				for _, spec := range node.Specs {
					vs := spec.(*ast.ValueSpec)
					for _, name := range vs.Names {
						s := Symbol{
							Name:     name.Name,
							Kind:     kind,
							Package:  pkg,
							Line:     fset.Position(name.Pos()).Line,
							Exported: token.IsExported(name.Name),
						}
						if vs.Doc != nil {
							s.Doc = strings.TrimSpace(vs.Doc.Text())
						} else if node.Doc != nil && len(node.Specs) == 1 {
							s.Doc = strings.TrimSpace(node.Doc.Text())
						}
						if vs.Type != nil {
							s.Signature = fmt.Sprintf("%s %s %s", kind, name.Name, exprString(vs.Type))
						} else {
							s.Signature = fmt.Sprintf("%s %s", kind, name.Name)
						}
						symbols = append(symbols, s)
					}
				}
			}
		}
		return true
	})

	return symbols, nil
}

func receiverTypeName(expr ast.Expr) string {
	switch t := expr.(type) {
	case *ast.StarExpr:
		if ident, ok := t.X.(*ast.Ident); ok {
			return ident.Name
		}
	case *ast.Ident:
		return t.Name
	}
	return ""
}

func formatFieldList(fl *ast.FieldList) string {
	if fl == nil || len(fl.List) == 0 {
		return ""
	}
	var parts []string
	for _, field := range fl.List {
		typeStr := exprString(field.Type)
		if len(field.Names) == 0 {
			parts = append(parts, typeStr)
		} else {
			for _, name := range field.Names {
				parts = append(parts, name.Name+" "+typeStr)
			}
		}
	}
	return strings.Join(parts, ", ")
}

func formatResults(fl *ast.FieldList) string {
	if fl == nil || len(fl.List) == 0 {
		return ""
	}
	rendered := formatFieldList(fl)
	if len(fl.List) == 1 && len(fl.List[0].Names) == 0 {
		return " " + rendered
	}
	return " (" + rendered + ")"
}

func exprString(expr ast.Expr) string {
	switch t := expr.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.StarExpr:
		return "*" + exprString(t.X)
	case *ast.SelectorExpr:
		return exprString(t.X) + "." + t.Sel.Name
	case *ast.ArrayType:
		if t.Len == nil {
			return "[]" + exprString(t.Elt)
		}
		return "[...]" + exprString(t.Elt)
	case *ast.MapType:
		return "map[" + exprString(t.Key) + "]" + exprString(t.Value)
	case *ast.InterfaceType:
		return "interface{}"
	case *ast.FuncType:
		return "func(" + formatFieldList(t.Params) + ")" + formatResults(t.Results)
	case *ast.Ellipsis:
		return "..." + exprString(t.Elt)
	case *ast.ChanType:
		switch t.Dir {
		case ast.SEND:
			return "chan<- " + exprString(t.Value)
		case ast.RECV:
			return "<-chan " + exprString(t.Value)
		default:
			return "chan " + exprString(t.Value)
		}
	case *ast.StructType:
		return "struct{}"
	}
	return "unknown"
}

// BuildSymbolIndex walks a repository and extracts symbols from all Go files.
// By default, _test.go files are excluded. Use WithTests() to include them.
func BuildSymbolIndex(repoPath string, opts ...IndexOption) (*SymbolIndex, error) {
	cfg := &indexConfig{}
	for _, opt := range opts {
		opt(cfg)
	}

	var allSymbols []Symbol

	err := filepath.WalkDir(repoPath, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if d.IsDir() {
			if skipDir(d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}

		if filepath.Ext(path) != ".go" {
			return nil
		}

		if !cfg.includeTests && strings.HasSuffix(d.Name(), "_test.go") {
			return nil
		}

		relPath, err := filepath.Rel(repoPath, path)
		if err != nil {
			return nil
		}

		symbols, err := ExtractSymbols(path)
		if err != nil {
			// Skip files that fail to parse.
			return nil
		}

		for i := range symbols {
			symbols[i].File = relPath
		}
		allSymbols = append(allSymbols, symbols...)
		return nil
	})
	if err != nil {
		return nil, err
	}

	idx := &SymbolIndex{
		Symbols:   allSymbols,
		ByPackage: make(map[string][]Symbol),
		ByKind:    make(map[string][]Symbol),
		ByFile:    make(map[string][]Symbol),
	}
	for _, s := range allSymbols {
		idx.ByPackage[s.Package] = append(idx.ByPackage[s.Package], s)
		idx.ByKind[s.Kind] = append(idx.ByKind[s.Kind], s)
		idx.ByFile[s.File] = append(idx.ByFile[s.File], s)
	}

	return idx, nil
}
