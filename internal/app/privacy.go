package app

// LocalPlaybackCleaner removes saved local playback resume positions for one
// (entry, account) scope without affecting other entries, accounts, or data types.
type LocalPlaybackCleaner interface {
	ClearPlaybackPositions(entryID, owner string) error
}

// ClearLocalSearchHistory clears local search history strictly for (entryID, owner).
// Persistence runs first so a storage failure never leaves UI state falsely cleared.
func ClearLocalSearchHistory(store SearchHistoryStore, search *CatalogSearch, entryID, owner string) error {
	if entryID == "" {
		return nil
	}
	if search != nil && search.MatchesScope(entryID, owner) {
		return search.ClearHistory()
	}
	if store != nil {
		return store.SaveSearchHistory(entryID, owner, nil)
	}
	return nil
}

// ClearLocalWatchHistory clears local playback resume positions and in-memory
// watch history caches strictly for (entryID, owner). Remote cloud history and
// other entries/accounts/data types are untouched.
func ClearLocalWatchHistory(cleaner LocalPlaybackCleaner, activity *AccountActivity, playback *Playback, entryID, owner string) error {
	if entryID == "" {
		return nil
	}
	// Drain any in-flight asynchronous periodic position save before clearing
	// persistent storage so a background write cannot resurrect cleared history.
	if playback != nil {
		playback.flushPositionSaves()
	}
	if cleaner != nil {
		if err := cleaner.ClearPlaybackPositions(entryID, owner); err != nil {
			return err
		}
	}
	if playback != nil {
		playback.ClearLocalPositions(entryID, owner)
	}
	if activity != nil {
		activity.ClearHistoryCache(entryID, owner)
	}
	return nil
}
