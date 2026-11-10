package rating

import (
	"fmt"
	"math"
	"strings"

	"github.com/babyname/fate/v4/ent"
	"github.com/babyname/fate/v4/internal/chronosfate"
	"github.com/babyname/fate/v4/internal/wuge"
	"github.com/babyname/fate/v4/internal/wuxing"
)

type NameRating struct {
	WenHuaScore    float64
	WuXingScore    float64
	ShengXiaoScore float64
	WuGeScore      float64
	YinYunScore    float64
	TotalScore     float64
	// SemanticScore is the combined semantic (aesthetic) quality of the name
	// characters; used for ranking tie-breaks.
	SemanticScore   float64
	Grade           string
	Interpret       string
	WenHuaDetail    string
	WuXingDetail    string
	ShengXiaoDetail string
	WuGeDetail      string
	YinYunDetail    string
}

type Rater struct {
	fateData      *chronosfate.FateData
	l1            int
	l2            int
	surnamePinyin string
	aesthetic     *Aesthetic
}

func NewRater(fateData *chronosfate.FateData) *Rater {
	return &Rater{fateData: fateData, aesthetic: NewAesthetic()}
}

func NewRaterWithStrokes(fateData *chronosfate.FateData, l1, l2 int) *Rater {
	return &Rater{fateData: fateData, l1: l1, l2: l2, aesthetic: NewAesthetic()}
}

// SetSurnamePinyin sets the surname's pinyin (tone-marked, first reading) so
// that yin-yun scoring can evaluate the full three-syllable name.
func (r *Rater) SetSurnamePinyin(p string) {
	r.surnamePinyin = p
}

func (r *Rater) RateName(surname string, c1, c2 *ent.Character) *NameRating {
	rating := &NameRating{}

	r.rateWenHua(rating, c1, c2)
	r.rateWuXing(rating, c1, c2)
	r.rateShengXiao(rating, c1, c2)
	r.rateWuGe(rating, c1, c2)
	r.rateYinYun(rating, c1, c2)
	rating.SemanticScore = r.SemanticSum(c1, c2)

	// Aesthetics dominate: cultural/semantic 0.35, wuge 0.22, wuxing 0.22,
	// shengxiao 0.06, yinyun 0.15.
	wenHuaWeight := 0.35
	wuXingWeight := 0.22
	shengXiaoWeight := 0.06
	wuGeWeight := 0.22
	yinYunWeight := 0.15

	rating.TotalScore = math.Round(
		(rating.WenHuaScore*wenHuaWeight+
			rating.WuXingScore*wuXingWeight+
			rating.ShengXiaoScore*shengXiaoWeight+
			rating.WuGeScore*wuGeWeight+
			rating.YinYunScore*yinYunWeight)*10,
	) / 10

	rating.Grade = scoreToGrade(rating.TotalScore)
	rating.Interpret = r.generateInterpret(surname, c1, c2, rating)

	return rating
}

// SemanticSum returns the combined semantic score of the two name characters.
// Used as a tie-breaker during candidate ranking.
func (r *Rater) SemanticSum(c1, c2 *ent.Character) float64 {
	s1, _ := r.aesthetic.SemanticScoreCached(c1)
	s2, _ := r.aesthetic.SemanticScoreCached(c2)
	return s1 + s2
}

// rateWenHua scores the cultural impression of a name. The semantic quality
// of the characters (beauty/vulgarity of meaning) dominates; structural
// traits (strokes, common usage) are secondary refinements.
func (r *Rater) rateWenHua(rating *NameRating, c1, c2 *ent.Character) {
	// Semantic-first base: a neutral structural name starts mid-scale so the
	// semantic delta can truly separate beautiful from ugly names.
	score := 40.0
	var details []string

	s1, reason1 := r.aesthetic.SemanticScoreCached(c1)
	s2, reason2 := r.aesthetic.SemanticScoreCached(c2)
	score += s1 + s2
	if reason1 != "" {
		details = append(details, reason1)
	}
	if reason2 != "" {
		details = append(details, reason2)
	}

	if c1.Regular && c1.Nameable {
		score += 4
		details = append(details, fmt.Sprintf("【%s】为常用起名用字", c1.Char))
	} else if c1.Regular {
		score += 2
	}
	if c2.Regular && c2.Nameable {
		score += 4
		details = append(details, fmt.Sprintf("【%s】为常用起名用字", c2.Char))
	} else if c2.Regular {
		score += 2
	}

	if c1.Meaning != "" {
		score += 2
	}
	if c2.Meaning != "" {
		score += 2
	}

	stroke1 := c1.KangxiStroke
	stroke2 := c2.KangxiStroke
	if stroke1 >= 5 && stroke1 <= 15 {
		score += 1
	}
	if stroke2 >= 5 && stroke2 <= 15 {
		score += 1
	}
	diff := absInt(stroke1 - stroke2)
	if diff <= 5 && stroke1 > 0 && stroke2 > 0 {
		score += 2
		details = append(details, "笔画搭配匀称")
	}

	if len(c1.Pinyin) > 0 && len(c2.Pinyin) > 0 {
		score += 1
	}

	if score > 100 {
		score = 100
	}
	if score < 0 {
		score = 0
	}

	rating.WenHuaScore = score
	if len(details) > 0 {
		rating.WenHuaDetail = strings.Join(details, "; ")
	} else {
		rating.WenHuaDetail = "文化印象一般"
	}
}

func (r *Rater) rateWuXing(rating *NameRating, c1, c2 *ent.Character) {
	if r.fateData == nil {
		rating.WuXingScore = 80
		rating.WuXingDetail = "五行信息良好"
		return
	}

	xiWuxing := r.fateData.WuxingXijiInfo.Xi
	jiWuxing := r.fateData.WuxingXijiInfo.Ji

	score := 70.0
	var details []string

	chars := []*ent.Character{c1, c2}
	for _, c := range chars {
		wx := c.WuXing
		if wx == "" {
			continue
		}

		isXi := wx == xiWuxing
		isJi := wx == jiWuxing

		if isXi {
			score += 12
			details = append(details, fmt.Sprintf("【%s】五行属%s，为喜用神，加分", c.Char, wx))
		} else if isJi {
			score -= 8
			details = append(details, fmt.Sprintf("【%s】五行属%s，为忌神，减分", c.Char, wx))
		} else {
			details = append(details, fmt.Sprintf("【%s】五行属%s，中性", c.Char, wx))
		}
	}

	wx1 := c1.WuXing
	wx2 := c2.WuXing
	if wx1 != "" && wx2 != "" {
		if isSheng(wx1, wx2) || isSheng(wx2, wx1) {
			score += 8
			details = append(details, "五行相生，搭配协调")
		}
		if isKe(wx1, wx2) || isKe(wx2, wx1) {
			score -= 5
			details = append(details, "五行相克，需注意")
		}
	}

	if score > 100 {
		score = 100
	}
	if score < 0 {
		score = 0
	}

	rating.WuXingScore = score
	if len(details) > 0 {
		rating.WuXingDetail = strings.Join(details, "; ")
	} else {
		rating.WuXingDetail = "五行信息良好"
	}
}

func (r *Rater) rateShengXiao(rating *NameRating, c1, c2 *ent.Character) {
	score := 80.0
	var details []string

	if r.fateData == nil {
		rating.ShengXiaoScore = score
		rating.ShengXiaoDetail = "生肖信息良好"
		return
	}

	zodiac := r.fateData.BaziInfo.Zodiac
	wx1 := c1.WuXing
	wx2 := c2.WuXing
	zodiacWuXing := getZodiacWuXing(zodiac)

	if zodiacWuXing != "" {
		details = append(details, fmt.Sprintf("生肖%s，五行属%s", zodiac, zodiacWuXing))

		if (isSheng(zodiacWuXing, wx1) || isSheng(wx1, zodiacWuXing)) && wx1 != "" {
			score += 7
			details = append(details, fmt.Sprintf("【%s】与生肖五行相生，吉利", c1.Char))
		}
		if (isSheng(zodiacWuXing, wx2) || isSheng(wx2, zodiacWuXing)) && wx2 != "" {
			score += 7
			details = append(details, fmt.Sprintf("【%s】与生肖五行相生，吉利", c2.Char))
		}
		if (isKe(zodiacWuXing, wx1) || isKe(wx1, zodiacWuXing)) && wx1 != "" {
			score -= 5
			details = append(details, fmt.Sprintf("【%s】与生肖五行相克", c1.Char))
		}
		if (isKe(zodiacWuXing, wx2) || isKe(wx2, zodiacWuXing)) && wx2 != "" {
			score -= 5
			details = append(details, fmt.Sprintf("【%s】与生肖五行相克", c2.Char))
		}
	}

	if score > 100 {
		score = 100
	}
	if score < 0 {
		score = 0
	}

	rating.ShengXiaoScore = score
	if len(details) > 0 {
		rating.ShengXiaoDetail = strings.Join(details, "; ")
	} else {
		rating.ShengXiaoDetail = "生肖信息良好"
	}
}

func (r *Rater) rateWuGe(rating *NameRating, c1, c2 *ent.Character) {
	if r.l1 == 0 {
		rating.WuGeScore = 70
		rating.WuGeDetail = "五格数理良好"
		return
	}

	f1 := c1.ScienceStroke
	f2 := c2.ScienceStroke
	ge := wuge.CalcWuGe(r.l1, r.l2, f1, f2)

	tianGe := ge.TianGe()
	renGe := ge.RenGe()
	diGe := ge.DiGe()
	waiGe := ge.WaiGe()
	zongGe := ge.ZongGe()

	tianDaYan := wuge.Find(tianGe)
	renDaYan := wuge.Find(renGe)
	diDaYan := wuge.Find(diGe)
	waiDaYan := wuge.Find(waiGe)
	zongDaYan := wuge.Find(zongGe)

	score := 60.0
	var details []string

	luckyCount := 0
	geItems := []struct {
		name   string
		daYan  wuge.DaYan
		weight float64
	}{
		{"天格", tianDaYan, 0.15},
		{"人格", renDaYan, 0.30},
		{"地格", diDaYan, 0.20},
		{"外格", waiDaYan, 0.15},
		{"总格", zongDaYan, 0.20},
	}

	for _, item := range geItems {
		switch {
		case strings.Contains(item.daYan.Lucky, "吉") && !strings.Contains(item.daYan.Lucky, "凶"):
			luckyCount++
			score += 15 * item.weight
			details = append(details, fmt.Sprintf("%s%d(%s)吉", item.name, item.daYan.Number, item.daYan.Lucky))
		case strings.Contains(item.daYan.Lucky, "半吉"):
			score += 8 * item.weight
			details = append(details, fmt.Sprintf("%s%d(%s)", item.name, item.daYan.Number, item.daYan.Lucky))
		default:
			score -= 5 * item.weight
			details = append(details, fmt.Sprintf("%s%d(%s)凶", item.name, item.daYan.Number, item.daYan.Lucky))
		}
	}

	if zongDaYan.Max {
		score += 5
		details = append(details, "总格为最大好运数")
	}

	sanCaiObj := wuxing.NewSanCai(tianGe, renGe, diGe)
	sanCaiStr := sanCaiObj.String()
	sanCaiLuck, _ := wuxing.GetWuXing(sanCaiStr)
	switch sanCaiLuck {
	case "大吉":
		score += 12
		details = append(details, fmt.Sprintf("三才%s大吉", sanCaiStr))
	case "吉", "吉多于凶":
		score += 8
		details = append(details, fmt.Sprintf("三才%s吉", sanCaiStr))
	case "中吉":
		score += 5
		details = append(details, fmt.Sprintf("三才%s中吉", sanCaiStr))
	case "凶多于吉", "吉凶参半":
		score -= 2
	case "凶", "大凶":
		score -= 6
		details = append(details, fmt.Sprintf("三才%s凶", sanCaiStr))
	}

	if score > 100 {
		score = 100
	}
	if score < 0 {
		score = 0
	}

	rating.WuGeScore = score
	rating.WuGeDetail = strings.Join(details, "; ")
}

func (r *Rater) rateYinYun(rating *NameRating, c1, c2 *ent.Character) {
	score := 80.0
	var details []string

	pinyin1 := c1.Pinyin
	pinyin2 := c2.Pinyin
	if len(pinyin1) == 0 || len(pinyin2) == 0 {
		rating.YinYunScore = score
		rating.YinYunDetail = "音韵信息良好"
		return
	}

	p1 := pinyin1[0]
	p2 := pinyin2[0]

	tone1 := getTone(p1)
	tone2 := getTone(p2)

	// Full-name (surname + given name) tonal flow when the surname pinyin is
	// available: three distinct tones sound livelier than three identical ones.
	surnameTone := 0
	if r.surnamePinyin != "" {
		surnameTone = getTone(r.surnamePinyin)
		tonePattern := []int{}
		if surnameTone != 0 {
			tonePattern = append(tonePattern, surnameTone)
		}
		if tone1 != 0 {
			tonePattern = append(tonePattern, tone1)
		}
		if tone2 != 0 {
			tonePattern = append(tonePattern, tone2)
		}
		distinct := make(map[int]bool)
		for _, t := range tonePattern {
			distinct[t] = true
		}
		switch {
		case len(distinct) >= 3:
			score += 8
			details = append(details, "姓名连读声调起伏，抑扬顿挫")
		case len(tonePattern) >= 2 && len(distinct) == 1:
			score -= 8
			details = append(details, "姓名连读声调单一，略显平淡")
		}

		// Surname and first given-name char sharing initials/finals stutters.
		sInitial := getShengMu(stripTone(r.surnamePinyin))
		sFinal := getYunMu(stripTone(r.surnamePinyin))
		i1 := getShengMu(stripTone(p1))
		f1 := getYunMu(stripTone(p1))
		if sInitial != "" && sInitial == i1 {
			score -= 4
			details = append(details, "姓与名首字声母相同，读起来拗口")
		}
		if sFinal != "" && sFinal == f1 && sInitial != i1 {
			score -= 3
			details = append(details, "姓与名首字韵母相近")
		}
	}

	if tone1 != tone2 && tone1 != 0 && tone2 != 0 {
		score += 8
		details = append(details, "两字声调不同，抑扬顿挫")
	} else if tone1 == tone2 && tone1 != 0 {
		score -= 5
		details = append(details, "两字声调相同")
	}

	initial1 := getShengMu(p1)
	initial2 := getShengMu(p2)
	if initial1 != initial2 && initial1 != "" && initial2 != "" {
		score += 5
		details = append(details, "声母不同，发音清晰")
	} else if initial1 == initial2 && initial1 != "" {
		score -= 3
	}

	final1 := getYunMu(p1)
	final2 := getYunMu(p2)
	if final1 != final2 && final1 != "" && final2 != "" {
		score += 4
		details = append(details, "韵母不同，朗朗上口")
	} else if final1 == final2 && final1 != "" {
		score -= 3
	}

	// Unfortunate homophone screening over the full pinyin sequence.
	syllables := make([]string, 0, 3)
	if r.surnamePinyin != "" {
		syllables = append(syllables, stripTone(r.surnamePinyin))
	}
	syllables = append(syllables, stripTone(p1), stripTone(p2))
	if penalty, phrase := r.aesthetic.HomophonePenalty(syllables); penalty != 0 {
		score += penalty
		details = append(details, fmt.Sprintf("全名读音与「%s」谐音，需注意", phrase))
	}

	if score > 100 {
		score = 100
	}
	if score < 0 {
		score = 0
	}

	rating.YinYunScore = score
	if len(details) > 0 {
		rating.YinYunDetail = strings.Join(details, "; ")
	} else {
		rating.YinYunDetail = "音韵信息良好"
	}
}

func (r *Rater) generateInterpret(surname string, c1, c2 *ent.Character, rating *NameRating) string {
	var parts []string

	parts = append(parts, fmt.Sprintf("【%s%s】", surname, c1.Char+c2.Char))

	if r.fateData != nil {
		parts = append(parts, fmt.Sprintf("喜用%s。", r.fateData.WuxingXijiInfo.Xi))
	}

	wx1 := c1.WuXing
	wx2 := c2.WuXing
	if wx1 != "" && wx2 != "" {
		parts = append(parts, fmt.Sprintf("名字五行组合：%s%s", wx1, wx2))
		if r.fateData != nil {
			if wx1 == r.fateData.WuxingXijiInfo.Xi || wx2 == r.fateData.WuxingXijiInfo.Xi {
				parts = append(parts, "与八字喜用神相合。")
			}
		}
	}

	parts = append(parts, fmt.Sprintf("综合评分%.1f分（%s）", rating.TotalScore, rating.Grade))

	return strings.Join(parts, "")
}

func scoreToGrade(score float64) string {
	switch {
	case score >= 90:
		return "上上"
	case score >= 80:
		return "上吉"
	case score >= 70:
		return "中吉"
	case score >= 60:
		return "中平"
	case score >= 50:
		return "中下"
	default:
		return "下下"
	}
}

var shengMap = map[string]string{
	"木": "火", "火": "土", "土": "金", "金": "水", "水": "木",
}

var keMap = map[string]string{
	"木": "土", "土": "水", "水": "火", "火": "金", "金": "木",
}

func isSheng(a, b string) bool {
	if v, ok := shengMap[a]; ok {
		return v == b
	}
	return false
}

func isKe(a, b string) bool {
	if v, ok := keMap[a]; ok {
		return v == b
	}
	return false
}

func getZodiacWuXing(zodiac string) string {
	m := map[string]string{
		"鼠": "水", "牛": "土", "虎": "木", "兔": "木",
		"龙": "土", "蛇": "火", "马": "火", "羊": "土",
		"猴": "金", "鸡": "金", "狗": "土", "猪": "水",
	}
	if v, ok := m[zodiac]; ok {
		return v
	}
	return ""
}

func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

// toneMarkMap maps tone-marked vowels to their tone number.
var toneMarkMap = map[rune]int{
	'ā': 1, 'á': 2, 'ǎ': 3, 'à': 4,
	'ē': 1, 'é': 2, 'ě': 3, 'è': 4,
	'ī': 1, 'í': 2, 'ǐ': 3, 'ì': 4,
	'ō': 1, 'ó': 2, 'ǒ': 3, 'ò': 4,
	'ū': 1, 'ú': 2, 'ǔ': 3, 'ù': 4,
	'ǖ': 1, 'ǘ': 2, 'ǚ': 3, 'ǜ': 4,
}

// asciiVowelMap maps tone-marked vowels to their plain ASCII form.
var asciiVowelMap = map[rune]rune{
	'ā': 'a', 'á': 'a', 'ǎ': 'a', 'à': 'a',
	'ē': 'e', 'é': 'e', 'ě': 'e', 'è': 'e',
	'ī': 'i', 'í': 'i', 'ǐ': 'i', 'ì': 'i',
	'ō': 'o', 'ó': 'o', 'ǒ': 'o', 'ò': 'o',
	'ū': 'u', 'ú': 'u', 'ǔ': 'u', 'ù': 'u',
	'ǖ': 'v', 'ǘ': 'v', 'ǚ': 'v', 'ǜ': 'v',
	'ü': 'v',
}

func getTone(pinyin string) int {
	runes := []rune(pinyin)
	if len(runes) == 0 {
		return 0
	}
	// Digit-suffixed form, e.g. "yang2".
	if last := runes[len(runes)-1]; last >= '1' && last <= '4' {
		return int(last - '0')
	}
	// Diacritic form, e.g. "liáng".
	for _, r := range runes {
		if t, ok := toneMarkMap[r]; ok {
			return t
		}
	}
	return 0
}

// stripTone converts a pinyin syllable to plain ASCII without tones,
// e.g. "liáng" -> "liang", "yang2" -> "yang".
func stripTone(pinyin string) string {
	var b strings.Builder
	for _, r := range pinyin {
		if repl, ok := asciiVowelMap[r]; ok {
			b.WriteRune(repl)
			continue
		}
		if r >= '1' && r <= '4' {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

func getInitial(pinyin string) string {
	runes := []rune(pinyin)
	if len(runes) == 0 {
		return ""
	}
	return string(runes[0])
}

func getFinal(pinyin string) string {
	runes := []rune(pinyin)
	if len(runes) <= 1 {
		return ""
	}
	return string(runes[1:])
}

func absInt(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

func getShengMu(pinyin string) string {
	if len(pinyin) == 0 {
		return ""
	}
	shengmuList := []string{"zh", "ch", "sh", "b", "p", "m", "f", "d", "t", "n", "l", "g", "k", "h", "j", "q", "x", "z", "c", "s", "r", "y", "w"}
	for _, sm := range shengmuList {
		if len(pinyin) >= len(sm) && pinyin[:len(sm)] == sm {
			return sm
		}
	}
	return ""
}

func getYunMu(pinyin string) string {
	sm := getShengMu(pinyin)
	if sm == "" {
		return pinyin
	}
	return pinyin[len(sm):]
}
