package app

import (
	"fmt"

	"github.com/yuchanns/MikuTV/internal/provider"
)

type Page string

const (
	Entries             Page = "entries"
	Home                Page = "home"
	HomeAction          Page = "homeAction"
	VideoDetail         Page = "videoDetail"
	Creator             Page = "creator"
	Player              Page = "player"
	VideoConfirm        Page = "videoConfirm"
	VideoCommentCompose Page = "videoCommentCompose"
	Search              Page = "search"
	SearchFilters       Page = "searchFilters"
	Following           Page = "following"
	FollowingAll        Page = "followingAll"
	Series              Page = "series"
	SeriesIndex         Page = "seriesIndex"
	SeriesIndexFilter   Page = "seriesIndexFilter"
	SeriesCalendar      Page = "seriesCalendar"
	SeriesAll           Page = "seriesAll"
	Live                Page = "live"
	LiveAll             Page = "liveAll"
	Library             Page = "library"
	Settings              Page = "settings"
	PlaybackSettingPicker Page = "playbackSettingPicker"
	DanmakuSettingPicker  Page = "danmakuSettingPicker"
	DanmakuRuleEditor     Page = "danmakuRuleEditor"
	PrivacyConfirm        Page = "privacyConfirm"
	PrivacyDetails        Page = "privacyDetails"
	Account             Page = "account"
	AccountDetails        Page = "accountDetails"
	AccountMessages       Page = "accountMessages"
	PrivateMessageCompose Page = "privateMessageCompose"
	AccountPicker         Page = "accountPicker"
	SignInQR            Page = "signInQR"
	SignOut             Page = "signOut"
	Licenses            Page = "licenses"
)

type FocusID string

const (
	ManagePlugins FocusID = "entries/manage"
	ContentAction      FocusID = "content/entries"
	ShowLicenses       FocusID = "about/licenses"
	ShowPrivacyDetails FocusID = "about/privacy-details"
	LicenseText        FocusID = "licenses/text"
	CloseModal    FocusID = "modal/close"
	AccountAction FocusID = "account/action"
	AccountInfo   FocusID = "account/info"
	AccountInbox  FocusID = "account/messages"
	AccountSync   FocusID = "account/sync"
	HistorySync   FocusID = "settings/history-sync"
	AccountSwitch FocusID = "account/switch"
	AccountAdd    FocusID = "account/add"
	AccountExit   FocusID = "account/exit"
	QRAction      FocusID = "auth/qr"
	ConfirmExit   FocusID = "auth/sign-out"
)

func RailFocus(page Page) FocusID    { return FocusID("rail/" + page) }
func EntryFocus(id string) FocusID   { return FocusID("entry/" + id) }
func SectionFocus(id string) FocusID { return FocusID("section/" + id) }
func AccountFocus(id string) FocusID { return FocusID("account/saved/" + id) }

type Route struct {
	Page    Page
	Section string
}

type FocusArea uint8

const (
	ContentArea FocusArea = iota
	NavigationArea
)

type Position struct {
	Focus FocusID
	Area  FocusArea
	X, Y  float32
}

type Location struct {
	Route
	Position
}

// RouteSpec describes navigation policy without depending on a UI toolkit.
type RouteSpec struct {
	Route         Route
	Parent        Route
	Redirect      Route
	Initial       Position
	ResolveFocus  func(*State) FocusID
	ContentFocus  FocusID
	ExitFocus     FocusID
	RequiresEntry bool
	Capability    provider.Capability
	Capabilities  []provider.Capability
	Transient     bool
	Modal         bool
}

type Routes struct {
	Root   Route
	Picker Route
	specs  map[Route]RouteSpec
}

func NewRoutes(root, picker Route, specs []RouteSpec) (*Routes, error) {
	r := &Routes{Root: root, Picker: picker, specs: make(map[Route]RouteSpec)}
	for _, spec := range specs {
		if spec.Route.Page == "" {
			return nil, fmt.Errorf("route identity is required")
		}
		if _, exists := r.specs[spec.Route]; exists {
			return nil, fmt.Errorf("duplicate route: %+v", spec.Route)
		}
		r.specs[spec.Route] = spec
	}
	for _, required := range []Route{root, picker} {
		if _, exists := r.specs[required]; !exists {
			return nil, fmt.Errorf("unregistered application root: %+v", required)
		}
	}
	for route, spec := range r.specs {
		if spec.Parent.Page != "" {
			if _, exists := r.specs[spec.Parent]; !exists {
				return nil, fmt.Errorf("route %+v has an unregistered parent", route)
			}
		}
		seen := make(map[Route]bool)
		for current := route; current.Page != ""; current = r.specs[current].Parent {
			if seen[current] {
				return nil, fmt.Errorf("route parent cycle at %+v", route)
			}
			seen[current] = true
		}
	}
	for route := range r.specs {
		if _, ok := r.Resolve(route); !ok {
			return nil, fmt.Errorf("invalid route redirect at %+v", route)
		}
	}
	return r, nil
}

func (r *Routes) Spec(route Route) (RouteSpec, bool) {
	spec, ok := r.specs[route]
	return spec, ok
}

func (r *Routes) Resolve(route Route) (RouteSpec, bool) {
	for range len(r.specs) + 1 {
		spec, ok := r.specs[route]
		if !ok {
			return RouteSpec{}, false
		}
		if spec.Redirect.Page == "" {
			var caps []provider.Capability
			if spec.Capability != "" {
				caps = append(caps, spec.Capability)
			}
			for parent := spec.Parent; parent.Page != ""; parent = r.specs[parent].Parent {
				parentSpec := r.specs[parent]
				spec.RequiresEntry = spec.RequiresEntry || parentSpec.RequiresEntry
				if parentSpec.Capability != "" {
					if spec.Capability == "" {
						spec.Capability = parentSpec.Capability
					}
					caps = append(caps, parentSpec.Capability)
				}
			}
			spec.Capabilities = caps
			return spec, true
		}
		route = spec.Redirect
	}
	return RouteSpec{}, false
}

func (r *Routes) Ancestors(route Route) []Route {
	var chain []Route
	for route.Page != "" {
		chain = append(chain, route)
		route = r.specs[route].Parent
	}
	for left, right := 0, len(chain)-1; left < right; left, right = left+1, right-1 {
		chain[left], chain[right] = chain[right], chain[left]
	}
	return chain
}

func (r *Routes) initial(state *State, spec RouteSpec) Location {
	position := spec.Initial
	if spec.ResolveFocus != nil {
		position.Focus = spec.ResolveFocus(state)
	}
	return Location{Route: spec.Route, Position: position}
}
