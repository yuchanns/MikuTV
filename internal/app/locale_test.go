package app

import (
	"archive/zip"
	"bytes"
	"path/filepath"
	"testing"

	"github.com/yuchanns/MikuTV/internal/plugin"
)

func makeTestLocalePackage(t *testing.T, id, lang, nativeName string) *plugin.LocalePackage {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	files := map[string]string{
		"plugin.json": `{"id":"` + id + `","name":"` + nativeName + `","kind":"locale","version":"1.0.0","apiVersion":1,"minAppVersion":"0.1.0","language":"` + lang + `","nativeName":"` + nativeName + `","locale":"locale.json"}`,
		"locale.json": `{"SettingsTitle":"設定"}`,
	}
	for name, body := range files {
		hdr := &zip.FileHeader{Name: name, Method: zip.Deflate}
		hdr.SetMode(0o644)
		w, err := zw.CreateHeader(hdr)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	pkg, err := plugin.OpenLocale(buf.Bytes(), "0.1.0")
	if err != nil {
		t.Fatalf("OpenLocale: %v", err)
	}
	return pkg
}

func TestLocaleSettingsFollowSystemAutoMatchesInstalledPlugin(t *testing.T) {
	store := plugin.NewPackageStore(filepath.Join(t.TempDir(), "plugins"))
	var appliedTag string
	var appliedLen int
	ls := NewLocaleSettings(
		store,
		nil,
		[]string{"ja-JP", "en-US"},
		func(tag string, payload []byte) {
			appliedTag = tag
			appliedLen = len(payload)
		},
		nil,
		nil,
	)

	// Before installing Japanese locale pack, system "ja-JP, en-US" falls back to built-in "en".
	if got := ls.ActiveTag(); got != LocaleBuiltinEN {
		t.Fatalf("expected fallback en before ja plugin install, got %q", got)
	}

	// Installing locale-ja in Follow System mode automatically activates Japanese!
	jaPkg := makeTestLocalePackage(t, "locale-ja", "ja", "日本語")
	ls.RegisterLocale(jaPkg)
	if got := ls.ActiveTag(); got != "ja" || appliedTag != "ja" || appliedLen == 0 {
		t.Fatalf("expected auto-activation of ja on RegisterLocale, got active=%q applied=%q len=%d", got, appliedTag, appliedLen)
	}

	// Manual switch to zh-Hans persists and overrides system language.
	if err := ls.Select(LocaleBuiltinZH); err != nil {
		t.Fatalf("Select zh-Hans: %v", err)
	}
	if got := ls.ActiveTag(); got != LocaleBuiltinZH || appliedTag != LocaleBuiltinZH {
		t.Fatalf("expected zh-Hans after Select, got active=%q applied=%q", got, appliedTag)
	}

	// Removing inactive locale-ja keeps zh-Hans; switching back to system falls back to en.
	ls.RemoveLocale("locale-ja")
	if got := ls.ActiveTag(); got != LocaleBuiltinZH {
		t.Fatalf("expected zh-Hans retained, got %q", got)
	}
	if err := ls.Select(LocaleModeSystem); err != nil {
		t.Fatalf("Select system: %v", err)
	}
	if got := ls.ActiveTag(); got != LocaleBuiltinEN {
		t.Fatalf("expected en fallback after removing locale-ja, got %q", got)
	}
}

func TestLocaleSettingsDisableSystemMatchedPackPersistsFallback(t *testing.T) {
	store := plugin.NewPackageStore(filepath.Join(t.TempDir(), "plugins"))
	jaPkg := makeTestLocalePackage(t, "locale-ja", "ja", "日本語")
	ls := NewLocaleSettings(store, []*plugin.LocalePackage{jaPkg}, []string{"ja-JP", "en-US"}, nil, nil, nil)
	if got := ls.ActiveTag(); got != "ja" {
		t.Fatalf("expected initial system match ja, got %q", got)
	}

	// Disabling the system-matched pack must fall back to built-in en AND persist en so restart stays disabled.
	ls.DisableLocale("locale-ja")
	if got := ls.ActiveTag(); got != LocaleBuiltinEN {
		t.Fatalf("expected en after DisableLocale, got %q", got)
	}

	restarted := NewLocaleSettings(store, []*plugin.LocalePackage{jaPkg}, []string{"ja-JP", "en-US"}, nil, nil, nil)
	if got := restarted.ActiveTag(); got != LocaleBuiltinEN {
		t.Fatalf("disabled locale pack reactivated after restart: got %q, want %q", got, LocaleBuiltinEN)
	}
}
