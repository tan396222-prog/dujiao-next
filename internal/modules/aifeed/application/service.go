package application

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/dujiao-next/internal/logger"
	aifeedcontract "github.com/dujiao-next/internal/modules/aifeed/contract"
)

// Public, anonymous, read-only aggregate of AIHOT.
// Schema: https://aihot.news/openapi-v1.json (v1). Guide: https://aihot.news/llms.txt.
// This store does not claim written authorization or an official partnership.
// Only the fixed allowlist is requested. Full texts are not stored or republished.

const (
	attributionName = "AIHOT"
	maxBodyBytes    = 512 * 1024
	requestTimeout  = 8 * time.Second
	titleRunes      = 120
	summaryRunes    = 160
	newsLimit       = 12
	hotLimit        = 8
	resetLimit      = 40
	refusalHold     = 6 * time.Hour
	backoffCap      = time.Hour
	userAgent       = "dujiao-storefront-feed/1 (+https://aihot.news/terms)"
)

type sourceKind string

const (
	kindItems sourceKind = "items"
	kindHot   sourceKind = "hot"
	kindDaily sourceKind = "daily"
	kindReset sourceKind = "reset"
)

type source struct {
	Key      string
	Kind     sourceKind
	Provider string
	PageURL  string
	URL      string
	TTL      time.Duration
	MinGap   time.Duration
}

// Options configures the read-only cache. Production leaves Sources empty.
type Options struct {
	Now     func() time.Time
	Client  *http.Client
	Enabled func() bool
	Sources []source
}

// Service caches allowlisted public responses and never accepts a caller-supplied URL.
type Service struct {
	now            func() time.Time
	client         *http.Client
	enabled        func() bool
	sources        []source
	allowTestHosts bool

	mu      sync.Mutex
	entries map[string]*cached
	flights map[string]*flight
}

type flight struct {
	wg sync.WaitGroup
}

type cached struct {
	status    string
	stale     bool
	fetchedAt time.Time
	readyAt   time.Time
	etag      string
	failures  int
	checkedAt string
	news      []aifeedcontract.NewsItem
	daily     aifeedcontract.DailySection
	events    []aifeedcontract.ResetEvent
}

func defaultSources() []source {
	newsTTL := 5 * time.Minute
	newsGap := 60 * time.Second
	resetTTL := 10 * time.Minute
	return []source{
		{Key: "items", Kind: kindItems, URL: "https://aihot.news/api/v1/items?mode=selected&window=24h&by=timeline&limit=20", TTL: newsTTL, MinGap: newsGap},
		{Key: "hot", Kind: kindHot, URL: "https://aihot.news/api/v1/hot-topics", TTL: newsTTL, MinGap: newsGap},
		{Key: "daily", Kind: kindDaily, URL: "https://aihot.news/api/v1/dailies/latest", TTL: newsTTL, MinGap: newsGap},
		{Key: "codex", Kind: kindReset, Provider: "codex", PageURL: "https://aihot.news/codex-reset", URL: "https://aihot.news/api/v1/codex-resets/recent", TTL: resetTTL, MinGap: resetTTL},
		{Key: "antigravity", Kind: kindReset, Provider: "antigravity", PageURL: "https://aihot.news/antigravity-reset", URL: "https://aihot.news/api/v1/antigravity-resets/recent", TTL: resetTTL, MinGap: resetTTL},
		{Key: "claude", Kind: kindReset, Provider: "claude", PageURL: "https://aihot.news/claude-reset", URL: "https://aihot.news/api/v1/claude-resets/recent", TTL: resetTTL, MinGap: resetTTL},
		{Key: "grok_bot", Kind: kindReset, Provider: "grok_bot", PageURL: "https://aihot.news/grok-bot-reset", URL: "https://aihot.news/api/v1/grok-bot-resets/recent", TTL: resetTTL, MinGap: resetTTL},
	}
}

// ProductionSourceURLs is the only upstream list used when Sources is empty.
func ProductionSourceURLs() []string {
	sources := defaultSources()
	out := make([]string, 0, len(sources))
	for _, source := range sources {
		out = append(out, source.URL)
	}
	return out
}

// NewService builds the cache. A non-empty Sources list is still host-checked.
func NewService(opts Options) *Service {
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	sources := opts.Sources
	if len(sources) == 0 {
		sources = defaultSources()
	}
	client := opts.Client
	service := &Service{
		now:     now,
		client:  client,
		enabled: opts.Enabled,
		sources: sources,
		entries: map[string]*cached{},
		flights: map[string]*flight{},
	}
	if client == nil {
		service.client = &http.Client{Timeout: requestTimeout}
	}
	service.client.CheckRedirect = service.checkRedirect
	return service
}

// Snapshot returns the sanitized public view. It does not touch orders or payments.
func (s *Service) Snapshot(ctx context.Context) aifeedcontract.Snapshot {
	if s == nil || (s.enabled != nil && !s.enabled()) {
		return disabledSnapshot()
	}
	if ctx == nil {
		ctx = context.Background()
	}
	loaded := make([]*cached, len(s.sources))
	var group sync.WaitGroup
	for index, src := range s.sources {
		group.Add(1)
		go func(index int, src source) {
			defer group.Done()
			loaded[index] = s.load(ctx, src)
		}(index, src)
	}
	group.Wait()

	snapshot := aifeedcontract.Snapshot{
		Enabled:     true,
		Attribution: attributionName,
		Partnership: false,
		Coverage:    []string{"codex", "antigravity", "claude", "grok_bot"},
		News:        aifeedcontract.ItemSection{Status: aifeedcontract.StatusEmpty, Items: []aifeedcontract.NewsItem{}},
		HotTopics:   aifeedcontract.ItemSection{Status: aifeedcontract.StatusEmpty, Items: []aifeedcontract.NewsItem{}},
		Daily:       aifeedcontract.DailySection{Status: aifeedcontract.StatusEmpty},
		Resets:      []aifeedcontract.ResetSection{},
	}
	for index, src := range s.sources {
		entry := loaded[index]
		if entry == nil {
			entry = &cached{status: aifeedcontract.StatusUnavailable}
		}
		switch src.Kind {
		case kindItems:
			snapshot.News = itemSection(entry, newsLimit)
		case kindHot:
			snapshot.HotTopics = itemSection(entry, hotLimit)
		case kindDaily:
			snapshot.Daily = dailySection(entry)
		case kindReset:
			snapshot.Resets = append(snapshot.Resets, resetSection(src, entry))
		}
	}
	return snapshot
}

func (s *Service) load(ctx context.Context, src source) *cached {
	now := s.now()
	s.mu.Lock()
	if entry := s.entries[src.Key]; entry != nil && now.Before(entry.readyAt) {
		cloned := cloneCached(entry)
		s.mu.Unlock()
		return cloned
	}
	if existing := s.flights[src.Key]; existing != nil {
		s.mu.Unlock()
		existing.wg.Wait()
		s.mu.Lock()
		cloned := cloneCached(s.entries[src.Key])
		s.mu.Unlock()
		return cloned
	}
	call := &flight{}
	call.wg.Add(1)
	s.flights[src.Key] = call
	etag := ""
	if entry := s.entries[src.Key]; entry != nil {
		etag = entry.etag
	}
	s.mu.Unlock()

	next := s.fetch(ctx, src, etag)
	s.mu.Lock()
	if ctx.Err() != nil && next.status == aifeedcontract.StatusUnavailable && s.entries[src.Key] != nil {
		next = s.entries[src.Key]
	} else {
		s.entries[src.Key] = next
	}
	delete(s.flights, src.Key)
	cloned := cloneCached(s.entries[src.Key])
	s.mu.Unlock()
	call.wg.Done()
	return cloned
}

func (s *Service) fetch(ctx context.Context, src source, etag string) *cached {
	previous := s.previous(src.Key)
	parsed, err := url.Parse(src.URL)
	if err != nil || !s.hostAllowed(parsed) {
		return hold(previous, aifeedcontract.StatusRefused, s.now().Add(refusalHold))
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, src.URL, nil)
	if err != nil {
		return hold(previous, aifeedcontract.StatusUnavailable, s.now().Add(src.gap()))
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", userAgent)
	if etag != "" {
		req.Header.Set("If-None-Match", etag)
	}
	resp, err := s.client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			if previous != nil {
				return previous
			}
			return &cached{status: aifeedcontract.StatusUnavailable, readyAt: s.now().Add(src.gap())}
		}
		logger.Warnw("ai_feed_fetch_failed", "source", src.Key, "error", "transport")
		return hold(previous, aifeedcontract.StatusUnavailable, s.backoff(previous, src, 0))
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusNotModified:
		if previous == nil {
			return &cached{status: aifeedcontract.StatusEmpty, fetchedAt: s.now(), readyAt: s.now().Add(src.TTL), etag: clipHeader(resp.Header.Get("ETag"))}
		}
		kept := cloneCached(previous)
		kept.status = okStatus(kept)
		kept.stale = false
		kept.fetchedAt = s.now()
		kept.readyAt = s.now().Add(src.TTL)
		kept.failures = 0
		if tag := clipHeader(resp.Header.Get("ETag")); tag != "" {
			kept.etag = tag
		}
		return kept
	case http.StatusTooManyRequests, http.StatusServiceUnavailable:
		wait := retryAfter(resp.Header.Get("Retry-After"), s.now())
		logger.Warnw("ai_feed_rate_limited", "source", src.Key, "status", resp.StatusCode)
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxBodyBytes))
		return hold(previous, aifeedcontract.StatusRateLimited, s.backoff(previous, src, wait))
	case http.StatusUnauthorized, http.StatusForbidden, http.StatusMethodNotAllowed:
		logger.Warnw("ai_feed_refused", "source", src.Key, "status", resp.StatusCode)
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxBodyBytes))
		return hold(previous, aifeedcontract.StatusRefused, s.now().Add(refusalHold))
	case http.StatusOK:
		body, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes+1))
		if err != nil || len(body) > maxBodyBytes {
			logger.Warnw("ai_feed_body_rejected", "source", src.Key)
			return hold(previous, aifeedcontract.StatusUnavailable, s.backoff(previous, src, 0))
		}
		parsedBody, ok := parseSource(src, body)
		if !ok {
			logger.Warnw("ai_feed_schema_rejected", "source", src.Key)
			return hold(previous, aifeedcontract.StatusUnavailable, s.backoff(previous, src, 0))
		}
		parsedBody.fetchedAt = s.now()
		parsedBody.readyAt = s.now().Add(src.TTL)
		parsedBody.etag = clipHeader(resp.Header.Get("ETag"))
		parsedBody.failures = 0
		parsedBody.stale = false
		return parsedBody
	default:
		logger.Warnw("ai_feed_unexpected_status", "source", src.Key, "status", resp.StatusCode)
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxBodyBytes))
		return hold(previous, aifeedcontract.StatusUnavailable, s.backoff(previous, src, 0))
	}
}

func (s *Service) previous(key string) *cached {
	s.mu.Lock()
	defer s.mu.Unlock()
	return cloneCached(s.entries[key])
}

func (s *Service) backoff(previous *cached, src source, retryAfter time.Duration) time.Time {
	failures := 1
	if previous != nil {
		failures = previous.failures + 1
	}
	wait := src.gap()
	for i := 1; i < failures; i++ {
		wait *= 2
		if wait >= backoffCap {
			wait = backoffCap
			break
		}
	}
	if retryAfter > wait {
		wait = retryAfter
	}
	if wait > backoffCap {
		wait = backoffCap
	}
	next := s.now().Add(wait)
	if previous != nil {
		previous.failures = failures
	}
	return next
}

func (s *Service) hostAllowed(target *url.URL) bool {
	if target == nil || target.User != nil {
		return false
	}
	host := strings.ToLower(target.Hostname())
	if host == "aihot.news" && target.Scheme == "https" && (target.Port() == "" || target.Port() == "443") {
		return true
	}
	if !s.allowTestHosts {
		return false
	}
	return host != "" && (target.Scheme == "http" || target.Scheme == "https")
}

func (s *Service) checkRedirect(req *http.Request, via []*http.Request) error {
	if req == nil || req.URL == nil || len(via) == 0 || len(via) > 1 || via[0] == nil || via[0].URL == nil {
		return http.ErrUseLastResponse
	}
	if !strings.EqualFold(req.URL.Host, via[0].URL.Host) {
		return http.ErrUseLastResponse
	}
	if !s.hostAllowed(req.URL) {
		return http.ErrUseLastResponse
	}
	return nil
}

func (src source) gap() time.Duration {
	if src.MinGap > 0 {
		return src.MinGap
	}
	if src.TTL > 0 {
		return src.TTL
	}
	return time.Minute
}

func hold(previous *cached, status string, ready time.Time) *cached {
	if previous == nil {
		return &cached{status: status, readyAt: ready, failures: 1}
	}
	kept := cloneCached(previous)
	kept.status = status
	kept.stale = hasPayload(kept)
	kept.readyAt = ready
	if kept.failures < 1 {
		kept.failures = 1
	}
	return kept
}

func hasPayload(entry *cached) bool {
	if entry == nil {
		return false
	}
	return len(entry.news) > 0 || len(entry.events) > 0 || entry.daily.Title != "" || entry.daily.Summary != ""
}

func okStatus(entry *cached) string {
	if entry == nil || !hasPayload(entry) {
		return aifeedcontract.StatusEmpty
	}
	return aifeedcontract.StatusOK
}

func parseSource(src source, body []byte) (*cached, bool) {
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, false
	}
	if !schemaV1(payload["schemaVersion"]) {
		return nil, false
	}
	switch src.Kind {
	case kindItems:
		items, ok := parseNewsItems(payload["items"], newsLimit, false)
		if !ok {
			return nil, false
		}
		return &cached{status: listStatus(len(items)), news: items}, true
	case kindHot:
		items, ok := parseNewsItems(payload["items"], hotLimit, true)
		if !ok {
			return nil, false
		}
		return &cached{status: listStatus(len(items)), news: items}, true
	case kindDaily:
		daily, ok := parseDaily(payload["report"])
		if !ok {
			return nil, false
		}
		status := aifeedcontract.StatusEmpty
		if daily.Title != "" || daily.Summary != "" {
			status = aifeedcontract.StatusOK
		}
		return &cached{status: status, daily: daily}, true
	case kindReset:
		events, checkedAt, ok := parseResets(payload)
		if !ok {
			return nil, false
		}
		return &cached{status: listStatus(len(events)), events: events, checkedAt: checkedAt}, true
	default:
		return nil, false
	}
}

func parseNewsItems(raw any, limit int, hot bool) ([]aifeedcontract.NewsItem, bool) {
	rows, ok := raw.([]any)
	if !ok {
		return nil, false
	}
	items := make([]aifeedcontract.NewsItem, 0, min(len(rows), limit))
	for _, row := range rows {
		if len(items) >= limit {
			break
		}
		record, ok := row.(map[string]any)
		if !ok {
			continue
		}
		title := plain(asString(record["title"]), titleRunes)
		if title == "" {
			continue
		}
		links, _ := record["links"].(map[string]any)
		sourceName := ""
		if source, ok := record["source"].(map[string]any); ok {
			sourceName = plain(asString(source["name"]), 80)
		}
		updated := asString(record["discoveredAt"])
		if hot || updated == "" {
			if latest := asString(record["latestAt"]); latest != "" {
				updated = latest
			}
		}
		if updated == "" {
			updated = asString(record["publishedAt"])
		}
		items = append(items, aifeedcontract.NewsItem{
			Title:       title,
			Summary:     plain(asString(record["summary"]), summaryRunes),
			Source:      sourceName,
			OriginalURL: safeLink(asString(links["original"])),
			FeedURL:     aihotLink(asString(links["aihot"])),
			UpdatedAt:   updated,
		})
	}
	return items, true
}

func parseDaily(raw any) (aifeedcontract.DailySection, bool) {
	record, ok := raw.(map[string]any)
	if !ok || record == nil {
		return aifeedcontract.DailySection{}, false
	}
	lead, _ := record["lead"].(map[string]any)
	links, _ := record["links"].(map[string]any)
	return aifeedcontract.DailySection{
		Date:      plain(asString(record["date"]), 32),
		Title:     plain(asString(lead["title"]), titleRunes),
		Summary:   plain(asString(lead["leadParagraph"]), summaryRunes),
		FeedURL:   aihotLink(asString(links["aihot"])),
		UpdatedAt: asString(record["generatedAt"]),
	}, true
}

func parseResets(payload map[string]any) ([]aifeedcontract.ResetEvent, string, bool) {
	rows, ok := payload["events"].([]any)
	if !ok {
		return nil, "", false
	}
	events := make([]aifeedcontract.ResetEvent, 0, min(len(rows), resetLimit))
	for _, row := range rows {
		if len(events) >= resetLimit {
			break
		}
		record, ok := row.(map[string]any)
		if !ok {
			continue
		}
		title := plain(asString(record["title"]), titleRunes)
		summary, original, announced, postURL := firstPost(record["posts"])
		if title == "" && summary == "" && original == "" {
			continue
		}
		if title == "" {
			title = plain(summary, titleRunes)
		}
		label := plain(asString(record["displayLabel"]), 80)
		if label == "" {
			legacy := plain(asString(record["label"]), 80)
			// 旧字段「全员重置」只是兼容标签，不表示所有账号都会重置。
			if legacy != "" && legacy != "全员重置" {
				label = legacy
			}
		}
		scopeLabel := ""
		schedule := ""
		var scheduleDates []string
		estimated := false
		if item, ok := record["schedule"].(map[string]any); ok {
			schedule = plain(asString(item["label"]), summaryRunes)
			scheduleDates = announcementScheduleDates(asString(item["from"]), asString(item["through"]))
			switch asString(item["precision"]) {
			case "approximate", "date", "window", "deadline":
				estimated = true
			}
		}
		estimate := ""
		if item, ok := record["estimate"].(map[string]any); ok && item != nil {
			estimate = plain(asString(item["label"]), summaryRunes)
			estimated = true
		}
		if presentation, ok := record["presentation"].(map[string]any); ok {
			scopeLabel = plain(asString(presentation["scopeLabel"]), 80)
			if truthy(presentation["timeInferred"]) {
				estimated = true
			}
		}
		feedURL := aihotLink(asString(record["url"]))
		originalURL := safeLink(postURL)
		if originalURL == "" {
			originalURL = safeLink(asString(record["url"]))
			if aihotLink(originalURL) != "" {
				originalURL = ""
			}
		}
		events = append(events, aifeedcontract.ResetEvent{
			Title:         title,
			Label:         label,
			ScopeLabel:    scopeLabel,
			Summary:       summary,
			Original:      original,
			AnnouncedAt:   announced,
			ConfirmedAt:   asString(record["confirmedAt"]),
			OccurredOn:    beijingCalendarDate(asString(record["occurredOn"])),
			Schedule:      schedule,
			ScheduleDates: scheduleDates,
			Estimate:      estimate,
			Estimated:     estimated,
			OriginalURL:   originalURL,
			FeedURL:       feedURL,
		})
	}
	return events, asString(payload["checkedAt"]), true
}

var shanghai = time.FixedZone("Asia/Shanghai", 8*60*60)

// beijingCalendarDate accepts only a verified YYYY-MM-DD day. A timestamp is
// not converted, so a confirmation post or a crawl time cannot become a reset day.
func beijingCalendarDate(raw string) string {
	raw = strings.TrimSpace(raw)
	if len(raw) != 10 {
		return ""
	}
	parsed, err := time.ParseInLocation("2006-01-02", raw, shanghai)
	if err != nil || parsed.Format("2006-01-02") != raw {
		return ""
	}
	return raw
}

// announcementScheduleDates lists Beijing days covered by the announcement's
// own stated window. A model estimate is not passed here. A span wider than a
// week is left unmarked instead of being painted as a cycle.
func announcementScheduleDates(fromRaw, throughRaw string) []string {
	from, fromErr := time.Parse(time.RFC3339, strings.TrimSpace(fromRaw))
	through, throughErr := time.Parse(time.RFC3339, strings.TrimSpace(throughRaw))
	if fromErr != nil && throughErr != nil {
		return nil
	}
	if fromErr != nil {
		from = through
	}
	if throughErr != nil {
		through = from
	}
	if through.Before(from) {
		from, through = through, from
	}
	start := from.In(shanghai)
	end := through.In(shanghai)
	start = time.Date(start.Year(), start.Month(), start.Day(), 0, 0, 0, 0, shanghai)
	end = time.Date(end.Year(), end.Month(), end.Day(), 0, 0, 0, 0, shanghai)
	days := make([]string, 0, 4)
	for day := start; !day.After(end); day = day.AddDate(0, 0, 1) {
		days = append(days, day.Format("2006-01-02"))
		if len(days) > 7 {
			return nil
		}
	}
	return days
}

func firstPost(raw any) (summary string, original string, published string, link string) {
	rows, ok := raw.([]any)
	if !ok {
		return "", "", "", ""
	}
	for _, row := range rows {
		record, ok := row.(map[string]any)
		if !ok {
			continue
		}
		summary = plain(asString(record["text"]), summaryRunes)
		original = plain(asString(record["originalText"]), summaryRunes)
		published = asString(record["publishedAt"])
		link = asString(record["url"])
		if summary != "" || original != "" || link != "" {
			return summary, original, published, link
		}
	}
	return "", "", "", ""
}

func itemSection(entry *cached, limit int) aifeedcontract.ItemSection {
	items := entry.news
	if items == nil {
		items = []aifeedcontract.NewsItem{}
	}
	if len(items) > limit {
		items = items[:limit]
	}
	return aifeedcontract.ItemSection{
		Status:    entry.status,
		Stale:     entry.stale,
		FetchedAt: formatTime(entry.fetchedAt),
		Items:     items,
	}
}

func dailySection(entry *cached) aifeedcontract.DailySection {
	daily := entry.daily
	daily.Status = entry.status
	daily.Stale = entry.stale
	daily.FetchedAt = formatTime(entry.fetchedAt)
	return daily
}

func resetSection(src source, entry *cached) aifeedcontract.ResetSection {
	events := entry.events
	if events == nil {
		events = []aifeedcontract.ResetEvent{}
	}
	return aifeedcontract.ResetSection{
		Provider:  src.Provider,
		PageURL:   src.PageURL,
		Status:    entry.status,
		Stale:     entry.stale,
		FetchedAt: formatTime(entry.fetchedAt),
		CheckedAt: entry.checkedAt,
		Events:    events,
	}
}

func disabledSnapshot() aifeedcontract.Snapshot {
	emptyItems := aifeedcontract.ItemSection{Status: aifeedcontract.StatusDisabled, Items: []aifeedcontract.NewsItem{}}
	resets := make([]aifeedcontract.ResetSection, 0, 4)
	for _, src := range defaultSources() {
		if src.Kind != kindReset {
			continue
		}
		resets = append(resets, aifeedcontract.ResetSection{
			Provider: src.Provider,
			PageURL:  src.PageURL,
			Status:   aifeedcontract.StatusDisabled,
			Events:   []aifeedcontract.ResetEvent{},
		})
	}
	return aifeedcontract.Snapshot{
		Enabled:     false,
		Attribution: attributionName,
		Partnership: false,
		Coverage:    []string{"codex", "antigravity", "claude", "grok_bot"},
		News:        emptyItems,
		HotTopics:   emptyItems,
		Daily:       aifeedcontract.DailySection{Status: aifeedcontract.StatusDisabled},
		Resets:      resets,
	}
}

func listStatus(count int) string {
	if count == 0 {
		return aifeedcontract.StatusEmpty
	}
	return aifeedcontract.StatusOK
}

func schemaV1(value any) bool {
	switch typed := value.(type) {
	case float64:
		return typed == 1
	case int:
		return typed == 1
	case json.Number:
		return typed.String() == "1"
	default:
		return false
	}
}

func asString(value any) string {
	text, _ := value.(string)
	return strings.TrimSpace(text)
}

func truthy(value any) bool {
	flag, ok := value.(bool)
	return ok && flag
}

func formatTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339)
}

func clipHeader(value string) string {
	value = strings.TrimSpace(value)
	if len(value) > 128 {
		return value[:128]
	}
	return value
}

func retryAfter(header string, now time.Time) time.Duration {
	header = strings.TrimSpace(header)
	if header == "" {
		return 0
	}
	if seconds, err := strconv.Atoi(header); err == nil && seconds > 0 {
		return time.Duration(seconds) * time.Second
	}
	if when, err := http.ParseTime(header); err == nil && when.After(now) {
		return when.Sub(now)
	}
	return 0
}

var (
	scriptPattern = regexp.MustCompile(`(?is)<(script|style)[^>]*>.*?</(script|style)>`)
	tagPattern    = regexp.MustCompile(`<[^>]*>`)
)

func plain(value string, limit int) string {
	value = scriptPattern.ReplaceAllString(value, " ")
	value = tagPattern.ReplaceAllString(value, " ")
	value = strings.Map(func(r rune) rune {
		if r == '\n' || r == '\r' || r == '\t' {
			return ' '
		}
		if r < 32 {
			return -1
		}
		return r
	}, value)
	value = strings.Join(strings.Fields(value), " ")
	if limit > 0 && utf8.RuneCountInString(value) > limit {
		runes := []rune(value)
		value = strings.TrimSpace(string(runes[:limit]))
	}
	return value
}

func safeLink(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.User != nil || parsed.Host == "" {
		return ""
	}
	if parsed.Scheme != "https" && parsed.Scheme != "http" {
		return ""
	}
	return parsed.String()
}

func aihotLink(raw string) string {
	link := safeLink(raw)
	if link == "" {
		return ""
	}
	parsed, err := url.Parse(link)
	if err != nil || !strings.EqualFold(parsed.Hostname(), "aihot.news") {
		return ""
	}
	return link
}

func cloneCached(entry *cached) *cached {
	if entry == nil {
		return nil
	}
	cloned := *entry
	if entry.news != nil {
		cloned.news = append([]aifeedcontract.NewsItem(nil), entry.news...)
	}
	if entry.events != nil {
		cloned.events = append([]aifeedcontract.ResetEvent(nil), entry.events...)
	}
	return &cloned
}
