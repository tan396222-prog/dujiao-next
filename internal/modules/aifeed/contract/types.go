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

// ResetSection is one provider's published reset notices.
type ResetSection struct {
	Provider  string       `json:"provider"`
	PageURL   string       `json:"page_url,omitempty"`
	Status    string       `json:"status"`
	Stale     bool         `json:"stale"`
	FetchedAt string       `json:"fetched_at,omitempty"`
	CheckedAt string       `json:"checked_at,omitempty"`
	Events    []ResetEvent `json:"events"`
}

// ResetEvent is one published announcement. Estimated times stay labeled.
type ResetEvent struct {
	Title       string `json:"title"`
	Label       string `json:"label,omitempty"`
	Summary     string `json:"summary,omitempty"`
	Original    string `json:"original,omitempty"`
	AnnouncedAt string `json:"announced_at,omitempty"`
	ConfirmedAt string `json:"confirmed_at,omitempty"`
	Schedule    string `json:"schedule,omitempty"`
	Estimate    string `json:"estimate,omitempty"`
	Estimated   bool   `json:"estimated"`
	OriginalURL string `json:"original_url,omitempty"`
	FeedURL     string `json:"feed_url,omitempty"`
}

const (
	StatusOK          = "ok"
	StatusEmpty       = "empty"
	StatusUnavailable = "unavailable"
	StatusRateLimited = "rate_limited"
	StatusRefused     = "refused"
	StatusDisabled    = "disabled"
)
