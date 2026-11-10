package rating

import (
	"testing"

	"github.com/babyname/fate/v4/ent"
)

func testChar(char, meaning, pinyin string) *ent.Character {
	return &ent.Character{
		Char:     char,
		Meaning:  meaning,
		Pinyin:   []string{pinyin},
		Regular:  true,
		Nameable: true,
	}
}

// Tier-1 negative characters must score far below beautiful ones.
func TestSemanticScoreSeparation(t *testing.T) {
	a := NewAesthetic()

	ugly := []string{"猪", "虫", "账", "怠", "尸", "棺", "蠢", "懒"}
	nice := []string{"瑾", "瑜", "霖", "萱", "睿", "哲", "华", "明"}

	for _, c := range ugly {
		s, _ := a.SemanticScore(testChar(c, "测试释义"+c, "ce4"))
		if s > -20 {
			t.Errorf("char %q: expected strong negative semantic score, got %.1f", c, s)
		}
	}
	for _, c := range nice {
		s, _ := a.SemanticScore(testChar(c, "测试释义", "ce4"))
		if s < 8 {
			t.Errorf("char %q: expected positive semantic score, got %.1f", c, s)
		}
	}
}

// Meaning keyword fallback: a char outside the curated lists whose meaning
// mentions livestock/disease repeatedly must be penalized.
func TestSemanticScoreMeaningFallback(t *testing.T) {
	a := NewAesthetic()
	s, reason := a.SemanticScore(&ent.Character{
		Char:    "豚",
		Meaning: "小猪。也泛指猪，家畜之一。",
	})
	if s > -5 {
		t.Errorf("豚: expected negative semantic score from meaning fallback, got %.1f", s)
	}
	if reason == "" {
		t.Errorf("豚: expected a reason to be reported")
	}

	// A neutral char with a single incidental keyword hit must NOT be penalized.
	s2, reason2 := a.SemanticScore(&ent.Character{
		Char:    "治",
		Meaning: "治理；医疗，治病；安定。",
	})
	if s2 < 0 {
		t.Errorf("治: expected no penalty for a single incidental keyword hit, got %.1f (%s)", s2, reason2)
	}
}

// Full-name homophone screening: the curated bad sequences must be caught.
func TestHomophonePenalty(t *testing.T) {
	a := NewAesthetic()
	if p, phrase := a.HomophonePenalty([]string{"du", "zi", "teng"}); p == 0 {
		t.Errorf("expected duziteng to be flagged")
	} else {
		_ = phrase
	}
	if p, _ := a.HomophonePenalty([]string{"li", "ming", "hua"}); p != 0 {
		t.Errorf("li-ming-hua must not be flagged")
	}
}

// Tone extraction must handle both diacritic and digit pinyin forms.
func TestGetTone(t *testing.T) {
	cases := map[string]int{
		"liáng": 2,
		"yùn":   4,
		"míng":  2,
		"huá":   2,
		"bō":    1,
		"yang2": 2,
		"shen":  0,
	}
	for p, want := range cases {
		if got := getTone(p); got != want {
			t.Errorf("getTone(%q) = %d, want %d", p, got, want)
		}
	}
}

func TestStripTone(t *testing.T) {
	cases := map[string]string{
		"liáng": "liang",
		"shì":   "shi",
		"lǜ":    "lv",
		"yang2": "yang",
	}
	for p, want := range cases {
		if got := stripTone(p); got != want {
			t.Errorf("stripTone(%q) = %q, want %q", p, got, want)
		}
	}
}

// RateName on a vulgar pair must produce a clearly lower total than a
// beautiful pair with otherwise identical structural traits.
func TestRateNameAestheticGap(t *testing.T) {
	r := NewRaterWithStrokes(nil, 7, 0)
	r.SetSurnamePinyin("lǐ3")

	ugly := r.RateName("李", testChar("账", "债务账目", "zhàng4"), testChar("怠", "懒惰怠慢", "dài4"))
	nice := r.RateName("李", testChar("瑾", "美玉", "jǐn3"), testChar("华", "光彩美丽", "huá2"))

	if nice.TotalScore-ugly.TotalScore < 15 {
		t.Errorf("expected a large aesthetic score gap, nice=%.1f ugly=%.1f",
			nice.TotalScore, ugly.TotalScore)
	}
	if ugly.WenHuaScore >= nice.WenHuaScore {
		t.Errorf("expected ugly wenHua < nice wenHua, got %.1f vs %.1f",
			ugly.WenHuaScore, nice.WenHuaScore)
	}
}
