package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

type Poem struct {
	ent.Schema
}

func (Poem) Fields() []ent.Field {
	return []ent.Field{
		field.Int("id").
			StorageKey("id"),
		field.String("title").
			NotEmpty().
			Comment("诗词标题"),
		field.String("author").
			Optional().
			Comment("作者"),
		field.String("dynasty").
			Optional().
			Comment("朝代，如唐/宋/先秦"),
		field.Text("content").
			Optional().
			Comment("全文内容"),
		field.Strings("keywords").
			Optional().
			Comment("关键词列表"),
		field.Strings("tags").
			Optional().
			Comment("标签列表"),
		field.String("type").
			Default("other").
			Comment("体裁：shi(诗)/ci(词)/fu(赋)/jing(经)/other(其他)"),
		field.String("source").
			Optional().
			Comment("数据来源子集标识，如poet.tang/shijing/lunyu"),
	}
}

func (Poem) Edges() []ent.Edge {
	return []ent.Edge{
		edge.To("poem_chars", PoemChar.Type),
	}
}

func (Poem) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("title"),
		index.Fields("author"),
		index.Fields("dynasty"),
		index.Fields("type"),
		index.Fields("author", "dynasty"),
	}
}

func (Poem) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{Table: "poem"},
	}
}
