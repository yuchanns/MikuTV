package tvui

import (
	"archive/zip"
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/yuchanns/MikuTV/internal/app"
	"github.com/yuchanns/MikuTV/internal/plugin"
	"github.com/yuchanns/MikuTV/internal/provider"
	"github.com/yuchanns/MikuTV/internal/theme"
)

func parseGoModRequires(raw string) map[string]struct {
	version  string
	indirect bool
} {
	out := make(map[string]struct {
		version  string
		indirect bool
	})
	inBlock := false
	for _, line := range strings.Split(raw, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "//") {
			continue
		}
		if strings.HasPrefix(trimmed, "require (") {
			inBlock = true
			continue
		}
		if inBlock && trimmed == ")" {
			inBlock = false
			continue
		}
		entry := trimmed
		if !inBlock {
			if !strings.HasPrefix(trimmed, "require ") {
				continue
			}
			entry = strings.TrimSpace(strings.TrimPrefix(trimmed, "require "))
		}
		indirect := strings.Contains(entry, "// indirect")
		if idx := strings.Index(entry, "//"); idx >= 0 {
			entry = strings.TrimSpace(entry[:idx])
		}
		fields := strings.Fields(entry)
		if len(fields) >= 2 {
			out[fields[0]] = struct {
				version  string
				indirect bool
			}{version: fields[1], indirect: indirect}
		}
	}
	return out
}

func TestDynamicLicensesGenerationAndVersionAlignment(t *testing.T) {
	repoRoot := filepath.Join("..", "..")
	assets, err := LoadAssets()
	if err != nil {
		t.Fatalf("LoadAssets: %v", err)
	}
	// Static LICENSE.txt should cover MikuTV + non-Go assets (Lucide, Brand artwork) without duplicating Go modules.
	for _, required := range []string{"Apache License", "Lucide", "Brand artwork"} {
		if !strings.Contains(assets.License, required) {
			t.Errorf("embedded LICENSE.txt missing %q", required)
		}
	}
	if _, err := os.Stat(filepath.Join("assets", "dependencies.json")); !os.IsNotExist(err) {
		t.Fatalf("static assets/dependencies.json should not exist; Go dependency licenses are generated at build time")
	}

	// Verify scripts/tvos.py APP_VERSION stays aligned with main.go default appVersion.
	tvosPy, err := os.ReadFile(filepath.Join(repoRoot, "scripts", "tvos.py"))
	if err != nil {
		t.Fatalf("read scripts/tvos.py: %v", err)
	}
	mainGo, err := os.ReadFile(filepath.Join(repoRoot, "main.go"))
	if err != nil {
		t.Fatalf("read main.go: %v", err)
	}
	rePy := regexp.MustCompile(`APP_VERSION\s*=\s*"([^"]+)"`)
	reGo := regexp.MustCompile(`appVersion\s*=\s*"([^"]+)"`)
	mPy := rePy.FindSubmatch(tvosPy)
	mGo := reGo.FindSubmatch(mainGo)
	if len(mPy) != 2 || len(mGo) != 2 {
		t.Fatalf("could not extract version constants from scripts/tvos.py / main.go")
	}
	if string(mPy[1]) != string(mGo[1]) {
		t.Fatalf("version mismatch: scripts/tvos.py=%q, main.go=%q", mPy[1], mGo[1])
	}

	// Verify dynamic build-time generator collects all Go modules & verbatim vendor LICENSE texts.
	if _, err := os.Stat(filepath.Join(repoRoot, "vendor", "modules.txt")); os.IsNotExist(err) {
		t.Skip("vendor directory not prepared")
	}
	cmd := exec.Command("python3", filepath.Join(repoRoot, "scripts", "tvos.py"), "licenses")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("scripts/tvos.py licenses failed: %v\n%s", err, out)
	}
	deps, err := app.DecodeBundledDependencies(out)
	if err != nil {
		t.Fatalf("DecodeBundledDependencies: %v", err)
	}

	goModRaw, err := os.ReadFile(filepath.Join(repoRoot, "go.mod"))
	if err != nil {
		t.Fatalf("read go.mod: %v", err)
	}
	want := parseGoModRequires(string(goModRaw))

	got := make(map[string]app.AboutDependency, len(deps))
	for _, dep := range deps {
		got[dep.Module] = dep
	}
	if len(got) != len(want) {
		t.Fatalf("generated dependency count = %d, go.mod require count = %d", len(got), len(want))
	}
	for modPath, req := range want {
		dep, ok := got[modPath]
		if !ok {
			t.Errorf("go.mod module %q missing from dynamically generated licenses", modPath)
			continue
		}
		if dep.Version != req.version {
			t.Errorf("module %q version = %q, want %q from go.mod", modPath, dep.Version, req.version)
		}
		if dep.Indirect != req.indirect {
			t.Errorf("module %q indirect = %t, want %t from go.mod", modPath, dep.Indirect, req.indirect)
		}
		if dep.License == "" {
			t.Errorf("module %q missing detected SPDX license identifier", modPath)
		}
		if dep.LicenseText == "" {
			t.Errorf("module %q missing verbatim license text", modPath)
		}
	}

	// Verify composed license document includes every dynamically collected module and deduplicates identical notices cleanly.
	doc := buildLicenseDocument(app.AboutInfo{
		Version:      "0.1.0",
		Build:        "286",
		Commit:       "abc123def456",
		Dependencies: deps,
	}.Normalized(""), assets.License, assets.Text.About)
	for modPath := range want {
		if !strings.Contains(doc, modPath) {
			t.Errorf("composed document missing module %q", modPath)
		}
	}
	if count := strings.Count(doc, "Copyright 2009 The Go Authors."); count != 1 {
		t.Errorf("expected identical golang.org/x/* BSD-3-Clause notice to be deduplicated once, got %d", count)
	}
	if !strings.Contains(doc, "Apache-2.0 (included above).") {
		t.Errorf("expected identical Apache-2.0 dependency text to reference base license cleanly")
	}
	if !strings.Contains(doc, "Copyright 2021 The go-text authors") || !strings.Contains(doc, "Copyright (c) 2014 Tom Harwood") {
		t.Errorf("composed document missing verbatim dependency license notices")
	}
}

func TestPluginLicenseExtractionAndAboutPageAndModals(t *testing.T) {
	// Verify plugin archive extracts optional LICENSE file + manifest license field.
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	manifestJSON := `{
		"id": "demo-src",
		"name": "Demo Source",
		"kind": "source",
		"version": "1.2.3",
		"apiVersion": 1,
		"minAppVersion": "0.1.0",
		"license": "MIT",
		"main": "index.js",
		"capabilities": ["catalog.browse"]
	}`
	addFile := func(name, body string) {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	addFile("plugin.json", manifestJSON)
	addFile("index.js", `export function browse() { return { items: [] }; }`)
	addFile("LICENSE", "MIT License\nCopyright (c) Demo Author")
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	src, err := plugin.Open(t.Context(), buf.Bytes(), "0.1.0")
	if err != nil {
		t.Fatalf("plugin.Open: %v", err)
	}
	if got := src.Manifest().License; got != "MIT" {
		t.Fatalf("manifest license = %q, want MIT", got)
	}
	if !strings.Contains(src.LicenseText(), "Copyright (c) Demo Author") {
		t.Fatalf("plugin license text = %q", src.LicenseText())
	}

	assets, err := LoadAssets()
	if err != nil {
		t.Fatal(err)
	}
	pages, err := NewPages(assets)
	if err != nil {
		t.Fatal(err)
	}
	entry := app.Entry{
		ID: "demo-src", SourceID: "demo-src", Name: "Demo Source",
		Capabilities: provider.Capabilities{provider.BrowseCatalog},
	}
	state, err := app.NewState([]app.Entry{entry}, entry.ID, pages.Routes)
	if err != nil {
		t.Fatal(err)
	}
	about := app.AboutInfo{
		Version: "0.1.0",
		Build:   "286",
		Commit:  "abc123def456",
		Plugins: []app.AboutPlugin{{
			ID:           src.Manifest().ID,
			Name:         src.Manifest().Name,
			Kind:         string(src.Manifest().Kind),
			Version:      src.Manifest().Version,
			Bundled:      false,
			SourceCommit: "deadbeef1234567890",
			License:      src.Manifest().License,
			LicenseText:  src.LicenseText(),
		}},
		Dependencies: []app.AboutDependency{
			{Module: "github.com/example/alpha", Version: "v1.0.0", License: "MIT", LicenseText: "MIT License\nCopyright (c) Alpha"},
			{Module: "github.com/example/beta", Version: "v2.0.0", License: "BSD-3-Clause", Indirect: true, LicenseText: "BSD-3-Clause\nCopyright (c) Beta"},
		},
	}
	view := newTestView(t, Options{
		State: state, Pages: pages, Assets: assets, Theme: theme.Default(), About: about,
	})
	tester := ui.NewTester(view.Render, 1920, 1080)

	// 1. Navigate to Settings -> About and verify adaptive brand marks, version, build, plugin summary, and action buttons.
	if assets.Brand == nil || assets.BrandLight == nil || assets.Brand == assets.BrandLight {
		t.Fatal("LoadAssets did not decode distinct dark and light frameless brand marks")
	}
	if assets.BrandMark(true) != assets.Brand || assets.BrandMark(false) != assets.BrandLight {
		t.Fatal("BrandMark did not select matching dark/light brand variant")
	}
	state.Navigate(app.Route{Page: app.Settings, Section: "about"})
	tester.Frame()
	if !tester.HasText("0.1.0 (286)") {
		t.Fatal("About page did not render actual version with build number")
	}
	if !tester.HasText("Demo Source v1.2.3") {
		t.Fatal("About page did not render loaded plugin summary")
	}
	if view.focus.item(app.ShowLicenses) == nil || view.focus.item(app.ShowPrivacyDetails) == nil || view.focus.item(app.ShowProjectHome) == nil {
		t.Fatal("About page missing one of Licenses / Privacy / Project action buttons")
	}
	// Verify light appearance renders About page cleanly with light brand variant.
	view.Theme = theme.DefaultLight()
	tester.Frame()
	if !tester.HasText("MikuTV") {
		t.Fatal("About page did not render cleanly under light appearance")
	}
	view.Theme = theme.Default()
	tester.Frame()

	// 2. Open Open Source Licenses drawer and verify build, plugin, plugin LICENSE text, and dynamic dependencies.
	doc := buildLicenseDocument(about.Normalized(""), assets.License, assets.Text.About)
	for _, needle := range []string{
		"MikuTV 0.1.0 (286)",
		"Commit: abc123def456",
		"Demo Source (demo-src) v1.2.3",
		"Copyright (c) Demo Author",
		"github.com/example/alpha v1.0.0 (MIT)",
		"github.com/example/beta v2.0.0 (BSD-3-Clause) [indirect]",
		"Copyright (c) Alpha",
		"Copyright (c) Beta",
		"Apache License",
	} {
		if !strings.Contains(doc, needle) {
			t.Errorf("composed license document missing %q", needle)
		}
	}

	state.Focus = app.ShowLicenses
	view.reveal = true
	tester.Frame()
	tester.Key(0, ui.KeyEnter)
	tester.Frame()
	if state.Page != app.Licenses || state.Focus != app.LicenseText {
		t.Fatalf("expected Licenses drawer focused on LicenseText, got page=%s focus=%q", state.Page, state.Focus)
	}
	tester.Key(0, ui.KeyDown)
	tester.Frame()
	if view.activeScroll == nil || view.activeScroll.Y <= 0 {
		t.Fatal("Down key did not scroll license document")
	}
	tester.Key(0, ui.KeyEscape)
	tester.Frame()
	if state.Page != app.Settings || state.Focus != app.ShowLicenses {
		t.Fatalf("Escape did not restore ShowLicenses focus, got page=%s focus=%q", state.Page, state.Focus)
	}

	// 3. Open Project Home frameless QR modal and dismiss cleanly.
	state.Focus = app.ShowProjectHome
	view.reveal = true
	tester.Frame()
	tester.Key(0, ui.KeyEnter)
	tester.Frame()
	if state.Page != app.AboutProject || state.Focus != app.CloseModal {
		t.Fatalf("expected AboutProject modal focused on CloseModal, got page=%s focus=%q", state.Page, state.Focus)
	}
	if !tester.HasText(app.DefaultAppProjectURL) {
		t.Fatal("Project Home modal missing repository URL")
	}
	tester.Key(0, ui.KeyEnter)
	tester.Frame()
	if state.Page != app.Settings || state.Focus != app.ShowProjectHome {
		t.Fatalf("Closing Project Home did not restore focus, got page=%s focus=%q", state.Page, state.Focus)
	}
}
