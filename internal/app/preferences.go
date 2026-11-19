package app

type Preferences struct {
	RecentEntry string
}

type PreferencesStore interface {
	Save(Preferences) error
}
