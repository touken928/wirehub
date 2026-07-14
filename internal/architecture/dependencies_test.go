package architecture_test

import (
	"fmt"
	"sort"
	"strings"
	"testing"
)

// Production HTTP handlers and DTOs consume service contracts exclusively.
var progressiveHandlerRepoAllowlist = map[string]map[string]bool{
	modulePath + "/internal/api/http/handlers": {},
}

func TestLayering(t *testing.T) {
	packages := listInternalPackages(t)
	var violations []string
	observed := map[string]edgeSet{
		"handler-repo": make(edgeSet),
		"auth-repo":    make(edgeSet),
		"domain-vpn":   make(edgeSet),
		"service-vpn":  make(edgeSet),
		"vpn-service":  make(edgeSet),
	}

	for _, pkg := range packages {
		for _, imported := range pkg.Imports {
			switch {
			case under(pkg.ImportPath, modulePath+"/internal/api/http/handlers") && under(imported, modulePath+"/internal/repo"):
				addEdge(observed["handler-repo"], pkg.ImportPath, imported)
			case under(pkg.ImportPath, modulePath+"/internal/api/http/auth") && under(imported, modulePath+"/internal/repo"):
				addEdge(observed["auth-repo"], pkg.ImportPath, imported)
			case under(pkg.ImportPath, modulePath+"/internal/domain") && under(imported, modulePath+"/internal/vpn"):
				addEdge(observed["domain-vpn"], pkg.ImportPath, imported)
			case under(pkg.ImportPath, modulePath+"/internal/service") && under(imported, modulePath+"/internal/vpn"):
				addEdge(observed["service-vpn"], pkg.ImportPath, imported)
			case under(pkg.ImportPath, modulePath+"/internal/vpn") && under(imported, modulePath+"/internal/service"):
				addEdge(observed["vpn-service"], pkg.ImportPath, imported)
			}
		}
	}

	violations = append(violations, compareAllowlist("progressive handler-repo", observed["handler-repo"], progressiveHandlerRepoAllowlist)...)
	violations = append(violations, compareAllowlist("auth-repo", observed["auth-repo"], map[string]map[string]bool{})...)
	violations = append(violations, compareAllowlist("domain-vpn", observed["domain-vpn"], map[string]map[string]bool{})...)
	violations = append(violations, compareAllowlist("service-vpn", observed["service-vpn"], map[string]map[string]bool{})...)
	violations = append(violations, compareAllowlist("vpn-service", observed["vpn-service"], map[string]map[string]bool{})...)

	sort.Strings(violations)
	if len(violations) != 0 {
		t.Fatalf("dependency layering violations:\n- %s", strings.Join(violations, "\n- "))
	}
}

func TestProgressiveProductionHTTPRepoBoundary(t *testing.T) {
	violations, err := productionHTTPRepoImports()
	if err != nil {
		t.Fatal(err)
	}
	if len(violations) != 0 {
		t.Fatalf("handler repo boundary violations: %s", strings.Join(violations, ", "))
	}
}

func compareAllowlist(name string, observed, allowed map[string]map[string]bool) []string {
	var violations []string
	for source, targets := range observed {
		for target := range targets {
			if !allowed[source][target] {
				violations = append(violations, fmt.Sprintf("%s imports %s outside the progressive %s allowlist", source, target, name))
			}
		}
	}
	for source, targets := range allowed {
		for target := range targets {
			if !observed[source][target] {
				violations = append(violations, fmt.Sprintf("stale progressive %s allowlist entry: %s imports %s", name, source, target))
			}
		}
	}
	return violations
}
