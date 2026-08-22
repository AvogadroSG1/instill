package main

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

func main() {
	if len(os.Args) != 2 {
		fail("usage: manifest-semantics <apm.yml>")
	}
	data, err := os.ReadFile(os.Args[1]) //nolint:gosec // The BATS fixture path is supplied by the test.
	if err != nil {
		fail(err.Error())
	}
	var manifest map[string]any
	if err := yaml.Unmarshal(data, &manifest); err != nil {
		fail(err.Error())
	}

	flow := mapping(manifest["x-flow"], "x-flow")
	equal(flow["owner"], "user", "x-flow.owner")
	values := sequence(flow["values"], "x-flow.values")
	if len(values) != 2 || values[0] != "one" || values[1] != "two" {
		fail(fmt.Sprintf("x-flow.values = %#v", values))
	}

	dependencies := mapping(manifest["dependencies"], "dependencies")
	lsp := sequence(dependencies["lsp"], "dependencies.lsp")
	if len(lsp) != 1 {
		fail(fmt.Sprintf("dependencies.lsp = %#v", lsp))
	}
	lspEntry := mapping(lsp[0], "dependencies.lsp[0]")
	equal(lspEntry["name"], "gopls", "dependencies.lsp[0].name")
	equal(lspEntry["x-user"], true, "dependencies.lsp[0].x-user")

	apm := sequence(dependencies["apm"], "dependencies.apm")
	if !hasScalar(apm, "owner/remote#main") || !hasMapping(apm, "marketplace", "private", "name", "opaque") {
		fail(fmt.Sprintf("dependencies.apm lost opaque entries: %#v", apm))
	}
	mcp := sequence(dependencies["mcp"], "dependencies.mcp")
	if !hasScalar(mcp, "io.example/opaque") || !hasMapping(mcp, "x-custom", true, "", nil) {
		fail(fmt.Sprintf("dependencies.mcp lost opaque entries: %#v", mcp))
	}
}

func mapping(value any, path string) map[string]any {
	result, ok := value.(map[string]any)
	if !ok {
		fail(fmt.Sprintf("%s = %#v, want mapping", path, value))
	}
	return result
}

func sequence(value any, path string) []any {
	result, ok := value.([]any)
	if !ok {
		fail(fmt.Sprintf("%s = %#v, want sequence", path, value))
	}
	return result
}

func equal(got any, want any, path string) {
	if got != want {
		fail(fmt.Sprintf("%s = %#v, want %#v", path, got, want))
	}
}

func hasScalar(values []any, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func hasMapping(values []any, key1 string, value1 any, key2 string, value2 any) bool {
	for _, value := range values {
		entry, ok := value.(map[string]any)
		if !ok || entry[key1] != value1 {
			continue
		}
		if key2 == "" || entry[key2] == value2 {
			return true
		}
	}
	return false
}

func fail(message string) {
	_, _ = fmt.Fprintln(os.Stderr, message)
	os.Exit(1)
}
