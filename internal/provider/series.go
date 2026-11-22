package provider

import "context"

type SeriesMode string

const (
	SeriesModeHub      SeriesMode = "hub"
	SeriesModeIndex    SeriesMode = "index"
	SeriesModeCalendar SeriesMode = "calendar"
)

func (m SeriesMode) Valid() bool {
	switch m {
	case "", SeriesModeHub, SeriesModeIndex, SeriesModeCalendar:
		return true
	default:
		return false
	}
}

type SeriesFilterOption struct {
	Value string `json:"value"`
	Label string `json:"label"`
}

type SeriesFilterGroup struct {
	Key     string               `json:"key"`
	Label   string               `json:"label"`
	Options []SeriesFilterOption `json:"options,omitempty"`
}

type SeriesEntry struct {
	Item    MediaItem `json:"item"`
	Badge   string    `json:"badge,omitempty"`
	Score   string    `json:"score,omitempty"`
	Status  string    `json:"status,omitempty"`
	Eyebrow string    `json:"eyebrow,omitempty"`
	Tracked bool      `json:"tracked,omitempty"`
}

type SeriesScheduleEntry struct {
	Time         string      `json:"time,omitempty"`
	EpisodeLabel string      `json:"episodeLabel,omitempty"`
	Entry        SeriesEntry `json:"entry"`
}

type SeriesCalendarDay struct {
	Date    string                `json:"date"`
	Month   int                   `json:"month"`
	Day     int                   `json:"day"`
	Weekday int                   `json:"weekday"`
	IsToday bool                  `json:"isToday,omitempty"`
	Entries []SeriesScheduleEntry `json:"entries,omitempty"`
}

type SeriesRequest struct {
	Mode    SeriesMode        `json:"mode,omitempty"`
	Genre   string            `json:"genre,omitempty"`
	Filters map[string]string `json:"filters,omitempty"`
	Cursor  string            `json:"cursor,omitempty"`
}

type SeriesPage struct {
	Genres           []SeriesFilterOption `json:"genres,omitempty"`
	Hero             *SeriesEntry         `json:"hero,omitempty"`
	Items            []SeriesEntry        `json:"items,omitempty"`
	Tracked          []SeriesEntry        `json:"tracked,omitempty"`
	FilterGroups     []SeriesFilterGroup  `json:"filterGroups,omitempty"`
	Days             []SeriesCalendarDay  `json:"days,omitempty"`
	Total            *int64               `json:"total,omitempty"`
	NextCursor       string               `json:"nextCursor,omitempty"`
	SupportsIndex    bool                 `json:"supportsIndex,omitempty"`
	SupportsCalendar bool                 `json:"supportsCalendar,omitempty"`
}

type SeriesReader interface {
	ReadSeries(context.Context, SeriesRequest) (SeriesPage, error)
}
