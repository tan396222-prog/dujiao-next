package contract

// Snapshot is the storefront view of public AI news and reset notices.
// It is a short summary, not a copy of the upstream API.
type Snapshot struct {
	Enabled     bool           `json:"enabled"`
	Attribution string         `json:"attribution"`
	Partnership bool           `json:"partnership"`
	Coverage    []string       `json:"coverage"`
	News        ItemSection    `json:"news"`
	HotTopics   ItemSection    `json:"hot_topics"`
	Daily       DailySection   `json:"daily"`
	Resets      []ResetSection `json:"resets"`
}

// ItemSection is a cached news or hot-topic list.
type ItemSection struct {
	Status    string     `json:"status"`
	Stale     bool       `json:"stale"`
	FetchedAt string     `json:"fetched_at,omitempty"`
	Items     []NewsItem `json:"items"`
}

// NewsItem is one public headline. Summary is clipped. Links are http(s) only.
type NewsItem struct {
	Title       string `json:"title"`
	Summary     string `json:"summary,omitempty"`
	Source      string `json:"source,omitempty"`
	OriginalURL string `json:"original_url,omitempty"`
	FeedURL     string `json:"feed_url,omitempty"`
	UpdatedAt   string `json:"updated_at,omitempty"`
}

// DailySection is the latest public daily lead, not the full report.
type DailySection struct {
	Status    string `json:"status"`
	Stale     bool   `json:"stale"`
	FetchedAt string `json:"fetched_at,omitempty"`
	Date      string `json:"date,omitempty"`
	Title     string `json:"title,omitempty"`
	Summary   string `json:"summary,omitempty"`
	FeedURL   string `json:"feed_url,omitempty"`
	UpdatedAt string `json:"updated_at,omitempty"`
}

// ResetSection is one provider's calendar facts. It has no post text, author,
// platform, or source entry. Those stay in the unexported cache for dedup.
type ResetSection struct {
	Provider  string       `json:"provider"`
	Status    string       `json:"status"`
	Stale     bool         `json:"stale"`
	FetchedAt string       `json:"fetched_at,omitempty"`
	CheckedAt string       `json:"checked_at,omitempty"`
	Events    []ResetEvent `json:"events"`
}

// ResetEvent is one calendar fact: a Beijing date, the event type, and a
// factual status. quota_reset and reset_credit are never interchangeable.
// effective is an occurredOn day on or before today in Asia/Shanghai.
// Confirmation posts, publication times, and schedule windows are not emitted.
type ResetEvent struct {
	Kind   string `json:"kind"`
	Status string `json:"status"`
	Date   string `json:"date"`
}

const (
	ResetKindQuota = "quota_reset"
	ResetKindCard  = "reset_credit"

	ResetStatusEffective   = "effective"
	ResetStatusAnnounced   = "announced"
	ResetStatusUnconfirmed = "unconfirmed"
)

const (
	StatusOK          = "ok"
	StatusEmpty       = "empty"
	StatusUnavailable = "unavailable"
	StatusRateLimited = "rate_limited"
	StatusRefused     = "refused"
	StatusDisabled    = "disabled"
)
