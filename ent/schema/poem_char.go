package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

type PoemChar struct {
	ent.Schema
}

func (PoemChar) Fields() []ent.Field {
	return []ent.Field{
		field.Int("id").
			StorageKey("id"),
		field.Int("poem_id").
			Comment("所属诗词ID"),
		field.String("char").
			NotEmpty().
			Comment("可起名的汉字"),
		field.Int("position").
			Default(0).
			Comment("首次出现位置（全文汉字序号）"),
		field.String("sentence").
			Optional().
			Comment("首次出现的句子"),
		field.String("context").
			Optional().
			Comment("上下文（所在段落/联句）"),
	}
}

func (PoemChar) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("poem", Poem.Type).
			Ref("poem_chars").
			Unique().
			Required().
			Field("poem_id"),
	}
}

func (PoemChar) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("poem_id"),
		index.Fields("char"),
		index.Fields("char", "poem_id"),
		index.Fields("poem_id", "position"),
	}
}

func (PoemChar) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{Table: "poem_char"},
	}
}
