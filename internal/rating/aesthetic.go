package rating

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/babyname/fate/v4/ent"
	"github.com/babyname/fate/v4/resources"
)

// AestheticData holds curated character-quality datasets used by the
// semantic scoring dimension (ROADMAP: naming aesthetics).
type AestheticData struct {
	NegativeTier1          []string `json:"negative_tier1"`
	NegativeTier2          []string `json:"negative_tier2"`
	PositiveTop            []string `json:"positive_top"`
	PositiveMale           []string `json:"positive_male"`
	PositiveFemale         []string `json:"positive_female"`
	StrongNegativeKeywords []string `json:"meaning_strong_negative_keywords"`
	NegativeKeywords       []string `json:"meaning_negative_keywords"`
	PositiveKeywords       []string `json:"meaning_positive_keywords"`
	BadHomophones          []string `json:"bad_homophones"`
}

// Semantic score adjustments (per character).
const (
	negativeTier1Penalty = -35.0
	negativeTier2Penalty = -12.0
	positiveTopBonus     = 15.0
	positiveGenderBonus  = 10.0
	strongNegativeHit    = -30.0
	negativeKeywordHit   = -9.0
	negativeKeywordCap   = -27.0
	positiveKeywordHit   = 4.0
	positiveKeywordCap   = 12.0
	badHomophonePenalty  = -12.0
)

var (
	aestheticOnce sync.Once
	aestheticData *AestheticData
)

// LoadAesthetic parses the embedded aesthetic dataset once.
func LoadAesthetic() *AestheticData {
	aestheticOnce.Do(func() {
		data := &AestheticData{}
		if err := json.Unmarshal(resources.NameAestheticJSON, data); err != nil {
			// Leave an empty dataset on parse failure; scoring degrades to
			// structural-only (same as before the aesthetic dimension).
			aestheticData = data
			return
		}
		aestheticData = data
	})
	return aestheticData
}

// Aesthetic reports per-character semantic quality lookups. It is safe for
// concurrent use: results are memoized per character in a sync.Map because
// the same character is scored across millions of candidate pairs.
type Aesthetic struct {
	tier1         map[string]bool
	tier2         map[string]bool
	top           map[string]bool
	male          map[string]bool
	female        map[string]bool
	strongNeg     []string
	negKeywords   []string
	posKeywords   []string
	badHomophones map[string]bool
	cache         sync.Map
}

type semanticResult struct {
	score  float64
	reason string
}

// SemanticScoreCached returns SemanticScore memoized by character.
func (a *Aesthetic) SemanticScoreCached(c *ent.Character) (float64, string) {
	if a == nil || c == nil {
		return 0, ""
	}
	if v, ok := a.cache.Load(c.Char); ok {
		r := v.(semanticResult)
		return r.score, r.reason
	}
	score, reason := a.SemanticScore(c)
	a.cache.Store(c.Char, semanticResult{score, reason})
	return score, reason
}

// NewAesthetic builds the lookup tables from the embedded dataset.
func NewAesthetic() *Aesthetic {
	data := LoadAesthetic()
	a := &Aesthetic{
		tier1:         make(map[string]bool, len(data.NegativeTier1)),
		tier2:         make(map[string]bool, len(data.NegativeTier2)),
		top:           make(map[string]bool, len(data.PositiveTop)),
		male:          make(map[string]bool, len(data.PositiveMale)),
		female:        make(map[string]bool, len(data.PositiveFemale)),
		strongNeg:     data.StrongNegativeKeywords,
		negKeywords:   data.NegativeKeywords,
		posKeywords:   data.PositiveKeywords,
		badHomophones: make(map[string]bool, len(data.BadHomophones)),
	}
	for _, c := range data.NegativeTier1 {
		a.tier1[c] = true
	}
	for _, c := range data.NegativeTier2 {
		if !a.tier1[c] {
			a.tier2[c] = true
		}
	}
	for _, c := range data.PositiveTop {
		a.top[c] = true
	}
	for _, c := range data.PositiveMale {
		a.male[c] = true
	}
	for _, c := range data.PositiveFemale {
		a.female[c] = true
	}
	for _, p := range data.BadHomophones {
		a.badHomophones[strings.ToLower(p)] = true
	}
	return a
}

// SemanticScore returns the semantic quality of a single character and the
// reasons behind it: score roughly [-35, +25]. Positive is beautiful and
// auspicious, negative is vulgar.
func (a *Aesthetic) SemanticScore(c *ent.Character) (float64, string) {
	if a == nil || c == nil {
		return 0, ""
	}
	score := 0.0
	var reasons []string

	switch {
	case a.tier1[c.Char]:
		score += negativeTier1Penalty
		reasons = append(reasons, fmt.Sprintf("【%s】字义欠佳", c.Char))
	case a.tier2[c.Char]:
		score += negativeTier2Penalty
		reasons = append(reasons, fmt.Sprintf("【%s】字义欠佳", c.Char))
	case a.top[c.Char]:
		score += positiveTopBonus
		reasons = append(reasons, fmt.Sprintf("【%s】寓意美好", c.Char))
	case a.male[c.Char], a.female[c.Char]:
		score += positiveGenderBonus
		reasons = append(reasons, fmt.Sprintf("【%s】常用佳字", c.Char))
	}

	// Meaning keyword scan as a fallback for characters outside the lists.
	if c.Meaning != "" {
		strong := false
		negHits := 0
		for _, kw := range a.strongNeg {
			if strings.Contains(c.Meaning, kw) {
				strong = true
				break
			}
		}
		for _, kw := range a.negKeywords {
			if strings.Contains(c.Meaning, kw) {
				negHits++
			}
		}
		if strong {
			score += strongNegativeHit
			reasons = append(reasons, "字义含贬义")
		} else if negHits >= 2 {
			penalty := negativeKeywordHit * float64(negHits)
			if penalty < negativeKeywordCap {
				penalty = negativeKeywordCap
			}
			score += penalty
			reasons = append(reasons, "字义含消极成分")
		}
		if score >= 0 && posHits(c, a) > 0 {
			bonus := float64(posHits(c, a)) * positiveKeywordHit
			if bonus > positiveKeywordCap {
				bonus = positiveKeywordCap
			}
			score += bonus
		}
	}

	if len(reasons) > 0 && (score <= -10 || score >= 10) {
		return score, strings.Join(reasons, "; ")
	}
	return score, ""
}

// posHits counts positive keyword hits in the character meaning, stopping at
// the cap.
func posHits(c *ent.Character, a *Aesthetic) int {
	if c.Meaning == "" {
		return 0
	}
	count := 0
	for _, kw := range a.posKeywords {
		if strings.Contains(c.Meaning, kw) {
			count++
			if float64(count)*positiveKeywordHit >= positiveKeywordCap {
				break
			}
		}
	}
	return count
}

// HomophonePenalty checks the full-name pinyin sequence (no tones) against a
// curated list of unfortunate homophone phrases. Returns the penalty and the
// matched phrase.
func (a *Aesthetic) HomophonePenalty(syllables []string) (float64, string) {
	if a == nil || len(syllables) < 2 {
		return 0, ""
	}
	joined := strings.Join(syllables, "")
	if a.badHomophones[joined] {
		return badHomophonePenalty, joined
	}
	return 0, ""
}
