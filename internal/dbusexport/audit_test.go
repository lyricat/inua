package dbusexport

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// Inspect syntax rather than build-selected packages so platform-specific files
// and tests are covered too. Check method values as well as direct calls.
func TestNoBareExports(t *testing.T) {
	root := filepath.Join("..", "..")
	fset := token.NewFileSet()
	for _, dir := range []string{"internal", "cmd"} {
		err := filepath.WalkDir(filepath.Join(root, dir), func(name string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() || !strings.HasSuffix(name, ".go") {
				return nil
			}
			if name == filepath.Join(root, "internal", "dbusexport", "export.go") {
				return nil
			}
			file, err := parser.ParseFile(fset, name, nil, 0)
			if err != nil {
				return err
			}
			imports := make(map[string]string)
			for _, spec := range file.Imports {
				pkg, err := strconv.Unquote(spec.Path.Value)
				if err != nil {
					return err
				}
				alias := path.Base(pkg)
				if spec.Name != nil {
					alias = spec.Name.Name
				}
				imports[alias] = pkg
				if alias == "." && strings.HasPrefix(pkg, "github.com/godbus/dbus/") {
					t.Errorf("%s: use a named godbus import so exports can be audited", fset.Position(spec.Pos()))
				}
			}
			ast.Inspect(file, func(node ast.Node) bool {
				sel, ok := node.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				pkg := ""
				if id, ok := sel.X.(*ast.Ident); ok {
					pkg = imports[id.Name]
				}
				if sel.Sel.Name == "New" && pkg == "github.com/godbus/dbus/v5/prop" {
					t.Errorf("%s: prop.New exports without protection; use dbusexport.Properties", fset.Position(sel.Pos()))
				}
				switch sel.Sel.Name {
				case "Export", "ExportAll", "ExportWithMap", "ExportSubtree", "ExportSubtreeWithMap", "ExportMethodTable", "ExportSubtreeMethodTable":
					if pkg == "github.com/lyricat/inua/internal/dbusexport" {
						return true
					}
					// These are service-level functions, not godbus exports.
					if sel.Sel.Name == "Export" && (pkg == "github.com/lyricat/inua/internal/portal" || pkg == "github.com/lyricat/inua/internal/apps/gpg") {
						return true
					}
					t.Errorf("%s: bare %s; use internal/dbusexport", fset.Position(sel.Pos()), sel.Sel.Name)
				}
				return true
			})
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}
