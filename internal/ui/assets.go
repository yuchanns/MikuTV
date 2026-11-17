package tvui

import (
	"embed"
	"encoding/json"
	"fmt"

	"github.com/egoist/mygo/ui"
	"github.com/yuchanns/MikuTV/internal/app"
)

//go:embed assets
var files embed.FS

type section struct {
	ID, Name, Icon string
}

type labels struct {
	Nav                                                                   map[app.Page]string
	EntriesTitle, EntriesNote, CurrentEntry, EmptyEntries, EmptyEntryNote string
	Manage, EmptyTitle, EmptyNote, SwitchEntry                            string
	SettingsTitle, SettingsNote, EmptySettings                            string
	Version, Platform, AboutNote, Licenses, Close, SaveFailed             string
	Sections                                                              []section
}

type Assets struct {
	Text         labels
	Entries      []app.Entry
	Brand        ui.ImageSource
	AboutArtwork ui.ImageSource
	License      string
	icons        map[string]*ui.SVG
	images       map[string]ui.ImageSource
}

func LoadAssets() (*Assets, error) {
	a := &Assets{icons: make(map[string]*ui.SVG), images: make(map[string]ui.ImageSource)}
	for path, target := range map[string]any{"assets/labels.json": &a.Text, "assets/entries.json": &a.Entries} {
		data, err := files.ReadFile(path)
		if err != nil {
			return nil, err
		}
		if err := json.Unmarshal(data, target); err != nil {
			return nil, fmt.Errorf("decode %s: %w", path, err)
		}
	}
	icons, err := files.ReadDir("assets/icons")
	if err != nil {
		return nil, err
	}
	for _, file := range icons {
		data, err := files.ReadFile("assets/icons/" + file.Name())
		if err != nil {
			return nil, err
		}
		icon, err := ui.ParseSVG(data)
		if err != nil {
			return nil, fmt.Errorf("decode icon %s: %w", file.Name(), err)
		}
		a.icons[file.Name()[:len(file.Name())-4]] = icon
	}
	for _, image := range []struct {
		path string
		dst  *ui.ImageSource
	}{{"assets/brand.svg", &a.Brand}, {"assets/about-icon.svg", &a.AboutArtwork}} {
		data, err := files.ReadFile(image.path)
		if err != nil {
			return nil, err
		}
		*image.dst, err = ui.DecodeImage(data)
		if err != nil {
			return nil, fmt.Errorf("decode %s: %w", image.path, err)
		}
	}
	license, err := files.ReadFile("assets/LICENSE.txt")
	if err != nil {
		return nil, err
	}
	a.License = string(license)
	// Prepare bundled covers before the window opens, outside navigation frames.
	for _, entry := range a.Entries {
		if _, cached := a.images[entry.Artwork]; cached {
			continue
		}
		var image ui.ImageSource
		if data, err := files.ReadFile("assets/" + entry.Artwork); err == nil {
			image, _ = ui.DecodeImage(data)
		}
		a.images[entry.Artwork] = image
	}
	return a, nil
}

func (a *Assets) Icon(name string) *ui.SVG {
	if icon := a.icons[name]; icon != nil {
		return icon
	}
	return a.icons["info"]
}

func (a *Assets) Artwork(path string) ui.ImageSource {
	return a.images[path]
}
