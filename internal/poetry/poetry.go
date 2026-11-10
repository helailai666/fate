// Package poetry provides poetry-based naming queries over the poem and
// poem_char tables populated by cmd/seeddb, plus scoring helpers used by the
// naming pipeline. See docs/ROADMAP.md section 1.
package poetry

import (
	"context"

	"github.com/babyname/fate/v4/internal/repository"
)

// PoemCharResult is a character occurrence inside a poem.
type PoemCharResult = repository.PoemCharHit

// PoemPairResult is a pair of characters from the same poem sentence.
type PoemPairResult = repository.PoemPairHit

// PoetryNamer exposes poetry lookup capabilities for naming flows.
type PoetryNamer interface {
	// FindByChar finds poems containing the given character.
	FindByChar(ctx context.Context, char string, limit int) ([]PoemCharResult, error)
	// FindByWuXing finds suitable characters by wu_xing with a poem source.
	FindByWuXing(ctx context.Context, wuxing string, limit int) ([]PoemCharResult, error)
	// FindPair finds same-sentence character pairs with matching wu_xing.
	FindPair(ctx context.Context, wuxing1, wuxing2 string, limit int) ([]PoemPairResult, error)
	// GetSentence returns the sentence of the character at a poem position.
	GetSentence(ctx context.Context, poemID int, position int) (string, error)
	// Origin returns the most authoritative source for a character.
	Origin(ctx context.Context, char string) (*PoemCharResult, error)
}

// Namer is the default PoetryNamer implementation backed by the repository.
type Namer struct {
	repo *repository.Repository
}

// NewNamer creates a poetry Namer on top of the repository.
func NewNamer(repo *repository.Repository) *Namer {
	return &Namer{repo: repo}
}

// FindByChar finds poems containing the given character.
func (n *Namer) FindByChar(ctx context.Context, char string, limit int) ([]PoemCharResult, error) {
	return n.repo.FindPoemCharsByChar(ctx, char, limit)
}

// FindByWuXing finds suitable characters by wu_xing with a poem source.
func (n *Namer) FindByWuXing(ctx context.Context, wuxing string, limit int) ([]PoemCharResult, error) {
	return n.repo.FindPoemCharsByWuXing(ctx, wuxing, limit)
}

// FindPair finds same-sentence character pairs with matching wu_xing.
func (n *Namer) FindPair(ctx context.Context, wuxing1, wuxing2 string, limit int) ([]PoemPairResult, error) {
	return n.repo.FindPoemPairs(ctx, wuxing1, wuxing2, limit)
}

// GetSentence returns the sentence covering the character at position.
func (n *Namer) GetSentence(ctx context.Context, poemID int, position int) (string, error) {
	return n.repo.PoemSentenceAt(ctx, poemID, position)
}

// Origin returns the most authoritative poem source for a character.
func (n *Namer) Origin(ctx context.Context, char string) (*PoemCharResult, error) {
	return n.repo.BestPoemOrigin(ctx, char)
}

// Search performs keyword/filter/paged poetry search via the repository.
func (n *Namer) Search(ctx context.Context, keyword, author, dynasty, ptype string, page, size int) ([]PoemCharResult, int, error) {
	return n.repo.SearchPoems(ctx, keyword, author, dynasty, ptype, page, size)
}

// authorityWeights ranks source types by naming-tradition authority:
// 诗经/经 > 唐诗 > 宋词 > 楚辞 > 其他.
var authorityWeights = map[string]float64{
	"jing":  1.0,
	"shi":   0.9,
	"ci":    0.8,
	"fu":    0.75,
	"other": 0.5,
}

// AuthorityScore returns the authority weight (0-1) of a poem type.
func AuthorityScore(poemType string) float64 {
	if w, ok := authorityWeights[poemType]; ok {
		return w
	}
	return 0.5
}

// auspiciousChars are characters whose presence suggests a positive meaning.
var auspiciousChars = map[rune]bool{
	'吉': true, '祥': true, '瑞': true, '安': true, '宁': true, '康': true,
	'乐': true, '福': true, '喜': true, '庆': true, '嘉': true, '美': true,
	'善': true, '贤': true, '慧': true, '雅': true, '清': true, '明': true,
	'志': true, '德': true, '和': true, '泰': true, '锦': true, '华': true,
	'月': true, '风': true, '云': true, '霖': true, '泽': true, '春': true,
}

// SentenceJiBonus scores the auspiciousness of a sentence (0-1).
func SentenceJiBonus(sentence string) float64 {
	if sentence == "" {
		return 0
	}
	runes := []rune(sentence)
	count := 0
	for _, r := range runes {
		if auspiciousChars[r] {
			count++
		}
	}
	if count == 0 {
		return 0
	}
	bonus := float64(count) / float64(len(runes)) * 4
	if bonus > 1 {
		bonus = 1
	}
	return bonus
}

// PairScore computes a 0-1 quality score for a poem pair result:
// authority of the source + auspicious meaning + same-sentence coherence.
func PairScore(p PoemPairResult) float64 {
	score := AuthorityScore(p.Type)*0.6 + SentenceJiBonus(p.Sentence)*0.4
	return score
}
