package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/yuchanns/MikuTV/internal/provider"
	"golang.org/x/net/http/httpguts"
)

func validPreferredQuality(pref string) bool {
	switch pref {
	case "", "4k-hdr", "4k", "1080p", "720p":
		return true
	}
	return false
}

func validPlaybackMediaKind(kind string) bool {
	switch kind {
	case "", provider.PlaybackMediaAudio, provider.PlaybackMediaVideo:
		return true
	}
	return false
}

func (s *Session) ResolvePlayback(ctx context.Context, request provider.PlaybackRequest) (provider.Playback, error) {
	var result provider.Playback
	if !s.ownsPlayback(request.PlaybackTarget) || len(request.Quality) > 512 || !validPreferredQuality(request.PreferredQuality) ||
		len(request.Line) > 128 || !validPlaybackMediaKind(request.MediaKind) || len(request.BranchID) > 256 || len(request.BranchState) > 4096 {
		return result, &provider.Error{Code: provider.Denied}
	}
	s.requests.RLock()
	client, scope, signedIn, secret := s.active, s.scope, s.accountID != "", s.secret
	s.requests.RUnlock()
	input := struct {
		provider.PlaybackRequest
		SignedIn bool            `json:"signedIn,omitempty"`
		Secret   json.RawMessage `json:"secret,omitempty"`
	}{request, signedIn, secret}
	if err := s.invoke(ctx, scope, client, provider.ResolvePlayback, "resolvePlayback", input, &result.Plan); err != nil {
		return provider.Playback{}, err
	}
	if client == nil {
		return provider.Playback{}, &provider.Error{Code: provider.PluginFailed}
	}
	allowed := s.allowRequestForSession(true)
	if !s.ownPlaybackPlan(&result.Plan, request, allowed) {
		return provider.Playback{}, &provider.Error{Code: provider.PluginFailed}
	}
	mediaClient := *client
	mediaClient.Timeout = 0
	singleClient := *client
	singleClient.Timeout = 0
	if transport, ok := client.Transport.(*http.Transport); ok && transport != nil {
		cloned := transport.Clone()
		cloned.ResponseHeaderTimeout = 5 * time.Second
		if cloned.MaxIdleConnsPerHost < 8 {
			cloned.MaxIdleConnsPerHost = 8
		}
		mediaClient.Transport = cloned

		singleCloned := transport.Clone()
		singleCloned.ResponseHeaderTimeout = 20 * time.Second
		if singleCloned.MaxIdleConnsPerHost < 8 {
			singleCloned.MaxIdleConnsPerHost = 8
		}
		singleClient.Transport = singleCloned
	}
	checkRedirect := func(req *http.Request, via []*http.Request) error {
		if len(via) >= 10 {
			return errors.New("media redirect limit exceeded")
		}
		if err := allowed(req); err != nil {
			return err
		}
		origin := via[0].URL
		sameOrigin := req.URL.Scheme == origin.Scheme && strings.EqualFold(req.URL.Host, origin.Host)
		for _, hop := range via[1:] {
			if hop.URL.Scheme != origin.Scheme || !strings.EqualFold(hop.URL.Host, origin.Host) {
				sameOrigin = false
				break
			}
		}
		if !sameOrigin {
			preserved := make(http.Header)
			for _, name := range []string{"Accept", "Accept-Language", "Range", "Referer", "User-Agent"} {
				if value := via[0].Header.Get(name); value != "" {
					preserved.Set(name, value)
				}
			}
			req.Header = preserved
		}
		return nil
	}
	mediaClient.CheckRedirect = checkRedirect
	singleClient.CheckRedirect = checkRedirect
	cdn := s.cdn
	if cdn == nil {
		cdn = newCDNTracker()
	}
	result.Transport = &playbackTransport{session: s, client: &mediaClient, singleClient: &singleClient, scope: scope, allow: allowed, cdn: cdn}
	return result, nil
}

func (s *Session) ownsPlayback(target provider.PlaybackTarget) bool {
	return s.ownsVideo(target.Ref) && len(target.PartID) <= 512
}

func (s *Session) ownPlaybackPlan(plan *provider.PlaybackPlan, request provider.PlaybackRequest, allow func(*http.Request) error) bool {
	if plan.Duration < 0 || math.IsInf(plan.Duration, 0) || math.IsNaN(plan.Duration) || plan.ExpiresAt < 0 ||
		plan.ResumeSeconds < 0 || plan.DanmakuSegmentSeconds < 0 || len(plan.PartID) > 512 || len(plan.ItemID) > 512 ||
		request.PartID != "" && request.PartID != plan.PartID || !validPlaybackLimit(plan.Restriction) {
		return false
	}
	switch plan.Access {
	case "", provider.VideoAccessGranted, provider.VideoAccessPreview, provider.VideoAccessRestricted:
	default:
		return false
	}
	if plan.PreviewRestriction != "" && (plan.Access != provider.VideoAccessPreview ||
		plan.PreviewRestriction != provider.PlaybackMembership && plan.PreviewRestriction != provider.PlaybackPurchase) {
		return false
	}
	if plan.LiveEnded && !plan.Live {
		return false
	}
	if plan.Access != provider.VideoAccessRestricted && !plan.LiveEnded && len(plan.Streams) == 0 {
		return false
	}
	qualities := make(map[string]bool, len(plan.Qualities))
	for _, quality := range plan.Qualities {
		if quality.ID == "" || qualities[quality.ID] || strings.TrimSpace(quality.Label) == "" || !validPlaybackLimit(quality.Limit) {
			return false
		}
		qualities[quality.ID] = true
	}
	if plan.Quality != "" && !qualities[plan.Quality] {
		return false
	}
	lines := make(map[string]bool, len(plan.Lines))
	for i := range plan.Lines {
		line := &plan.Lines[i]
		line.Label = strings.TrimSpace(line.Label)
		if line.ID == "" || len(line.ID) > 128 || lines[line.ID] || line.Label == "" {
			return false
		}
		lines[line.ID] = true
	}
	if plan.Line != "" && !lines[plan.Line] {
		return false
	}
	if len(plan.AvailableKinds) > 0 {
		seenKinds := make(map[string]bool, len(plan.AvailableKinds))
		kinds := plan.AvailableKinds[:0]
		for _, k := range plan.AvailableKinds {
			k = strings.TrimSpace(k)
			if k != provider.PlaybackMediaAudio && k != provider.PlaybackMediaVideo {
				return false
			}
			if !seenKinds[k] {
				seenKinds[k] = true
				kinds = append(kinds, k)
			}
		}
		plan.AvailableKinds = kinds
	}
	for index := range plan.Streams {
		stream := &plan.Streams[index]
		switch stream.Format {
		case provider.PlaybackDirect, provider.PlaybackHLS:
			if len(stream.AudioTracks) != 0 {
				return false
			}
			hasVideo := len(stream.Video.Resource.URLs) > 0
			hasAudio := stream.Audio != nil && len(stream.Audio.Resource.URLs) > 0
			switch {
			case hasVideo && stream.Audio == nil:
				if !s.ownPlaybackResource(&stream.Video.Resource, allow) {
					return false
				}
			case !hasVideo && hasAudio:
				if !s.ownPlaybackResource(&stream.Audio.Resource, allow) {
					return false
				}
			default:
				return false
			}
		case provider.PlaybackDASH:
			if !s.ownPlaybackResource(&stream.Video.Resource, allow) || !validDASHTrack(stream.Video, true) {
				return false
			}
			if stream.Audio != nil && (!validDASHTrack(*stream.Audio, false) || !s.ownPlaybackResource(&stream.Audio.Resource, allow)) {
				return false
			}
			seenAudio := make(map[string]bool, len(stream.AudioTracks))
			audioTracks := stream.AudioTracks[:0]
			for i := range stream.AudioTracks {
				track := stream.AudioTracks[i]
				if !validDASHTrack(track, false) || !s.ownPlaybackResource(&track.Resource, allow) {
					return false
				}
				track.ID = strings.TrimSpace(track.ID)
				if track.ID == "" {
					track.ID = "audio-" + strconv.Itoa(i)
				}
				if len(track.ID) > 128 {
					track.ID = track.ID[:128]
				}
				if seenAudio[track.ID] {
					continue
				}
				seenAudio[track.ID] = true
				track.Label = strings.TrimSpace(track.Label)
				if len(track.Label) > 128 {
					track.Label = track.Label[:128]
				}
				track.Language = strings.TrimSpace(track.Language)
				if len(track.Language) > 64 {
					track.Language = track.Language[:64]
				}
				audioTracks = append(audioTracks, track)
			}
			stream.AudioTracks = audioTracks
		default:
			return false
		}
	}
	tracks := plan.Subtitles[:0]
	for _, track := range plan.Subtitles {
		track.ID = strings.TrimSpace(track.ID)
		track.Label = strings.TrimSpace(track.Label)
		track.Language = strings.TrimSpace(track.Language)
		if len(track.Language) > 64 {
			track.Language = track.Language[:64]
		}
		if track.ID == "" || track.Label == "" {
			return false
		}
		if len(track.Resource.URLs) == 0 || s.ownPlaybackResource(&track.Resource, allow) {
			tracks = append(tracks, track)
		}
	}
	plan.Subtitles = tracks
	if thumbs := plan.Thumbnails; thumbs != nil {
		if thumbs.Columns < 1 || thumbs.Rows < 1 || thumbs.Width < 1 || thumbs.Height < 1 || len(thumbs.Images) == 0 ||
			thumbs.Columns > math.MaxInt/thumbs.Rows || thumbs.Width > math.MaxInt/thumbs.Columns || thumbs.Height > math.MaxInt/thumbs.Rows ||
			!slices.IsSorted(thumbs.Times) || len(thumbs.Times) > 0 && thumbs.Times[0] < 0 {
			plan.Thumbnails = nil
		} else {
			for index, location := range thumbs.Images {
				image := s.allowedURL(location)
				if image == nil || allow(&http.Request{URL: image}) != nil {
					thumbs.Images[index] = ""
				}
			}
		}
	}
	for index := range plan.Chapters {
		chapter := &plan.Chapters[index]
		if chapter.Start < 0 || strings.TrimSpace(chapter.Title) == "" || index != 0 && chapter.Start <= plan.Chapters[index-1].Start {
			return false
		}
		if chapter.ImageURL != "" {
			image := s.allowedURL(chapter.ImageURL)
			if image == nil || allow(&http.Request{URL: image}) != nil {
				chapter.ImageURL = ""
			}
		}
	}
	for _, segment := range plan.Episodes {
		if !segment.Unavailable && segment.Ref == nil && segment.PartID == "" || len(segment.PartID) > 512 {
			return false
		}
	}
	if !ownPlaybackBranch(plan.Branch) {
		return false
	}
	return s.ownVideoSections(&provider.VideoDetails{Segments: plan.Episodes}) == nil
}

func ownPlaybackBranch(branch *provider.PlaybackBranch) bool {
	if branch == nil {
		return true
	}
	branch.NodeTitle = strings.TrimSpace(branch.NodeTitle)
	branch.Question = strings.TrimSpace(branch.Question)
	if len(branch.NodeID) > 256 || len(branch.NodeTitle) > 256 || len(branch.Question) > 512 ||
		len(branch.State) > 4096 || len(branch.Options) > 16 ||
		branch.TriggerSeconds < 0 || math.IsInf(branch.TriggerSeconds, 0) || math.IsNaN(branch.TriggerSeconds) {
		return false
	}
	seen := make(map[string]bool, len(branch.Options))
	for i := range branch.Options {
		opt := &branch.Options[i]
		opt.Title = strings.TrimSpace(opt.Title)
		opt.Subtitle = strings.TrimSpace(opt.Subtitle)
		if opt.ID == "" || len(opt.ID) > 256 || seen[opt.ID] ||
			len(opt.Title) > 256 || len(opt.Subtitle) > 256 ||
			len(opt.PartID) > 512 || len(opt.State) > 4096 {
			return false
		}
		if opt.Title == "" && !(branch.AutoAdvance && len(branch.Options) == 1) {
			return false
		}
		seen[opt.ID] = true
	}
	return true
}

func validPlaybackLimit(limit provider.PlaybackLimit) bool {
	switch limit {
	case "", provider.PlaybackLogin, provider.PlaybackMembership, provider.PlaybackPurchase, provider.PlaybackRegion,
		provider.PlaybackDevice, provider.PlaybackSource:
		return true
	}
	return false
}

func validDASHTrack(track provider.PlaybackTrack, video bool) bool {
	validRange := func(r *provider.PlaybackRange) bool { return r != nil && r.First >= 0 && r.Last >= r.First }
	return track.Bandwidth > 0 && track.Codec != "" && (!video || track.Height > 0 && track.Width > 0) &&
		track.FrameRate >= 0 && validRange(track.Init) && validRange(track.Index) && track.Index.First > track.Init.Last
}

func (s *Session) ownPlaybackResource(resource *provider.PlaybackResource, allow func(*http.Request) error) bool {
	urls := make([]string, 0, len(resource.URLs))
	for _, address := range resource.URLs {
		target := s.allowedURL(address)
		if target != nil && allow(&http.Request{URL: target}) == nil {
			urls = append(urls, address)
		}
	}
	resource.URLs = urls
	if len(urls) == 0 {
		return false
	}
	for name, value := range resource.Headers {
		switch strings.ToLower(name) {
		case "accept", "accept-language", "origin", "referer", "user-agent":
			if httpguts.ValidHeaderFieldValue(value) {
				continue
			}
		}
		return false
	}
	return true
}

const (
	cdnMaxHosts          = 32
	cdnBasePenalty       = 30 * time.Second
	cdnMaxPenalty        = 2 * time.Minute
	cdnSlowEwmaCap       = 256 << 10
	cdnMinSampleBytes    = 64 << 10
	cdnMinSampleDuration = 250 * time.Millisecond
)

type cdnHostState struct {
	penaltyUntil time.Time
	failStreak   int
	ewmaBps      float64
	lastSuccess  time.Time
	lastSeen     time.Time
}

type cdnTracker struct {
	mu    sync.Mutex
	hosts map[string]*cdnHostState
}

func newCDNTracker() *cdnTracker {
	return &cdnTracker{hosts: make(map[string]*cdnHostState)}
}

func cdnHost(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	return strings.ToLower(u.Hostname())
}

func (c *cdnTracker) stateLocked(host string, now time.Time) *cdnHostState {
	if st, ok := c.hosts[host]; ok {
		st.lastSeen = now
		return st
	}
	if len(c.hosts) >= cdnMaxHosts {
		var oldestHost string
		var oldestTime time.Time
		for k, v := range c.hosts {
			if oldestHost == "" || v.lastSeen.Before(oldestTime) {
				oldestHost, oldestTime = k, v.lastSeen
			}
		}
		if oldestHost != "" {
			delete(c.hosts, oldestHost)
		}
	}
	st := &cdnHostState{lastSeen: now}
	c.hosts[host] = st
	return st
}

func (c *cdnTracker) order(urls []string, now time.Time) []string {
	if c == nil || len(urls) <= 1 {
		return urls
	}
	type ranked struct {
		url          string
		index        int
		penalized    bool
		penaltyUntil time.Time
		ewmaBps      float64
	}
	items := make([]ranked, len(urls))
	c.mu.Lock()
	for i, raw := range urls {
		r := ranked{url: raw, index: i}
		if host := cdnHost(raw); host != "" {
			if st, ok := c.hosts[host]; ok {
				r.ewmaBps = st.ewmaBps
				if now.Before(st.penaltyUntil) {
					r.penalized = true
					r.penaltyUntil = st.penaltyUntil
				}
			}
		}
		items[i] = r
	}
	c.mu.Unlock()
	slices.SortStableFunc(items, func(a, b ranked) int {
		if a.penalized != b.penalized {
			if !a.penalized {
				return -1
			}
			return 1
		}
		if !a.penalized {
			if a.ewmaBps != b.ewmaBps {
				if a.ewmaBps > b.ewmaBps {
					return -1
				}
				return 1
			}
			return a.index - b.index
		}
		if !a.penaltyUntil.Equal(b.penaltyUntil) {
			if a.penaltyUntil.Before(b.penaltyUntil) {
				return -1
			}
			return 1
		}
		return a.index - b.index
	})
	ordered := make([]string, len(items))
	for i, item := range items {
		ordered[i] = item.url
	}
	return ordered
}

func (c *cdnTracker) recordSuccess(rawURL string, bytes int64, elapsed time.Duration, now time.Time) {
	if c == nil {
		return
	}
	host := cdnHost(rawURL)
	if host == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	st := c.stateLocked(host, now)
	st.failStreak = 0
	st.penaltyUntil = time.Time{}
	st.lastSuccess = now
	if bytes >= cdnMinSampleBytes || elapsed >= cdnMinSampleDuration {
		secs := elapsed.Seconds()
		if secs < 0.001 {
			secs = 0.001
		}
		bps := float64(bytes) / secs
		if st.ewmaBps == 0 {
			st.ewmaBps = bps
		} else {
			st.ewmaBps = 0.35*bps + 0.65*st.ewmaBps
		}
	}
}

func (c *cdnTracker) penalize(rawURL string, slow bool, now time.Time) {
	if c == nil {
		return
	}
	host := cdnHost(rawURL)
	if host == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	st := c.stateLocked(host, now)
	st.failStreak++
	shift := min(st.failStreak-1, 2)
	cooldown := min(cdnMaxPenalty, cdnBasePenalty<<shift)
	st.penaltyUntil = now.Add(cooldown)
	if slow {
		if st.ewmaBps == 0 || st.ewmaBps > cdnSlowEwmaCap {
			st.ewmaBps = cdnSlowEwmaCap
		}
	}
}

func (c *cdnTracker) hasHealthyAlternative(urls []string, currentURL string, now time.Time) bool {
	if c == nil || len(urls) < 2 {
		return false
	}
	currentHost := cdnHost(currentURL)
	c.mu.Lock()
	defer c.mu.Unlock()
	seen := make(map[string]struct{}, len(urls))
	healthyTotal := 0
	otherHealthy := false
	for _, raw := range urls {
		host := cdnHost(raw)
		if host == "" {
			continue
		}
		if _, dup := seen[host]; dup {
			continue
		}
		seen[host] = struct{}{}
		st := c.hosts[host]
		healthy := st == nil || !now.Before(st.penaltyUntil)
		if healthy {
			healthyTotal++
			if currentHost != "" && host != currentHost {
				otherHealthy = true
			}
		}
	}
	if len(seen) < 2 {
		return false
	}
	if currentHost != "" {
		return otherHealthy
	}
	return healthyTotal >= 2
}

type playbackTransport struct {
	session      *Session
	client       *http.Client
	singleClient *http.Client
	scope        context.Context
	allow        func(*http.Request) error
	cdn          *cdnTracker
}

func (t *playbackTransport) ReportSlow(rawURL string) {
	t.cdn.penalize(rawURL, true, time.Now())
}

func (t *playbackTransport) HasHealthyAlternative(urls []string, currentURL string) bool {
	return t.cdn.hasHealthyAlternative(urls, currentURL, time.Now())
}

func (t *playbackTransport) OpenResource(ctx context.Context, resource provider.PlaybackResource, method, byteRange string) (*http.Response, error) {
	if (method != http.MethodGet && method != http.MethodHead) || t.session == nil || !t.session.ownPlaybackResource(&resource, t.allow) {
		return nil, &provider.Error{Code: provider.Denied}
	}
	ctx, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(t.scope, cancel)
	closeRequest := func() { stop(); cancel() }
	code := provider.Network
	candidates := t.cdn.order(resource.URLs, time.Now())
	httpClient := t.client
	if len(candidates) <= 1 && t.singleClient != nil {
		httpClient = t.singleClient
	}
	for _, address := range candidates {
		if err := t.scope.Err(); err != nil {
			closeRequest()
			return nil, err
		}
		request, err := http.NewRequestWithContext(ctx, method, address, nil)
		if err != nil {
			continue
		}
		for name, value := range resource.Headers {
			request.Header.Set(name, value)
		}
		if byteRange != "" {
			request.Header.Set("Range", byteRange)
		}
		started := time.Now()
		response, err := httpClient.Do(request)
		if err != nil {
			if ctx.Err() != nil {
				closeRequest()
				return nil, ctx.Err()
			}
			t.cdn.penalize(address, false, time.Now())
			continue
		}
		switch response.StatusCode {
		case http.StatusOK, http.StatusPartialContent, http.StatusRequestedRangeNotSatisfiable:
			response.Body = &playbackBody{
				ReadCloser:   response.Body,
				closeRequest: closeRequest,
				cdn:          t.cdn,
				url:          address,
				started:      started,
				ctx:          ctx,
			}
			return response, nil
		case http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound, http.StatusGone:
			code = provider.Expired
		case http.StatusTooManyRequests:
			code = provider.RateLimited
			t.cdn.penalize(address, false, time.Now())
		default:
			if response.StatusCode >= 500 {
				t.cdn.penalize(address, false, time.Now())
			}
		}
		response.Body.Close()
	}
	closeRequest()
	return nil, &provider.Error{Code: code}
}

type playbackBody struct {
	io.ReadCloser
	closeRequest func()
	cdn          *cdnTracker
	url          string
	started      time.Time
	ctx          context.Context
	bytes        int64
	readErr      error
	closed       bool
}

func (b *playbackBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	b.bytes += int64(n)
	if err != nil && !errors.Is(err, io.EOF) && b.readErr == nil {
		b.readErr = err
	}
	return n, err
}

func (b *playbackBody) Close() error {
	defer b.closeRequest()
	err := b.ReadCloser.Close()
	if !b.closed {
		b.closed = true
		if b.readErr != nil && b.ctx.Err() == nil {
			b.cdn.penalize(b.url, false, time.Now())
		} else if b.readErr == nil && b.ctx.Err() == nil && b.bytes > 0 {
			b.cdn.recordSuccess(b.url, b.bytes, time.Since(b.started), time.Now())
		}
	}
	return err
}

func (s *Session) ReportPlayback(ctx context.Context, report provider.PlaybackProgress) error {
	if !s.ownsPlayback(report.PlaybackTarget) || report.Seconds < 0 || len(report.ItemID) > 512 {
		return &provider.Error{Code: provider.Denied}
	}
	return s.invokePlaybackWrite(ctx, provider.ReportPlayback, "reportPlayback", report.Owner, report)
}

func (s *Session) Danmaku(ctx context.Context, query provider.DanmakuQuery) ([]provider.Danmaku, error) {
	if !s.ownsPlayback(query.PlaybackTarget) || query.Segment < 1 || len(query.ItemID) > 512 {
		return nil, &provider.Error{Code: provider.Denied}
	}
	var result struct {
		Items []provider.Danmaku `json:"items"`
	}
	s.requests.RLock()
	client, scope, secret := s.active, s.scope, s.secret
	s.requests.RUnlock()
	input := struct {
		provider.DanmakuQuery
		Secret json.RawMessage `json:"secret,omitempty"`
	}{query, secret}
	if err := s.invoke(ctx, scope, client, provider.ReadDanmaku, "readDanmaku", input, &result); err != nil {
		return nil, err
	}
	for i := range result.Items {
		item := &result.Items[i]
		if item.Seconds < 0 || !validDanmakuMode(item.Mode) || strings.TrimSpace(item.Text) == "" {
			return nil, &provider.Error{Code: provider.PluginFailed}
		}
		item.SenderID = strings.TrimSpace(item.SenderID)
		item.SenderHash = strings.TrimSpace(item.SenderHash)
		if len(item.SenderID) > 64 {
			item.SenderID = item.SenderID[:64]
		}
		if len(item.SenderHash) > 64 {
			item.SenderHash = item.SenderHash[:64]
		}
	}
	return result.Items, nil
}

func (s *Session) DanmakuRules(ctx context.Context) ([]provider.DanmakuRule, error) {
	s.requests.RLock()
	client, scope, owner, secret := s.active, s.scope, s.accountID, s.secret
	s.requests.RUnlock()
	if owner == "" {
		return nil, &provider.Error{Code: provider.NeedsSignIn}
	}
	var result struct {
		Rules []provider.DanmakuRule `json:"rules"`
	}
	input := struct {
		Secret json.RawMessage `json:"secret,omitempty"`
	}{secret}
	if err := s.invoke(ctx, scope, client, provider.DanmakuRules, "readDanmakuRules", input, &result); err != nil {
		return nil, err
	}
	cleaned := make([]provider.DanmakuRule, 0, len(result.Rules))
	for _, rule := range result.Rules {
		sanitized, ok := sanitizeDanmakuRule(rule, true)
		if !ok {
			continue
		}
		cleaned = append(cleaned, sanitized)
	}
	return cleaned, nil
}

func (s *Session) AddDanmakuRule(ctx context.Context, draft provider.DanmakuRuleDraft) (provider.DanmakuRule, error) {
	draft.Pattern = strings.TrimSpace(draft.Pattern)
	if !provider.ValidDanmakuRuleKind(draft.Kind) || !provider.ValidDanmakuRulePattern(draft.Kind, draft.Pattern) {
		return provider.DanmakuRule{}, &provider.Error{Code: provider.Denied}
	}
	s.requests.RLock()
	client, scope, owner, secret := s.active, s.scope, s.accountID, s.secret
	s.requests.RUnlock()
	if owner == "" {
		return provider.DanmakuRule{}, &provider.Error{Code: provider.NeedsSignIn}
	}
	if draft.Owner != "" && draft.Owner != owner {
		return provider.DanmakuRule{}, context.Canceled
	}
	input := struct {
		Data   provider.DanmakuRuleDraft `json:"data"`
		Secret json.RawMessage           `json:"secret"`
	}{draft, secret}
	var result struct {
		Rule provider.DanmakuRule `json:"rule"`
	}
	if err := s.invoke(ctx, scope, client, provider.DanmakuRules, "addDanmakuRule", input, &result); err != nil {
		return provider.DanmakuRule{}, err
	}
	if result.Rule.Kind == "" {
		result.Rule.Kind = draft.Kind
	}
	if strings.TrimSpace(result.Rule.Pattern) == "" {
		result.Rule.Pattern = draft.Pattern
	}
	sanitized, ok := sanitizeDanmakuRule(result.Rule, true)
	if !ok {
		return provider.DanmakuRule{}, &provider.Error{Code: provider.PluginFailed}
	}
	return sanitized, nil
}

func (s *Session) RemoveDanmakuRule(ctx context.Context, removal provider.DanmakuRuleRemoval) error {
	removal.RemoteID = strings.TrimSpace(removal.RemoteID)
	if removal.RemoteID == "" || len(removal.RemoteID) > 128 {
		return &provider.Error{Code: provider.Denied}
	}
	return s.invokePlaybackWrite(ctx, provider.DanmakuRules, "removeDanmakuRule", removal.Owner, removal)
}

func sanitizeDanmakuRule(rule provider.DanmakuRule, synced bool) (provider.DanmakuRule, bool) {
	if !provider.ValidDanmakuRuleKind(rule.Kind) {
		return provider.DanmakuRule{}, false
	}
	rule.Pattern = strings.TrimSpace(rule.Pattern)
	if rule.Pattern == "" || utf8.RuneCountInString(rule.Pattern) > 64 {
		return provider.DanmakuRule{}, false
	}
	rule.RemoteID = strings.TrimSpace(rule.RemoteID)
	if len(rule.RemoteID) > 128 {
		rule.RemoteID = rule.RemoteID[:128]
	}
	rule.ID = strings.TrimSpace(rule.ID)
	if len(rule.ID) > 128 {
		rule.ID = rule.ID[:128]
	}
	if synced {
		rule.Synced = true
		rule.Enabled = true
	}
	return rule, true
}

func (s *Session) PostDanmaku(ctx context.Context, draft provider.DanmakuPost) error {
	if !s.ownsPlayback(draft.PlaybackTarget) || len(draft.ItemID) > 512 || draft.Start < 0 ||
		!validDanmakuMode(draft.Mode) || strings.TrimSpace(draft.Text) == "" || utf8.RuneCountInString(draft.Text) > provider.DanmakuLimit {
		return &provider.Error{Code: provider.Denied}
	}
	return s.invokePlaybackWrite(ctx, provider.WriteDanmaku, "postDanmaku", draft.Owner, draft)
}

func (s *Session) invokePlaybackWrite(ctx context.Context, capability provider.Capability, operation, expectedOwner string, data any) error {
	s.requests.RLock()
	client, scope, owner, secret := s.active, s.scope, s.accountID, s.secret
	s.requests.RUnlock()
	if owner == "" {
		return &provider.Error{Code: provider.NeedsSignIn}
	}
	if expectedOwner != "" && expectedOwner != owner {
		return context.Canceled
	}
	input := struct {
		Data   any             `json:"data"`
		Secret json.RawMessage `json:"secret"`
	}{data, secret}
	return s.invoke(ctx, scope, client, capability, operation, input, &struct{}{})
}

func (s *Session) PlaybackSubtitles(ctx context.Context, query provider.PlaybackSubtitleQuery) ([]provider.SubtitleCue, error) {
	if !s.ownsPlayback(query.PlaybackTarget) || strings.TrimSpace(query.Track.ID) == "" ||
		(len(query.Track.Resource.URLs) > 0 && !s.ownPlaybackResource(&query.Track.Resource, s.allowRequestForSession(true))) {
		return nil, &provider.Error{Code: provider.Denied}
	}
	s.requests.RLock()
	client, scope, secret := s.active, s.scope, s.secret
	s.requests.RUnlock()
	input := struct {
		provider.PlaybackSubtitleQuery
		Secret json.RawMessage `json:"secret,omitempty"`
	}{query, secret}
	var result struct {
		Cues []provider.SubtitleCue `json:"cues"`
	}
	if err := s.invoke(ctx, scope, client, provider.ReadSubtitles, "readSubtitles", input, &result); err != nil {
		return nil, err
	}
	for _, cue := range result.Cues {
		if cue.Start < 0 || cue.End <= cue.Start || strings.TrimSpace(cue.Text) == "" {
			return nil, &provider.Error{Code: provider.PluginFailed}
		}
	}
	return result.Cues, nil
}

func validDanmakuMode(mode provider.DanmakuMode) bool {
	return mode == provider.DanmakuScroll || mode == provider.DanmakuTop || mode == provider.DanmakuBottom
}
