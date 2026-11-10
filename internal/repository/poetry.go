package repository

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"sync"
)

// PoemCharHit is a poem_char row joined with its poem metadata.
type PoemCharHit struct {
	Char     string `json:"char"`
	PoemID   int    `json:"poem_id"`
	Title    string `json:"title"`
	Author   string `json:"author"`
	Dynasty  string `json:"dynasty"`
	Type     string `json:"type"`
	Sentence string `json:"sentence"`
	Context  string `json:"context"`
	Position int    `json:"position"`
}

// PoemPairHit is a pair of characters co-occurring in the same sentence.
type PoemPairHit struct {
	Char1    string `json:"char1"`
	Char2    string `json:"char2"`
	PoemID   int    `json:"poem_id"`
	Title    string `json:"title"`
	Author   string `json:"author"`
	Dynasty  string `json:"dynasty"`
	Type     string `json:"type"`
	Sentence string `json:"sentence"`
}

// ErrPoetryUnavailable is returned when poetry queries run without a raw DB.
var ErrPoetryUnavailable = errors.New("poetry data unavailable (import poetry data with cmd/seeddb)")

// authorityCase orders results by source authority:
// jing (诗经/经) > shi (唐诗) > ci (宋词) > fu (楚辞) > other.
const authorityCase = `CASE p.type WHEN 'jing' THEN 0 WHEN 'shi' THEN 1 WHEN 'ci' THEN 2 WHEN 'fu' THEN 3 ELSE 4 END`

func (m *Repository) scanPoemCharHit(rows *sql.Rows) (PoemCharHit, error) {
	var h PoemCharHit
	err := rows.Scan(&h.Char, &h.PoemID, &h.Title, &h.Author, &h.Dynasty, &h.Type, &h.Sentence, &h.Context, &h.Position)
	return h, err
}

// FindPoemCharsByChar returns poem sources containing the given character.
func (m *Repository) FindPoemCharsByChar(ctx context.Context, char string, limit int) ([]PoemCharHit, error) {
	if m.rawDB == nil {
		return nil, ErrPoetryUnavailable
	}
	if limit <= 0 {
		limit = 20
	}
	rows, err := m.rawDB.QueryContext(ctx, `
		SELECT pc.char, pc.poem_id, p.title, p.author, p.dynasty, p.type,
		       pc.sentence, pc.context, pc.position
		FROM poem_char pc JOIN poem p ON p.id = pc.poem_id
		WHERE pc.char = ?
		ORDER BY `+authorityCase+`, pc.position, p.id
		LIMIT ?`, char, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var hits []PoemCharHit
	for rows.Next() {
		h, err := m.scanPoemCharHit(rows)
		if err != nil {
			return nil, err
		}
		hits = append(hits, h)
	}
	return hits, rows.Err()
}

// BestPoemOrigin returns the most authoritative poem source for a character.
func (m *Repository) BestPoemOrigin(ctx context.Context, char string) (*PoemCharHit, error) {
	hits, err := m.FindPoemCharsByChar(ctx, char, 1)
	if err != nil {
		return nil, err
	}
	if len(hits) == 0 {
		return nil, nil
	}
	return &hits[0], nil
}

// FindPoemCharsByWuXing returns one poem source per distinct character whose
// wu_xing matches, ordered by source authority.
func (m *Repository) FindPoemCharsByWuXing(ctx context.Context, wuxing string, limit int) ([]PoemCharHit, error) {
	if m.rawDB == nil {
		return nil, ErrPoetryUnavailable
	}
	if limit <= 0 {
		limit = 50
	}
	rows, err := m.rawDB.QueryContext(ctx, `
		SELECT t.char, t.poem_id, p.title, p.author, p.dynasty, p.type,
		       t.sentence, t.context, t.position
		FROM (
			SELECT char, MIN(id) AS min_id
			FROM poem_char
			WHERE char IN (SELECT char FROM character WHERE wu_xing = ? AND nameable = 1)
			GROUP BY char
		) g
		JOIN poem_char t ON t.id = g.min_id
		JOIN poem p ON p.id = t.poem_id
		ORDER BY `+authorityCase+`, t.char
		LIMIT ?`, wuxing, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var hits []PoemCharHit
	for rows.Next() {
		h, err := m.scanPoemCharHit(rows)
		if err != nil {
			return nil, err
		}
		hits = append(hits, h)
	}
	return hits, rows.Err()
}

var (
	wuxingMapOnce sync.Once
	wuxingMap     map[string]string
)

// wuxingByNameable loads char->wu_xing for all nameable characters once.
func (m *Repository) wuxingByNameable(ctx context.Context) (map[string]string, error) {
	if m.rawDB == nil {
		return nil, ErrPoetryUnavailable
	}
	wuxingMapOnce.Do(func() {
		rows, err := m.rawDB.QueryContext(ctx,
			`SELECT char, wu_xing FROM character WHERE nameable = 1`)
		if err != nil {
			return
		}
		defer rows.Close()
		mm := make(map[string]string, 30000)
		for rows.Next() {
			var c, w string
			if err := rows.Scan(&c, &w); err == nil {
				mm[c] = w
			}
		}
		wuxingMap = mm
	})
	if wuxingMap == nil {
		return nil, ErrPoetryUnavailable
	}
	return wuxingMap, nil
}

// FindPoemPairs finds character pairs with the given wu_xing combination that
// co-occur in the same poem sentence. Executed as a bounded two-step query to
// avoid expensive sentence-level joins on large tables.
func (m *Repository) FindPoemPairs(ctx context.Context, wuxing1, wuxing2 string, limit int) ([]PoemPairHit, error) {
	if m.rawDB == nil {
		return nil, ErrPoetryUnavailable
	}
	if limit <= 0 {
		limit = 50
	}

	// Step 1: candidate poems containing a char of wuxing1.
	poemIDs, err := m.poemIDsByWuXing(ctx, wuxing1, 500)
	if err != nil {
		return nil, err
	}
	if len(poemIDs) == 0 {
		return nil, nil
	}

	// Step 2: all chars of those poems, grouped by sentence.
	type row struct {
		poemID   int
		char     string
		sentence string
	}
	var sb strings.Builder
	args := make([]any, 0, len(poemIDs))
	sb.WriteString(`SELECT poem_id, char, sentence FROM poem_char WHERE poem_id IN (`)
	for i, id := range poemIDs {
		if i > 0 {
			sb.WriteByte(',')
		}
		sb.WriteByte('?')
		args = append(args, id)
	}
	sb.WriteString(`)`)
	rows, err := m.rawDB.QueryContext(ctx, sb.String(), args...)
	if err != nil {
		return nil, err
	}
	var rs []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.poemID, &r.char, &r.sentence); err == nil {
			rs = append(rs, r)
		}
	}
	rows.Close()

	wuxing, err := m.wuxingByNameable(ctx)
	if err != nil {
		return nil, err
	}

	// Group by (poem_id, sentence) and emit matching pairs.
	type groupKey struct {
		poemID   int
		sentence string
	}
	groups := make(map[groupKey][]string)
	for _, r := range rs {
		k := groupKey{r.poemID, r.sentence}
		groups[k] = append(groups[k], r.char)
	}

	var hits []PoemPairHit
	seen := make(map[string]bool)
	for k, charsInSentence := range groups {
		for i := 0; i < len(charsInSentence) && len(hits) < limit; i++ {
			for j := 0; j < len(charsInSentence) && len(hits) < limit; j++ {
				if i == j {
					continue
				}
				c1, c2 := charsInSentence[i], charsInSentence[j]
				if wuxing[c1] != wuxing1 || wuxing[c2] != wuxing2 {
					continue
				}
				key := c1 + c2
				if seen[key] {
					continue
				}
				seen[key] = true
				hits = append(hits, PoemPairHit{
					Char1:    c1,
					Char2:    c2,
					Sentence: k.sentence,
					PoemID:   k.poemID,
				})
			}
		}
		if len(hits) >= limit {
			break
		}
	}

	// Attach poem metadata.
	if err := m.fillPoemMeta(ctx, hits); err != nil {
		return nil, err
	}
	return hits, nil
}

func (m *Repository) poemIDsByWuXing(ctx context.Context, wuxing string, limit int) ([]int, error) {
	rows, err := m.rawDB.QueryContext(ctx, `
		SELECT DISTINCT pc.poem_id
		FROM poem_char pc JOIN character c ON c.char = pc.char
		WHERE c.wu_xing = ? AND c.nameable = 1
		LIMIT ?`, wuxing, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []int
	for rows.Next() {
		var id int
		if err := rows.Scan(&id); err == nil {
			ids = append(ids, id)
		}
	}
	return ids, rows.Err()
}

func (m *Repository) fillPoemMeta(ctx context.Context, hits []PoemPairHit) error {
	if len(hits) == 0 {
		return nil
	}
	ids := make(map[int]bool, len(hits))
	for _, h := range hits {
		ids[h.PoemID] = true
	}
	type meta struct{ title, author, dynasty, ptype string }
	metas := make(map[int]meta, len(ids))
	var sb strings.Builder
	args := make([]any, 0, len(ids))
	sb.WriteString(`SELECT id, title, author, dynasty, type FROM poem WHERE id IN (`)
	i := 0
	for id := range ids {
		if i > 0 {
			sb.WriteByte(',')
		}
		sb.WriteByte('?')
		args = append(args, id)
		i++
	}
	sb.WriteString(`)`)
	rows, err := m.rawDB.QueryContext(ctx, sb.String(), args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id int
		var mt meta
		if err := rows.Scan(&id, &mt.title, &mt.author, &mt.dynasty, &mt.ptype); err == nil {
			metas[id] = mt
		}
	}
	for k := range hits {
		if mt, ok := metas[hits[k].PoemID]; ok {
			hits[k].Title, hits[k].Author = mt.title, mt.author
			hits[k].Dynasty, hits[k].Type = mt.dynasty, mt.ptype
		}
	}
	return rows.Err()
}

// PoemPairBoost returns a sampled set of character pairs ("c1c2") that
// co-occur in the same poem sentence, restricted to the given character pool.
// Used to add a coherence bonus during name scoring.
func (m *Repository) PoemPairBoost(ctx context.Context, chars []string, maxRows, maxPairs int) (map[string]struct{}, error) {
	if m.rawDB == nil {
		return nil, ErrPoetryUnavailable
	}
	if maxRows <= 0 {
		maxRows = 150000
	}
	if maxPairs <= 0 {
		maxPairs = 100000
	}

	type row struct {
		poemID   int
		sentence string
		char     string
	}
	var rs []row
	for start := 0; start < len(chars) && len(rs) < maxRows; start += 400 {
		end := start + 400
		if end > len(chars) {
			end = len(chars)
		}
		chunk := chars[start:end]
		var sb strings.Builder
		args := make([]any, 0, len(chunk)+1)
		sb.WriteString(`SELECT poem_id, sentence, char FROM poem_char
			WHERE char IN (`)
		for i, c := range chunk {
			if i > 0 {
				sb.WriteByte(',')
			}
			sb.WriteByte('?')
			args = append(args, c)
		}
		sb.WriteString(`) AND id % 100 = 0 LIMIT ?`)
		args = append(args, maxRows-len(rs))
		rows, err := m.rawDB.QueryContext(ctx, sb.String(), args...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var r row
			if err := rows.Scan(&r.poemID, &r.sentence, &r.char); err == nil {
				rs = append(rs, r)
			}
		}
		rows.Close()
	}

	type groupKey struct {
		poemID   int
		sentence string
	}
	groups := make(map[groupKey][]string)
	for _, r := range rs {
		k := groupKey{r.poemID, r.sentence}
		groups[k] = append(groups[k], r.char)
	}

	boost := make(map[string]struct{}, 1024)
	for _, charsInSentence := range groups {
		if len(charsInSentence) < 2 || len(charsInSentence) > 24 {
			continue
		}
		for i := 0; i < len(charsInSentence) && len(boost) < maxPairs; i++ {
			for j := i + 1; j < len(charsInSentence) && len(boost) < maxPairs; j++ {
				a, b := charsInSentence[i], charsInSentence[j]
				if a > b {
					a, b = b, a
				}
				boost[a+b] = struct{}{}
			}
		}
		if len(boost) >= maxPairs {
			break
		}
	}
	return boost, nil
}

// SearchPoems searches poems by keyword (single char or text substring) with
// optional author/dynasty/type filters. Returns page results and total count.
func (m *Repository) SearchPoems(ctx context.Context, keyword, author, dynasty, ptype string, page, size int) ([]PoemCharHit, int, error) {
	if m.rawDB == nil {
		return nil, 0, ErrPoetryUnavailable
	}
	if size <= 0 || size > 100 {
		size = 20
	}
	if page <= 0 {
		page = 1
	}

	conds := []string{"1=1"}
	var args []any
	if author != "" {
		conds = append(conds, "p.author = ?")
		args = append(args, author)
	}
	if dynasty != "" {
		conds = append(conds, "p.dynasty = ?")
		args = append(args, dynasty)
	}
	if ptype != "" {
		conds = append(conds, "p.type = ?")
		args = append(args, ptype)
	}
	where := strings.Join(conds, " AND ")

	runes := []rune(keyword)

	// Single-character keyword: use the poem_char index (sentence-level hits).
	if len(runes) == 1 {
		char := string(runes[0])
		var total int
		row := m.rawDB.QueryRowContext(ctx, `
			SELECT COUNT(*) FROM poem_char pc JOIN poem p ON p.id = pc.poem_id
			WHERE pc.char = ? AND `+where, append([]any{char}, args...)...)
		if err := row.Scan(&total); err != nil {
			return nil, 0, err
		}
		rows, err := m.rawDB.QueryContext(ctx, `
			SELECT pc.char, pc.poem_id, p.title, p.author, p.dynasty, p.type,
			       pc.sentence, pc.context, pc.position
			FROM poem_char pc JOIN poem p ON p.id = pc.poem_id
			WHERE pc.char = ? AND `+where+`
			ORDER BY `+authorityCase+`, pc.position, p.id
			LIMIT ? OFFSET ?`, append([]any{char}, append(args, size, (page-1)*size)...)...)
		if err != nil {
			return nil, 0, err
		}
		defer rows.Close()
		var hits []PoemCharHit
		for rows.Next() {
			h, err := m.scanPoemCharHit(rows)
			if err != nil {
				return nil, 0, err
			}
			hits = append(hits, h)
		}
		return hits, total, rows.Err()
	}

	// Multi-character keyword: substring match on poem content/title/author.
	like := "%" + keyword + "%"
	base := `FROM poem p WHERE (` + where + `) AND (p.content LIKE ? OR p.title LIKE ? OR p.author LIKE ?)`
	likeArgs := append(append([]any{}, args...), like, like, like)
	var total int
	if err := m.rawDB.QueryRowContext(ctx, `SELECT COUNT(*) `+base, likeArgs...).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := m.rawDB.QueryContext(ctx,
		`SELECT p.id, p.title, p.author, p.dynasty, p.type, p.content `+base+`
		 ORDER BY p.id LIMIT ? OFFSET ?`, append(likeArgs, size, (page-1)*size)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var hits []PoemCharHit
	for rows.Next() {
		var id int
		var h PoemCharHit
		var content string
		if err := rows.Scan(&id, &h.Title, &h.Author, &h.Dynasty, &h.Type, &content); err != nil {
			return nil, 0, err
		}
		h.PoemID = id
		h.Sentence = firstSentenceWith(content, keyword)
		h.Context = h.Sentence
		hits = append(hits, h)
	}
	return hits, total, rows.Err()
}

// PoemSentenceAt returns the sentence covering a character position in a poem.
func (m *Repository) PoemSentenceAt(ctx context.Context, poemID int, position int) (string, error) {
	if m.rawDB == nil {
		return "", ErrPoetryUnavailable
	}
	var sentence string
	err := m.rawDB.QueryRowContext(ctx, `
		SELECT sentence FROM poem_char
		WHERE poem_id = ? AND sentence <> '' AND position <= ?
		ORDER BY position DESC LIMIT 1`, poemID, position).Scan(&sentence)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return sentence, err
}

// firstSentenceWith returns the first line of content containing the keyword.
func firstSentenceWith(content, keyword string) string {
	for _, line := range strings.Split(content, "\n") {
		if strings.Contains(line, keyword) {
			return truncateSentence(line)
		}
	}
	return truncateSentence(content)
}

func truncateSentence(s string) string {
	s = strings.TrimSpace(s)
	runes := []rune(s)
	if len(runes) > 120 {
		return string(runes[:120])
	}
	return s
}
