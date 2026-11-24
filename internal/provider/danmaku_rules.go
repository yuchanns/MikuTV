package provider

import (
	"context"
	"strings"
	"unicode"
)

const (
	DanmakuRules     Capability = "account.danmakuRules"
	DanmakuRuleLimit            = 40
)

type DanmakuRuleKind string

const (
	DanmakuRuleKeyword DanmakuRuleKind = "keyword"
	DanmakuRuleUser    DanmakuRuleKind = "user"
)

func ValidDanmakuRuleKind(kind DanmakuRuleKind) bool {
	return kind == DanmakuRuleKeyword || kind == DanmakuRuleUser
}

func ValidDanmakuUserID(pattern string) bool {
	if pattern == "" || len(pattern) > DanmakuRuleLimit {
		return false
	}
	for _, r := range pattern {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func ValidDanmakuRulePattern(kind DanmakuRuleKind, pattern string) bool {
	trimmed := strings.TrimSpace(pattern)
	if trimmed == "" || len([]rune(trimmed)) > DanmakuRuleLimit {
		return false
	}
	if strings.IndexFunc(trimmed, unicode.IsControl) >= 0 {
		return false
	}
	if kind == DanmakuRuleUser {
		return ValidDanmakuUserID(trimmed)
	}
	return true
}

type DanmakuRule struct {
	ID       string          `json:"id,omitempty"`
	Kind     DanmakuRuleKind `json:"kind"`
	Pattern  string          `json:"pattern"`
	Enabled  bool            `json:"enabled"`
	Synced   bool            `json:"synced,omitempty"`
	RemoteID string          `json:"remoteID,omitempty"`
}

type DanmakuRuleDraft struct {
	Owner   string          `json:"owner,omitempty"`
	Kind    DanmakuRuleKind `json:"kind"`
	Pattern string          `json:"pattern"`
}

type DanmakuRuleRemoval struct {
	Owner    string `json:"owner,omitempty"`
	RemoteID string `json:"remoteID"`
}

type DanmakuRulesProvider interface {
	DanmakuRules(context.Context) ([]DanmakuRule, error)
	AddDanmakuRule(context.Context, DanmakuRuleDraft) (DanmakuRule, error)
	RemoveDanmakuRule(context.Context, DanmakuRuleRemoval) error
}
