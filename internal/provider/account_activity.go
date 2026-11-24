package provider

import (
	"context"
	"strings"
	"unicode/utf8"
)

const EditFavoriteFolders Capability = "account.editFavoriteFolders"
const EditWatchLater Capability = "account.editWatchLater"
const EditHistory Capability = "account.editHistory"
const EditTracking Capability = "account.editTracking"
const EditNotifications Capability = "account.editNotifications"

const FavoriteFolderTitleLimit = 24

type FavoriteFolderAction string

const (
	FavoriteFolderCreate FavoriteFolderAction = "create"
	FavoriteFolderUpdate FavoriteFolderAction = "update"
	FavoriteFolderDelete FavoriteFolderAction = "delete"
	FavoriteFolderRemove FavoriteFolderAction = "remove"
)

type FavoriteFolderEdit struct {
	Action    FavoriteFolderAction `json:"action"`
	Folder    string               `json:"folder,omitempty"`
	Title     string               `json:"title,omitempty"`
	Private   bool                 `json:"private"`
	Resources []string             `json:"resources,omitempty"`
}

func (edit FavoriteFolderEdit) Valid() bool {
	if len(edit.Folder) > 512 {
		return false
	}
	validTitle := strings.TrimSpace(edit.Title) != "" && utf8.RuneCountInString(edit.Title) <= FavoriteFolderTitleLimit
	switch edit.Action {
	case FavoriteFolderCreate:
		return edit.Folder == "" && validTitle && len(edit.Resources) == 0
	case FavoriteFolderUpdate:
		return edit.Folder != "" && validTitle && len(edit.Resources) == 0
	case FavoriteFolderDelete:
		return edit.Folder != "" && len(edit.Resources) == 0
	case FavoriteFolderRemove:
		if edit.Folder == "" || len(edit.Resources) == 0 {
			return false
		}
		for _, resource := range edit.Resources {
			if resource == "" || len(resource) > 512 {
				return false
			}
		}
		return true
	default:
		return false
	}
}

type FavoriteFolderEditor interface {
	EditFavoriteFolder(context.Context, FavoriteFolderEdit) error
}

type AccountItemsEditor interface {
	RemoveAccountItems(context.Context, AccountSection, []string) error
}

type AccountSection string

const (
	AccountHistory       AccountSection = "history"
	AccountWatchLater    AccountSection = "watchLater"
	AccountFavorites     AccountSection = "favorites"
	AccountTracking      AccountSection = "tracking"
	AccountCourses       AccountSection = "courses"
	AccountNotifications AccountSection = "notifications"
)

func (section AccountSection) Capability() Capability {
	switch section {
	case AccountHistory, AccountWatchLater, AccountFavorites, AccountTracking, AccountCourses, AccountNotifications:
		return Capability("account." + section)
	default:
		return ""
	}
}

func (section AccountSection) RemoveCapability() Capability {
	switch section {
	case AccountHistory:
		return EditHistory
	case AccountWatchLater:
		return EditWatchLater
	case AccountTracking:
		return EditTracking
	default:
		return ""
	}
}

type NoticeFilter string

const (
	NoticeAll    NoticeFilter = "all"
	NoticeReply  NoticeFilter = "reply"
	NoticeLike   NoticeFilter = "like"
	NoticeSystem NoticeFilter = "system"
)

type AccountQuery struct {
	Section AccountSection `json:"section"`
	Folder  string         `json:"folder,omitempty"`
	Filter  NoticeFilter   `json:"filter,omitempty"`
	Summary bool           `json:"summary,omitempty"`
}

func (query AccountQuery) Valid() bool {
	if query.Section.Capability() == "" || query.Folder != "" && query.Section != AccountFavorites {
		return false
	}
	if query.Section != AccountNotifications {
		return query.Filter == "" && !query.Summary
	}
	if query.Summary {
		return query.Filter == ""
	}
	switch query.Filter {
	case NoticeAll, NoticeReply, NoticeLike, NoticeSystem:
		return true
	default:
		return false
	}
}

type AccountRequest struct {
	AccountQuery
	Cursor string `json:"cursor,omitempty"`
}

type AccountRow struct {
	Item             *MediaItem `json:"item,omitempty"`
	ID               string     `json:"id"`
	Title            string     `json:"title"`
	Subtitle         string     `json:"subtitle,omitempty"`
	Time             int64      `json:"time,omitempty"`
	Count            *int       `json:"count,omitempty"`
	CoverURL         string     `json:"coverURL,omitempty"`
	CreatorAvatarURL string     `json:"creatorAvatarURL,omitempty"`
	Private          *bool      `json:"private,omitempty"`
	CanDelete        bool       `json:"canDelete,omitempty"`
	RemovalID        string     `json:"removalID,omitempty"`
	DurationSeconds  *int64     `json:"durationSeconds,omitempty"`
	ProgressSeconds  *int64     `json:"progressSeconds,omitempty"`
	Finished         bool       `json:"finished,omitempty"`
	Views            *int64     `json:"views,omitempty"`
	Ref              *MediaRef  `json:"ref,omitempty"`

	Notice *AccountNotice `json:"notice,omitempty"`
}

type NoticeTarget string

const (
	NoticeTargetDefault  NoticeTarget = ""
	NoticeTargetComments NoticeTarget = "comments"
)

type AccountNotice struct {
	Actor   string       `json:"actor,omitempty"`
	ActorID string       `json:"actorID,omitempty"`
	Action  string       `json:"action,omitempty"`
	Unread  bool         `json:"unread,omitempty"`
	Filter  NoticeFilter `json:"filter,omitempty"`
	Target  NoticeTarget `json:"target,omitempty"`
	Cursor  string       `json:"cursor,omitempty"`
}

type NotificationReadRequest struct {
	All    bool         `json:"all,omitempty"`
	Filter NoticeFilter `json:"filter,omitempty"`
	IDs    []string     `json:"ids,omitempty"`
	Cursor string       `json:"cursor,omitempty"`
}

func (request NotificationReadRequest) Valid() bool {
	if len(request.Cursor) > 512 {
		return false
	}
	switch request.Filter {
	case "", NoticeAll, NoticeReply, NoticeLike, NoticeSystem:
	default:
		return false
	}
	if request.All {
		return len(request.IDs) == 0
	}
	if len(request.IDs) == 0 || len(request.IDs) > 100 {
		return false
	}
	for _, id := range request.IDs {
		if id == "" || len(id) > 512 {
			return false
		}
	}
	return true
}

type NotificationReadResult struct {
	Unread *int `json:"unread,omitempty"`
}

type NotificationsEditor interface {
	MarkNotificationsRead(context.Context, NotificationReadRequest) (NotificationReadResult, error)
}

type AccountPage struct {
	Rows       []AccountRow `json:"rows,omitempty"`
	Total      *int         `json:"total,omitempty"`
	Unread     *int         `json:"unread,omitempty"`
	Paused     *bool        `json:"paused,omitempty"`
	NextCursor string       `json:"nextCursor,omitempty"`
}

type AccountReader interface {
	ReadAccount(context.Context, AccountRequest) (AccountPage, error)
}
