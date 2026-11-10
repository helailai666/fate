package http

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/babyname/fate/v4"
	"github.com/babyname/fate/v4/ent"
	"github.com/babyname/fate/v4/internal/analysis"
	"github.com/babyname/fate/v4/internal/chronosfate"
	"github.com/babyname/fate/v4/internal/repository"
	chronos "github.com/godcong/chronos/v2"
)

type Handler struct {
	fate       fate.Fate
	mux        *http.ServeMux
	store      *taskStore
	staticFS   fs.FS
	fileServer http.Handler
	repo       *repository.Repository
}

type taskEntry struct {
	mu      sync.RWMutex
	result  *fate.GenerateResult
	request generateRequest
	created time.Time
}

func (e *taskEntry) loadResult() *fate.GenerateResult {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.result
}

func (e *taskEntry) storeResult(r *fate.GenerateResult) {
	e.mu.Lock()
	e.result = r
	e.mu.Unlock()
}

type taskStore struct {
	mu    sync.RWMutex
	tasks map[string]*taskEntry
}

type generateRequest struct {
	Surname      string   `json:"surname"`
	Born         string   `json:"born"`
	Sex          string   `json:"sex"`
	PoetryMode   int      `json:"poetry_mode"`
	XiYongMethod string   `json:"xiyong_method"`
	AvoidChars   []string `json:"avoid_chars"`
	RequireChars []string `json:"require_chars"`
	FilterType   int      `json:"filter_type"`
}

type nameScoreRequest struct {
	Surname string `json:"surname"`
	Name1   string `json:"name1"`
	Name2   string `json:"name2"`
	Born    string `json:"born"`
	Sex     string `json:"sex"`
}

// NewHandler creates an initialized Handler with all routes registered.
func NewHandler(f fate.Fate, staticFS fs.FS, repo *repository.Repository) *Handler {
	h := &Handler{
		fate:     f,
		mux:      http.NewServeMux(),
		store:    newTaskStore(),
		staticFS: staticFS,
		repo:     repo,
	}
	h.fileServer = http.FileServer(http.FS(h.staticFS))
	h.registerRoutes()
	return h
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	origin := r.Header.Get("Origin")
	if origin != "" {
		w.Header().Set("Access-Control-Allow-Origin", origin)
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
	}
	if r.Method == http.MethodOptions {
		w.WriteHeader(204)
		return
	}
	h.mux.ServeHTTP(w, r)
}

func (h *Handler) registerRoutes() {
	h.mux.HandleFunc("POST /api/generate", h.handleGenerate)
	h.mux.HandleFunc("GET /api/generate/status", h.handleStatus)
	h.mux.HandleFunc("GET /api/generate/result", h.handleResult)
	h.mux.HandleFunc("GET /api/generate/names", h.handleNames)
	h.mux.HandleFunc("GET /api/generate/explore", h.handleExplore)
	h.mux.HandleFunc("GET /api/name-detail", h.handleNameDetail)
	h.mux.HandleFunc("POST /api/name-score", h.handleNameScore)
	h.mux.HandleFunc("GET /api/poetry/search", h.handlePoetrySearch)
	h.mux.HandleFunc("GET /api/v1/characters", h.handleCharacters)
	h.mux.HandleFunc("GET /health", h.handleHealth)
	h.mux.HandleFunc("GET /", h.handleStatic)
}

func newTaskStore() *taskStore {
	return &taskStore{tasks: make(map[string]*taskEntry)}
}

func (s *taskStore) Get(key string) (*taskEntry, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	entry, ok := s.tasks[key]
	return entry, ok
}

func (s *taskStore) Set(key string, entry *taskEntry) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tasks[key] = entry
}

func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func (h *Handler) handleGenerate(w http.ResponseWriter, r *http.Request) {
	var req generateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, 400, "invalid request body")
		return
	}
	if req.Surname == "" {
		writeError(w, 400, "surname is required")
		return
	}

	born, err := time.Parse("2006/01/02 15:04", req.Born)
	if err != nil {
		writeError(w, 400, "invalid born date format, use 2006/01/02 15:04")
		return
	}

	sex := fate.SexBoy
	if req.Sex == "girl" {
		sex = fate.SexGirl
	}

	// Generate via the streaming pipeline so scoring runs in parallel across
	// CPU cores; the final batch carries the complete result.
	var result *fate.GenerateResult
	err = h.fate.GenerateStream(r.Context(), fate.GenerateRequest{
		LastName: req.Surname,
		Born:     born,
		Sex:      sex,
		Filter: &fate.FilterOption{
			CharacterFilter:     true,
			RegularFilter:       true,
			WuXingFilter:        true,
			PoetryMode:          req.PoetryMode,
			XiYongMethod:        req.XiYongMethod,
			AvoidCharacters:     req.AvoidChars,
			RequireCharacters:   req.RequireChars,
			CharacterFilterType: fate.CharacterFilterType(req.FilterType),
		},
	}, func(batch fate.StreamBatch) {
		if batch.IsFinal {
			result = batch.Result
		}
	})
	if err != nil {
		writeError(w, 500, "failed to generate: "+err.Error())
		return
	}
	if result == nil {
		writeError(w, 500, "failed to generate: no result")
		return
	}

	taskID := fmt.Sprintf("%d", time.Now().UnixNano())
	entry := &taskEntry{
		result:  result,
		request: req,
		created: time.Now(),
	}
	h.store.Set(taskID, entry)

	writeJSON(w, 200, map[string]any{
		"task_id": taskID,
		"state":   "done",
		"total":   result.ExcellentTable.Len(),
	})
}

func (h *Handler) handleStatus(w http.ResponseWriter, r *http.Request) {
	taskID := r.URL.Query().Get("task_id")
	if taskID == "" {
		writeError(w, 400, "task_id is required")
		return
	}

	_, ok := h.store.Get(taskID)
	if !ok {
		writeError(w, 404, "task not found")
		return
	}

	writeJSON(w, 200, map[string]any{
		"task_id": taskID,
		"state":   "done",
	})
}

func (h *Handler) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]any{"status": "ok"})
}

// handleNameScore scores an existing name (surname + two given-name chars).
func (h *Handler) handleNameScore(w http.ResponseWriter, r *http.Request) {
	var req nameScoreRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, 400, "invalid request body")
		return
	}
	if req.Surname == "" || req.Name1 == "" || req.Name2 == "" {
		writeError(w, 400, "surname, name1 and name2 are required")
		return
	}

	born := time.Now()
	if req.Born != "" {
		var err error
		for _, layout := range []string{
			"2006-01-02T15:04", "2006-01-02T15:04:05",
			"2006/01/02 15:04", "2006-01-02 15:04",
			"2006/01/02", "2006-01-02",
		} {
			born, err = time.Parse(layout, req.Born)
			if err == nil {
				break
			}
		}
		if err != nil {
			writeError(w, 400, "invalid born date format")
			return
		}
	}

	sex := fate.SexBoy
	if req.Sex == "2" || req.Sex == "girl" {
		sex = fate.SexGirl
	}

	ctx := r.Context()

	lookup := func(name string) (*ent.Character, error) {
		c, err := h.repo.GetCharacter(ctx, repository.Char(name))
		if err != nil {
			return nil, err
		}
		return c, nil
	}

	// Surname may be one or two characters (compound surname).
	surnameRunes := []rune(req.Surname)
	surnameChars := make([]*ent.Character, 0, len(surnameRunes))
	for _, sr := range surnameRunes {
		c, err := lookup(string(sr))
		if err != nil {
			if ent.IsNotFound(err) {
				writeError(w, 404, "character not found: "+string(sr))
				return
			}
			writeError(w, 500, "query failed: "+err.Error())
			return
		}
		surnameChars = append(surnameChars, c)
	}

	c1, err := lookup(req.Name1)
	if err != nil {
		if ent.IsNotFound(err) {
			writeError(w, 404, "character not found: "+req.Name1)
			return
		}
		writeError(w, 500, "query failed: "+err.Error())
		return
	}

	c2, err := lookup(req.Name2)
	if err != nil {
		if ent.IsNotFound(err) {
			writeError(w, 404, "character not found: "+req.Name2)
			return
		}
		writeError(w, 500, "query failed: "+err.Error())
		return
	}

	// Surname strokes (science stroke, same as the default filter mode).
	l1 := surnameChars[0].ScienceStroke
	l2 := 0
	if len(surnameChars) > 1 {
		l2 = surnameChars[1].ScienceStroke
	}

	fateData, err := chronosfate.GetFateData(chronosfate.FateInput{
		Calendar:     chronos.NewSolarCalendar(born),
		Gender:       int(sex),
		XiYongMethod: chronosfate.XiYongMethodBalance,
	})
	if err != nil {
		fateData = nil
	}

	nr := analysis.BuildNameResult(0, req.Surname, c1, c2, l1, l2, fateData,
		analysis.WithSurnamePinyin(surnamePinyinOf(surnameChars[0])))

	// Attach poetry sources when available.
	for _, ci := range []*ent.Character{c1, c2} {
		hit, err := h.repo.BestPoemOrigin(ctx, ci.Char)
		if err != nil || hit == nil {
			continue
		}
		origin := &analysis.PoetryOrigin{
			Title:    hit.Title,
			Author:   hit.Author,
			Dynasty:  hit.Dynasty,
			Sentence: hit.Sentence,
			Type:     hit.Type,
		}
		if ci.Char == c1.Char {
			nr.Char1.PoetryOrigin = origin
			nr.Char1.HasPoetry = true
		}
		if ci.Char == c2.Char {
			nr.Char2.PoetryOrigin = origin
			nr.Char2.HasPoetry = true
		}
	}

	writeJSON(w, 200, map[string]any{"name_result": nr})
}

// surnamePinyinOf returns the first pinyin reading of a character.
func surnamePinyinOf(c *ent.Character) string {
	if c != nil && len(c.Pinyin) > 0 {
		return c.Pinyin[0]
	}
	return ""
}

// handlePoetrySearch searches poetry by keyword (single char or text) with
// optional author/dynasty/type filters. Degrades gracefully with an empty
// result set when the poetry dataset has not been imported (see data/README.md).
func (h *Handler) handlePoetrySearch(w http.ResponseWriter, r *http.Request) {
	keyword := r.URL.Query().Get("keyword")
	if keyword == "" {
		keyword = r.URL.Query().Get("char")
	}
	if keyword == "" {
		writeError(w, 400, "keyword is required")
		return
	}

	page := 1
	if p := r.URL.Query().Get("page"); p != "" {
		fmt.Sscanf(p, "%d", &page)
		if page < 1 {
			page = 1
		}
	}
	size := 20
	if s := r.URL.Query().Get("size"); s != "" {
		fmt.Sscanf(s, "%d", &size)
		if size <= 0 || size > 100 {
			size = 20
		}
	}

	author := r.URL.Query().Get("author")
	dynasty := r.URL.Query().Get("dynasty")
	ptype := r.URL.Query().Get("type")

	hits, total, err := h.repo.SearchPoems(r.Context(), keyword, author, dynasty, ptype, page, size)
	if err != nil {
		if errors.Is(err, repository.ErrPoetryUnavailable) {
			writeJSON(w, 200, map[string]any{
				"results":          []any{},
				"total":            0,
				"page":             page,
				"size":             size,
				"poetry_available": false,
			})
			return
		}
		writeError(w, 500, "search failed: "+err.Error())
		return
	}

	if hits == nil {
		hits = []repository.PoemCharHit{}
	}
	writeJSON(w, 200, map[string]any{
		"results":          hits,
		"total":            total,
		"page":             page,
		"size":             size,
		"poetry_available": true,
	})
}

func (h *Handler) handleResult(w http.ResponseWriter, r *http.Request) {
	taskID := r.URL.Query().Get("task_id")
	if taskID == "" {
		writeError(w, 400, "task_id is required")
		return
	}

	entry, ok := h.store.Get(taskID)
	if !ok {
		writeError(w, 404, "task not found")
		return
	}

	result := entry.loadResult()
	if result == nil {
		writeError(w, 500, "result not available")
		return
	}

	topNames := result.TopNames

	top3 := make([]fate.NameResult, 0, 3)
	for i := range topNames {
		if i >= 3 {
			break
		}
		top3 = append(top3, topNames[i])
	}

	top10Entries := result.ExcellentTable.TopN(10)
	for i := range top10Entries {
		result.ExcellentTable.MarkShown(top10Entries[i].Char1, top10Entries[i].Char2)
	}

	writeJSON(w, 200, map[string]any{
		"task_id":   taskID,
		"top_names": topNames,
		"top3":      top3,
		"top10":     top10Entries,
		"total":     result.ExcellentTable.Len(),
	})
}

func (h *Handler) handleExplore(w http.ResponseWriter, r *http.Request) {
	taskID := r.URL.Query().Get("task_id")
	if taskID == "" {
		writeError(w, 400, "task_id is required")
		return
	}

	entry, ok := h.store.Get(taskID)
	if !ok {
		writeError(w, 404, "task not found")
		return
	}

	result := entry.loadResult()
	if result == nil {
		writeError(w, 500, "result not available")
		return
	}

	table := result.ExcellentTable

	count := 10
	if c := r.URL.Query().Get("count"); c != "" {
		fmt.Sscanf(c, "%d", &count)
		if count <= 0 || count > 100 {
			count = 10
		}
	}

	hasPoetry := r.URL.Query().Get("has_poetry")
	wuxing := r.URL.Query().Get("wuxing")

	var filter func(fate.ExcellentEntry) bool
	if hasPoetry == "true" || wuxing != "" {
		filter = func(e fate.ExcellentEntry) bool {
			if hasPoetry == "true" && !e.HasPoetry {
				return false
			}
			if wuxing != "" && e.WuXing1 != wuxing && e.WuXing2 != wuxing {
				return false
			}
			return true
		}
	}

	names := table.Explore(count, filter)

	writeJSON(w, 200, map[string]any{
		"task_id":     taskID,
		"names":       names,
		"shown_count": table.ShownCount(),
		"total":       table.Len(),
	})
}

func (h *Handler) handleNames(w http.ResponseWriter, r *http.Request) {
	taskID := r.URL.Query().Get("task_id")
	if taskID == "" {
		writeError(w, 400, "task_id is required")
		return
	}

	entry, ok := h.store.Get(taskID)
	if !ok {
		writeError(w, 404, "task not found")
		return
	}

	result := entry.loadResult()
	if result == nil {
		writeError(w, 500, "result not available")
		return
	}

	topNames := result.TopNames

	page := 1
	if p := r.URL.Query().Get("page"); p != "" {
		fmt.Sscanf(p, "%d", &page)
		if page < 1 {
			page = 1
		}
	}

	pageSize := 20
	if s := r.URL.Query().Get("size"); s != "" {
		fmt.Sscanf(s, "%d", &pageSize)
		if pageSize <= 0 || pageSize > 100 {
			pageSize = 20
		}
	}

	total := len(topNames)
	start := (page - 1) * pageSize
	if start >= total {
		start = 0
		page = 1
	}
	end := start + pageSize
	if end > total {
		end = total
	}

	pagedNames := topNames[start:end]

	writeJSON(w, 200, map[string]any{
		"task_id": taskID,
		"names":   pagedNames,
		"page":    page,
		"size":    pageSize,
		"total":   total,
	})
}

func (h *Handler) handleNameDetail(w http.ResponseWriter, r *http.Request) {
	taskID := r.URL.Query().Get("task_id")
	char1 := r.URL.Query().Get("char1")
	char2 := r.URL.Query().Get("char2")

	if char1 == "" || char2 == "" {
		writeError(w, 400, "char1, char2 are required")
		return
	}

	if taskID != "" {
		entry, ok := h.store.Get(taskID)
		if ok {
			result := entry.loadResult()
			if result != nil {
				// Search top names for a match.
				for i := range result.TopNames {
					if result.TopNames[i].Char1.Char == char1 && result.TopNames[i].Char2.Char == char2 {
						writeJSON(w, 200, map[string]any{"name_result": result.TopNames[i]})
						return
					}
				}

				// Return partial info from CharMap.
				c1, ok1 := result.CharMap[char1]
				c2, ok2 := result.CharMap[char2]
				if ok1 && ok2 {
					surname := ""
					if len(result.TopNames) > 0 {
						surname = result.TopNames[0].Surname
					}
					nr := fate.NameResult{
						FullName: surname + char1 + char2,
						Surname:  surname,
						Char1:    c1,
						Char2:    c2,
					}
					writeJSON(w, 200, map[string]any{"name_result": nr})
					return
				}
			}
		}
	}

	writeError(w, 404, "name not found")
}

func (h *Handler) handleCharacters(w http.ResponseWriter, r *http.Request) {
	char := r.URL.Query().Get("char")
	if char == "" {
		writeError(w, 400, "character is required")
		return
	}

	ctx := r.Context()
	results, err := h.repo.GetCharacters(ctx, repository.Char(char))
	if err != nil {
		writeError(w, 500, "query failed: "+err.Error())
		return
	}

	if results == nil {
		results = []*ent.Character{}
	}

	writeJSON(w, 200, map[string]any{"characters": results})
}

func (h *Handler) handleStatic(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	if path == "/" || path == "" {
		path = "/index.html"
	}

	// If staticFS is nil, fallback gracefully.
	if h.staticFS == nil {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(200)
		_, _ = w.Write([]byte("Fate API Server\n\nAvailable endpoints:\n  POST /api/generate\n  GET  /api/generate/status\n  GET  /api/generate/result\n  GET  /api/generate/names\n  GET  /api/generate/explore\n  GET  /api/name-detail\n  POST /api/name-score\n  GET  /api/poetry/search\n  GET  /api/v1/characters\n  GET  /health\n"))
		return
	}

	// Try the requested path.
	cleaned := strings.TrimPrefix(path, "/")
	f, err := h.staticFS.Open(cleaned)
	if err != nil {
		// Fallback to index.html for SPA routes.
		f, err = h.staticFS.Open("index.html")
		if err != nil {
			http.NotFound(w, r)
			return
		}
	}
	defer f.Close()

	// Read file content and serve.
	data, err := io.ReadAll(f)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	// Set content type based on extension.
	switch {
	case strings.HasSuffix(path, ".html"):
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
	case strings.HasSuffix(path, ".css"):
		w.Header().Set("Content-Type", "text/css; charset=utf-8")
	case strings.HasSuffix(path, ".js"):
		w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
	case strings.HasSuffix(path, ".png"):
		w.Header().Set("Content-Type", "image/png")
	case strings.HasSuffix(path, ".jpg") || strings.HasSuffix(path, ".jpeg"):
		w.Header().Set("Content-Type", "image/jpeg")
	case strings.HasSuffix(path, ".svg"):
		w.Header().Set("Content-Type", "image/svg+xml")
	case strings.HasSuffix(path, ".ico"):
		w.Header().Set("Content-Type", "image/x-icon")
	case strings.HasSuffix(path, ".json"):
		w.Header().Set("Content-Type", "application/json")
	case strings.HasSuffix(path, ".woff2"):
		w.Header().Set("Content-Type", "font/woff2")
	case strings.HasSuffix(path, ".ttf"):
		w.Header().Set("Content-Type", "font/ttf")
	}

	w.WriteHeader(200)
	_, _ = w.Write(data)
}
