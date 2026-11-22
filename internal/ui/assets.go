package tvui

import (
	"bytes"
	"embed"
	"encoding/json"
	"fmt"

	"github.com/egoist/mygo/ui"
	"github.com/yuchanns/MikuTV/internal/app"
	"github.com/yuchanns/MikuTV/internal/provider"
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
	LoadingTitle, LoadingNote, FailedTitle, FailedNote                    string
	CanceledTitle, Retry, Cancel, Refresh                                 string
	SettingsTitle, SettingsNote, EmptySettings                            string
	Version, Platform, AboutNote, Licenses, Close, SaveFailed             string
	Sections                                                              []section
	Auth                                                                  authLabels
	Activity                                                              accountActivityLabels
	Home                                                                  homeLabels
	Search                                                                searchLabels
	Following                                                             followingLabels
	Series                                                                seriesLabels
	Creator                                                               creatorLabels
	Detail                                                                detailLabels
	Playback                                                              playbackLabels
	Modal                                                                 modalLabels
}

type modalLabels struct {
	CurrentPrefix, Current, ActiveNote, SwitchNote, ItemsCount string
	HintSelect, HintSwitch, HintApply, HintConfirm, HintSave   string
	HintOpen, HintClose                                        string
}

type followingLabels struct {
	Eyebrow, Title, Subtitle                        string
	TabAll, TabLive                                 string
	UpdatesTitle, LiveTitle, LiveNote, LiveBadge    string
	GuestTitle, GuestNote                           string
	EmptyCreatorsTitle, EmptyCreatorsNote           string
	EmptyUpdates, EmptyLive                         string
	Loading, LoadFailed, MoreFailed                 string
}

type seriesLabels struct {
	Eyebrow, PicksTitle, PicksNote, TrackedTitle, TrackedNote   string
	Index, IndexNote, Calendar, CalendarNote                    string
	Reset, Results, WorksCount, RatingSuffix                    string
	NoFilterResults, NoFilterNote                               string
	Today, MonthSuffix, Scheduled, NoUpdates, NoUpdatesNote     string
	Track, Tracked, TrackRemoved                                string
	Loading, LoadFailed, MoreFailed                             string
	Weekdays                                                    []string
}

type creatorLabels struct {
	Eyebrow, Videos, Posts, Collections, About         string
	Followers, Works, CollectionWorks                  string
	Location, Category, EmptyBio                       string
	NoVideos, NoPosts, NoCollections                   string
	Loading, LoadFailed, MoreFailed                    string
}

type searchLabels struct {
	Eyebrow, Title, Hint, Placeholder                                 string
	History, Suggestions, HotSearch                                   string
	PopularHeading, ResultsHeading, NoResults, NoResultsHint          string
	KindAll, KindVideo, KindCreator, KindSeries, KindLive             string
	SortDefault, SortNewest, SortViews                                string
	FiltersTitle, SortLabel, DurationLabel                            string
	DurationAll, DurationShort, DurationMedium, DurationLong          string
	ApplyFilters, Followers, VideoCount, CreatorVideos, DetailPending string
	MoreFailed                                                        string
}

type playbackLabels struct {
	Info, Previous, Next, Play, Pause, Comments, Danmaku, Subtitles, Quality, Speed, Progress, Chapters   string
	ShowControls, On, Off, Auto, Buffering, Failed, Ended, Replay                                         string
	FailedNote, EndedNote, Reconnect, Episodes                                                            string
	Access                                                                                                playbackAccessLabels
	LoginLimit, MembershipLimit, PurchaseLimit, RegionLimit, DeviceLimit, SourceLimit                     string
	Density, Opacity, SendDanmaku, DanmakuPlaceholder, Send, DanmakuSent, ScrollMode, TopMode, BottomMode string
	SubtitleFailed, PickFavoriteFolders, FavoriteSaved, ChapterHere, SeekTo, MoreComments                 string
	QualityGuest, QualitySignedIn, QualityMember                                                          string
}

type playbackAccessLabels struct {
	DeviceTitle, DeviceBody, DeviceGeneric, DeviceGenericBody, UseSDR, Keep, UseBest, SourceTitle, SourceBody, SourceGeneric      string
	RegionTitle, RegionBody, SeeRelated, VIPTitle, VIPBody, VIPNoPreview, MembershipHelp, MembershipInstructions, Refresh, Return string
	PurchaseTitle, PurchaseBody, TrialMinutes, TrialState, TrialLeft                                                              string
	TrialEndedTitle, TrialEndedBody, PurchaseTrialEndedBody, QualityVIPTitle, QualityVIPBody                                      string
	TrialSeconds, MobileClient                                                                                                    string
}

type detailLabels struct {
	Overview, Loading, Empty, CommentCount, Unavailable, Denied, SignIn, LoadFailed        string
	AccessGranted, AccessPreview, AccessRestricted                                         string
	Episodes, CurrentVideo, Collection, Comments, Related                                  string
	Follow, Followed, Unfollowed, Like, Favorite, Preview, GetAccess                       string
	ActionsUnavailable, NoComments, NoRelated, NoSegments, UnknownAuthor                   string
	UpdateFailed, Partial                                                                  string
	Confirm, Reconsider, ActionDone                                                        string
	LaterAdded, LaterRemoved, FavoriteAdded, FavoriteRemoved                               string
	FavoriteAddedTo                                                                        string
	WriteComment, Reply, Liked, More, Collapse                                             string
	Hot, Latest, AllComments, RootComment, AllReplies, ReplyCount, MoreReplies             string
	CommentPlaceholder, ReplyTo, CreatorMark, Pinned, Dislike, EditComment                 string
	PostComment, CommentInput, CommentPosted, CommentLoadFailed, Done, KeyboardPlaceholder string
	MinutesAgo, HoursAgo, DaysAgo, JustNow, Kicker, SeriesKicker                           string
	SeriesUnit, EpisodeUpcoming, EpisodeUpcomingNotice                                     string
}

type homeLabels struct {
	ForYou, Popular, Ranking, Regions, RankingNote                                         string
	PopularEyebrow, RankingEyebrow, RegionsEyebrow, RankingHeat                            string
	HeroCaption, EyebrowAfterHours, EyebrowMorning, EyebrowMidday, EyebrowAfternoon        string
	EyebrowTonight                                                                         string
	Continue, ContinueNote, Picks, PicksNote, Following, FollowingNote, SeeAll             string
	Play, Details, Later, Featured, Feature, Refresh, RefreshFailed, MoreFailed string
	Recommendations, RateLimited, Plays, TenThousand, HundredMillion            string
}

type accountActivityLabels struct {
	History, Favorites, WatchLater, Messages, MessagesNote, MessagesUnread  string
	NotificationTab, ReadAllNotices                                         string
	RefreshingRights, RefreshedRights                                       string
	All, Replies, Likes, System                                             string
	SyncOn, SyncOff, SyncNote, SyncAction                                   string
	HistorySync, HistorySyncNote, Enabled, Disabled                         string
	PrivacyIntro                                                            string
	More, Unavailable, SignInNeeded, SignInAction                           string
	FavoritesTitle, FavoritesNote, FoldersBack, FolderContents              string
	FolderCount, FolderPrivate, FolderPublic                                string
	CreateFolder, FolderSettings, EditFolder, DeleteFolder                  string
	FolderName, FolderNamePlaceholder, FolderNameRequired, FolderDuplicate  string
	PrivateFolder, SaveFolder, ManageFolder, DoneManaging                   string
	CancelFolder                                                            string
	SelectedFavorites, SelectAll, ClearSelection                            string
	DeleteFolderTitle, DeleteFolderNote, RemoveFavoritesNote, SavingFolder  string
	LibraryHistory, Tracking, Courses, PlayAll, DiscoverContent             string
	LibraryGuestTitle, LibraryGuestNote, LibraryUnavailable, RemoveSelected string
	ConfirmRemove                                                           string
	LibraryEmpty                                                            map[provider.AccountSection]struct{ Title, Note string }
}

type authLabels struct {
	Body, QR, Waiting, Scanned, PhoneConfirm, Secure                string
	Success, Continuing, Expired, ExpiredBody, Refresh, Loading     string
	Accounts, AccountsNote, Add, SignOut, SignOutTitle, SignOutNote string
	Failed, NetworkFailed, AccountExpired                           string
	AccountTitle, ScanLogin                                         string
	Connected, AccountStatus, AccountNormal, SignOutCancel          string
}

type Assets struct {
	Text         labels
	Brand        ui.ImageSource
	AboutArtwork ui.ImageSource
	License      string
	icons        map[string]*ui.SVG
	images       map[string]ui.ImageSource
}

func LoadAssets() (*Assets, error) {
	a := &Assets{icons: make(map[string]*ui.SVG), images: make(map[string]ui.ImageSource)}
	text, err := files.ReadFile("assets/labels.json")
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(text, &a.Text); err != nil {
		return nil, fmt.Errorf("decode labels: %w", err)
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
		name := file.Name()[:len(file.Name())-4]
		a.icons[name] = icon
		if err := a.loadDetailIconVariants(name, data); err != nil {
			return nil, err
		}
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
	return a, nil
}

func (a *Assets) loadDetailIconVariants(name string, data []byte) error {
	type strokeVariant struct{ prefix, from, to string }
	var variants []strokeVariant
	switch name {
	case "back", "check", "comment", "pin", "play", "plus", "reply", "star", "user":
		variants = []strokeVariant{{"detail/", "2", "1.7"}}
	case "coin":
		variants = []strokeVariant{{"detail-primary/", "1.7", "2"}}
	case "like":
		variants = []strokeVariant{{"detail-primary/", "1.7", "2"}, {"detail-vote/", "1.7", "1.8"}}
	case "dislike":
		variants = []strokeVariant{{"detail-vote/", "2", "1.8"}}
	}
	for _, variant := range variants {
		from, to := fmt.Sprintf(`stroke-width="%s"`, variant.from), fmt.Sprintf(`stroke-width="%s"`, variant.to)
		variantData := bytes.Replace(data, []byte(from), []byte(to), 1)
		if bytes.Equal(data, variantData) {
			return fmt.Errorf("missing %s on icon %s", from, name)
		}
		icon, err := ui.ParseSVG(variantData)
		if err != nil {
			return fmt.Errorf("decode icon %s%s: %w", variant.prefix, name, err)
		}
		a.icons[variant.prefix+name] = icon
	}
	return nil
}

// PrepareArtwork decodes an already validated package cover before rendering.
func (a *Assets) PrepareArtwork(key string, data []byte) {
	if len(data) != 0 {
		a.images[key], _ = ui.DecodeImage(data)
	}
}

func (a *Assets) PrepareAccountArtwork(entryID string, data []byte) {
	a.PrepareArtwork("account/"+entryID, data)
}

func (a *Assets) AccountArtwork(entryID string) ui.ImageSource {
	return a.Artwork("account/" + entryID)
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
