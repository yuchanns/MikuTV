package app

type Preferences struct {
	RecentEntry string
}

type PreferencesStore interface {
	Load() (Preferences, error)
	Save(Preferences) error
}
