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
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/yuchanns/MikuTV/internal/provider"
	"golang.org/x/net/http/httpguts"
)

func (s *Session) ResolvePlayback(ctx context.Context, request provider.PlaybackRequest) (provider.Playback, error) {
	var result provider.Playback
	if !s.ownsPlayback(request.PlaybackTarget) || len(request.Quality) > 512 {
		return result, &provider.Error{Code: provider.Denied}
	}
	s.requests.RLock()
	client, scope, signedIn := s.active, s.scope, s.accountID != ""
	s.requests.RUnlock()
	input := struct {
		provider.PlaybackRequest
		SignedIn bool `json:"signedIn,omitempty"`
	}{request, signedIn}
	if err := s.invoke(ctx, scope, client, provider.ResolvePlayback, "resolvePlayback", input, &result.Plan); err != nil {
		return provider.Playback{}, err
	}
	if client == nil {
		return provider.Playback{}, &provider.Error{Code: provider.PluginFailed}
	}
	allowed := s.source.allowRequest(true)
	if !s.ownPlaybackPlan(&result.Plan, request, allowed) {
		return provider.Playback{}, &provider.Error{Code: provider.PluginFailed}
	}
	mediaClient := *client
	mediaClient.Timeout = 0
	if transport, ok := client.Transport.(*http.Transport); ok && transport != nil {
		cloned := transport.Clone()
		cloned.ResponseHeaderTimeout = 5 * time.Second
		if cloned.MaxIdleConnsPerHost < 6 {
			cloned.MaxIdleConnsPerHost = 6
		}
		mediaClient.Transport = cloned
	}
	mediaClient.CheckRedirect = func(req *http.Request, via []*http.Request) error {
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
	cdn := s.cdn
	if cdn == nil {
		cdn = newCDNTracker()
	}
	result.Transport = &playbackTransport{client: &mediaClient, scope: scope, allow: allowed, cdn: cdn}
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
	if plan.Access != provider.VideoAccessRestricted && len(plan.Streams) == 0 {
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
	for index := range plan.Streams {
		stream := &plan.Streams[index]
		if !ownPlaybackResource(&stream.Video.Resource, allow) {
			return false
		}
		switch stream.Format {
		case provider.PlaybackDirect, provider.PlaybackHLS:
			if stream.Audio != nil || len(stream.AudioTracks) != 0 {
				return false
			}
		case provider.PlaybackDASH:
			if !validDASHTrack(stream.Video, true) {
				return false
			}
			if stream.Audio != nil && (!validDASHTrack(*stream.Audio, false) || !ownPlaybackResource(&stream.Audio.Resource, allow)) {
				return false
			}
			for i := range stream.AudioTracks {
				if !validDASHTrack(stream.AudioTracks[i], false) || !ownPlaybackResource(&stream.AudioTracks[i].Resource, allow) {
					return false
				}
			}
		default:
			return false
		}
	}
	tracks := plan.Subtitles[:0]
	for _, track := range plan.Subtitles {
		if track.ID == "" || strings.TrimSpace(track.Label) == "" {
			return false
		}
		if ownPlaybackResource(&track.Resource, allow) {
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
				image := httpsURL(location)
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
			image := httpsURL(chapter.ImageURL)
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
	return s.ownVideoSections(&provider.VideoDetails{Segments: plan.Episodes}) == nil
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

func ownPlaybackResource(resource *provider.PlaybackResource, allow func(*http.Request) error) bool {
	urls := make([]string, 0, len(resource.URLs))
	for _, address := range resource.URLs {
		target := httpsURL(address)
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
	client *http.Client
	scope  context.Context
	allow  func(*http.Request) error
	cdn    *cdnTracker
}

func (t *playbackTransport) ReportSlow(rawURL string) {
	t.cdn.penalize(rawURL, true, time.Now())
}

func (t *playbackTransport) HasHealthyAlternative(urls []string, currentURL string) bool {
	return t.cdn.hasHealthyAlternative(urls, currentURL, time.Now())
}

func (t *playbackTransport) OpenResource(ctx context.Context, resource provider.PlaybackResource, method, byteRange string) (*http.Response, error) {
	if (method != http.MethodGet && method != http.MethodHead) || !ownPlaybackResource(&resource, t.allow) {
		return nil, &provider.Error{Code: provider.Denied}
	}
	ctx, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(t.scope, cancel)
	closeRequest := func() { stop(); cancel() }
	code := provider.Network
	candidates := t.cdn.order(resource.URLs, time.Now())
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
		response, err := t.client.Do(request)
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
	client, scope := s.active, s.scope
	s.requests.RUnlock()
	if err := s.invoke(ctx, scope, client, provider.ReadDanmaku, "readDanmaku", query, &result); err != nil {
		return nil, err
	}
	for _, item := range result.Items {
		if item.Seconds < 0 || !validDanmakuMode(item.Mode) || strings.TrimSpace(item.Text) == "" {
			return nil, &provider.Error{Code: provider.PluginFailed}
		}
	}
	return result.Items, nil
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
	if !s.ownsPlayback(query.PlaybackTarget) || !ownPlaybackResource(&query.Track.Resource, s.source.allowRequest(true)) {
		return nil, &provider.Error{Code: provider.Denied}
	}
	s.requests.RLock()
	client, scope := s.active, s.scope
	s.requests.RUnlock()
	var result struct {
		Cues []provider.SubtitleCue `json:"cues"`
	}
	if err := s.invoke(ctx, scope, client, provider.ReadSubtitles, "readSubtitles", query, &result); err != nil {
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
