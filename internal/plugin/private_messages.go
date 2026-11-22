package plugin

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/yuchanns/MikuTV/internal/provider"
)

func (s *Session) ReadPrivateConversations(ctx context.Context, request provider.PrivateConversationsRequest) (provider.PrivateConversationsPage, error) {
	if !request.Valid() {
		return provider.PrivateConversationsPage{}, &provider.Error{Code: provider.Denied}
	}
	s.requests.RLock()
	client, scope, owner := s.active, s.scope, s.accountID
	s.requests.RUnlock()
	if owner == "" {
		return provider.PrivateConversationsPage{}, &provider.Error{Code: provider.NeedsSignIn}
	}
	input := struct {
		provider.PrivateConversationsRequest
		Owner string `json:"owner"`
	}{request, owner}
	var page provider.PrivateConversationsPage
	if err := s.invoke(ctx, scope, client, provider.ReadPrivateMessages, "readPrivateConversations", input, &page); err != nil {
		return provider.PrivateConversationsPage{}, err
	}
	if len(page.NextCursor) > 512 || (page.Unread != nil && *page.Unread < 0) {
		return provider.PrivateConversationsPage{}, &provider.Error{Code: provider.PluginFailed}
	}
	for index := range page.Conversations {
		conv := &page.Conversations[index]
		if conv.ID == "" || len(conv.ID) > 256 || conv.Peer.ID == "" || len(conv.Peer.ID) > 256 ||
			strings.TrimSpace(conv.Peer.Name) == "" || conv.Unread < 0 || conv.UpdatedAt < 0 {
			return provider.PrivateConversationsPage{}, &provider.Error{Code: provider.PluginFailed}
		}
		if conv.Peer.AvatarURL != "" && httpsURL(conv.Peer.AvatarURL) == nil {
			conv.Peer.AvatarURL = ""
		}
		if len(conv.LastSeq) > 128 {
			conv.LastSeq = ""
		}
		if len(conv.AckSeq) > 128 {
			conv.AckSeq = ""
		}
	}
	return page, nil
}

func (s *Session) ReadPrivateMessages(ctx context.Context, request provider.PrivateMessagesRequest) (provider.PrivateMessagesPage, error) {
	if !request.Valid() {
		return provider.PrivateMessagesPage{}, &provider.Error{Code: provider.Denied}
	}
	s.requests.RLock()
	client, scope, owner := s.active, s.scope, s.accountID
	s.requests.RUnlock()
	if owner == "" {
		return provider.PrivateMessagesPage{}, &provider.Error{Code: provider.NeedsSignIn}
	}
	input := struct {
		provider.PrivateMessagesRequest
		Owner string `json:"owner"`
	}{request, owner}
	var page provider.PrivateMessagesPage
	if err := s.invoke(ctx, scope, client, provider.ReadPrivateMessages, "readPrivateMessages", input, &page); err != nil {
		return provider.PrivateMessagesPage{}, err
	}
	if len(page.NextCursor) > 512 {
		return provider.PrivateMessagesPage{}, &provider.Error{Code: provider.PluginFailed}
	}
	for index := range page.Messages {
		msg := &page.Messages[index]
		if msg.ID == "" || len(msg.ID) > 256 || msg.SentAt < 0 || strings.TrimSpace(msg.Body) == "" {
			return provider.PrivateMessagesPage{}, &provider.Error{Code: provider.PluginFailed}
		}
		if len(msg.Seq) > 128 {
			msg.Seq = ""
		}
		if len(msg.SenderID) > 256 {
			msg.SenderID = ""
		}
	}
	return page, nil
}

func (s *Session) SendPrivateMessage(ctx context.Context, request provider.PrivateMessageSendRequest) (provider.PrivateMessageSendResult, error) {
	if !request.Valid() {
		return provider.PrivateMessageSendResult{}, &provider.Error{Code: provider.Denied}
	}
	s.requests.RLock()
	client, scope, owner, secret := s.active, s.scope, s.accountID, s.secret
	s.requests.RUnlock()
	if owner == "" {
		return provider.PrivateMessageSendResult{}, &provider.Error{Code: provider.NeedsSignIn}
	}
	input := struct {
		provider.PrivateMessageSendRequest
		Owner  string          `json:"owner"`
		Secret json.RawMessage `json:"secret"`
	}{request, owner, secret}
	var result provider.PrivateMessageSendResult
	if err := s.invoke(ctx, scope, client, provider.WritePrivateMessages, "sendPrivateMessage", input, &result); err != nil {
		return provider.PrivateMessageSendResult{}, err
	}
	msg := &result.Message
	if msg.ID == "" || len(msg.ID) > 256 || msg.SentAt < 0 || strings.TrimSpace(msg.Body) == "" {
		return provider.PrivateMessageSendResult{}, &provider.Error{Code: provider.PluginFailed}
	}
	if msg.SenderID == "" {
		msg.SenderID = owner
	}
	msg.FromSelf = true
	return result, nil
}

func (s *Session) MarkPrivateMessagesRead(ctx context.Context, request provider.PrivateMessageReadRequest) (provider.PrivateMessageReadResult, error) {
	if !request.Valid() {
		return provider.PrivateMessageReadResult{}, &provider.Error{Code: provider.Denied}
	}
	s.requests.RLock()
	client, scope, owner, secret := s.active, s.scope, s.accountID, s.secret
	s.requests.RUnlock()
	if owner == "" {
		return provider.PrivateMessageReadResult{}, &provider.Error{Code: provider.NeedsSignIn}
	}
	input := struct {
		provider.PrivateMessageReadRequest
		Owner  string          `json:"owner"`
		Secret json.RawMessage `json:"secret"`
	}{request, owner, secret}
	var result provider.PrivateMessageReadResult
	if err := s.invoke(ctx, scope, client, provider.WritePrivateMessages, "markPrivateMessagesRead", input, &result); err != nil {
		return provider.PrivateMessageReadResult{}, err
	}
	if result.Unread != nil && *result.Unread < 0 {
		return provider.PrivateMessageReadResult{}, &provider.Error{Code: provider.PluginFailed}
	}
	return result, nil
}
