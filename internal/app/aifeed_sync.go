package app

import "context"

// aiFeedRunner is the one background refresh for the public news cache.
type aiFeedRunner interface {
	Run(context.Context) error
}

type aiFeedSync struct {
	feed aiFeedRunner
}

func newAIFeedSync(feed aiFeedRunner) *aiFeedSync {
	return &aiFeedSync{feed: feed}
}

func (s *aiFeedSync) Name() string {
	return "ai-feed-sync"
}

// Start blocks until the process context is cancelled.
func (s *aiFeedSync) Start(ctx context.Context) error {
	if s == nil || s.feed == nil {
		if ctx != nil {
			<-ctx.Done()
		}
		return nil
	}
	return s.feed.Run(ctx)
}

// Stop returns immediately. Cancelling the start context ends the loop.
func (s *aiFeedSync) Stop(context.Context) error {
	return nil
}
