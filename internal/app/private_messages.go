package app

import (
	"context"
	"slices"
	"strings"

	"github.com/yuchanns/MikuTV/internal/provider"
)

type PrivateConversationsSnapshot struct {
	Phase         CatalogPhase
	Conversations []provider.PrivateConversation
	Unread        int
	HasUnread     bool
	NextCursor    string
	Appending     bool
	Error         provider.ErrorCode
}

type PrivateThreadSnapshot struct {
	Phase      CatalogPhase
	Messages   []provider.PrivateMessage
	HasMore    bool
	NextCursor string
	Prepending bool
	Error      provider.ErrorCode
	Revision   uint64
}

type privateThreadState struct {
	cancel     context.CancelFunc
	cursor     string
	appending  bool
	snapshot   PrivateThreadSnapshot
}

// PrivateMessages owns direct-message state on the application thread with strict
// (entry, generation, owner) isolation.
type PrivateMessages struct {
	post    func(func())
	changed func()

	entry      Entry
	owner      string
	reader     provider.PrivateMessageReader
	writer     provider.PrivateMessageWriter
	generation uint64

	summaryCancel context.CancelFunc
	summaryLoaded bool
	unread        int
	hasUnread     bool

	listCancel context.CancelFunc
	listCursor string
	list       PrivateConversationsSnapshot
	seeded     *provider.PrivateConversation

	activeID string
	threads  map[string]*privateThreadState

	sendCancel context.CancelFunc
	ackCancel  context.CancelFunc
	convAcks   map[string]context.CancelFunc
}

func NewPrivateMessages(post func(func()), changed func()) *PrivateMessages {
	return &PrivateMessages{post: post, changed: changed}
}

func (m *PrivateMessages) Bind(entry Entry, owner string, reader provider.PrivateMessageReader, writer provider.PrivateMessageWriter) bool {
	if m.entry.ID == entry.ID && m.entry.Generation == entry.Generation && m.owner == owner &&
		slices.Equal(m.entry.Capabilities, entry.Capabilities) && m.reader == reader && m.writer == writer {
		return false
	}
	m.Close()
	m.entry, m.owner, m.reader, m.writer = entry, owner, reader, writer
	m.threads = make(map[string]*privateThreadState)
	m.convAcks = make(map[string]context.CancelFunc)
	return true
}

func (m *PrivateMessages) Available() bool {
	if m == nil {
		return false
	}
	return m.owner != "" && m.reader != nil && m.entry.Capabilities.Has(provider.ReadPrivateMessages)
}

func (m *PrivateMessages) CanSend() bool {
	if !m.Available() {
		return false
	}
	return m.writer != nil && m.entry.Capabilities.Has(provider.WritePrivateMessages)
}

func (m *PrivateMessages) CanMarkRead() bool {
	return m.CanSend()
}

func (m *PrivateMessages) Sending() bool {
	return m != nil && m.sendCancel != nil
}

func (m *PrivateMessages) MarkingRead() bool {
	return m != nil && m.ackCancel != nil
}

func (m *PrivateMessages) UnreadCount() int {
	if m == nil || !m.Available() {
		return 0
	}
	if m.hasUnread {
		return m.unread
	}
	total := 0
	for _, conv := range m.list.Conversations {
		total += conv.Unread
	}
	return total
}

func (m *PrivateMessages) Conversations() PrivateConversationsSnapshot {
	if m == nil {
		return PrivateConversationsSnapshot{}
	}
	snap := m.list
	snap.Unread = m.UnreadCount()
	snap.HasUnread = m.hasUnread
	return snap
}

func (m *PrivateMessages) ActiveConversationID() string {
	if m == nil {
		return ""
	}
	return m.activeID
}

func (m *PrivateMessages) ActiveConversation() (provider.PrivateConversation, bool) {
	if m == nil || m.activeID == "" {
		return provider.PrivateConversation{}, false
	}
	for _, conv := range m.list.Conversations {
		if conv.ID == m.activeID {
			return conv, true
		}
	}
	return provider.PrivateConversation{}, false
}

func (m *PrivateMessages) Thread(conversationID string) PrivateThreadSnapshot {
	if m == nil || conversationID == "" {
		return PrivateThreadSnapshot{}
	}
	if state := m.threads[conversationID]; state != nil {
		return state.snapshot
	}
	return PrivateThreadSnapshot{}
}

func (m *PrivateMessages) EnsureSummary() {
	if !m.Available() || m.summaryLoaded || m.summaryCancel != nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	m.summaryCancel = cancel
	gen := m.generation
	reader := m.reader
	go func() {
		page, err := reader.ReadPrivateConversations(ctx, provider.PrivateConversationsRequest{Summary: true})
		m.post(func() {
			if ctx.Err() != nil || m.generation != gen {
				return
			}
			cancel()
			m.summaryCancel = nil
			if err == nil {
				m.summaryLoaded = true
				if page.Unread != nil {
					m.unread = *page.Unread
					m.hasUnread = true
				}
				m.changed()
			}
		})
	}()
}

func (m *PrivateMessages) EnsureConversations() {
	if !m.Available() {
		return
	}
	if m.list.Phase == CatalogIdle {
		m.loadConversations("", false)
	}
}

func (m *PrivateMessages) RetryConversations() {
	if !m.Available() {
		return
	}
	if m.list.Phase == CatalogFailed || m.list.Phase == CatalogCanceled {
		m.loadConversations(m.listCursor, m.list.Appending)
	}
}

func (m *PrivateMessages) LoadMoreConversations() {
	if !m.Available() || m.list.Phase == CatalogLoading || m.list.NextCursor == "" {
		return
	}
	m.loadConversations(m.list.NextCursor, true)
}

func (m *PrivateMessages) loadConversations(cursor string, appending bool) {
	if m.listCancel != nil {
		m.listCancel()
		m.listCancel = nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	m.listCancel = cancel
	m.listCursor = cursor
	m.list.Phase = CatalogLoading
	m.list.Appending = appending
	m.list.Error = ""
	gen := m.generation
	reader := m.reader
	m.changed()
	go func() {
		page, err := reader.ReadPrivateConversations(ctx, provider.PrivateConversationsRequest{Cursor: cursor})
		m.post(func() {
			if ctx.Err() != nil || m.generation != gen {
				return
			}
			cancel()
			m.listCancel = nil
			if err != nil {
				m.list.Phase = CatalogFailed
				m.list.Error = provider.CodeOf(err)
				m.changed()
				return
			}
			var merged []provider.PrivateConversation
			if appending {
				merged = mergePrivateConversations(m.list.Conversations, page.Conversations)
			} else {
				merged = mergePrivateConversations(nil, page.Conversations)
			}
			if m.seeded != nil {
				merged = includeSeededConversation(merged, *m.seeded)
			}
			nextCursor := page.NextCursor
			if nextCursor == cursor {
				nextCursor = ""
			}
			if page.Unread != nil {
				m.unread = *page.Unread
				m.hasUnread = true
				m.summaryLoaded = true
			}
			m.list = PrivateConversationsSnapshot{
				Phase:         CatalogReady,
				Conversations: merged,
				Unread:        m.UnreadCount(),
				HasUnread:     m.hasUnread,
				NextCursor:    nextCursor,
			}
			if m.activeID == "" && len(merged) > 0 {
				m.selectConversationLocked(merged[0].ID)
			} else if m.activeID != "" {
				found := false
				for _, conv := range merged {
					if conv.ID == m.activeID {
						found = true
						break
					}
				}
				if !found && len(merged) > 0 {
					m.selectConversationLocked(merged[0].ID)
				}
			}
			m.changed()
		})
	}()
}

func mergePrivateConversations(previous, incoming []provider.PrivateConversation) []provider.PrivateConversation {
	seen := make(map[string]bool, len(previous)+len(incoming))
	out := make([]provider.PrivateConversation, 0, len(previous)+len(incoming))
	for _, batch := range [][]provider.PrivateConversation{previous, incoming} {
		for _, conv := range batch {
			if conv.ID == "" || seen[conv.ID] {
				continue
			}
			seen[conv.ID] = true
			out = append(out, conv)
		}
	}
	return out
}

func includeSeededConversation(list []provider.PrivateConversation, seed provider.PrivateConversation) []provider.PrivateConversation {
	for i := range list {
		if list[i].ID == seed.ID || (seed.Peer.ID != "" && list[i].Peer.ID == seed.Peer.ID) {
			if list[i].Peer.AvatarURL == "" && seed.Peer.AvatarURL != "" {
				list[i].Peer.AvatarURL = seed.Peer.AvatarURL
			}
			return list
		}
	}
	return append([]provider.PrivateConversation{seed}, list...)
}

func (m *PrivateMessages) SelectConversation(id string) {
	if !m.Available() || id == "" {
		return
	}
	changed := m.activeID != id
	m.selectConversationLocked(id)
	if changed {
		m.changed()
	}
}

func (m *PrivateMessages) selectConversationLocked(id string) {
	m.activeID = id
	m.ensureMessagesLocked(id)
	for _, conv := range m.list.Conversations {
		if conv.ID == id && conv.Unread > 0 && m.CanMarkRead() {
			m.markConversationReadLocked(conv)
			break
		}
	}
}

// OpenPeer ensures a conversation with peer exists, selects it, and loads its messages.
func (m *PrivateMessages) OpenPeer(peer provider.PrivatePeer) string {
	if !m.Available() || strings.TrimSpace(peer.ID) == "" {
		return ""
	}
	name := strings.TrimSpace(peer.Name)
	if name == "" {
		name = peer.ID
	}
	peer.Name = name
	for i := range m.list.Conversations {
		if m.list.Conversations[i].Peer.ID == peer.ID || m.list.Conversations[i].ID == peer.ID {
			if m.list.Conversations[i].Peer.AvatarURL == "" && peer.AvatarURL != "" {
				m.list.Conversations[i].Peer.AvatarURL = peer.AvatarURL
			}
			id := m.list.Conversations[i].ID
			m.selectConversationLocked(id)
			m.changed()
			return id
		}
	}
	conv := provider.PrivateConversation{
		ID:   peer.ID,
		Peer: peer,
	}
	m.seeded = &conv
	m.list.Conversations = append([]provider.PrivateConversation{conv}, m.list.Conversations...)
	m.selectConversationLocked(conv.ID)
	if m.list.Phase == CatalogIdle {
		m.loadConversations("", false)
	} else {
		m.changed()
	}
	return conv.ID
}

func (m *PrivateMessages) ensureMessagesLocked(conversationID string) {
	if state := m.threads[conversationID]; state != nil && state.snapshot.Phase != CatalogIdle {
		return
	}
	m.loadMessagesLocked(conversationID, "", false)
}

func (m *PrivateMessages) RetryMessages(conversationID string) {
	if !m.Available() || conversationID == "" {
		return
	}
	state := m.threads[conversationID]
	if state == nil {
		m.loadMessagesLocked(conversationID, "", false)
		return
	}
	if state.snapshot.Phase == CatalogFailed || state.snapshot.Phase == CatalogCanceled {
		m.loadMessagesLocked(conversationID, state.cursor, state.appending)
	}
}

func (m *PrivateMessages) LoadOlderMessages(conversationID string) {
	if !m.Available() || conversationID == "" {
		return
	}
	state := m.threads[conversationID]
	if state == nil || state.snapshot.Phase == CatalogLoading || !state.snapshot.HasMore || state.snapshot.NextCursor == "" {
		return
	}
	m.loadMessagesLocked(conversationID, state.snapshot.NextCursor, true)
}

func (m *PrivateMessages) peerIDForConversation(conversationID string) string {
	for _, conv := range m.list.Conversations {
		if conv.ID == conversationID {
			return conv.Peer.ID
		}
	}
	return conversationID
}

func (m *PrivateMessages) loadMessagesLocked(conversationID, cursor string, prepending bool) {
	state := m.threads[conversationID]
	if state == nil {
		state = &privateThreadState{}
		m.threads[conversationID] = state
	}
	if state.cancel != nil {
		state.cancel()
		state.cancel = nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	state.cancel = cancel
	state.cursor = cursor
	state.appending = prepending
	state.snapshot.Phase = CatalogLoading
	state.snapshot.Prepending = prepending
	state.snapshot.Error = ""
	peerID := m.peerIDForConversation(conversationID)
	gen := m.generation
	reader := m.reader
	m.changed()
	go func() {
		page, err := reader.ReadPrivateMessages(ctx, provider.PrivateMessagesRequest{
			ConversationID: conversationID,
			PeerID:         peerID,
			Cursor:         cursor,
		})
		m.post(func() {
			if ctx.Err() != nil || m.generation != gen || m.threads[conversationID] != state {
				return
			}
			cancel()
			state.cancel = nil
			if err != nil {
				state.snapshot.Phase = CatalogFailed
				state.snapshot.Prepending = prepending
				state.snapshot.Error = provider.CodeOf(err)
				m.changed()
				return
			}
			var merged []provider.PrivateMessage
			if prepending {
				merged = mergePrivateMessages(page.Messages, state.snapshot.Messages)
			} else {
				merged = mergePrivateMessages(nil, page.Messages)
			}
			nextCursor := page.NextCursor
			if nextCursor == cursor {
				nextCursor = ""
			}
			hasMore := page.HasMore && nextCursor != ""
			rev := state.snapshot.Revision
			if !prepending {
				rev++
			}
			state.snapshot = PrivateThreadSnapshot{
				Phase:      CatalogReady,
				Messages:   merged,
				HasMore:    hasMore,
				NextCursor: nextCursor,
				Revision:   rev,
			}
			if len(merged) > 0 {
				latest := merged[len(merged)-1]
				m.syncConversationLatestLocked(conversationID, latest.Body, latest.SentAt, latest.Seq, false)
			}
			m.changed()
		})
	}()
}

func mergePrivateMessages(older, newer []provider.PrivateMessage) []provider.PrivateMessage {
	seen := make(map[string]bool, len(older)+len(newer))
	out := make([]provider.PrivateMessage, 0, len(older)+len(newer))
	for _, batch := range [][]provider.PrivateMessage{older, newer} {
		for _, msg := range batch {
			if msg.ID == "" || seen[msg.ID] {
				continue
			}
			seen[msg.ID] = true
			out = append(out, msg)
		}
	}
	return out
}

func (m *PrivateMessages) syncConversationLatestLocked(conversationID, preview string, updatedAt int64, seq string, promote bool) {
	idx := -1
	for i := range m.list.Conversations {
		if m.list.Conversations[i].ID == conversationID {
			idx = i
			break
		}
	}
	if idx < 0 {
		return
	}
	conv := m.list.Conversations[idx]
	if preview != "" && (promote || conv.Preview == "") {
		conv.Preview = preview
	}
	if updatedAt > conv.UpdatedAt {
		conv.UpdatedAt = updatedAt
	}
	if seq != "" && (promote || conv.LastSeq == "") {
		conv.LastSeq = seq
	}
	updated := slices.Clone(m.list.Conversations)
	updated[idx] = conv
	if promote && idx > 0 {
		copy(updated[1:idx+1], updated[0:idx])
		updated[0] = conv
	}
	m.list.Conversations = updated
}

func (m *PrivateMessages) SendMessage(conversationID, body string, done func(provider.PrivateMessage, error)) bool {
	if !m.CanSend() || m.sendCancel != nil || conversationID == "" {
		return false
	}
	peerID := m.peerIDForConversation(conversationID)
	req := provider.PrivateMessageSendRequest{
		ConversationID: conversationID,
		PeerID:         peerID,
		Body:           strings.TrimSpace(body),
	}
	if !req.Valid() {
		return false
	}
	ctx, cancel := context.WithCancel(context.Background())
	m.sendCancel = cancel
	gen := m.generation
	writer := m.writer
	m.changed()
	go func() {
		result, err := writer.SendPrivateMessage(ctx, req)
		m.post(func() {
			if ctx.Err() != nil || m.generation != gen {
				return
			}
			cancel()
			m.sendCancel = nil
			if err == nil {
				state := m.threads[conversationID]
				if state == nil {
					state = &privateThreadState{
						snapshot: PrivateThreadSnapshot{Phase: CatalogReady},
					}
					m.threads[conversationID] = state
				}
				state.snapshot.Messages = mergePrivateMessages(state.snapshot.Messages, []provider.PrivateMessage{result.Message})
				if state.snapshot.Phase != CatalogLoading {
					state.snapshot.Phase = CatalogReady
				}
				state.snapshot.Revision++
				m.syncConversationLatestLocked(conversationID, result.Message.Body, result.Message.SentAt, result.Message.Seq, true)
				if m.seeded != nil && m.seeded.ID == conversationID {
					m.seeded = nil
				}
			}
			m.changed()
			if done != nil {
				done(result.Message, err)
			}
		})
	}()
	return true
}

func (m *PrivateMessages) markConversationReadLocked(conv provider.PrivateConversation) {
	if !m.CanMarkRead() || conv.Unread <= 0 {
		return
	}
	req := provider.PrivateMessageReadRequest{
		ConversationID: conv.ID,
		PeerID:         conv.Peer.ID,
		AckSeq:         conv.LastSeq,
	}
	if !req.Valid() {
		return
	}
	if prev := m.convAcks[conv.ID]; prev != nil {
		prev()
	}
	ctx, cancel := context.WithCancel(context.Background())
	if m.convAcks == nil {
		m.convAcks = make(map[string]context.CancelFunc)
	}
	m.convAcks[conv.ID] = cancel
	gen := m.generation
	writer := m.writer
	go func() {
		result, err := writer.MarkPrivateMessagesRead(ctx, req)
		m.post(func() {
			if m.generation == gen {
				delete(m.convAcks, conv.ID)
			}
			if ctx.Err() != nil || m.generation != gen {
				cancel()
				return
			}
			cancel()
			if err != nil {
				return
			}
			m.applyConversationReadLocked(conv.ID, result)
			m.changed()
		})
	}()
}

func (m *PrivateMessages) applyConversationReadLocked(conversationID string, result provider.PrivateMessageReadResult) {
	cleared := 0
	updated := slices.Clone(m.list.Conversations)
	for i := range updated {
		if updated[i].ID == conversationID {
			cleared = updated[i].Unread
			updated[i].Unread = 0
			if updated[i].LastSeq != "" {
				updated[i].AckSeq = updated[i].LastSeq
			}
			break
		}
	}
	m.list.Conversations = updated
	if result.Unread != nil {
		m.unread = *result.Unread
		m.hasUnread = true
	} else if m.hasUnread && cleared > 0 {
		m.unread = max(0, m.unread-cleared)
	}
	m.list.Unread = m.UnreadCount()
}

func (m *PrivateMessages) MarkAllRead(done func(error)) bool {
	if !m.CanMarkRead() || m.ackCancel != nil {
		return false
	}
	targets := make([]provider.PrivateMessageAckTarget, 0, len(m.list.Conversations))
	for _, conv := range m.list.Conversations {
		if conv.Unread > 0 && conv.Peer.ID != "" {
			targets = append(targets, provider.PrivateMessageAckTarget{
				ConversationID: conv.ID,
				PeerID:         conv.Peer.ID,
				AckSeq:         conv.LastSeq,
			})
		}
	}
	req := provider.PrivateMessageReadRequest{All: true, Targets: targets}
	ctx, cancel := context.WithCancel(context.Background())
	m.ackCancel = cancel
	gen := m.generation
	writer := m.writer
	m.changed()
	go func() {
		result, err := writer.MarkPrivateMessagesRead(ctx, req)
		m.post(func() {
			if ctx.Err() != nil || m.generation != gen {
				return
			}
			cancel()
			m.ackCancel = nil
			if err == nil {
				updated := slices.Clone(m.list.Conversations)
				for i := range updated {
					updated[i].Unread = 0
					if updated[i].LastSeq != "" {
						updated[i].AckSeq = updated[i].LastSeq
					}
				}
				m.list.Conversations = updated
				if result.Unread != nil {
					m.unread = *result.Unread
				} else {
					m.unread = 0
				}
				m.hasUnread = true
				m.summaryLoaded = true
				m.list.Unread = m.unread
			}
			m.changed()
			if done != nil {
				done(err)
			}
		})
	}()
	return true
}

func (m *PrivateMessages) Close() {
	if m == nil {
		return
	}
	m.generation++
	if m.summaryCancel != nil {
		m.summaryCancel()
		m.summaryCancel = nil
	}
	if m.listCancel != nil {
		m.listCancel()
		m.listCancel = nil
	}
	if m.sendCancel != nil {
		m.sendCancel()
		m.sendCancel = nil
	}
	if m.ackCancel != nil {
		m.ackCancel()
		m.ackCancel = nil
	}
	for _, state := range m.threads {
		if state != nil && state.cancel != nil {
			state.cancel()
		}
	}
	for _, cancel := range m.convAcks {
		if cancel != nil {
			cancel()
		}
	}
	m.entry, m.owner, m.reader, m.writer = Entry{}, "", nil, nil
	m.summaryLoaded, m.unread, m.hasUnread = false, 0, false
	m.listCursor, m.list, m.seeded = "", PrivateConversationsSnapshot{}, nil
	m.activeID, m.threads, m.convAcks = "", nil, nil
}
