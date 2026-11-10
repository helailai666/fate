package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/babyname/fate/v4/ent"
	"github.com/babyname/fate/v4/ent/character"
	_ "github.com/sqlite3ent/sqlite3"
)

// poemRecord is a normalized poetry entry extracted from the chinese-poetry
// dataset (https://github.com/chinese-poetry/chinese-poetry).
type poemRecord struct {
	Title      string
	Author     string
	Paragraphs []string
}

type sourceInfo struct {
	kind    string // shi / ci / fu / jing / other
	dynasty string
	skip    bool
}

var (
	hanziRe          = regexp.MustCompile(`\p{Han}`)
	sentenceSplitRe  = regexp.MustCompile(`[。！？；!?;…]+`)
	traditionalChars = regexp.MustCompile(`[壹贰叁肆伍陆柒捌玖拾佰仟萬]`)
)

const (
	contextLimit = 200
	insertPoems  = 2000
	insertChars  = 20000
)

// classifySource maps a dataset file path to its poem type and dynasty.
// The dataset uses top-level Chinese directory names.
func classifySource(rel string) sourceInfo {
	rel = filepath.ToSlash(rel)
	parts := strings.SplitN(rel, "/", 2)
	top := parts[0]
	name := rel
	if len(parts) > 1 {
		name = parts[1]
	}
	switch {
	// Non-content files.
	case strings.Contains(rel, "/error/"),
		strings.Contains(name, "authors"),
		strings.Contains(name, "表面结构字"),
		strings.HasSuffix(name, "intro.json"),
		strings.HasSuffix(name, "preface.json"),
		strings.HasSuffix(name, "README.md"):
		return sourceInfo{skip: true}
	}

	switch {
	case strings.HasPrefix(name, "poet.song"):
		return sourceInfo{kind: "shi", dynasty: "宋"}
	case top == "全唐诗", top == "水墨唐诗", top == "御定全唐詩":
		return sourceInfo{kind: "shi", dynasty: "唐"}
	case top == "曹操诗集":
		return sourceInfo{kind: "shi", dynasty: "汉"}
	case strings.Contains(name, "qianjiashi"), // 千家诗
		strings.Contains(name, "tangshisanbaishou"): // 唐诗三百首(蒙学版)
		return sourceInfo{kind: "shi", dynasty: "唐"}
	case top == "宋词":
		return sourceInfo{kind: "ci", dynasty: "宋"}
	case top == "五代诗词":
		return sourceInfo{kind: "ci", dynasty: "五代"}
	case top == "纳兰性德":
		return sourceInfo{kind: "ci", dynasty: "清"}
	case top == "楚辞":
		return sourceInfo{kind: "fu", dynasty: "先秦"}
	case top == "诗经":
		return sourceInfo{kind: "jing", dynasty: "先秦"}
	case top == "论语":
		return sourceInfo{kind: "jing", dynasty: "春秋"}
	case top == "四书五经":
		return sourceInfo{kind: "jing", dynasty: "先秦"}
	case top == "元曲":
		return sourceInfo{kind: "other", dynasty: "元"}
	case top == "幽梦影":
		return sourceInfo{kind: "other", dynasty: "清"}
	case top == "蒙学":
		return sourceInfo{kind: "other"}
	default:
		return sourceInfo{kind: "other"}
	}
}

// loadPoemRecords parses one dataset JSON file into normalized records.
// The dataset uses many slightly different shapes; the parser accepts
// records as dicts or lists and flattens nested dict content.
func loadPoemRecords(path string) ([]poemRecord, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var raw any
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, err
	}
	var records []poemRecord
	flatten(raw, "", &records)
	return records, nil
}

// flatten converts arbitrary dataset shapes into normalized poem records.
func flatten(node any, parentTitle string, out *[]poemRecord) {
	switch v := node.(type) {
	case []any:
		isDicts := false
		for _, item := range v {
			if _, ok := item.(map[string]any); ok {
				isDicts = true
				break
			}
		}
		if isDicts {
			for _, item := range v {
				flatten(item, parentTitle, out)
			}
			return
		}
		// Plain list of strings: paragraphs of the current parent record.
		var paras []string
		for _, item := range v {
			if s, ok := item.(string); ok && strings.TrimSpace(s) != "" {
				paras = append(paras, s)
			}
		}
		if len(paras) > 0 {
			*out = append(*out, poemRecord{Title: parentTitle, Paragraphs: paras})
		}
	case map[string]any:
		title := str(v["title"])
		if title == "" {
			title = str(v["rhythmic"])
		}
		if title == "" {
			title = str(v["section"])
		}
		if title == "" {
			title = str(v["chapter"])
		}
		if parentTitle != "" && title != "" && title != parentTitle {
			title = parentTitle + "·" + title
		} else if title == "" {
			title = parentTitle
		}
		author := str(v["author"])

		if paras := strList(v["paragraphs"]); len(paras) > 0 {
			*out = append(*out, poemRecord{Title: title, Author: author, Paragraphs: paras})
		}
		if paras := strList(v["para"]); len(paras) > 0 {
			*out = append(*out, poemRecord{Title: title, Author: author, Paragraphs: paras})
		}
		if paras := strList(v["content"]); len(paras) > 0 {
			*out = append(*out, poemRecord{Title: title, Author: author, Paragraphs: paras})
		}
		// Nested dict content: recurse with the parent title context.
		if sub, ok := v["content"].([]any); ok {
			for _, item := range sub {
				if _, isDict := item.(map[string]any); isDict {
					flatten(item, title, out)
				}
			}
		}
	}
}

func str(v any) string {
	s, _ := v.(string)
	return strings.TrimSpace(s)
}

func strList(v any) []string {
	list, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(list))
	for _, item := range list {
		if s, ok := item.(string); ok && strings.TrimSpace(s) != "" {
			out = append(out, s)
		}
	}
	return out
}

// splitSentences splits a paragraph into sentence-level units.
func splitSentences(paragraph string) []string {
	parts := sentenceSplitRe.Split(paragraph, -1)
	sentences := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if hanziRe.MatchString(p) {
			sentences = append(sentences, p)
		}
	}
	return sentences
}

// extractChars returns the unique nameable hanzi of a sentence in order of
// first appearance. Traditional characters are converted via the t2s map.
func extractChars(sentence string, nameable map[string]bool, t2s map[string]string) []string {
	seen := make(map[rune]bool)
	var chars []string
	for _, r := range sentence {
		if seen[r] {
			continue
		}
		if !hanziRe.MatchString(string(r)) {
			continue
		}
		seen[r] = true
		c := string(r)
		if !nameable[c] {
			if mapped, ok := t2s[c]; ok {
				c = mapped
			} else {
				continue
			}
		}
		chars = append(chars, c)
	}
	return chars
}

func truncate(s string) string {
	s = strings.TrimSpace(s)
	runes := []rune(s)
	if len(runes) > contextLimit {
		s = string(runes[:contextLimit])
	}
	return s
}

type poemCharRow struct {
	poemID   int
	char     string
	position int
	sentence string
	context  string
}

type seeder struct {
	tx       *sql.Tx
	nameable map[string]bool
	t2s      map[string]string

	poemID   int
	poemRows [][]any
	charRows []poemCharRow
	charID   int

	poems     int
	poemChars int
}

func (s *seeder) flushPoems() error {
	if len(s.poemRows) == 0 {
		return nil
	}
	stmt, err := s.tx.Prepare(`INSERT INTO poem (id, title, author, dynasty, content, keywords, tags, type, source) VALUES (?,?,?,?,?,?,?,?,?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, row := range s.poemRows {
		if _, err := stmt.Exec(row...); err != nil {
			return err
		}
	}
	s.poems += len(s.poemRows)
	s.poemRows = s.poemRows[:0]
	return nil
}

func (s *seeder) flushChars() error {
	if len(s.charRows) == 0 {
		return nil
	}
	stmt, err := s.tx.Prepare(`INSERT INTO poem_char (id, poem_id, char, position, sentence, context) VALUES (?,?,?,?,?,?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, r := range s.charRows {
		if _, err := stmt.Exec(s.charID, r.poemID, r.char, r.position, r.sentence, r.context); err != nil {
			return err
		}
		s.charID++
	}
	s.poemChars += len(s.charRows)
	s.charRows = s.charRows[:0]
	return nil
}

func (s *seeder) seedRecords(records []poemRecord, info sourceInfo, source string) error {
	for _, rec := range records {
		if len(rec.Paragraphs) == 0 {
			continue
		}
		title := rec.Title
		if title == "" {
			title = rec.Author
		}
		if title == "" {
			title = "无题"
		}

		s.poemID++
		s.poemRows = append(s.poemRows, []any{
			s.poemID, truncate(title), truncate(rec.Author), info.dynasty,
			strings.Join(rec.Paragraphs, "\n"), "[]", "[]", info.kind, source,
		})

		// Extract unique nameable chars with first occurrence info.
		pos := 0
		seen := make(map[string]bool)
		for _, paragraph := range rec.Paragraphs {
			for _, sentence := range splitSentences(paragraph) {
				for _, c := range extractChars(sentence, s.nameable, s.t2s) {
					if seen[c] {
						continue
					}
					seen[c] = true
					s.charRows = append(s.charRows, poemCharRow{
						poemID:   s.poemID,
						char:     c,
						position: pos,
						sentence: truncate(sentence),
						context:  truncate(paragraph),
					})
				}
				for range hanziRe.FindAllString(sentence, -1) {
					pos++
				}
			}
		}

		if len(s.poemRows) >= insertPoems {
			if err := s.flushPoems(); err != nil {
				return err
			}
		}
		if len(s.charRows) >= insertChars {
			// Poems must be flushed before their chars to satisfy the FK.
			if err := s.flushPoems(); err != nil {
				return err
			}
			if err := s.flushChars(); err != nil {
				return err
			}
		}
	}
	return nil
}

func main() {
	dbName := flag.String("db", "fate", "database file name (without extension)")
	poetryDir := flag.String("poetry", "data/chinese-poetry", "chinese-poetry dataset directory")
	limit := flag.Int("limit", 0, "max records per file to import (0 = unlimited)")
	flag.Parse()

	// Normalize the database file name (the sqlite3 driver does not add
	// an extension automatically).
	name := *dbName
	if !strings.HasSuffix(name, ".db") {
		name += ".db"
	}
	dsn := fmt.Sprintf("file:%s?cache=shared&_journal=WAL&_fk=1", name)
	client, err := ent.Open("sqlite3", dsn)
	if err != nil {
		log.Fatalf("Failed to open database: %v", err)
	}
	defer client.Close()

	if err := client.Schema.Create(context.Background()); err != nil {
		log.Fatalf("Failed to create schema: %v", err)
	}

	// Load the nameable character set and the traditional->simplified map.
	chars, err := client.Character.Query().
		Where(character.Nameable(true)).
		Select(character.FieldChar).
		Strings(context.Background())
	if err != nil {
		log.Fatalf("Failed to load characters: %v", err)
	}
	nameable := make(map[string]bool, len(chars))
	for _, c := range chars {
		nameable[c] = true
	}
	log.Printf("Loaded %d nameable characters", len(nameable))

	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		log.Fatalf("Failed to open raw database: %v", err)
	}
	defer db.Close()

	t2s := make(map[string]string)
	rows, err := db.Query(`SELECT a.char, b.char FROM character a
		JOIN character b ON a.character_traditional_to_simplified = b.id
		WHERE b.nameable = 1`)
	if err != nil {
		log.Fatalf("Failed to load t2s map: %v", err)
	}
	for rows.Next() {
		var t, s string
		if err := rows.Scan(&t, &s); err == nil {
			t2s[t] = s
		}
	}
	rows.Close()
	log.Printf("Loaded %d traditional->simplified mappings", len(t2s))

	// Collect dataset files.
	var files []string
	err = filepath.WalkDir(*poetryDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == ".git" || d.Name() == "error" || d.Name() == "strains" || d.Name() == "rank" || d.Name() == "loader" || d.Name() == "images" {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(d.Name(), ".json") {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		log.Fatalf("Failed to list dataset files: %v", err)
	}
	sort.Strings(files)
	log.Printf("Found %d JSON files in %s", len(files), *poetryDir)

	// Resume counters from existing data (idempotent re-runs).
	var maxPoemID, maxCharID int
	_ = db.QueryRow(`SELECT COALESCE(MAX(id),0) FROM poem`).Scan(&maxPoemID)
	_ = db.QueryRow(`SELECT COALESCE(MAX(id),0) FROM poem_char`).Scan(&maxCharID)

	// Ensure the import log table (tracks completed files for resumable runs).
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS poem_import_log (
		source TEXT PRIMARY KEY, imported_unix INTEGER NOT NULL)`); err != nil {
		log.Fatalf("Failed to create import log: %v", err)
	}

	s := &seeder{nameable: nameable, t2s: t2s, poemID: maxPoemID, charID: maxCharID + 1}

	for _, f := range files {
		rel, _ := filepath.Rel(*poetryDir, f)
		info := classifySource(rel)
		if info.skip {
			continue
		}
		source := filepath.Base(f)

		// Skip files fully imported by a previous run.
		var imported int
		_ = db.QueryRow(`SELECT COUNT(*) FROM poem_import_log WHERE source = ?`, source).Scan(&imported)
		if imported > 0 {
			continue
		}

		records, err := loadPoemRecords(f)
		if err != nil {
			log.Printf("Skip %s: %v", rel, err)
			continue
		}
		if *limit > 0 {
			records = records[:min(len(records), *limit)]
		}
		if len(records) == 0 {
			continue
		}

		// Remove partial rows from an interrupted previous run of this file.
		tx, err := db.Begin()
		if err != nil {
			log.Fatalf("Failed to begin transaction: %v", err)
		}
		if _, err := tx.Exec(`DELETE FROM poem_char WHERE poem_id IN (SELECT id FROM poem WHERE source = ?)`, source); err != nil {
			_ = tx.Rollback()
			log.Fatalf("Failed to clean %s: %v", source, err)
		}
		if _, err := tx.Exec(`DELETE FROM poem WHERE source = ?`, source); err != nil {
			_ = tx.Rollback()
			log.Fatalf("Failed to clean %s: %v", source, err)
		}
		if err := tx.Commit(); err != nil {
			log.Fatalf("Failed to commit cleanup: %v", err)
		}

		tx, err = db.Begin()
		if err != nil {
			log.Fatalf("Failed to begin transaction: %v", err)
		}
		s.tx = tx
		if err := s.seedRecords(records, info, source); err != nil {
			_ = tx.Rollback()
			log.Printf("Skip %s: %v", rel, err)
			continue
		}
		if err := s.flushPoems(); err != nil {
			_ = tx.Rollback()
			log.Printf("Skip %s: %v", rel, err)
			continue
		}
		if err := s.flushChars(); err != nil {
			_ = tx.Rollback()
			log.Printf("Skip %s: %v", rel, err)
			continue
		}
		if err := tx.Commit(); err != nil {
			log.Fatalf("Failed to commit %s: %v", rel, err)
		}
		if _, err := db.Exec(`INSERT OR REPLACE INTO poem_import_log (source, imported_unix) VALUES (?, strftime('%s','now'))`, source); err != nil {
			log.Printf("Warning: failed to log import for %s: %v", source, err)
		}
		log.Printf("Imported %-40s type=%-5s dynasty=%-4s poems=%d poem_chars=%d",
			rel, info.kind, info.dynasty, s.poems, s.poemChars)
	}

	// Backfill character.has_poetry from poem_char occurrences.
	res, err := db.Exec(`UPDATE character SET has_poetry = 1 WHERE char IN (SELECT DISTINCT char FROM poem_char)`)
	if err != nil {
		log.Fatalf("Failed to backfill has_poetry: %v", err)
	}
	updated, _ := res.RowsAffected()
	log.Printf("Backfilled has_poetry on %d characters", updated)
	log.Printf("Done: poems=%d poem_chars=%d", s.poems, s.poemChars)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
