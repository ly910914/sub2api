package admin

import (
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/model"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
)

type AdminBillingHandler struct {
	billingService *service.AdminBillingService
}

func NewAdminBillingHandler(billingService *service.AdminBillingService) *AdminBillingHandler {
	return &AdminBillingHandler{billingService: billingService}
}

type adminBillingRecordRequest struct {
	PersonName string       `json:"person_name" binding:"required"`
	Source     string       `json:"source"`
	Cost       float64      `json:"cost" binding:"min=0"`
	Profit     float64      `json:"profit" binding:"min=0"`
	OccurredAt flexibleTime `json:"occurred_at"`
	Note       *string      `json:"note"`
}

func parseAdminBillingSuggestionLimit(c *gin.Context) int {
	limit := 10
	if raw := c.Query("limit"); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 && parsed <= 50 {
			limit = parsed
		}
	}
	return limit
}

type flexibleTime struct {
	time  time.Time
	valid bool
}

func (t *flexibleTime) UnmarshalJSON(data []byte) error {
	raw := strings.TrimSpace(string(data))
	if raw == "" || raw == "null" {
		return nil
	}

	var unix int64
	if err := json.Unmarshal(data, &unix); err == nil {
		if unix > 0 {
			t.time = time.Unix(unix, 0)
			t.valid = true
		}
		return nil
	}

	var text string
	if err := json.Unmarshal(data, &text); err != nil {
		return err
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	parsed, err := parseAdminBillingTime(text)
	if err != nil {
		return err
	}
	t.time = parsed
	t.valid = true
	return nil
}

// List handles listing manual billing records.
// GET /api/v1/admin/billing-records
func (h *AdminBillingHandler) List(c *gin.Context) {
	page, pageSize := response.ParsePagination(c)
	params := pagination.PaginationParams{
		Page:      page,
		PageSize:  pageSize,
		SortBy:    c.DefaultQuery("sort_by", "occurred_at"),
		SortOrder: c.DefaultQuery("sort_order", "desc"),
	}

	records, paginationResult, err := h.billingService.List(c.Request.Context(), params, parseAdminBillingFilter(c))
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	response.Paginated(c, records, paginationResult.Total, page, pageSize)
}

// Create handles creating a manual billing record.
// POST /api/v1/admin/billing-records
func (h *AdminBillingHandler) Create(c *gin.Context) {
	var req adminBillingRecordRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}

	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not found in context")
		return
	}

	record := requestToAdminBillingRecord(req)
	record.CreatedBy = subject.UserID

	created, err := h.billingService.Create(c.Request.Context(), record)
	if err != nil {
		if _, ok := err.(*model.ValidationError); ok {
			response.BadRequest(c, err.Error())
			return
		}
		response.ErrorFrom(c, err)
		return
	}

	response.Success(c, created)
}

// Update handles updating a manual billing record.
// PUT /api/v1/admin/billing-records/:id
func (h *AdminBillingHandler) Update(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		response.BadRequest(c, "Invalid billing record ID")
		return
	}

	var req adminBillingRecordRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}

	updated, err := h.billingService.Update(c.Request.Context(), id, requestToAdminBillingRecord(req))
	if err != nil {
		if _, ok := err.(*model.ValidationError); ok {
			response.BadRequest(c, err.Error())
			return
		}
		response.ErrorFrom(c, err)
		return
	}

	response.Success(c, updated)
}

// Delete handles deleting a manual billing record.
// DELETE /api/v1/admin/billing-records/:id
func (h *AdminBillingHandler) Delete(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		response.BadRequest(c, "Invalid billing record ID")
		return
	}

	if err := h.billingService.Delete(c.Request.Context(), id); err != nil {
		response.ErrorFrom(c, err)
		return
	}

	response.Success(c, gin.H{"message": "Billing record deleted successfully"})
}

// Stats handles manual billing statistics.
// GET /api/v1/admin/billing-records/stats
func (h *AdminBillingHandler) Stats(c *gin.Context) {
	stats, err := h.billingService.Stats(c.Request.Context(), parseAdminBillingFilter(c))
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, stats)
}

// Suggestions handles person name suggestions.
// GET /api/v1/admin/billing-records/suggestions?q=
func (h *AdminBillingHandler) Suggestions(c *gin.Context) {
	names, err := h.billingService.Suggestions(c.Request.Context(), c.Query("q"), parseAdminBillingSuggestionLimit(c))
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, names)
}

// SourceSuggestions handles billing source suggestions.
// GET /api/v1/admin/billing-records/source-suggestions?q=
func (h *AdminBillingHandler) SourceSuggestions(c *gin.Context) {
	sources, err := h.billingService.SourceSuggestions(c.Request.Context(), c.Query("q"), parseAdminBillingSuggestionLimit(c))
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, sources)
}

func requestToAdminBillingRecord(req adminBillingRecordRequest) *model.AdminBillingRecord {
	record := &model.AdminBillingRecord{
		PersonName: req.PersonName,
		Source:     req.Source,
		Cost:       req.Cost,
		Profit:     req.Profit,
		Note:       normalizeOptionalString(req.Note),
	}
	if req.OccurredAt.valid {
		record.OccurredAt = req.OccurredAt.time
	}
	return record
}

func parseAdminBillingFilter(c *gin.Context) model.AdminBillingFilter {
	filter := model.AdminBillingFilter{
		PersonName: strings.TrimSpace(c.Query("q")),
	}
	userTZ := c.Query("timezone")
	if t := parseAdminBillingStartTime(c.Query("from"), userTZ); t != nil {
		filter.From = t
	}
	if t := parseAdminBillingEndTime(c.Query("to"), userTZ); t != nil {
		filter.To = t
	}
	return filter
}

func parseAdminBillingStartTime(raw string, userTZ string) *time.Time {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	if parsed, err := parseAdminBillingTime(raw); err == nil {
		return &parsed
	}
	if parsed, err := timezone.ParseInUserLocation("2006-01-02", raw, userTZ); err == nil {
		return &parsed
	}
	return nil
}

func parseAdminBillingEndTime(raw string, userTZ string) *time.Time {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	if parsed, err := parseAdminBillingTime(raw); err == nil {
		return &parsed
	}
	if parsed, err := timezone.ParseInUserLocation("2006-01-02", raw, userTZ); err == nil {
		end := parsed.AddDate(0, 0, 1).Add(-time.Nanosecond)
		return &end
	}
	return nil
}

func parseAdminBillingTime(raw string) (time.Time, error) {
	rfc3339Layouts := []string{
		time.RFC3339Nano,
		time.RFC3339,
	}
	var lastErr error
	for _, layout := range rfc3339Layouts {
		if parsed, err := time.Parse(layout, raw); err == nil {
			return parsed, nil
		} else {
			lastErr = err
		}
	}

	localLayouts := []string{
		"2006-01-02T15:04",
		"2006-01-02 15:04:05",
		"2006-01-02 15:04",
	}
	for _, layout := range localLayouts {
		if parsed, err := timezone.ParseInLocation(layout, raw); err == nil {
			return parsed, nil
		} else {
			lastErr = err
		}
	}
	return time.Time{}, lastErr
}

func normalizeOptionalString(value *string) *string {
	if value == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}
