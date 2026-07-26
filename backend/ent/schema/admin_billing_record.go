package schema

import (
	"time"

	"github.com/Wei-Shaw/sub2api/ent/schema/mixins"

	"entgo.io/ent"
	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// AdminBillingRecord 管理员手工记账：按 person_name(自由文本) 记录每笔订单的成本与利润。
//
// 用途：独立于自动计费(usage_logs / payment_orders)的人工台账，便于按"归属人"核算成本与盈利。
//   - person_name 为自由文本，不关联 users 表
//   - source 为自由文本，内置微信/咸鱼/淘宝，也允许管理员自定义
//   - 营收(revenue) = cost + profit，仅展示派生，不存储
//   - 删除策略：软删除(deleted_at)
type AdminBillingRecord struct {
	ent.Schema
}

// Annotations 指定表名。
func (AdminBillingRecord) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{Table: "admin_billing_records"},
	}
}

// Mixin 复用时间戳与软删除混入。
func (AdminBillingRecord) Mixin() []ent.Mixin {
	return []ent.Mixin{
		mixins.TimeMixin{},       // created_at, updated_at
		mixins.SoftDeleteMixin{}, // deleted_at + 查询/删除拦截器
	}
}

// Fields 定义业务字段。
func (AdminBillingRecord) Fields() []ent.Field {
	return []ent.Field{
		field.String("person_name").
			MaxLen(100).
			NotEmpty().
			Comment("记账归属人名（自由文本，不关联 users）"),
		field.String("source").
			MaxLen(100).
			Default("").
			Comment("订单来源（自由文本，内置微信/咸鱼/淘宝，可自定义）"),
		field.Float("cost").
			SchemaType(map[string]string{dialect.Postgres: "decimal(20,2)"}).
			Default(0).
			Comment("成本"),
		field.Float("profit").
			SchemaType(map[string]string{dialect.Postgres: "decimal(20,2)"}).
			Default(0).
			Comment("利润"),
		field.Time("occurred_at").
			Default(time.Now).
			SchemaType(map[string]string{dialect.Postgres: "timestamptz"}).
			Comment("业务发生日期（可由管理员指定，默认 now）"),
		field.String("note").
			Optional().
			Nillable().
			SchemaType(map[string]string{dialect.Postgres: "text"}).
			Comment("备注"),
		field.Int64("created_by").
			Comment("创建该记录的管理员用户 ID（审计）"),
	}
}

// Indexes 定义查询索引。
func (AdminBillingRecord) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("person_name"),
		index.Fields("source"),
		index.Fields("occurred_at"),
		index.Fields("created_at"),
	}
}
