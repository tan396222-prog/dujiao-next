package application

import (
	"testing"
	"time"
)

func TestCryptoRewardStaysInsideInclusiveRange(t *testing.T) {
	source := CryptoReward{}
	assertBand(t, source, RewardBandDaily, DailyMinRewardCents, DailyMaxRewardCents)
	assertBand(t, source, RewardBandFirst, FirstMinRewardCents, FirstMaxRewardCents)
	if _, err := source.Cents(RewardBand(9)); err == nil {
		t.Fatal("unknown band should fail")
	}
}

func assertBand(t *testing.T, source CryptoReward, band RewardBand, min, max int64) {
	t.Helper()
	seen := map[int64]int{}
	for i := 0; i < 8000; i++ {
		cents, err := source.Cents(band)
		if err != nil {
			t.Fatal(err)
		}
		if cents < min || cents > max {
			t.Fatalf("band %d cents %d outside %d..%d", band, cents, min, max)
		}
		seen[cents]++
	}
	if seen[min] == 0 || seen[max] == 0 {
		t.Fatalf("band %d missing endpoint, %d=%d %d=%d", band, min, seen[min], max, seen[max])
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
