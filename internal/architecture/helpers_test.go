package architecture_test

import (
	"encoding/json"
	"go/parser"
	"go/token"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

const modulePath = "github.com/touken928/wirehub"

type goPackage struct {
	ImportPath string
	Imports    []string
}

type edgeSet map[string]map[string]bool

func productionHTTPRepoImports() ([]string, error) {
	_, sourceFile, _, ok := runtime.Caller(0)
	if !ok {
		return nil, os.ErrNotExist
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(sourceFile), "../.."))
	var violations []string
	err := filepath.Walk(filepath.Join(root, "internal"), func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || strings.HasSuffix(path, "_test.go") || filepath.Ext(path) != ".go" {
			return nil
		}
		file, parseErr := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if parseErr != nil {
			return parseErr
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		for _, imported := range file.Imports {
			importPath := strings.Trim(imported.Path.Value, `"`)
			if importPath != modulePath+"/internal/repo" && !strings.HasPrefix(importPath, modulePath+"/internal/repo/") {
				continue
			}
			if strings.HasPrefix(rel, "internal/api/http/handlers/") || strings.HasPrefix(rel, "internal/api/http/dto/") {
				violations = append(violations, rel+" imports repo outside the production HTTP boundary")
			}
		}
		return nil
	})
	return violations, err
}

func addEdge(edges map[string]map[string]bool, source, target string) {
	if edges[source] == nil {
		edges[source] = make(map[string]bool)
	}
	edges[source][target] = true
}

func listInternalPackages(t *testing.T) []goPackage {
	t.Helper()
	_, sourceFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed while locating the module root")
	}

	cmd := exec.Command("go", "list", "-json", "./internal/...")
	cmd.Dir = filepath.Clean(filepath.Join(filepath.Dir(sourceFile), "../.."))
	output, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			t.Fatalf("go list failed: %v\n%s", err, exitErr.Stderr)
		}
		t.Fatalf("go list failed: %v", err)
	}

	decoder := json.NewDecoder(strings.NewReader(string(output)))
	var packages []goPackage
	for {
		var pkg goPackage
		err := decoder.Decode(&pkg)
		if err != nil {
			if err == io.EOF {
				break
			}
			t.Fatalf("decode go list output: %v", err)
		}
		if strings.HasPrefix(pkg.ImportPath, modulePath+"/internal/") {
			packages = append(packages, pkg)
		}
	}
	return packages
}

func under(importPath, parent string) bool {
	return importPath == parent || strings.HasPrefix(importPath, parent+"/")
}
