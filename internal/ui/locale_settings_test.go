package tvui

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/yuchanns/MikuTV/internal/app"
	"github.com/yuchanns/MikuTV/internal/plugin"
	"github.com/yuchanns/MikuTV/internal/theme"
)

func TestLabelsJSONAndEnglishParity(t *testing.T) {
	zhData, err := files.ReadFile("assets/labels.json")
	if err != nil {
		t.Fatalf("read labels.json: %v", err)
	}
	enData, err := files.ReadFile("assets/labels.en.json")
	if err != nil {
		t.Fatalf("read labels.en.json: %v", err)
	}
	var zhMap, enMap map[string]any
	if err := json.Unmarshal(zhData, &zhMap); err != nil {
		t.Fatalf("decode labels.json: %v", err)
	}
	if err := json.Unmarshal(enData, &enMap); err != nil {
		t.Fatalf("decode labels.en.json: %v", err)
	}
	zhFlat := flattenJSONStrings("", zhMap)
	enFlat := flattenJSONStrings("", enMap)

	var missingInEN, missingInZH []string
	for k := range zhFlat {
		if _, ok := enFlat[k]; !ok {
			missingInEN = append(missingInEN, k)
		}
	}
	for k := range enFlat {
		if _, ok := zhFlat[k]; !ok {
			missingInZH = append(missingInZH, k)
		}
	}
	sort.Strings(missingInEN)
	sort.Strings(missingInZH)
	if len(missingInEN) > 0 {
		t.Fatalf("keys present in labels.json but missing in labels.en.json: %v", missingInEN)
	}
	if len(missingInZH) > 0 {
		t.Fatalf("keys present in labels.en.json but missing in labels.json: %v", missingInZH)
	}

	verbRe := regexp.MustCompile(`%(\[[0-9]+\])?[#0\- +]*[0-9]*(\.[0-9]*)?[vTtbcdoqxXUeEfFgGsp%]`)
	for k, zhVal := range zhFlat {
		enVal := enFlat[k]
		if strings.TrimSpace(zhVal) == "" {
			t.Errorf("empty zh value at %s", k)
		}
		if strings.TrimSpace(enVal) == "" {
			t.Errorf("empty en value at %s", k)
		}
		zhVerbs := verbRe.FindAllString(zhVal, -1)
		enVerbs := verbRe.FindAllString(enVal, -1)
		if strings.Join(zhVerbs, ",") != strings.Join(enVerbs, ",") {
			t.Errorf("format verb mismatch at %s: zh=%v (%q) en=%v (%q)", k, zhVerbs, zhVal, enVerbs, enVal)
		}
	}
}

func flattenJSONStrings(prefix string, v any) map[string]string {
	out := make(map[string]string)
	switch typed := v.(type) {
	case map[string]any:
		for k, val := range typed {
			next := k
			if prefix != "" {
				next = prefix + "." + k
			}
			for fk, fv := range flattenJSONStrings(next, val) {
				out[fk] = fv
			}
		}
	case []any:
		for i, val := range typed {
			next := fmt.Sprintf("%s[%d]", prefix, i)
			for fk, fv := range flattenJSONStrings(next, val) {
				out[fk] = fv
			}
		}
	case string:
		out[prefix] = typed
	}
	return out
}

func makeTestLocaleZip(t *testing.T, id, name, lang, nativeName, version string, labelOverrides map[string]any) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	add := func(path string, data []byte) {
		hdr := &zip.FileHeader{Name: path, Method: zip.Deflate}
		hdr.SetMode(0o644)
		w, err := zw.CreateHeader(hdr)
		if err != nil {
			t.Fatalf("zip create %s: %v", path, err)
		}
		if _, err := w.Write(data); err != nil {
			t.Fatalf("zip write %s: %v", path, err)
		}
	}
	manifest, _ := json.Marshal(map[string]any{
		"id":            id,
		"name":          name,
		"description":   nativeName + " UI localization.",
		"kind":          "locale",
		"version":       version,
		"apiVersion":    1,
		"minAppVersion": "0.1.0",
		"license":       "Apache-2.0",
		"language":      lang,
		"nativeName":    nativeName,
		"locale":        "locale.json",
	})
	localeBody, _ := json.Marshal(labelOverrides)
	add("plugin.json", manifest)
	add("locale.json", localeBody)
	add("LICENSE", []byte("Apache-2.0\n"))
	if err := zw.Close(); err != nil {
		t.Fatalf("zip close: %v", err)
	}
	return buf.Bytes()
}

func TestLanguagePickerLiveSwitchAndLocalePluginDiscoveryFlow(t *testing.T) {
	assets, err := LoadAssets()
	if err != nil {
		t.Fatalf("LoadAssets: %v", err)
	}
	pages, err := NewPages(assets)
	if err != nil {
		t.Fatalf("NewPages: %v", err)
	}

	jaZip := makeTestLocaleZip(t, "mikutv-locale-ja", "Japanese (日本語)", "ja", "日本語", "1.0.0", map[string]any{
		"SettingsTitle": "設定",
		"Nav": map[string]any{
			"home":     "ホーム",
			"settings": "設定",
		},
		"Sections": []map[string]any{
			{"ID": "appearance", "Name": "外観とモーション"},
		},
		"Appearance": map[string]any{
			"Title":               "外観とモーション",
			"LanguageLabel":       "表示言語",
			"LanguagePickerTitle": "言語を選択",
		},
	})
	sum := sha256.Sum256(jaZip)
	jaSHA := hex.EncodeToString(sum[:])

	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/search/repositories":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"total_count":1,"items":[{"full_name":"example/mikutv-locale-ja","description":"Japanese UI","pushed_at":"2026-11-23T00:00:00Z","archived":false}]}`))
		case "/repos/example/mikutv-locale-ja/releases":
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintf(w, `[{"id":901,"tag_name":"v1.0.0","draft":false,"prerelease":false,"published_at":"2026-11-23T00:00:00Z","assets":[{"id":902,"name":"plugin.zip","size":%d,"browser_download_url":"%s/downloads/ja.zip","digest":"sha256:%s"}]}]`, len(jaZip), server.URL, jaSHA)
		case "/downloads/ja.zip":
			w.Header().Set("Content-Type", "application/zip")
			_, _ = w.Write(jaZip)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	store := plugin.NewPackageStore(t.TempDir())
	var view *View
	var catalog *app.PluginCatalog
	queue := make(chan func(), 32)
	runOnMain := func(fn func()) { queue <- fn }
	drain := func(until func() bool) {
		t.Helper()
		for i := 0; i < 64 && !until(); i++ {
			fn := <-queue
			fn()
		}
		if !until() {
			t.Fatalf("condition not met while draining queue")
		}
	}

	localeSettings := app.NewLocaleSettings(
		store,
		nil,
		[]string{"zh-Hans-CN"},
		func(tag string, overrideJSON []byte) {
			if view != nil {
				if err := view.SetLocale(tag, overrideJSON); err != nil {
					t.Errorf("SetLocale(%s): %v", tag, err)
				}
			}
		},
		func(activeID string) {
			if catalog != nil {
				catalog.SyncActiveLocale(activeID)
			}
		},
		func() {},
	)

	catalog = app.NewPluginCatalog(nil, plugin.NewCatalogClient("0.1.0", server.URL), nil, runOnMain, func() {})
	catalog.SetInstaller(store, "0.1.0", func(pkg plugin.InstalledPackage) error {
		if pkg.Locale != nil {
			localeSettings.RegisterLocale(pkg.Locale)
			return nil
		}
		return plugin.ErrIncompatible
	})
	catalog.SetLifecycleHooks(app.PluginLifecycleHooks{
		EnableLocale: func(pluginID string) error {
			return localeSettings.Select(pluginID)
		},
		DisableLocale: func(pluginID string) {
			localeSettings.DisableLocale(pluginID)
		},
		RemoveLocale: func(pluginID string) {
			localeSettings.RemoveLocale(pluginID)
		},
	})

	state, err := app.NewState([]app.Entry{{ID: "demo", Name: "Demo"}}, "demo", pages.Routes)
	if err != nil {
		t.Fatalf("NewState: %v", err)
	}
	view = newTestView(t, Options{
		State: state, Pages: pages, Assets: assets, Theme: theme.Default(),
		Locale: localeSettings, PluginCatalog: catalog,
	})
	tester := ui.NewTester(view.Render, 1920, 1080)

	// 1. Open Settings -> Appearance and open LanguagePicker.
	state.Navigate(app.Route{Page: app.Settings, Section: "appearance"})
	state.Focus = appearanceLanguageRow
	tester.Frame()
	if assets.Text.SettingsTitle != "设置" {
		t.Fatalf("expected initial ZH settings title, got %q", assets.Text.SettingsTitle)
	}
	tester.Key(0, ui.KeyEnter)
	tester.Frame()
	if state.Page != app.LanguagePicker {
		t.Fatalf("expected LanguagePicker drawer, got %+v", state.Route)
	}

	// 2. Switch to built-in English -> live hot-swap updates sidebar, sections, and titles.
	state.Focus = languagePickerOptionID(app.LocaleBuiltinEN)
	tester.Frame()
	tester.Key(0, ui.KeyEnter)
	tester.Frame()
	if assets.Text.SettingsTitle != "Settings" || assets.LocaleTag != "en" {
		t.Fatalf("expected live EN switch, got title=%q tag=%q", assets.Text.SettingsTitle, assets.LocaleTag)
	}
	if pages.components[app.Route{Page: app.Settings, Section: "appearance"}].Title != "Appearance" {
		t.Fatalf("expected refreshed section title Appearance, got %q", pages.components[app.Route{Page: app.Settings, Section: "appearance"}].Title)
	}
	if pages.components[app.Route{Page: app.Home}].Navigation.Title != "For You" {
		t.Fatalf("expected refreshed rail nav For You, got %q", pages.components[app.Route{Page: app.Home}].Navigation.Title)
	}

	// 3. Reopen LanguagePicker -> [Discover More Languages] -> install Japanese locale plugin ->
	// auto-returns to LanguagePicker focused on mikutv-locale-ja -> select Japanese!
	view.openLanguagePicker("")
	tester.Frame()
	state.Focus = languagePickerDiscoverBtn
	tester.Frame()
	tester.Key(0, ui.KeyEnter)
	tester.Frame()
	if state.Section != "plugins" || view.pluginsUI.filter != plugin.LocaleKind || !view.pluginsUI.returnToLanguagePicker {
		t.Fatalf("expected Discover filtered to LocaleKind with returnToLanguagePicker=true, got section=%s filter=%s ret=%v",
			state.Section, view.pluginsUI.filter, view.pluginsUI.returnToLanguagePicker)
	}
	drain(func() bool { return catalog.Snapshot().Status == app.PluginCatalogReady })
	tester.Frame()

	state.Focus = pluginsCatalogFocus("example/mikutv-locale-ja")
	tester.Frame()
	tester.Key(0, ui.KeyEnter)
	tester.Frame()
	if view.pluginsUI.selectedCatalogRepo != "example/mikutv-locale-ja" {
		t.Fatalf("expected catalog detail for locale repo, got %q", view.pluginsUI.selectedCatalogRepo)
	}
	state.Focus = pluginsDetailInstall
	tester.Frame()
	tester.Key(0, ui.KeyEnter)
	tester.Frame()
	if state.Page != app.PluginInstallDialog {
		t.Fatalf("expected PluginInstallDialog, got %+v", state.Route)
	}
	state.Focus = pluginInstallConfirmBtn
	tester.Frame()
	tester.Key(0, ui.KeyEnter)
	drain(func() bool { return state.Page == app.LanguagePicker })
	tester.Frame()
	if state.Focus != languagePickerOptionID("mikutv-locale-ja") {
		t.Fatalf("expected LanguagePicker focused on newly installed Japanese pack, got %q", state.Focus)
	}

	// Select Japanese and confirm hot-swap + fallback + Japanese font stack!
	tester.Key(0, ui.KeyEnter)
	tester.Frame()
	if assets.Text.SettingsTitle != "設定" || assets.LocaleTag != "ja" {
		t.Fatalf("expected live JA switch, got title=%q tag=%q", assets.Text.SettingsTitle, assets.LocaleTag)
	}
	if !strings.Contains(assets.FontFamily(), "Hiragino Sans, PingFang SC") {
		t.Fatalf("expected Hiragino Sans prioritized for ja, got %q", assets.FontFamily())
	}
	// Untranslated key falls back to built-in English, and canonical Icons are preserved!
	if assets.Text.About.ProjectHome != "Project Website" {
		t.Fatalf("expected English fallback for untranslated key, got %q", assets.Text.About.ProjectHome)
	}
	for _, s := range assets.Text.Sections {
		if s.ID == "appearance" && (s.Name != "外観とモーション" || s.Icon != "brand") {
			t.Fatalf("expected translated section preserving canonical icon, got %+v", s)
		}
	}
}
