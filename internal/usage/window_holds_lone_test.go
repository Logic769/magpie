package usage

import (
	"testing"
	"time"

	"github.com/yetone/magpie/internal/catalog"
	"github.com/yetone/magpie/internal/provider"
)

// windowHoldsOf reckons qs' windows' wholes over the calls made, magpie's log
// beginning before the first window does.
func windowHoldsOf(qs []provider.SubscriptionQuota, now time.Time, calls []Record) []provider.SubscriptionQuota {
	return windowHolds(qs, now, now.Add(-24*time.Hour), func(Record) *catalog.Price { return nil }, provider.Renames{}, nil,
		func(fn func(Record)) {
			for _, r := range calls {
				fn(r)
			}
		})
}

// #4507: a plan provider with one key is carded without a user name, and a
// call made on that key names no account — only the key, when it has a name.
// That one card is still the key's, so its whole is reckoned from those calls.
func TestWindowHoldsLoneKey(t *testing.T) {
	now := time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)
	reset := now.Add(3 * time.Hour) // the window began two hours ago
	qs := []provider.SubscriptionQuota{{
		Provider: "kimicode",
		Windows:  []provider.QuotaWindow{{Name: "5 hours", Used: 25, ResetsAt: &reset, ResetSecs: 5 * 3600}},
	}}
	calls := []Record{
		{Provider: "kimicode", Model: "kimi-k3", Time: now.Add(-time.Hour), Input: 1000, Output: 200},
		{Provider: "kimicode", Model: "kimi-k3", Time: now.Add(-time.Hour), ProviderKeyName: "kimi-plan", Input: 500, Output: 100},
	}
	out := windowHoldsOf(qs, now, calls)
	h := out[0].Windows[0].Holds
	if h == nil {
		t.Fatalf("a lone key's card got no whole, why %q", out[0].Windows[0].HoldsWhy)
	}
	if h.Routed.Calls != 2 {
		t.Errorf("counted %d calls, want the lone key's 2", h.Routed.Calls)
	}
	if out[0].Windows[0].HoldsWhy != "" {
		t.Errorf("why %q, want a whole", out[0].Windows[0].HoldsWhy)
	}
}

// The same unnamed card beside a named one of its provider is not the only
// key, so it takes no calls: those name the account they went out as.
func TestWindowHoldsUnnamedCardBesideNamed(t *testing.T) {
	now := time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)
	reset := now.Add(3 * time.Hour)
	window := provider.QuotaWindow{Name: "5 hours", Used: 25, ResetsAt: &reset, ResetSecs: 5 * 3600}
	qs := []provider.SubscriptionQuota{
		{Provider: "kimicode", User: "kimi-plan", Windows: []provider.QuotaWindow{window}},
		{Provider: "kimicode", Windows: []provider.QuotaWindow{window}},
	}
	calls := []Record{{Provider: "kimicode", Model: "kimi-k3", Time: now.Add(-time.Hour), ProviderKeyName: "kimi-plan", Input: 1000, Output: 200}}
	out := windowHoldsOf(qs, now, calls)
	if h := out[0].Windows[0].Holds; h == nil || h.Routed.Calls != 1 {
		t.Errorf("the named card: %+v, want the call on its key", h)
	}
	if h := out[1].Windows[0].Holds; h != nil {
		t.Errorf("the unnamed card took calls beside a named one: %+v", h)
	}
}

// #2010 on the same card: a lone key that ran dry is still its card's, so a
// call the vendor answered after a reading of none left may be credits', and
// the window's whole is not reckoned from it.
func TestWindowHoldsLoneKeyDryRun(t *testing.T) {
	now := time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)
	reset := now.Add(3 * time.Hour)
	qs := []provider.SubscriptionQuota{{
		Provider: "kimicode",
		Windows:  []provider.QuotaWindow{{Name: "5 hours", Used: 100, ResetsAt: &reset, ResetSecs: 5 * 3600}},
	}}
	hist := []provider.QuotaHistory{{Provider: "kimicode", Lines: []provider.QuotaLine{{Name: "5 hours", Points: []provider.QuotaPoint{
		{At: now.Add(-3 * time.Hour), Left: 40},
		{At: now.Add(-90 * time.Minute), Left: 0},
	}}}}}
	calls := []Record{
		{Provider: "kimicode", Model: "kimi-k3", Time: now.Add(-2 * time.Hour), ProviderKeyName: "kimi-plan", Input: 1000, Output: 200, Status: 200},
		{Provider: "kimicode", Model: "kimi-k3", Time: now.Add(-time.Hour), ProviderKeyName: "kimi-plan", Input: 900000, Output: 1000, Status: 200},
	}
	got := windowHolds(qs, now, now.Add(-24*time.Hour), func(Record) *catalog.Price { return nil }, provider.Renames{},
		dryRuns(qs, hist, now), func(fn func(Record)) {
			for _, r := range calls {
				fn(r)
			}
		})
	if w := got[0].Windows[0]; w.Holds != nil || w.HoldsWhy != provider.HoldsCredits {
		t.Fatalf("a lone key's window read dry: whole %+v, why %q, want credits", w.Holds, w.HoldsWhy)
	}
}

// A card no window of which is ever reckoned is no other card: a lone key
// beside a failed one is still lone (#4507).
func TestWindowHoldsLoneKeyBesideFailedCard(t *testing.T) {
	now := time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)
	reset := now.Add(3 * time.Hour)
	window := provider.QuotaWindow{Name: "5 hours", Used: 25, ResetsAt: &reset, ResetSecs: 5 * 3600}
	qs := []provider.SubscriptionQuota{
		{Provider: "kimicode", Windows: []provider.QuotaWindow{window}},
		{Provider: "kimicode", User: "other@example.com", Error: "the key was turned away", Windows: []provider.QuotaWindow{window}},
	}
	calls := []Record{{Provider: "kimicode", Model: "kimi-k3", Time: now.Add(-time.Hour), ProviderKeyName: "kimi-plan", Input: 1000, Output: 200}}
	out := windowHoldsOf(qs, now, calls)
	if h := out[0].Windows[0].Holds; h == nil || h.Routed.Calls != 1 {
		t.Errorf("the lone key beside a failed card: %+v, want the call on its key", h)
	}
}

// #4507 in a past cycle: the historical range counts a lone key's card too,
// one snapshot per window, unnamed as the card is.
func TestHistoricalWindowHoldsLoneKey(t *testing.T) {
	holdsHome(t)
	now := time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)
	from, to := now.Add(-13*24*time.Hour), now.Add(-6*24*time.Hour)
	at := to.Add(-2 * time.Hour)
	start, reset := at.Add(-2*time.Hour), to.Add(-time.Hour)
	Append(Record{Time: from.Add(-time.Hour), Provider: "kimicode", Model: "kimi-k3", Input: 1, Status: 200})
	Append(Record{Time: at.Add(-time.Hour), Provider: "kimicode", Model: "kimi-k3", ProviderKeyName: "kimi-plan", Input: 2200, Status: 200})
	hist := []provider.QuotaHistory{{Provider: "kimicode", Lines: []provider.QuotaLine{{Name: "5 hours", Points: []provider.QuotaPoint{
		{At: at, Left: 80, Start: &start, ResetsAt: &reset},
	}}}}}
	cards := []provider.SubscriptionQuota{{Provider: "kimicode", Windows: []provider.QuotaWindow{{Name: "5 hours", Used: 1, ResetsAt: &reset}}}}
	got := HistoricalWindowHolds(cards, hist, from, to)
	if len(got) != 1 {
		t.Fatalf("a lone key's past cycle: %+v", got)
	}
	if h := got[0].Windows[0].Holds; h == nil || h.Routed.Calls != 1 {
		t.Errorf("the lone key's past cycle: %+v, want the call on its key", h)
	}
}
