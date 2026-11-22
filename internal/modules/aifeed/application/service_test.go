package application

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	aifeedcontract "github.com/dujiao-next/internal/modules/aifeed/contract"
)

func TestProductionSourcesStayOnTheAllowlist(t *testing.T) {
	want := []string{
		"https://aihot.news/api/v1/items?mode=selected&window=24h&by=timeline&limit=20",
		"https://aihot.news/api/v1/hot-topics",
		"https://aihot.news/api/v1/dailies/latest",
		"https://aihot.news/api/v1/codex-resets",
		"https://aihot.news/api/v1/antigravity-resets",
		"https://aihot.news/api/v1/claude-resets",
		"https://aihot.news/api/v1/grok-bot-resets",
	}
	got := ProductionSourceURLs()
	if len(got) != len(want) {
		t.Fatalf("sources = %#v", got)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("source %d = %s", index, got[index])
		}
	}
}

func TestUnlistedHostIsNotDialed(t *testing.T) {
	dialed := atomic.Bool{}
	service := NewService(Options{
		Client: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			dialed.Store(true)
			return nil, errors.New("dialed")
		})},
		Sources: []source{{Key: "items", Kind: kindItems, URL: "http://127.0.0.1/latest"}},
	})
	snapshot := service.Snapshot(context.Background())
	if dialed.Load() {
		t.Fatal("unlisted host was dialed")
	}
	if snapshot.News.Status != aifeedcontract.StatusRefused {
		t.Fatalf("status = %s", snapshot.News.Status)
	}
}

func TestDisabledFeedDoesNotFetch(t *testing.T) {
	hits := atomic.Int32{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	service := testService(t, server.URL, func() bool { return false })
	snapshot := service.Snapshot(context.Background())
	if hits.Load() != 0 {
		t.Fatalf("hits = %d", hits.Load())
	}
	if snapshot.Enabled || snapshot.Partnership || snapshot.News.Status != aifeedcontract.StatusDisabled {
		t.Fatalf("snapshot = %+v", snapshot)
	}
}

func TestSanitizeXSSAndDropUnsafeLinks(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"schemaVersion": 1,
			"items": []map[string]any{{
				"title":   "<script>alert(1)</script>标题",
				"summary": "<img src=x onerror=alert(1)>短摘要",
				"source":  map[string]any{"name": "来源"},
				"links": map[string]any{
					"aihot":    "javascript:alert(1)",
					"original": "https://example.com/story",
				},
				"discoveredAt": "2026-11-21T01:02:03Z",
			}},
		})
	}))
	defer server.Close()
	snapshot := testService(t, server.URL, nil).Snapshot(context.Background())
	if len(snapshot.News.Items) != 1 {
		t.Fatalf("items = %#v", snapshot.News.Items)
	}
	item := snapshot.News.Items[0]
	if item.Title != "标题" || item.Summary != "短摘要" {
		t.Fatalf("text = %#v", item)
	}
	if item.FeedURL != "" || item.OriginalURL != "https://example.com/story" {
		t.Fatalf("links = %#v", item)
	}
}

func TestEmptyPayloadIsEmptyNotFabricated(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"schemaVersion":1,"items":[]}`))
	}))
	defer server.Close()
	snapshot := testService(t, server.URL, nil).Snapshot(context.Background())
	if snapshot.News.Status != aifeedcontract.StatusEmpty || len(snapshot.News.Items) != 0 {
		t.Fatalf("news = %+v", snapshot.News)
	}
}

func TestTimeoutDoesNotRetryImmediately(t *testing.T) {
	hits := atomic.Int32{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		time.Sleep(200 * time.Millisecond)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	service := testService(t, server.URL, nil)
	service.client.Timeout = 30 * time.Millisecond
	first := service.Snapshot(context.Background())
	second := service.Snapshot(context.Background())
	if hits.Load() != 1 {
		t.Fatalf("hits = %d", hits.Load())
	}
	if first.News.Status != aifeedcontract.StatusUnavailable || second.News.Status != aifeedcontract.StatusUnavailable {
		t.Fatalf("status %s / %s", first.News.Status, second.News.Status)
	}
}

func TestRateLimitHonorsRetryAfter(t *testing.T) {
	hits := atomic.Int32{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("Retry-After", "120")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer server.Close()
	service := testService(t, server.URL, nil)
	service.Snapshot(context.Background())
	service.Snapshot(context.Background())
	if hits.Load() != 1 {
		t.Fatalf("hits = %d", hits.Load())
	}
	if service.Snapshot(context.Background()).News.Status != aifeedcontract.StatusRateLimited {
		t.Fatal("status was not rate limited")
	}
}

func TestCacheStampedeAndNotModified(t *testing.T) {
	var hits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if r.Header.Get("If-None-Match") == `W/"feed"` {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		time.Sleep(80 * time.Millisecond)
		w.Header().Set("ETag", `W/"feed"`)
		_, _ = w.Write([]byte(`{"schemaVersion":1,"items":[{"title":"一条新闻","summary":"摘要","source":{"name":"源"},"links":{"aihot":"https://aihot.news/p/1","original":"https://example.com/a"},"discoveredAt":"2026-11-21T00:00:00Z"}]}`))
	}))
	defer server.Close()
	now := time.Date(2026, 11, 21, 0, 0, 0, 0, time.UTC)
	var clockMu sync.Mutex
	service := testService(t, server.URL, nil)
	service.now = func() time.Time {
		clockMu.Lock()
		defer clockMu.Unlock()
		return now
	}
	var group sync.WaitGroup
	for range 20 {
		group.Add(1)
		go func() {
			defer group.Done()
			service.Snapshot(context.Background())
		}()
	}
	group.Wait()
	if hits.Load() != 1 {
		t.Fatalf("stampede hits = %d", hits.Load())
	}
	clockMu.Lock()
	now = now.Add(2 * time.Millisecond)
	clockMu.Unlock()
	snapshot := service.Snapshot(context.Background())
	if hits.Load() != 2 {
		t.Fatalf("conditional hits = %d", hits.Load())
	}
	if snapshot.News.Status != aifeedcontract.StatusOK || len(snapshot.News.Items) != 1 || snapshot.News.Stale {
		t.Fatalf("news = %+v", snapshot.News)
	}
	if snapshot.News.Items[0].FeedURL != "https://aihot.news/p/1" {
		t.Fatalf("feed url = %s", snapshot.News.Items[0].FeedURL)
	}
}

func TestRedirectToAnotherHostIsNotFollowed(t *testing.T) {
	evilHits := atomic.Int32{}
	evil := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		evilHits.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer evil.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, evil.URL+"/secret", http.StatusFound)
	}))
	defer server.Close()
	snapshot := testService(t, server.URL, nil).Snapshot(context.Background())
	if evilHits.Load() != 0 {
		t.Fatal("followed redirect")
	}
	if snapshot.News.Status != aifeedcontract.StatusUnavailable {
		t.Fatalf("status = %s", snapshot.News.Status)
	}
}

func TestResetCalendarKeepsTypeAndDropsPosts(t *testing.T) {
	// Fixture, not a live capture. The dates exercise the public calendar rule.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{
			"schemaVersion":1,
			"checkedAt":"2026-11-22T20:23:00+08:00",
			"historyFrom":"2026-04-15T00:00:00.000+08:00",
			"events":[
				{
					"id":"card-confirmed",
					"type":"reset_credit",
					"title":"重置卡已发放",
					"label":"全员重置",
					"displayLabel":"Tibo 发重置卡",
					"status":"confirmed",
					"confirmedAt":"2026-11-01T17:18:00+08:00",
					"occurredOn":null,
					"schedule":{"precision":"deadline","from":"2026-11-02T00:00:00+08:00","through":"2026-11-02T00:00:00+08:00","label":"11-02"},
					"estimate":{"label":"模型窗口"},
					"presentation":{"status":"confirmed","scopeLabel":"Codex"},
					"posts":[{"author":"thsottiaux","text":"<script>alert(1)</script>帖子正文不该出现","originalText":"original post","url":"https://x.com/example/status/1","publishedAt":"2026-11-01T17:18:00+08:00"}],
					"url":"https://aihot.news/codex-reset"
				},
				{
					"id":"quota-occurred",
					"type":"direct_reset",
					"title":"额度重置已完成",
					"confirmedAt":"2026-11-20T17:18:00+08:00",
					"occurredOn":"2026-11-21",
					"schedule":{"precision":"date","from":"2026-11-20T16:00:00Z","through":"2026-11-21T15:59:00Z"},
					"presentation":{"status":"confirmed"},
					"posts":[{"publishedAt":"2026-11-22T01:00:00Z","text":"另一段正文"}]
				},
				{
					"id":"published-only",
					"type":"direct_reset",
					"status":"announced",
					"presentation":{"status":"likely_completed"},
					"posts":[{"publishedAt":"2026-10-23T09:00:00+08:00","text":"只有发布日"}]
				},
				{
					"id":"wide-window",
					"type":"direct_reset",
					"occurredOn":"2026-02-31",
					"schedule":{"precision":"window","from":"2026-11-01T00:00:00+08:00","through":"2026-11-20T00:00:00+08:00"},
					"presentation":{"status":"announced"}
				},
				{
					"id":"no-type",
					"title":"维护公告",
					"confirmedAt":"2026-11-18T09:00:00+08:00",
					"posts":[{"text":"这不是重置"}]
				},
				{
					"id":"expired",
					"type":"reset_credit",
					"presentation":{"status":"expired_unconfirmed"},
					"schedule":{"from":"2026-11-09T08:00:00+08:00","through":"2026-11-09T12:00:00+08:00"}
				}
			]
		}`))
	}))
	defer server.Close()
	service := resetTestService(t, server.URL, time.Minute)
	section := service.Snapshot(context.Background()).Resets[0]
	if len(section.Events) != 4 {
		t.Fatalf("events = %+v", section.Events)
	}
	want := []aifeedcontract.ResetEvent{
		{Kind: aifeedcontract.ResetKindCard, Status: aifeedcontract.ResetStatusEffective, Date: "2026-11-01"},
		{Kind: aifeedcontract.ResetKindQuota, Status: aifeedcontract.ResetStatusEffective, Date: "2026-11-21"},
		{Kind: aifeedcontract.ResetKindQuota, Status: aifeedcontract.ResetStatusEffective, Date: "2026-10-23"},
		{Kind: aifeedcontract.ResetKindCard, Status: aifeedcontract.ResetStatusUnconfirmed, Date: "2026-11-09"},
	}
	for index, event := range want {
		if section.Events[index] != event {
			t.Fatalf("event %d = %+v", index, section.Events[index])
		}
	}
	raw, _ := json.Marshal(section)
	text := string(raw)
	for _, leaked := range []string{"帖子正文", "original post", "x.com", "thsottiaux", "alert", "aihot.news", "模型窗口", "全员重置", "维护公告", "page_url", "title", "summary"} {
		if contains(text, leaked) {
			t.Fatalf("calendar payload leaked %q: %s", leaked, text)
		}
	}
}

func TestResetHistoryMergesShortWindowAndKeepsFailure(t *testing.T) {
	now := time.Date(2026, 11, 22, 15, 0, 0, 0, time.FixedZone("Asia/Shanghai", 8*60*60))
	var phase atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch phase.Add(1) {
		case 1:
			_, _ = w.Write([]byte(`{
				"schemaVersion":1,
				"checkedAt":"2026-11-22T15:00:00+08:00",
				"historyFrom":"2026-04-15T00:00:00.000+08:00",
				"events":[
					{"id":"old","type":"direct_reset","occurredOn":"2026-08-11","presentation":{"status":"confirmed"}},
					{"id":"keep","type":"reset_credit","occurredOn":"2026-09-23","presentation":{"status":"confirmed"}}
				]
			}`))
		case 2:
			_, _ = w.Write([]byte(`{
				"schemaVersion":1,
				"checkedAt":"2026-11-22T15:10:00+08:00",
				"historyFrom":"2026-11-15T00:00:00.000+08:00",
				"events":[
					{"id":"keep","type":"reset_credit","occurredOn":"2026-09-23","presentation":{"status":"confirmed"}},
					{"id":"new","type":"direct_reset","occurredOn":"2026-11-21","presentation":{"status":"confirmed"}}
				]
			}`))
		default:
			w.WriteHeader(http.StatusServiceUnavailable)
		}
	}))
	defer server.Close()
	service := NewService(Options{
		Now: func() time.Time { return now },
		Sources: []source{{
			Key: "codex", Kind: kindReset, Provider: "codex", URL: server.URL, TTL: 0,
		}},
	})
	service.allowTestHosts = true
	first := service.Snapshot(context.Background()).Resets[0].Events
	if len(first) != 2 || first[0].Date != "2026-08-11" {
		t.Fatalf("full snapshot = %+v", first)
	}
	second := service.Snapshot(context.Background()).Resets[0].Events
	if len(second) != 3 || second[0].Date != "2026-08-11" || second[2].Date != "2026-11-21" {
		t.Fatalf("short window dropped history: %+v", second)
	}
	third := service.Snapshot(context.Background()).Resets[0]
	if !third.Stale || len(third.Events) != 3 || third.Events[0].Date != "2026-08-11" {
		t.Fatalf("failure dropped history: %+v", third)
	}
}

func TestFullSnapshotRemovesWithdrawnEvent(t *testing.T) {
	var phase atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if phase.Add(1) == 1 {
			_, _ = w.Write([]byte(`{
				"schemaVersion":1,"historyFrom":"2026-04-15T00:00:00.000+08:00",
				"events":[
					{"id":"gone","type":"direct_reset","occurredOn":"2026-08-01","presentation":{"status":"confirmed"}},
					{"id":"stay","type":"reset_credit","occurredOn":"2026-09-04","presentation":{"status":"confirmed"}}
				]
			}`))
			return
		}
		_, _ = w.Write([]byte(`{
			"schemaVersion":1,"historyFrom":"2026-04-15T00:00:00.000+08:00",
			"events":[
				{"id":"stay","type":"reset_credit","occurredOn":"2026-09-04","presentation":{"status":"confirmed"}}
			]
		}`))
	}))
	defer server.Close()
	service := resetTestService(t, server.URL, 0)
	if events := service.Snapshot(context.Background()).Resets[0].Events; len(events) != 2 {
		t.Fatalf("first = %+v", events)
	}
	events := service.Snapshot(context.Background()).Resets[0].Events
	if len(events) != 1 || events[0].Kind != aifeedcontract.ResetKindCard || events[0].Date != "2026-09-04" {
		t.Fatalf("withdrawn event remained: %+v", events)
	}
}

func TestLiveAllowlistedSchema(t *testing.T) {
	if os.Getenv("AIHOT_LIVE") != "1" {
		t.Skip("set AIHOT_LIVE=1 to call the public AIHOT API")
	}
	service := NewService(Options{})
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	snapshot := service.Snapshot(ctx)
	if snapshot.Partnership {
		t.Fatal("partnership flag must stay false")
	}
	if len(snapshot.Coverage) != 4 || len(snapshot.Resets) != 4 {
		t.Fatalf("coverage = %#v resets = %d", snapshot.Coverage, len(snapshot.Resets))
	}
	for _, section := range []string{snapshot.News.Status, snapshot.HotTopics.Status, snapshot.Daily.Status} {
		if section == aifeedcontract.StatusRefused {
			t.Fatalf("upstream refused the anonymous read: %s", section)
		}
	}
	for _, reset := range snapshot.Resets {
		if reset.Status == aifeedcontract.StatusRefused {
			t.Fatalf("reset %s refused", reset.Provider)
		}
		if reset.Status == aifeedcontract.StatusOK && len(reset.Events) == 0 {
			t.Fatalf("reset %s claimed data without events", reset.Provider)
		}
	}
	if snapshot.News.Status == aifeedcontract.StatusOK {
		item := snapshot.News.Items[0]
		if item.Title == "" || (item.FeedURL == "" && item.OriginalURL == "") {
			t.Fatalf("live item missing title or link: %+v", item)
		}
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}

func resetTestService(t *testing.T, rawURL string, ttl time.Duration) *Service {
	t.Helper()
	service := NewService(Options{Sources: []source{{
		Key: "codex", Kind: kindReset, Provider: "codex", URL: rawURL, TTL: ttl,
	}}})
	service.allowTestHosts = true
	return service
}

func testService(t *testing.T, rawURL string, enabled func() bool) *Service {
	t.Helper()
	service := NewService(Options{
		Enabled: enabled,
		Sources: []source{{Key: "items", Kind: kindItems, URL: rawURL, TTL: time.Millisecond, MinGap: time.Minute}},
	})
	service.allowTestHosts = true
	return service
}

func contains(text, part string) bool {
	return len(part) > 0 && len(text) >= len(part) && (func() bool { return stringIndex(text, part) >= 0 })()
}

func stringIndex(text, part string) int {
	for index := 0; index+len(part) <= len(text); index++ {
		if text[index:index+len(part)] == part {
			return index
		}
	}
	return -1
}
