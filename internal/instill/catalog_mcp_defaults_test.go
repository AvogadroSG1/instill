package instill

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestLoadCatalogMCPDefaults(t *testing.T) {
	for _, tc := range []struct {
		cell string
		want *bool
	}{{"", nil}, {" true ", new(true)}, {"false", new(false)}} {
		t.Run(tc.cell, func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, "mcp", "catalog.csv")
			writeTypedLibraryMarker(t, path, "name,transport,command,args,url,env,description,default_enabled\nprobe,stdio,probe,,,,note,"+tc.cell+"\n")
			entries, err := LoadCatalog(root, LibraryTypeMCP)
			requireNoError(t, err)
			if !reflect.DeepEqual(entries[0].DefaultEnabled, tc.want) {
				t.Fatalf("default = %v, want %v", entries[0].DefaultEnabled, tc.want)
			}
			requireNoError(t, WriteCatalog(root, LibraryTypeMCP, entries))
			roundtrip, err := LoadCatalog(root, LibraryTypeMCP)
			requireNoError(t, err)
			requireEqual(t, entries, roundtrip)
		})
	}
	root := t.TempDir()
	path := filepath.Join(root, "mcp", "catalog.csv")
	legacy := []byte("name,transport,command,args,url,env,description\nprobe,stdio,probe,,,,note\n")
	writeTypedLibraryMarker(t, path, string(legacy))
	entries, err := LoadCatalog(root, LibraryTypeMCP)
	requireNoError(t, err)
	if entries[0].DefaultEnabled != nil {
		t.Fatal("legacy catalog acquired a default")
	}
	got, err := os.ReadFile(path)
	requireNoError(t, err)
	if !bytes.Equal(got, legacy) {
		t.Fatal("read migrated catalog")
	}
	for _, cell := range []string{"TRUE", "0", "nil", "yes"} {
		invalid := []byte("name,transport,command,args,url,env,description,default_enabled\nprobe,stdio,probe,,,,," + cell + "\n")
		requireNoError(t, os.WriteFile(path, invalid, 0600))
		_, err = LoadCatalog(root, LibraryTypeMCP)
		if err == nil || !strings.Contains(err.Error(), "default_enabled must be true, false, or empty") {
			t.Fatalf("invalid %q: %v", cell, err)
		}
		got, err = os.ReadFile(path)
		requireNoError(t, err)
		if !bytes.Equal(got, invalid) {
			t.Fatal("invalid read wrote file")
		}
	}
}

func TestWriteCatalogMCPDefaultScanPrecedence(t *testing.T) {
	root := t.TempDir()
	entry := CatalogEntry{Type: LibraryTypeMCP, Name: "probe", Transport: "stdio", Command: "probe", DefaultEnabled: new(false)}
	requireNoError(t, AddCatalogEntry(root, entry))
	requireNoError(t, ScanLibraryType(root, LibraryTypeMCP, nil))
	entries, err := LoadCatalog(root, LibraryTypeMCP)
	requireNoError(t, err)
	if len(entries) != 1 || entries[0].DefaultEnabled == nil || *entries[0].DefaultEnabled {
		t.Fatalf("scan = %#v", entries)
	}
	entries[0].DefaultEnabled = new(true)
	requireNoError(t, WriteCatalog(root, LibraryTypeMCP, entries))
	marker := filepath.Join(root, "mcp", "probe", "config.json")
	original, err := os.ReadFile(marker)
	requireNoError(t, err)
	requireNoError(t, AddCatalogEntry(root, entry))
	got, err := os.ReadFile(marker)
	requireNoError(t, err)
	if !bytes.Equal(original, got) {
		t.Fatal("add overwrote marker")
	}
	// Curated policy overrides the marker, while an unspecified row adopts it.
	requireNoError(t, WriteCatalog(root, LibraryTypeMCP, entries))
	requireNoError(t, ScanLibraryType(root, LibraryTypeMCP, nil))
	entries, err = LoadCatalog(root, LibraryTypeMCP)
	requireNoError(t, err)
	if entries[0].DefaultEnabled == nil || !*entries[0].DefaultEnabled {
		t.Fatal("marker overrode curated true")
	}
	entries[0].DefaultEnabled = nil
	requireNoError(t, WriteCatalog(root, LibraryTypeMCP, entries))
	requireNoError(t, ScanLibraryType(root, LibraryTypeMCP, nil))
	entries, err = LoadCatalog(root, LibraryTypeMCP)
	requireNoError(t, err)
	if entries[0].DefaultEnabled == nil || *entries[0].DefaultEnabled {
		t.Fatal("unspecified row did not adopt marker false")
	}
	for _, typ := range []LibraryType{LibraryTypeSkill, LibraryTypePlugin, LibraryTypeInstruction, LibraryTypePrompt} {
		entry.Type = typ
		entry.Path = "probe/marker"
		err = AddCatalogEntry(t.TempDir(), entry)
		if err == nil || !strings.Contains(err.Error(), "default_enabled is only supported for mcp") {
			t.Fatalf("%s: %v", typ, err)
		}
	}
}
