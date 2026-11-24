package app

import (
	"context"
	"testing"

	"github.com/yuchanns/MikuTV/internal/provider"
)

type historyViewLookup struct {
	ctx   context.Context
	ref   provider.MediaRef
	reply chan provider.MediaItem
}

type historyViewSource struct {
	provider.AccountReader
	provider.Catalog
	started chan historyViewLookup
}

func (s historyViewSource) Item(ctx context.Context, ref provider.MediaRef) (provider.MediaItem, error) {
	reply := make(chan provider.MediaItem, 1)
	s.started <- historyViewLookup{ctx, ref, reply}
	return <-reply, nil
}

func TestHistoryViewsUseTheCurrentViewportAndOwner(t *testing.T) {
	entry := Entry{ID: "source", Capabilities: provider.Capabilities{provider.AccountHistory.Capability(), provider.GetCatalogItem}}
	source := historyViewSource{started: make(chan historyViewLookup, 3)}
	callbacks := make(chan func(), 3)
	activity := NewAccountActivity(nil, func(fn func()) { callbacks <- fn }, func() {})
	defer activity.Close()
	activity.Bind(entry, "first", source)
	refs := []provider.MediaRef{{EntryID: entry.ID, Kind: provider.Video, ID: "first"},
		{EntryID: entry.ID, Kind: provider.Video, ID: "second"}, {EntryID: entry.ID, Kind: provider.Video, ID: "third"}}
	rows := []provider.AccountRow{{Ref: &refs[0]}, {Ref: &refs[1]}, {Ref: &refs[2]}}
	activity.PrepareHistoryViews(rows)
	first, second := waitAccountRead(t, source.started), waitAccountRead(t, source.started)
	activity.PauseHistoryViews()
	zero := int64(0)
	first.reply <- provider.MediaItem{Ref: first.ref, Views: &zero, Quality: "4K"}
	waitAccountRead(t, callbacks)()
	if count, quality := activity.HistoryMetadata(first.ref); count == nil || *count != 0 || quality != "4K" {
		t.Fatalf("video metadata was lost: %v, %q", count, quality)
	}
	select {
	case lookup := <-source.started:
		t.Fatalf("the hidden viewport started a lookup: %v", lookup.ref)
	default:
	}
	activity.Bind(entry, "second", source)
	if second.ctx.Err() != context.Canceled {
		t.Fatal("changing accounts left the previous lookup running")
	}
	second.reply <- provider.MediaItem{Ref: second.ref, Views: &zero}
	waitAccountRead(t, callbacks)()
	for _, ref := range []provider.MediaRef{first.ref, second.ref} {
		if count, quality := activity.HistoryMetadata(ref); count != nil || quality != "" {
			t.Fatal("previous account metadata reached the new account")
		}
	}
}
