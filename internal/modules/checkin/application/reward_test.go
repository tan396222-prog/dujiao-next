package application

import (
	"testing"
	"time"
)

func TestCryptoRewardStaysInsideInclusiveRange(t *testing.T) {
	seen := map[int64]int{}
	source := CryptoReward{}
	for i := 0; i < 4000; i++ {
		cents, err := source.Cents()
		if err != nil {
			t.Fatal(err)
		}
		if cents < MinRewardCents || cents > MaxRewardCents {
			t.Fatalf("cents %d outside 10..80", cents)
		}
		seen[cents]++
	}
	if seen[MinRewardCents] == 0 || seen[MaxRewardCents] == 0 {
		t.Fatalf("expected both endpoints, got 10=%d 80=%d", seen[MinRewardCents], seen[MaxRewardCents])
	}
}

func TestBeijingDateUsesShanghaiMidnight(t *testing.T) {
	before := time.Date(2026, 11, 22, 15, 59, 30, 0, time.UTC)
	after := time.Date(2026, 11, 22, 16, 0, 30, 0, time.UTC)
	if got := BeijingDate(before); got != "2026-11-22" {
		t.Fatalf("before midnight: %s", got)
	}
	if got := BeijingDate(after); got != "2026-11-23" {
		t.Fatalf("after midnight: %s", got)
	}
	if got := NextBeijingDate("2026-11-22"); got != "2026-11-23" {
		t.Fatalf("next date: %s", got)
	}
	if got := NextBeijingDate("2026-02-31"); got != "" {
		t.Fatalf("invalid date should not invent a day: %s", got)
	}
}
