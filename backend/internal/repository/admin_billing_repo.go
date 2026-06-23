package repository

import (
	"context"
	"strings"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/adminbillingrecord"
	"github.com/Wei-Shaw/sub2api/internal/model"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/service"

	entsql "entgo.io/ent/dialect/sql"
)

type adminBillingRepository struct {
	client *dbent.Client
}

func NewAdminBillingRepository(client *dbent.Client) service.AdminBillingRepository {
	return &adminBillingRepository{client: client}
}

func (r *adminBillingRepository) List(ctx context.Context, params pagination.PaginationParams, filter model.AdminBillingFilter) ([]*model.AdminBillingRecord, *pagination.PaginationResult, error) {
	q := r.applyFilter(r.client.AdminBillingRecord.Query(), filter)

	total, err := q.Clone().Count(ctx)
	if err != nil {
		return nil, nil, err
	}

	query := q.Offset(params.Offset()).Limit(params.Limit())
	for _, order := range adminBillingListOrder(params) {
		query = query.Order(order)
	}

	records, err := query.All(ctx)
	if err != nil {
		return nil, nil, err
	}

	return adminBillingEntitiesToModel(records), paginationResultFromTotal(int64(total), params), nil
}

func (r *adminBillingRepository) GetByID(ctx context.Context, id int64) (*model.AdminBillingRecord, error) {
	record, err := r.client.AdminBillingRecord.Query().
		Where(adminbillingrecord.IDEQ(id)).
		Only(ctx)
	if err != nil {
		if dbent.IsNotFound(err) {
			return nil, service.ErrAdminBillingRecordNotFound
		}
		return nil, err
	}
	return adminBillingEntityToModel(record), nil
}

func (r *adminBillingRepository) Create(ctx context.Context, record *model.AdminBillingRecord) (*model.AdminBillingRecord, error) {
	client := clientFromContext(ctx, r.client)
	builder := client.AdminBillingRecord.Create().
		SetPersonName(record.PersonName).
		SetSource(record.Source).
		SetCost(record.Cost).
		SetProfit(record.Profit).
		SetCreatedBy(record.CreatedBy)

	if !record.OccurredAt.IsZero() {
		builder.SetOccurredAt(record.OccurredAt)
	}
	if record.Note != nil {
		builder.SetNote(*record.Note)
	}

	created, err := builder.Save(ctx)
	if err != nil {
		return nil, err
	}
	return adminBillingEntityToModel(created), nil
}

func (r *adminBillingRepository) Update(ctx context.Context, record *model.AdminBillingRecord) (*model.AdminBillingRecord, error) {
	client := clientFromContext(ctx, r.client)
	builder := client.AdminBillingRecord.UpdateOneID(record.ID).
		SetPersonName(record.PersonName).
		SetSource(record.Source).
		SetCost(record.Cost).
		SetProfit(record.Profit).
		SetOccurredAt(record.OccurredAt)

	if record.Note != nil {
		builder.SetNote(*record.Note)
	} else {
		builder.ClearNote()
	}

	updated, err := builder.Save(ctx)
	if err != nil {
		if dbent.IsNotFound(err) {
			return nil, service.ErrAdminBillingRecordNotFound
		}
		return nil, err
	}
	return adminBillingEntityToModel(updated), nil
}

func (r *adminBillingRepository) Delete(ctx context.Context, id int64) error {
	client := clientFromContext(ctx, r.client)
	if err := client.AdminBillingRecord.DeleteOneID(id).Exec(ctx); err != nil {
		if dbent.IsNotFound(err) {
			return service.ErrAdminBillingRecordNotFound
		}
		return err
	}
	return nil
}

func (r *adminBillingRepository) DistinctNames(ctx context.Context, prefix string, limit int) ([]string, error) {
	if limit <= 0 || limit > 50 {
		limit = 10
	}
	prefix = strings.TrimSpace(prefix)

	query := r.client.AdminBillingRecord.Query().
		Order(dbent.Asc(adminbillingrecord.FieldPersonName)).
		Limit(limit).
		Unique(true)

	if prefix != "" {
		query = query.Where(adminbillingrecord.PersonNameContainsFold(prefix))
	}

	return query.Select(adminbillingrecord.FieldPersonName).Strings(ctx)
}

func (r *adminBillingRepository) DistinctSources(ctx context.Context, prefix string, limit int) ([]string, error) {
	if limit <= 0 || limit > 50 {
		limit = 10
	}
	prefix = strings.TrimSpace(prefix)

	query := r.client.AdminBillingRecord.Query().
		Where(adminbillingrecord.SourceNEQ("")).
		Order(dbent.Asc(adminbillingrecord.FieldSource)).
		Limit(limit).
		Unique(true)

	if prefix != "" {
		query = query.Where(adminbillingrecord.SourceContainsFold(prefix))
	}

	return query.Select(adminbillingrecord.FieldSource).Strings(ctx)
}

func (r *adminBillingRepository) ListByFilter(ctx context.Context, filter model.AdminBillingFilter) ([]*model.AdminBillingRecord, error) {
	records, err := r.applyFilter(r.client.AdminBillingRecord.Query(), filter).
		Order(dbent.Asc(adminbillingrecord.FieldOccurredAt), dbent.Asc(adminbillingrecord.FieldID)).
		All(ctx)
	if err != nil {
		return nil, err
	}
	return adminBillingEntitiesToModel(records), nil
}

func (r *adminBillingRepository) applyFilter(q *dbent.AdminBillingRecordQuery, filter model.AdminBillingFilter) *dbent.AdminBillingRecordQuery {
	personName := strings.TrimSpace(filter.PersonName)
	if personName != "" {
		q = q.Where(adminbillingrecord.PersonNameContainsFold(personName))
	}
	if filter.From != nil {
		q = q.Where(adminbillingrecord.OccurredAtGTE(*filter.From))
	}
	if filter.To != nil {
		q = q.Where(adminbillingrecord.OccurredAtLTE(*filter.To))
	}
	return q
}

func adminBillingListOrder(params pagination.PaginationParams) []func(*entsql.Selector) {
	sortBy := strings.ToLower(strings.TrimSpace(params.SortBy))
	sortOrder := params.NormalizedSortOrder(pagination.SortOrderDesc)

	field := adminbillingrecord.FieldOccurredAt
	switch sortBy {
	case "person_name":
		field = adminbillingrecord.FieldPersonName
	case "source":
		field = adminbillingrecord.FieldSource
	case "cost":
		field = adminbillingrecord.FieldCost
	case "profit":
		field = adminbillingrecord.FieldProfit
	case "created_at":
		field = adminbillingrecord.FieldCreatedAt
	case "occurred_at", "":
		field = adminbillingrecord.FieldOccurredAt
	}

	if sortOrder == pagination.SortOrderAsc {
		return []func(*entsql.Selector){dbent.Asc(field), dbent.Asc(adminbillingrecord.FieldID)}
	}
	return []func(*entsql.Selector){dbent.Desc(field), dbent.Desc(adminbillingrecord.FieldID)}
}

func adminBillingEntityToModel(e *dbent.AdminBillingRecord) *model.AdminBillingRecord {
	if e == nil {
		return nil
	}
	return &model.AdminBillingRecord{
		ID:         e.ID,
		PersonName: e.PersonName,
		Source:     e.Source,
		Cost:       e.Cost,
		Profit:     e.Profit,
		Revenue:    e.Cost + e.Profit,
		OccurredAt: e.OccurredAt,
		Note:       e.Note,
		CreatedBy:  e.CreatedBy,
		CreatedAt:  e.CreatedAt,
		UpdatedAt:  e.UpdatedAt,
	}
}

func adminBillingEntitiesToModel(records []*dbent.AdminBillingRecord) []*model.AdminBillingRecord {
	out := make([]*model.AdminBillingRecord, 0, len(records))
	for _, record := range records {
		if converted := adminBillingEntityToModel(record); converted != nil {
			out = append(out, converted)
		}
	}
	return out
}
