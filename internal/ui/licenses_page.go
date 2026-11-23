package tvui

import (
	"fmt"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/yuchanns/MikuTV/internal/app"
)

func buildLicenseDocument(info app.AboutInfo, baseLicense string, about aboutLabels) string {
	var b strings.Builder
	writeSection := func(title string) {
		if b.Len() > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString(title)
		b.WriteByte('\n')
		b.WriteString(strings.Repeat("=", max(16, len(title))))
		b.WriteByte('\n')
	}

	writeSection(about.LicenseBuildHeader)
	b.WriteString("MikuTV ")
	b.WriteString(info.DisplayVersion())
	b.WriteByte('\n')
	if info.Commit != "" {
		b.WriteString("Commit: ")
		b.WriteString(info.Commit)
		b.WriteByte('\n')
	}
	b.WriteString("Platform: ")
	b.WriteString(info.Platform)
	b.WriteByte('\n')
	b.WriteString("License: ")
	b.WriteString(info.License)
	b.WriteByte('\n')
	b.WriteString("Project: ")
	b.WriteString(info.ProjectURL)

	writeSection(about.LicensePluginsHeader)
	if len(info.Plugins) == 0 {
		b.WriteString(about.LicenseNoPlugins)
	} else {
		for i, pl := range info.Plugins {
			if i > 0 {
				b.WriteByte('\n')
			}
			name := strings.TrimSpace(pl.Name)
			if name == "" {
				name = pl.ID
			}
			origin := about.External
			if pl.Bundled {
				origin = about.Bundled
			}
			kind := about.KindSource
			if pl.Kind == "theme" {
				kind = about.KindTheme
			}
			license := strings.TrimSpace(pl.License)
			if license == "" {
				license = about.LicenseDeclaredNone
			}
			version := strings.TrimSpace(pl.Version)
			if version == "" {
				version = "-"
			}
			line := fmt.Sprintf("- %s (%s) v%s [%s / %s] · %s", name, pl.ID, version, kind, origin, license)
			if pl.SourceCommit != "" {
				short := pl.SourceCommit
				if len(short) > 12 {
					short = short[:12]
				}
				line += " · " + short
			}
			b.WriteString(line)
			if text := strings.TrimSpace(pl.LicenseText); text != "" {
				b.WriteString("\n\n")
				b.WriteString(text)
				b.WriteByte('\n')
			}
		}
	}

	writeSection(about.LicenseDepsHeader)
	for i, dep := range info.Dependencies {
		if i > 0 {
			b.WriteByte('\n')
		}
		line := "- " + dep.Module
		if dep.Version != "" {
			line += " " + dep.Version
		}
		if dep.License != "" {
			line += " (" + dep.License + ")"
		}
		if dep.Indirect {
			line += " [indirect]"
		}
		b.WriteString(line)
	}

	normalizedBase := strings.TrimSpace(strings.ReplaceAll(baseLicense, "\r\n", "\n"))
	type licenseGroup struct {
		modules  []string
		license  string
		text     string
		inBase   bool
	}
	var groups []*licenseGroup
	byText := make(map[string]*licenseGroup)
	for _, dep := range info.Dependencies {
		text := strings.TrimSpace(strings.ReplaceAll(dep.LicenseText, "\r\n", "\n"))
		if text == "" {
			continue
		}
		if existing, ok := byText[text]; ok {
			existing.modules = append(existing.modules, dep.Module)
			continue
		}
		g := &licenseGroup{
			modules: []string{dep.Module},
			license: dep.License,
			text:    text,
			inBase:  normalizedBase != "" && strings.Contains(normalizedBase, text),
		}
		byText[text] = g
		groups = append(groups, g)
	}

	if normalizedBase != "" || len(groups) > 0 {
		writeSection(about.LicenseFullHeader)
		if normalizedBase != "" {
			b.WriteString(normalizedBase)
		}
		for _, g := range groups {
			heading := strings.Join(g.modules, ", ")
			if b.Len() > 0 {
				b.WriteString("\n\n")
			}
			b.WriteString(heading)
			b.WriteByte('\n')
			b.WriteString(strings.Repeat("-", max(16, len(heading))))
			b.WriteByte('\n')
			if g.inBase {
				if g.license != "" {
					b.WriteString(g.license)
					b.WriteString(" (included above).")
				} else {
					b.WriteString("Included above.")
				}
			} else {
				b.WriteString(g.text)
			}
		}
	}
	return b.String()
}

type licensesPage struct{}

func (licensesPage) Render(c *ui.Context, p *PageContext) {
	p.region("modal", nil)
	colors, d := p.Theme.Colors, p.uiColors()
	scroll := p.scroll()
	info := p.resolvedAboutInfo()
	about := p.Assets.Text.About
	subtitle := fmt.Sprintf(about.LicenseSummary, info.DisplayVersion(), len(info.Plugins), len(info.Dependencies))
	document := buildLicenseDocument(info, p.Assets.License, about)
	ui.Column(c).Fill().Lang("zh-CN").Font("system-ui, PingFang SC").Children(func() {
		p.drawerHeader(c, "LEGAL \u00b7 OPEN SOURCE", p.Title, info.License, subtitle)
		e := ui.Scroll(c.Key(string(app.LicenseText))).FillWidth().Grow(1).TrackScroll(scroll).
			Label(p.Title).Padding(20, 22).Radius(14).Background(d.subtleFill)
		e.Children(func() {
			ui.Text(c, document).FillWidth().Font("monospace").FontSize(17).LineHeight(1.8).
				TextColor(paint(colors.Text.Secondary))
		})
		p.bindDirection(e, app.LicenseText, "modal", func(direction app.Direction) bool {
			step := float32(220)
			switch direction {
			case app.Up:
				step = -step
			case app.Down:
			default:
				return false
			}
			next := max(0, min(scroll.MaxY, scroll.Y+step))
			if next == scroll.Y {
				return false
			}
			scroll.Y = next
			return true
		})
		p.drawerFooter(c, p.Assets.Text.Modal.HintSelect)
	})
}
