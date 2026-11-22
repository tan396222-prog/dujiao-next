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
	"sync/atomic"
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
	maxBodyBytes    = 1 << 20
	requestTimeout  = 8 * time.Second
	titleRunes      = 120
	summaryRunes    = 160
	newsLimit       = 12
	hotLimit        = 8
	resetLimit      = 400
	refusalHold     = 6 * time.Hour
	backoffCap      = time.Hour
	resetSyncFloor  = 10 * time.Minute
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
	// Wait replaces the sync sleep in tests. Production leaves it nil.
	Wait func(context.Context, time.Duration) bool
}

// Service caches allowlisted public responses and never accepts a caller-supplied URL.
type Service struct {
	now            func() time.Time
	client         *http.Client
	enabled        func() bool
	waitFn         func(context.Context, time.Duration) bool
	sources        []source
	allowTestHosts bool
	running        atomic.Bool

	mu      sync.Mutex
	entries map[string]*cached
	flights map[string]*flight
}

type flight struct {
	wg sync.WaitGroup
}

type cached struct {
	status      string
	stale       bool
	fetchedAt   time.Time
	readyAt     time.Time
	etag        string
	failures    int
	checkedAt   string
	news        []aifeedcontract.NewsItem
	daily       aifeedcontract.DailySection
	resets      []resetRecord
	historyFrom string
}

// resetRecord keeps the upstream id and canonical URL for dedup and correction.
// Neither field is copied into the storefront snapshot.
type resetRecord struct {
	id        string
	sourceURL string
	event     aifeedcontract.ResetEvent
}

func defaultSources() []source {
	newsTTL := 5 * time.Minute
	newsGap := 60 * time.Second
	resetTTL := 10 * time.Minute
	return []source{
		{Key: "items", Kind: kindItems, URL: "https://aihot.news/api/v1/items?mode=selected&window=24h&by=timeline&limit=20", TTL: newsTTL, MinGap: newsGap},
		{Key: "hot", Kind: kindHot, URL: "https://aihot.news/api/v1/hot-topics", TTL: newsTTL, MinGap: newsGap},
		{Key: "daily", Kind: kindDaily, URL: "https://aihot.news/api/v1/dailies/latest", TTL: newsTTL, MinGap: newsGap},
		{Key: "codex", Kind: kindReset, Provider: "codex", PageURL: "https://aihot.news/codex-reset", URL: "https://aihot.news/api/v1/codex-resets", TTL: resetTTL, MinGap: resetTTL},
		{Key: "antigravity", Kind: kindReset, Provider: "antigravity", PageURL: "https://aihot.news/antigravity-reset", URL: "https://aihot.news/api/v1/antigravity-resets", TTL: resetTTL, MinGap: resetTTL},
		{Key: "claude", Kind: kindReset, Provider: "claude", PageURL: "https://aihot.news/claude-reset", URL: "https://aihot.news/api/v1/claude-resets", TTL: resetTTL, MinGap: resetTTL},
		{Key: "grok_bot", Kind: kindReset, Provider: "grok_bot", PageURL: "https://aihot.news/grok-bot-reset", URL: "https://aihot.news/api/v1/grok-bot-resets", TTL: resetTTL, MinGap: resetTTL},
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
		waitFn:  opts.Wait,
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

// Run refreshes the allowlisted sources until ctx is cancelled. One loop is
// enough for the process: a second call waits on ctx and does not fetch.
// The wait is never shorter than 10 minutes, and a 429 Retry-After that is
// longer wins. Disable skips the fetch but leaves the loop stoppable.
func (s *Service) Run(ctx context.Context) error {
	if s == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if !s.running.CompareAndSwap(false, true) {
		<-ctx.Done()
		return nil
	}
	defer s.running.Store(false)
	for {
		if ctx.Err() != nil {
			return nil
		}
		if s.enabled == nil || s.enabled() {
			s.sync(ctx)
		}
		if !s.wait(ctx, s.nextSyncDelay()) {
			return nil
		}
	}
}

func (s *Service) sync(ctx context.Context) {
	var group sync.WaitGroup
	for _, src := range s.sources {
		group.Add(1)
		go func(src source) {
			defer group.Done()
			s.load(ctx, src)
		}(src)
	}
	group.Wait()
}

func (s *Service) nextSyncDelay() time.Duration {
	delay := resetSyncFloor
	now := s.now()
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, src := range s.sources {
		if src.Kind != kindReset {
			continue
		}
		entry := s.entries[src.Key]
		if entry == nil || entry.readyAt.IsZero() {
			continue
		}
		if wait := entry.readyAt.Sub(now); wait > delay {
			delay = wait
		}
	}
	return delay
}

func (s *Service) wait(ctx context.Context, delay time.Duration) bool {
	if s.waitFn != nil {
		return s.waitFn(ctx, delay)
	}
	if delay < 0 {
		delay = 0
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
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
		parsedBody, ok := parseSource(src, body, shanghaiDate(s.now()))
		if !ok {
			logger.Warnw("ai_feed_schema_rejected", "source", src.Key)
			return hold(previous, aifeedcontract.StatusUnavailable, s.backoff(previous, src, 0))
		}
		if src.Kind == kindReset {
			parsedBody.resets = mergeResetRecords(previous, parsedBody, s.now())
			parsedBody.status = listStatus(len(parsedBody.resets))
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
	} else if wait > backoffCap {
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
	return len(entry.news) > 0 || len(entry.resets) > 0 || entry.daily.Title != "" || entry.daily.Summary != ""
}

func okStatus(entry *cached) string {
	if entry == nil || !hasPayload(entry) {
		return aifeedcontract.StatusEmpty
	}
	return aifeedcontract.StatusOK
}

func parseSource(src source, body []byte, today string) (*cached, bool) {
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
		records, checkedAt, historyFrom, ok := parseResets(payload, today)
		if !ok {
			return nil, false
		}
		return &cached{status: listStatus(len(records)), resets: records, checkedAt: checkedAt, historyFrom: historyFrom}, true
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

func parseResets(payload map[string]any, today string) ([]resetRecord, string, string, bool) {
	rows, ok := payload["events"].([]any)
	if !ok {
		return nil, "", "", false
	}
	records := make([]resetRecord, 0, min(len(rows), resetLimit))
	for _, row := range rows {
		if len(records) >= resetLimit {
			break
		}
		record, ok := row.(map[string]any)
		if !ok {
			continue
		}
		kind := calendarKind(asString(record["type"]))
		day := occurredOnDay(record, today)
		if kind == "" || day == "" {
			continue
		}
		records = append(records, resetRecord{
			id:        asString(record["id"]),
			sourceURL: asString(record["url"]),
			event: aifeedcontract.ResetEvent{
				Kind:   kind,
				Status: aifeedcontract.ResetStatusEffective,
				Date:   day,
			},
		})
	}
	return records, asString(payload["checkedAt"]), asString(payload["historyFrom"]), true
}

func calendarKind(raw string) string {
	switch raw {
	case "direct_reset":
		return aifeedcontract.ResetKindQuota
	case "reset_credit":
		return aifeedcontract.ResetKindCard
	default:
		return ""
	}
}

// occurredOnDay keeps only the upstream's separately verified Beijing day.
// confirmedAt is the confirmation post time, not an execution time. schedule
// is the original estimate and is never an execution receipt. publishedAt,
// createdAt, updatedAt, and checkedAt are record times. A day after today is
// not circled.
func occurredOnDay(record map[string]any, today string) string {
	day := beijingCalendarDate(asString(record["occurredOn"]))
	if day == "" || today == "" || day > today {
		return ""
	}
	return day
}

// recentResetWindow is the short poll payload. Its historyFrom starts inside
// the last two weeks. A full snapshot's historyFrom is the archive start and
// replaces the previous list, so a withdrawn event can disappear.
func recentResetWindow(raw string, now time.Time) bool {
	parsed, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(raw))
	if err != nil {
		return false
	}
	age := now.Sub(parsed)
	return age >= 0 && age < 14*24*time.Hour
}

func mergeResetRecords(previous, next *cached, now time.Time) []resetRecord {
	if next == nil {
		return nil
	}
	if previous == nil || !recentResetWindow(next.historyFrom, now) {
		return capResetRecords(next.resets)
	}
	merged := make(map[string]resetRecord, len(previous.resets)+len(next.resets))
	order := make([]string, 0, len(previous.resets)+len(next.resets))
	add := func(record resetRecord) {
		key := resetRecordKey(record)
		if _, ok := merged[key]; !ok {
			order = append(order, key)
		}
		merged[key] = record
	}
	for _, record := range previous.resets {
		add(record)
	}
	for _, record := range next.resets {
		add(record)
	}
	out := make([]resetRecord, 0, len(order))
	for _, key := range order {
		out = append(out, merged[key])
	}
	return capResetRecords(out)
}

func resetRecordKey(record resetRecord) string {
	if record.id != "" {
		return record.id
	}
	return record.event.Kind + "|" + record.event.Date + "|" + record.event.Status
}

func capResetRecords(records []resetRecord) []resetRecord {
	if len(records) > resetLimit {
		return records[:resetLimit]
	}
	return records
}

var shanghai = time.FixedZone("Asia/Shanghai", 8*60*60)

// beijingCalendarDate accepts only a YYYY-MM-DD day. A timestamp is not a day.
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

func shanghaiDate(now time.Time) string {
	if now.IsZero() {
		return ""
	}
	return now.In(shanghai).Format("2006-01-02")
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
	events := make([]aifeedcontract.ResetEvent, 0, len(entry.resets))
	for _, record := range entry.resets {
		events = append(events, record.event)
	}
	return aifeedcontract.ResetSection{
		Provider:  src.Provider,
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
	if entry.resets != nil {
		cloned.resets = append([]resetRecord(nil), entry.resets...)
	}
	return &cloned
}
