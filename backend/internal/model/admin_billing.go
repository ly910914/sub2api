package model

import (
	"strings"
	"time"
)

// AdminBillingRecord 管理员手工记账记录（服务层模型）。
// 按 person_name 归属，手动录入成本与利润；营收为派生值。
type AdminBillingRecord struct {
	ID         int64     `json:"id"`
	PersonName string    `json:"person_name"`
	Source     string    `json:"source"`
	Cost       float64   `json:"cost"`
	Profit     float64   `json:"profit"`
	Revenue    float64   `json:"revenue"` // 派生 = cost + profit，不入库
	OccurredAt time.Time `json:"occurred_at"`
	Note       *string   `json:"note"`
	CreatedBy  int64     `json:"created_by"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// AdminBillingFilter 列表与统计的过滤条件。
type AdminBillingFilter struct {
	PersonName string     // person_name 模糊匹配（ContainsFold）
	From       *time.Time // occurred_at >= From
	To         *time.Time // occurred_at <= To
}

// AdminBillingSummary 汇总统计。
type AdminBillingSummary struct {
	TotalCost    float64 `json:"total_cost"`
	TotalProfit  float64 `json:"total_profit"`
	TotalRevenue float64 `json:"total_revenue"` // = TotalCost + TotalProfit
	NetProfit    float64 `json:"net_profit"`    // = TotalProfit
	Count        int64   `json:"count"`
}

// AdminBillingPersonStat 按归属人分组的统计。
type AdminBillingPersonStat struct {
	PersonName   string  `json:"person_name"`
	TotalCost    float64 `json:"total_cost"`
	TotalProfit  float64 `json:"total_profit"`
	TotalRevenue float64 `json:"total_revenue"`
	Count        int64   `json:"count"`
}

// AdminBillingTrendPoint 按日趋势数据点。
type AdminBillingTrendPoint struct {
	Date   string  `json:"date"` // YYYY-MM-DD
	Cost   float64 `json:"cost"`
	Profit float64 `json:"profit"`
}

// AdminBillingStats 统计接口的聚合返回。
type AdminBillingStats struct {
	Summary   AdminBillingSummary       `json:"summary"`
	PerPerson []*AdminBillingPersonStat `json:"per_person"`
	Trend     []*AdminBillingTrendPoint `json:"trend"`
}

// Validate 校验记录的有效性。
func (r *AdminBillingRecord) Validate() error {
	r.PersonName = strings.TrimSpace(r.PersonName)
	r.Source = strings.TrimSpace(r.Source)
	if r.PersonName == "" {
		return &ValidationError{Field: "person_name", Message: "person_name is required"}
	}
	if len([]rune(r.PersonName)) > 100 {
		return &ValidationError{Field: "person_name", Message: "person_name must be at most 100 characters"}
	}
	if len([]rune(r.Source)) > 100 {
		return &ValidationError{Field: "source", Message: "source must be at most 100 characters"}
	}
	return nil
}
