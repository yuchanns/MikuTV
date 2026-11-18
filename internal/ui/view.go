package tvui

import (
	"log"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/yuchanns/MikuTV/internal/app"
	"github.com/yuchanns/MikuTV/internal/provider"
	"github.com/yuchanns/MikuTV/internal/theme"
)

const (
	focusDuration = 140 * time.Millisecond
	railDuration  = 220 * time.Millisecond
)

var focusScroll = ui.Animation{Duration: 220 * time.Millisecond, Ease: ui.EaseOut}

var menuTransition = ui.ElementTransition{
	Duration: 200 * time.Millisecond, Ease: ui.EaseOut, Position: true,
	Enter: &ui.Motion{Y: 14}, Exit: &ui.Motion{Y: -8},
}

type scrollKey struct {
	entry string
	route app.Route
}

type Options struct {
	State       *app.State
	Catalog     *app.Catalog
	Auth        *app.Auth
	Activity    *app.AccountActivity
	Pages       *Pages
	Assets      *Assets
	Preferences app.PreferencesStore
	Theme       theme.Theme
	Version     string
}

type View struct {
	State           *app.State
	Catalog         *app.Catalog
	Auth            *app.Auth
	Activity        *app.AccountActivity
	Pages           *Pages
	Assets          *Assets
	Preferences     app.PreferencesStore
	Theme           theme.Theme
	Version         string
	Trace           bool
	native          ui.Theme
	focus           focusController
	scrolls         map[scrollKey]*ui.ScrollState
	lists           map[listKey]*ui.ListState
	activeScroll    *ui.ScrollState
	drawnRevision   uint64
	reveal          bool
	railSpan        float32
	notice          string
	qrPayload       string
	qrImage         ui.ImageSource
	qrHasAction     bool
	accountAvatar   *app.AccountAvatar
	avatarImage     ui.ImageSource
	profileGlow     ui.ImageSource
	cardGlow        ui.ImageSource
	home            catalogHome
	favoriteArtwork map[string]favoritePicture
	favorites       favoritesUI

	accountDetailsPhone bool
	openNotice          provider.AccountRow
}

func NewView(options Options) *View {
	return &View{State: options.State, Catalog: options.Catalog, Auth: options.Auth, Activity: options.Activity, Pages: options.Pages, Assets: options.Assets, Preferences: options.Preferences,
		Theme: options.Theme, Version: options.Version, native: nativeTheme(options.Theme), scrolls: make(map[scrollKey]*ui.ScrollState),
		profileGlow: accountGlow(deriveColors(options.Theme).profileGlow, 0.15, 0.65),
		cardGlow:    accountGlow(withAlpha(options.Theme.Colors.Accent.Primary, 0x29), 1, 0.75)}
}

// SetTheme atomically replaces the active theme and refreshes the derived native UI theme.
func (v *View) SetTheme(palette theme.Theme) {
	v.Theme = palette
	v.native = nativeTheme(palette)
}

func (v *View) Render(c *ui.Context) {
	v.syncAccountAvatar()
	c.SetTheme(&v.native)
	c.OnWindowBack(v.goBack)
	width, height := c.Size()
	layers := v.State.Layers()
	v.advanceCatalogHold(c, height, len(layers) == 1 && layers[0].Page == app.Home)
	v.focus.begin()
	v.activeScroll = nil
	ui.Box(c).Fill().Clip().Background(paint(v.Theme.Colors.Surface.Canvas)).Children(func() {
		page := v.context(layers[0], len(layers) == 1, width-collapsedRailWidth, height)
		ui.Row(c).Fill().Children(func() {
			navigationRail{}.Render(c, page)
			ui.Box(c.Key("main")).Width(width - collapsedRailWidth).Height(height).Shrink(0).Clip().
				Children(func() { v.renderScreen(c, page) })
		})
		if v.notice != "" {
			ui.Text(c, v.notice).Absolute().Right(32).Bottom(28).Padding(18, 24).
				Background(paint(v.Theme.Colors.Surface.Raised)).Radius(12).FontSize(20)
		}
	})
	for index, location := range layers[1:] {
		v.renderModal(c, v.context(location, index == len(layers)-2, width, height))
	}
	v.drawnRevision = v.State.Revision
	v.reveal = false
}

func (v *View) syncAccountAvatar() {
	if v.Auth == nil {
		return
	}
	avatar := v.Auth.Avatar()
	if v.accountAvatar == avatar {
		return
	}
	v.accountAvatar, v.avatarImage = avatar, nil
	if avatar != nil {
		v.avatarImage = ui.NewBitmap(avatar.Image)
	}
}

func (v *View) context(location app.Location, active bool, width, height float32) *PageContext {
	owner := v.Pages.navigationOwner(location.Route)
	left := focusDestination{}
	if owner != "" {
		left.Focus = app.RailFocus(owner)
	}
	return &PageContext{State: v.State, Pages: v.Pages, Assets: v.Assets, Theme: v.Theme, Preferences: v.Preferences,
		Version: v.Version, Location: location, Width: width, Height: height, active: active, host: v, left: left}
}

func (v *View) renderScreen(c *ui.Context, page *PageContext) {
	chain := v.Pages.Routes.Ancestors(page.Location.Route)
	scroll := page.scroll()
	insets := [4]float32{148, 83, 80, 103}
	if override := v.Pages.components[chain[0]].ScreenInsets; override != nil {
		insets = *override
	}
	ui.Scroll(c.Key(page.Location.Route)).Fill().TrackScroll(scroll).Transition(menuTransition).Children(func() {
		if page.Location.Page == app.Library {
			v.Pages.render(c, page)
			return
		}
		ui.Column(c).FillWidth().Padding(insets[0], insets[1], insets[2], insets[3]).Gap(44).
			Children(func() { v.Pages.render(c, page) })
	})
}

func (v *View) renderModal(c *ui.Context, page *PageContext) {
	page.left = focusDestination{}
	registration := v.Pages.components[page.Location.Route]
	page.Title, page.Node = registration.Title, registration.Policy.Route
	width, height := registration.ModalWidth, registration.ModalHeight
	if registration.ResolveModalHeight != nil {
		height = registration.ResolveModalHeight(v)
	}
	if width == 0 {
		width = 1100
	}
	if height == 0 {
		height = 850
	}
	insets := registration.ModalInsets
	if insets == ([4]float32{}) {
		insets = [4]float32{45, 45, 45, 45}
	}
	colors := v.Theme.Colors
	ui.Overlay(c, func() {
		ui.Box(c.Key(page.Location.Route)).Absolute().Left(0).Top(0).Width(page.Width).Height(page.Height).
			Modal().Background(paint(colors.Overlay.Scrim)).Center().Transition(menuTransition).Children(func() {
			panel := ui.Column(c).Width(min(width, page.Width-180)).Height(min(height, page.Height-150)).
				Padding(insets[0], insets[1], insets[2], insets[3]).Gap(28).
				Background(paint(colors.Surface.Panel)).Radius(30)
			if registration.ModalInsets != ([4]float32{}) {
				d := deriveColors(v.Theme)
				panel.Background(ui.Transparent).Gradient(d.dialogStart, d.dialogEnd, 140).
					Border(1, d.dialogBorder)
			}
			panel.Children(func() { registration.Component.Render(c, page) })
		})
	})
}

func (v *View) scrollFor(location app.Location, active bool) *ui.ScrollState {
	key := scrollKey{v.State.CurrentEntry, location.Route}
	scroll := v.scrolls[key]
	if scroll == nil {
		scroll = &ui.ScrollState{}
		v.scrolls[key] = scroll
	}
	if v.drawnRevision != v.State.Revision {
		scroll.X, scroll.Y = location.X, location.Y
	}
	if active {
		v.activeScroll = scroll
	}
	return scroll
}

func (v *View) dispatch(action func()) {
	v.home.hold = catalogHold{}
	v.notice = ""
	position := v.State.Position
	position.Focus, position.Area = v.State.Focus, v.focus.area(v.State.Focus)
	if scroll := v.activeScroll; scroll != nil {
		position.X, position.Y = scroll.X, scroll.Y
	}
	v.State.RecordPosition(position)
	previous := v.State.Route
	if previous.Page == app.Home {
		v.home.rotateAt = time.Time{}
	}
	action()
	if previous != v.State.Route {
		if leave := v.Pages.components[previous].Leave; leave != nil {
			leave(v)
		}
	}
	v.SyncEntry()
	if v.Trace {
		log.Printf("navigation: entry=%s page=%s section=%s focus=%s", v.State.CurrentEntry, v.State.Page, v.State.Section, v.State.Focus)
	}
}

func (v *View) goBack() bool {
	if !v.State.CanGoBack() {
		return false
	}
	v.dispatch(func() {
		back := v.Pages.components[v.State.Route].Back
		if back != nil && back(v) {
			return
		}
		v.State.Back()
	})
	return true
}

func (v *View) SyncEntry() {
	entry, _ := v.State.Current()
	if entry.ID != v.home.entry {
		v.home.action.stop()
		v.home.shelfDetails.stop()
		v.home.featureInfo.stop()
		v.home = catalogHome{entry: entry.ID}
	}
	var service provider.Authenticator
	if v.Catalog != nil {
		v.Catalog.Select(entry)
		if entry.Capabilities.Has(provider.SignIn) {
			service = v.Catalog.Authenticator()
		}
	}
	if v.Auth != nil {
		v.Auth.Bind(entry, service)
	}
	v.SyncAccountActivity()
}

func (v *View) SyncAccountActivity() {
	if v.Activity == nil || v.Auth == nil || v.Catalog == nil {
		return
	}
	entry, _ := v.State.Current()
	if v.Activity.Bind(entry, v.Auth.Accounts().CurrentID, v.Catalog.AccountReader()) {
		v.favorites = favoritesUI{}
	}
	if sync := v.Pages.components[v.State.Route].Sync; sync != nil {
		sync(v)
	}
}
