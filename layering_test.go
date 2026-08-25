// layering_test.go enforces the architectural dependency direction:
//
//	cli → commands → protocol ← transport
//
// protocol must never import transport/commands/cli; commands must never
// import transport or cli; transport must never import cli/commands. This is
// the contract that keeps the protocol layer a clean translation unit.
package lint_test

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// allowed maps each internal package to the internal packages it may import.
var allowed = map[string]map[string]bool{
	"cli":       {"cli": true, "commands": true, "options": true, "protocol": true, "transport": true},
	"commands":  {"commands": true, "options": true, "protocol": true},
	"options":   {"options": true},
	"protocol":  {"protocol": true},
	"transport": {"transport": true, "protocol": true},
}

func TestLayering(t *testing.T) {
	fset := token.NewFileSet()
	err := filepath.WalkDir("internal", func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return err
		}
		pkg := filepath.Base(filepath.Dir(path))
		rules, known := allowed[pkg]
		if !known {
			t.Errorf("unknown package dir %s — update the layering map", path)
			return nil
		}
		f, perr := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if perr != nil {
			t.Errorf("parse %s: %v", path, perr)
			return nil
		}
		for _, imp := range f.Imports {
			v := strings.Trim(imp.Path.Value, `"`)
			if !strings.HasPrefix(v, modulePath) {
				continue // stdlib / external — fine
			}
			dep := v[len(modulePath)+len("/internal/"):]
			if !rules[dep] {
				t.Errorf("%s imports internal/%s — violates layering (cli → commands → protocol ← transport)", path, dep)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

const modulePath = "github.com/Kayungko/unity-pipe"
