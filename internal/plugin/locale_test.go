package plugin

import (
	"context"
	"testing"

	"github.com/yuchanns/MikuTV/internal/provider"
)

func TestOpenLocaleAndMatchTag(t *testing.T) {
	validZip := buildTestPluginZip(t, map[string]string{
		"plugin.json": `{
			"id": "locale-ja",
			"name": "Japanese",
			"description": "Japanese UI language pack",
			"kind": "locale",
			"version": "1.0.0",
			"apiVersion": 1,
			"minAppVersion": "0.1.0",
			"license": "Apache-2.0",
			"language": "ja",
			"nativeName": "日本語",
			"locale": "locale.json"
		}`,
		"locale.json": `{"SettingsTitle":"設定","Nav":{"home":"ホーム","settings":"設定"}}`,
		"LICENSE":     "Apache License 2.0",
	})

	pkg, err := OpenLocale(validZip, "0.1.0")
	if err != nil {
		t.Fatalf("OpenLocale valid: %v", err)
	}
	if pkg.Language() != "ja" || pkg.NativeName() != "日本語" || pkg.LicenseText() == "" {
		t.Fatalf("unexpected locale package: lang=%q native=%q lic=%q", pkg.Language(), pkg.NativeName(), pkg.LicenseText())
	}

	inspected, err := InspectPackage(validZip, "0.1.0")
	if err != nil {
		t.Fatalf("InspectPackage locale: %v", err)
	}
	if !inspected.Compatibility.Compatible || inspected.Language != "ja" || inspected.NativeName != "日本語" {
		t.Fatalf("unexpected inspection: %+v", inspected)
	}

	// Reject unknown source fields in locale manifest.
	badZip := buildTestPluginZip(t, map[string]string{
		"plugin.json": `{"id":"bad-loc","name":"Bad","kind":"locale","version":"1.0.0","apiVersion":1,"minAppVersion":"0.1.0","language":"ja","locale":"locale.json","main":"index.js"}`,
		"locale.json": `{"Back":"戻る"}`,
	})
	if _, err := OpenLocale(badZip, "0.1.0"); err == nil {
		t.Fatal("expected locale manifest with main field to fail")
	}

	// Verify BCP-47 normalization and matching.
	if got := NormalizeLocaleTag("zh_hans_cn"); got != "zh-Hans-CN" {
		t.Fatalf("NormalizeLocaleTag zh_hans_cn = %q", got)
	}
	if got := MatchLocaleTag([]string{"ja-JP", "en-US"}, []string{"ja", "zh-Hans", "en"}); got != "ja" {
		t.Fatalf("MatchLocaleTag ja-JP = %q, want ja", got)
	}
	if got := MatchLocaleTag([]string{"zh-CN"}, []string{"ja", "zh-Hans", "en"}); got != "zh-Hans" {
		t.Fatalf("MatchLocaleTag zh-CN = %q, want zh-Hans", got)
	}
	if got := MatchLocaleTag([]string{"fr-FR", "en-GB"}, []string{"zh-Hans", "en"}); got != "en" {
		t.Fatalf("MatchLocaleTag fr-FR/en-GB = %q, want en", got)
	}
}

func TestHostLocaleCurrentAndManifestLocales(t *testing.T) {
	sourceZip := buildTestPluginZip(t, map[string]string{
		"plugin.json": `{
			"id": "demo-i18n",
			"name": "Demo Default",
			"description": "Default Desc",
			"kind": "source",
			"version": "1.0.0",
			"apiVersion": 1,
			"minAppVersion": "0.1.0",
			"main": "index.js",
			"capabilities": ["catalog.browse"],
			"locales": {
				"ja": {
					"name": "デモソース",
					"description": "日本語の説明"
				},
				"en": {
					"name": "Demo English",
					"description": "English description"
				}
			}
		}`,
		"index.js": `export async function browse() {
			return {
				collections: [{ id: "home", title: host.locale.current(), items: [] }]
			};
		}`,
	})

	src, err := Open(context.Background(), sourceZip, "0.1.0")
	if err != nil {
		t.Fatalf("Open source: %v", err)
	}
	nameJA, descJA, _, _ := src.LocalizedMetadata("ja-JP")
	if nameJA != "デモソース" || descJA != "日本語の説明" {
		t.Fatalf("LocalizedMetadata(ja-JP) = %q / %q", nameJA, descJA)
	}
	nameEN, descEN, _, _ := src.LocalizedMetadata("en-US")
	if nameEN != "Demo English" || descEN != "English description" {
		t.Fatalf("LocalizedMetadata(en-US) = %q / %q", nameEN, descEN)
	}

	active := "en"
	SetHostLocaleProvider(func() string { return active })
	t.Cleanup(func() { SetHostLocaleProvider(nil) })

	sess := src.NewSession("demo-i18n", nil)
	page, err := sess.Browse(context.Background(), provider.BrowseRequest{})
	if err != nil {
		t.Fatalf("Browse en: %v", err)
	}
	if len(page.Collections) != 1 || page.Collections[0].Title != "en" {
		t.Fatalf("expected host.locale.current()=en, got %+v", page.Collections)
	}

	active = "ja"
	page2, err := sess.Browse(context.Background(), provider.BrowseRequest{})
	if err != nil {
		t.Fatalf("Browse ja: %v", err)
	}
	if len(page2.Collections) != 1 || page2.Collections[0].Title != "ja" {
		t.Fatalf("expected host.locale.current()=ja, got %+v", page2.Collections)
	}
}
