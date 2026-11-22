package provider

import (
	"context"
	"strings"
	"unicode/utf8"
)

const (
	ReadPrivateMessages  Capability = "account.privateMessages"
	WritePrivateMessages Capability = "account.writePrivateMessages"

	PrivateMessageLimit = 300
)

type PrivatePeer struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	AvatarURL string `json:"avatarURL,omitempty"`
}

type PrivateConversation struct {
	ID        string      `json:"id"`
	Peer      PrivatePeer `json:"peer"`
	Preview   string      `json:"preview,omitempty"`
	UpdatedAt int64       `json:"updatedAt,omitempty"`
	Unread    int         `json:"unread,omitempty"`
	LastSeq   string      `json:"lastSeq,omitempty"`
	AckSeq    string      `json:"ackSeq,omitempty"`
}

type PrivateMessage struct {
	ID       string `json:"id"`
	Seq      string `json:"seq,omitempty"`
	SenderID string `json:"senderID"`
	FromSelf bool   `json:"fromSelf,omitempty"`
	Body     string `json:"body"`
	SentAt   int64  `json:"sentAt,omitempty"`
}

type PrivateConversationsRequest struct {
	Cursor  string `json:"cursor,omitempty"`
	Summary bool   `json:"summary,omitempty"`
}

func (r PrivateConversationsRequest) Valid() bool {
	if len(r.Cursor) > 512 {
		return false
	}
	return !r.Summary || r.Cursor == ""
}

type PrivateConversationsPage struct {
	Conversations []PrivateConversation `json:"conversations,omitempty"`
	Unread        *int                  `json:"unread,omitempty"`
	NextCursor    string                `json:"nextCursor,omitempty"`
}

type PrivateMessagesRequest struct {
	ConversationID string `json:"conversationID"`
	PeerID         string `json:"peerID,omitempty"`
	Cursor         string `json:"cursor,omitempty"`
}

func (r PrivateMessagesRequest) Valid() bool {
	return r.ConversationID != "" && len(r.ConversationID) <= 256 &&
		len(r.PeerID) <= 256 && len(r.Cursor) <= 512
}

type PrivateMessagesPage struct {
	Messages   []PrivateMessage `json:"messages,omitempty"`
	NextCursor string           `json:"nextCursor,omitempty"`
	HasMore    bool             `json:"hasMore,omitempty"`
}

type PrivateMessageSendRequest struct {
	ConversationID string `json:"conversationID"`
	PeerID         string `json:"peerID"`
	Body           string `json:"body"`
}

func (r PrivateMessageSendRequest) Valid() bool {
	body := strings.TrimSpace(r.Body)
	return r.ConversationID != "" && len(r.ConversationID) <= 256 &&
		r.PeerID != "" && len(r.PeerID) <= 256 &&
		body != "" && utf8.RuneCountInString(body) <= PrivateMessageLimit
}

type PrivateMessageSendResult struct {
	Message PrivateMessage `json:"message"`
}

type PrivateMessageAckTarget struct {
	ConversationID string `json:"conversationID,omitempty"`
	PeerID         string `json:"peerID"`
	AckSeq         string `json:"ackSeq,omitempty"`
}

type PrivateMessageReadRequest struct {
	All            bool                      `json:"all,omitempty"`
	ConversationID string                    `json:"conversationID,omitempty"`
	PeerID         string                    `json:"peerID,omitempty"`
	AckSeq         string                    `json:"ackSeq,omitempty"`
	Targets        []PrivateMessageAckTarget `json:"targets,omitempty"`
}

func (r PrivateMessageReadRequest) Valid() bool {
	if len(r.ConversationID) > 256 || len(r.PeerID) > 256 || len(r.AckSeq) > 128 || len(r.Targets) > 100 {
		return false
	}
	for _, target := range r.Targets {
		if target.PeerID == "" || len(target.PeerID) > 256 || len(target.ConversationID) > 256 || len(target.AckSeq) > 128 {
			return false
		}
	}
	if r.All {
		return true
	}
	return r.ConversationID != "" && r.PeerID != ""
}

type PrivateMessageReadResult struct {
	Unread *int `json:"unread,omitempty"`
}

type PrivateMessageReader interface {
	ReadPrivateConversations(context.Context, PrivateConversationsRequest) (PrivateConversationsPage, error)
	ReadPrivateMessages(context.Context, PrivateMessagesRequest) (PrivateMessagesPage, error)
}

type PrivateMessageWriter interface {
	SendPrivateMessage(context.Context, PrivateMessageSendRequest) (PrivateMessageSendResult, error)
	MarkPrivateMessagesRead(context.Context, PrivateMessageReadRequest) (PrivateMessageReadResult, error)
}
