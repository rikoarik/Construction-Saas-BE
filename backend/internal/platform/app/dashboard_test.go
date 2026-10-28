package app

import (
	"context"
	"testing"
	"time"
)

func TestPeriodForUTCAndValidation(t *testing.T) {
	now := time.Date(2026, 3, 31, 14, 30, 0, 0, time.FixedZone("offset", 7*3600))
	for _, tc := range []struct {
		input    string
		expected string
	}{{"7d", "2026-03-24T07:30:00Z"}, {"30d", "2026-03-01T07:30:00Z"}, {"90d", "2025-12-31T07:30:00Z"}, {"12m", "2025-03-31T07:30:00Z"}} {
		p, err := PeriodFor(tc.input, now)
		if err != nil {
			t.Fatal(err)
		}
		if p.Start.Format(time.RFC3339) != tc.expected || p.End.Format(time.RFC3339) != "2026-03-31T07:30:00Z" {
			t.Fatalf("%s: %+v", tc.input, p)
		}
	}
	if _, err := PeriodFor("yesterday", now); err != ErrInvalid {
		t.Fatal("invalid range accepted")
	}
}
func TestStatusPercentages(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status Status
		want   [3]float64
	}{
		{name: "mixed", status: Status{Active: 2, Suspended: 1, Archived: 1, Total: 4}, want: [3]float64{50, 25, 25}},
		{name: "empty database", status: Status{}, want: [3]float64{0, 0, 0}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.status.WithPercentages()
			if actual := [3]float64{got.ActivePercent, got.SuspendedPercent, got.ArchivedPercent}; actual != tc.want {
				t.Fatalf("status percentages = %v, want %v", actual, tc.want)
			}
		})
	}
}

func TestUnavailableIsNotZero(t *testing.T) {
	s := Service{Read: stubRepository{storage: Storage{QuotaState: "unknown"}}}
	storage, err := s.Storage(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if storage.UsedBytes != nil || storage.QuotaBytes != nil || storage.QuotaState != "unknown" {
		t.Fatalf("storage: %+v", storage)
	}
	// Unmetered traffic is unavailable, never a confirmed zero.
	if storage.APIRequests != nil {
		t.Fatalf("api requests: %+v", storage.APIRequests)
	}
	for _, tc := range []struct{ limit, offset int }{{0, 0}, {101, 0}, {5, -1}, {5, 10001}} {
		if validPage(tc.limit, tc.offset) {
			t.Fatalf("accepted %+v", tc)
		}
	}
}

func TestStorageReportsMeasuredRequests(t *testing.T) {
	used, limit, storageLimit := int64(9), int64(100), int64(1000000000000)
	s := Service{Read: stubRepository{storage: Storage{QuotaState: "unknown", APIRequests: &APIRequestUsage{Used: &used}}}, APIRequestQuota: &limit, StorageQuota: &storageLimit}
	got, err := s.Storage(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got.APIRequests == nil || got.APIRequests.Used == nil || *got.APIRequests.Used != used || got.APIRequests.Limit == nil || *got.APIRequests.Limit != limit {
		t.Fatalf("storage usage: %+v", got)
	}
	if got.UsedBytes != nil || got.QuotaBytes == nil || *got.QuotaBytes != storageLimit || got.QuotaState != "limited" {
		t.Fatalf("configured storage quota: %+v", got)
	}
}

// stubRepository embeds the interface so only the method under test is needed.
type stubRepository struct {
	Repository
	distribution Distribution
	storage      Storage
}

func (s stubRepository) Distribution(context.Context) (Distribution, error) {
	return s.distribution, nil
}

func (s stubRepository) Storage(context.Context) (Storage, error) {
	return s.storage, nil
}

func TestDistributionReadsRepository(t *testing.T) {
	// An empty catalog is a confirmed empty distribution, not an unavailable one:
	// availability comes from the repository, so the widget can offer its empty
	// state instead of a "data unavailable" message.
	empty := Service{Read: stubRepository{distribution: Distribution{Items: []PlanCount{}, Available: true}}}
	got, err := empty.Distribution(context.Background())
	if err != nil || !got.Available || got.Items == nil || len(got.Items) != 0 {
		t.Fatalf("empty distribution: %+v %v", got, err)
	}
	s := Service{Read: stubRepository{distribution: Distribution{Available: true, Items: []PlanCount{{Plan: "Starter", Companies: 3}}}}}
	got, err = s.Distribution(context.Background())
	if err != nil || len(got.Items) != 1 || got.Items[0].Plan != "Starter" || got.Items[0].Companies != 3 {
		t.Fatalf("distribution passthrough: %+v %v", got, err)
	}
}
