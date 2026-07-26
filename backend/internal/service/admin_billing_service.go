package service

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/model"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
)

var ErrAdminBillingRecordNotFound = infraerrors.NotFound("ADMIN_BILLING_RECORD_NOT_FOUND", "admin billing record not found")

var defaultAdminBillingSources = []string{"微信", "咸鱼", "淘宝"}

type AdminBillingRepository interface {
	List(ctx context.Context, params pagination.PaginationParams, filter model.AdminBillingFilter) ([]*model.AdminBillingRecord, *pagination.PaginationResult, error)
	GetByID(ctx context.Context, id int64) (*model.AdminBillingRecord, error)
	Create(ctx context.Context, record *model.AdminBillingRecord) (*model.AdminBillingRecord, error)
	Update(ctx context.Context, record *model.AdminBillingRecord) (*model.AdminBillingRecord, error)
	Delete(ctx context.Context, id int64) error
	DistinctNames(ctx context.Context, prefix string, limit int) ([]string, error)
	DistinctSources(ctx context.Context, prefix string, limit int) ([]string, error)
	ListByFilter(ctx context.Context, filter model.AdminBillingFilter) ([]*model.AdminBillingRecord, error)
}

type AdminBillingService struct {
	repo AdminBillingRepository
}

func NewAdminBillingService(repo AdminBillingRepository) *AdminBillingService {
	return &AdminBillingService{repo: repo}
}

func (s *AdminBillingService) List(ctx context.Context, params pagination.PaginationParams, filter model.AdminBillingFilter) ([]*model.AdminBillingRecord, *pagination.PaginationResult, error) {
	filter.PersonName = strings.TrimSpace(filter.PersonName)
	return s.repo.List(ctx, params, filter)
}

func (s *AdminBillingService) GetByID(ctx context.Context, id int64) (*model.AdminBillingRecord, error) {
	return s.repo.GetByID(ctx, id)
}

func (s *AdminBillingService) Create(ctx context.Context, record *model.AdminBillingRecord) (*model.AdminBillingRecord, error) {
	if record == nil {
		return nil, infraerrors.BadRequest("ADMIN_BILLING_RECORD_REQUIRED", "admin billing record is required")
	}
	if err := record.Validate(); err != nil {
		return nil, err
	}
	if record.OccurredAt.IsZero() {
		record.OccurredAt = time.Now()
	}
	return s.repo.Create(ctx, record)
}

func (s *AdminBillingService) Update(ctx context.Context, id int64, record *model.AdminBillingRecord) (*model.AdminBillingRecord, error) {
	if record == nil {
		return nil, infraerrors.BadRequest("ADMIN_BILLING_RECORD_REQUIRED", "admin billing record is required")
	}
	if _, err := s.repo.GetByID(ctx, id); err != nil {
		return nil, err
	}
	record.ID = id
	if err := record.Validate(); err != nil {
		return nil, err
	}
	if record.OccurredAt.IsZero() {
		record.OccurredAt = time.Now()
	}
	return s.repo.Update(ctx, record)
}

func (s *AdminBillingService) Delete(ctx context.Context, id int64) error {
	return s.repo.Delete(ctx, id)
}

func (s *AdminBillingService) Suggestions(ctx context.Context, prefix string, limit int) ([]string, error) {
	names, err := s.repo.DistinctNames(ctx, strings.TrimSpace(prefix), limit)
	if err != nil {
		return nil, err
	}
	if names == nil {
		return []string{}, nil
	}
	return names, nil
}

func (s *AdminBillingService) SourceSuggestions(ctx context.Context, prefix string, limit int) ([]string, error) {
	if limit <= 0 || limit > 50 {
		limit = 10
	}
	prefix = strings.TrimSpace(prefix)
	sources := make([]string, 0, limit)
	seen := make(map[string]struct{})

	addSource := func(source string) {
		source = strings.TrimSpace(source)
		if source == "" {
			return
		}
		key := strings.ToLower(source)
		if _, ok := seen[key]; ok {
			return
		}
		if prefix != "" && !strings.Contains(strings.ToLower(source), strings.ToLower(prefix)) {
			return
		}
		seen[key] = struct{}{}
		sources = append(sources, source)
	}

	for _, source := range defaultAdminBillingSources {
		addSource(source)
	}

	dbSources, err := s.repo.DistinctSources(ctx, prefix, limit)
	if err != nil {
		return nil, err
	}
	for _, source := range dbSources {
		addSource(source)
	}

	if len(sources) > limit {
		sources = sources[:limit]
	}
	return sources, nil
}

func (s *AdminBillingService) Stats(ctx context.Context, filter model.AdminBillingFilter) (*model.AdminBillingStats, error) {
	filter.PersonName = strings.TrimSpace(filter.PersonName)
	records, err := s.repo.ListByFilter(ctx, filter)
	if err != nil {
		return nil, err
	}

	stats := &model.AdminBillingStats{
		PerPerson: []*model.AdminBillingPersonStat{},
		Trend:     []*model.AdminBillingTrendPoint{},
	}

	perPerson := make(map[string]*model.AdminBillingPersonStat)
	trend := make(map[string]*model.AdminBillingTrendPoint)

	for _, record := range records {
		if record == nil {
			continue
		}
		revenue := record.Cost + record.Profit

		stats.Summary.TotalCost += record.Cost
		stats.Summary.TotalProfit += record.Profit
		stats.Summary.TotalRevenue += revenue
		stats.Summary.Count++

		person := perPerson[record.PersonName]
		if person == nil {
			person = &model.AdminBillingPersonStat{PersonName: record.PersonName}
			perPerson[record.PersonName] = person
		}
		person.TotalCost += record.Cost
		person.TotalProfit += record.Profit
		person.TotalRevenue += revenue
		person.Count++

		date := record.OccurredAt.Format("2006-01-02")
		point := trend[date]
		if point == nil {
			point = &model.AdminBillingTrendPoint{Date: date}
			trend[date] = point
		}
		point.Cost += record.Cost
		point.Profit += record.Profit
	}

	stats.Summary.NetProfit = stats.Summary.TotalProfit

	for _, person := range perPerson {
		stats.PerPerson = append(stats.PerPerson, person)
	}
	sort.Slice(stats.PerPerson, func(i, j int) bool {
		if stats.PerPerson[i].TotalRevenue == stats.PerPerson[j].TotalRevenue {
			return stats.PerPerson[i].PersonName < stats.PerPerson[j].PersonName
		}
		return stats.PerPerson[i].TotalRevenue > stats.PerPerson[j].TotalRevenue
	})

	for _, point := range trend {
		stats.Trend = append(stats.Trend, point)
	}
	sort.Slice(stats.Trend, func(i, j int) bool {
		return stats.Trend[i].Date < stats.Trend[j].Date
	})

	return stats, nil
}
