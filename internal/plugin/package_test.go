package plugin

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/yuchanns/MikuTV/internal/provider"
)

const testVersion = "0.1.0"

func manifestForTest(t *testing.T, mutate func(*sourceManifest)) []byte {
	t.Helper()
	var manifest sourceManifest
	manifest.ID, manifest.Name, manifest.Kind = "fixture", "Fixture", SourceKind
	manifest.Version, manifest.MinAppVersion, manifest.APIVersion = testVersion, testVersion, 1
	manifest.Main = "index.js"
	manifest.Capabilities = provider.Capabilities{provider.BrowseCatalog}
	if mutate != nil {
		mutate(&manifest)
	}
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func zipFiles(t *testing.T, files map[string][]byte) []byte {
	t.Helper()
	var buffer bytes.Buffer
	archive := zip.NewWriter(&buffer)
	for name, data := range files {
		writer, err := archive.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := writer.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

func openForTest(t *testing.T, script string, mutate func(*sourceManifest)) *Source {
	t.Helper()
	data := zipFiles(t, map[string][]byte{"plugin.json": manifestForTest(t, mutate), "index.js": []byte(script)})
	source, err := Open(t.Context(), data, testVersion)
	if err != nil {
		t.Fatal(err)
	}
	return source
}

func TestPackageValidation(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*sourceManifest)
	}{
		{"missing kind", func(m *sourceManifest) { m.Kind = "" }},
		{"unknown kind", func(m *sourceManifest) { m.Kind = "other" }},
		{"theme", func(m *sourceManifest) { m.Kind = ThemeKind }},
		{"future host", func(m *sourceManifest) { m.MinAppVersion = "2.0.0" }},
		{"future protocol", func(m *sourceManifest) { m.APIVersion = 2 }},
		{"authentication", func(m *sourceManifest) { m.Authentication.Methods = []string{"password"} }},
		{"unknown operation", func(m *sourceManifest) { m.Capabilities = provider.Capabilities{"catalog.unknown"} }},
		{"missing export", func(m *sourceManifest) { m.Capabilities = append(m.Capabilities, provider.GetCatalogItem) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data := zipFiles(t, map[string][]byte{
				"plugin.json": manifestForTest(t, tc.change), "index.js": []byte("export function browse() {}"),
			})
			if _, err := Open(t.Context(), data, testVersion); !errors.Is(err, ErrIncompatible) {
				t.Fatalf("incompatible package was accepted: %v", err)
			}
		})
	}
	for _, name := range []string{"../escaped.js", "/absolute.js", `assets\copy.png`, "ASSETS/COVER.PNG"} {
		t.Run(name, func(t *testing.T) {
			data := zipFiles(t, map[string][]byte{
				"plugin.json": manifestForTest(t, nil), "index.js": []byte("export function browse() {}"),
				"assets/cover.png": []byte("broken image"), name: []byte("invalid package entry"),
			})
			if _, err := Open(t.Context(), data, testVersion); err == nil {
				t.Fatal("unsafe resource path was accepted")
			}
		})
	}
}

func TestCompileAndActivationAreSeparate(t *testing.T) {
	source := openForTest(t, "throw Error('evaluated'); export function browse() {}", nil)
	if err := source.Verify(t.Context()); err == nil {
		t.Fatal("activation did not observe the exception")
	}
	source = openForTest(t, "export const browse = 7", nil)
	if err := source.Verify(t.Context()); !errors.Is(err, ErrIncompatible) {
		t.Fatalf("non-callable export was accepted: %v", err)
	}
	data := zipFiles(t, map[string][]byte{
		"plugin.json": manifestForTest(t, func(m *sourceManifest) {
			m.Artwork.Cover = "assets/cover.png"
		}),
		"index.js":         []byte("export { browse } from './src/catalog.js'"),
		"src/catalog.js":   []byte("export function browse() { return { collections: [] }; }"),
		"assets/cover.png": []byte("damaged image"),
	})
	source, err := Open(t.Context(), data, testVersion)
	if err != nil {
		t.Fatal(err)
	}
	if len(source.Cover()) != 0 {
		t.Fatal("invalid cover did not fall back")
	}
	if err := source.Verify(t.Context()); err != nil {
		t.Fatal(err)
	}
	record := BundleRecord{ID: "fixture", Version: testVersion, AppVersion: testVersion, SHA256: fmt.Sprintf("%x", source.Digest())}
	index, err := json.Marshal([]BundleRecord{record})
	if err != nil {
		t.Fatal(err)
	}
	files := fstest.MapFS{"index.json": &fstest.MapFile{Data: index}, "fixture/plugin.zip": &fstest.MapFile{Data: data}}
	if result, failures := LoadBundled(context.Background(), files, testVersion); len(result) != 1 || len(failures) != 0 {
		t.Fatalf("bundle failed to activate: %v", failures)
	}
	files["fixture/plugin.zip"] = &fstest.MapFile{Data: append(bytes.Clone(data), 'x')}
	if result, failures := LoadBundled(context.Background(), files, testVersion); len(result) != 0 || len(failures) != 1 {
		t.Fatalf("bad digest did not exclude the package: %v", failures)
	}
}

func TestLoadBundledLoadsAllValidReceipts(t *testing.T) {
	const count = 33
	files := make(fstest.MapFS)
	var records []BundleRecord
	for i := range count {
		id := fmt.Sprintf("fixture-%d", i)
		data := zipFiles(t, map[string][]byte{
			"plugin.json": manifestForTest(t, func(m *sourceManifest) { m.ID = id }),
			"index.js":    []byte("export function browse() { return { collections: [] }; }"),
		})
		files[id+"/plugin.zip"] = &fstest.MapFile{Data: data}
		records = append(records, BundleRecord{
			ID: id, Version: testVersion, AppVersion: testVersion, SHA256: fmt.Sprintf("%x", sha256.Sum256(data)),
		})
	}
	index, err := json.Marshal(records)
	if err != nil {
		t.Fatal(err)
	}
	files["index.json"] = &fstest.MapFile{Data: index}
	sources, failures := LoadBundled(t.Context(), files, testVersion)
	if len(sources) != count || len(failures) != 0 {
		t.Fatalf("loaded %d of %d packages: %v", len(sources), count, failures)
	}
}

func TestPackageAllowsLongMetadataAndManyFiles(t *testing.T) {
	manifest := manifestForTest(t, func(m *sourceManifest) {
		m.Name = strings.Repeat("name", 50)
		m.Description = strings.Repeat("metadata ", 8000)
		m.Permissions.Network = []string{"media"}
		for i := range 40 {
			m.Permissions.Network = append(m.Permissions.Network, fmt.Sprintf("host%d.example", i))
		}
	})
	files := map[string][]byte{
		"plugin.json": manifest,
		"index.js":    []byte("//" + strings.Repeat("x", 1<<20) + "\nexport function browse() {}"),
	}
	for i := range 140 {
		files[fmt.Sprintf("assets/file-%d", i)] = []byte("asset")
	}
	source, err := Open(t.Context(), zipFiles(t, files), testVersion)
	if err != nil {
		t.Fatal(err)
	}
	if err := source.Verify(t.Context()); err != nil {
		t.Fatal(err)
	}
}
