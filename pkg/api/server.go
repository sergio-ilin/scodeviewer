package api

import (
	"encoding/json"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"scodeviewer/pkg/config"
	"scodeviewer/pkg/git"
	"scodeviewer/pkg/models"
)

type Server struct {
	cfgManager *config.Manager
	gitService *git.Service
	staticFS   fs.FS
}

func NewServer(cfgManager *config.Manager, gitService *git.Service, staticFS fs.FS) *Server {
	return &Server{
		cfgManager: cfgManager,
		gitService: gitService,
		staticFS:   staticFS,
	}
}

func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()

	// API endpoints
	mux.HandleFunc("GET /api/projects", s.handleGetProjects)
	mux.HandleFunc("POST /api/projects", s.handleAddProject)
	mux.HandleFunc("DELETE /api/projects", s.handleDeleteProject)
	mux.HandleFunc("POST /api/projects/select", s.handleSelectProject)

	mux.HandleFunc("GET /api/branches", s.handleGetBranches)
	mux.HandleFunc("GET /api/commits", s.handleGetCommits)
	mux.HandleFunc("GET /api/commit", s.handleGetCommitDetail)
	mux.HandleFunc("GET /api/updates", s.handleGetUpdates)
	mux.HandleFunc("POST /api/theme", s.handleSetTheme)

	// Embedded Static files handler
	fileServer := http.FileServer(http.FS(s.staticFS))
	mux.Handle("/", fileServer)

	return s.corsAndGzipMiddleware(mux)
}

func (s *Server) corsAndGzipMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func (s *Server) getActiveRepoPath() string {
	cfg := s.cfgManager.GetConfig()
	return cfg.ActivePath
}

func (s *Server) handleGetProjects(w http.ResponseWriter, r *http.Request) {
	cfg := s.cfgManager.GetConfig()
	writeJSON(w, http.StatusOK, map[string]any{
		"projects":        cfg.Projects,
		"activePath":      cfg.ActivePath,
		"themePreference": cfg.ThemePreference,
	})
}

func (s *Server) handleAddProject(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Path string `json:"path"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request JSON")
		return
	}

	cleanPath := filepath.Clean(strings.TrimSpace(body.Path))
	if cleanPath == "" {
		writeError(w, http.StatusBadRequest, "Path cannot be empty")
		return
	}

	fi, err := os.Stat(cleanPath)
	if err != nil || !fi.IsDir() {
		writeError(w, http.StatusBadRequest, "Directory does not exist")
		return
	}

	isGit := s.gitService.IsGitRepo(cleanPath)
	if !isGit {
		writeError(w, http.StatusBadRequest, "Directory is not a valid git repository")
		return
	}

	project := s.cfgManager.AddProject(cleanPath, isGit)
	writeJSON(w, http.StatusOK, project)
}

func (s *Server) handleDeleteProject(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "Missing id parameter")
		return
	}

	if s.cfgManager.RemoveProject(id) {
		writeJSON(w, http.StatusOK, map[string]bool{"success": true})
	} else {
		writeError(w, http.StatusNotFound, "Project not found")
	}
}

func (s *Server) handleSelectProject(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid JSON")
		return
	}

	proj, found := s.cfgManager.SetActive(body.ID)
	if !found {
		writeError(w, http.StatusNotFound, "Project not found")
		return
	}
	writeJSON(w, http.StatusOK, proj)
}

func (s *Server) handleGetBranches(w http.ResponseWriter, r *http.Request) {
	repoPath := s.getActiveRepoPath()
	if repoPath == "" {
		writeJSON(w, http.StatusOK, []models.BranchInfo{})
		return
	}

	branches, err := s.gitService.GetBranches(repoPath)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, branches)
}

func (s *Server) handleGetCommits(w http.ResponseWriter, r *http.Request) {
	repoPath := s.getActiveRepoPath()
	if repoPath == "" {
		writeJSON(w, http.StatusOK, []models.CommitSummary{})
		return
	}

	q := r.URL.Query()
	branch := q.Get("branch")
	author := q.Get("author")
	filterAI := q.Get("ai") // "all", "ai_only", "human_only"
	isMergeOnly := q.Get("merges") == "true"

	limit := 100
	if l, err := strconv.Atoi(q.Get("limit")); err == nil && l > 0 {
		limit = l
	}

	offset := 0
	if o, err := strconv.Atoi(q.Get("offset")); err == nil && o >= 0 {
		offset = o
	}

	commits, err := s.gitService.GetCommits(repoPath, branch, author, filterAI, isMergeOnly, limit, offset)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, commits)
}

func (s *Server) handleGetCommitDetail(w http.ResponseWriter, r *http.Request) {
	repoPath := s.getActiveRepoPath()
	if repoPath == "" {
		writeError(w, http.StatusBadRequest, "No active repository selected")
		return
	}

	hash := r.URL.Query().Get("hash")
	if hash == "" {
		writeError(w, http.StatusBadRequest, "Missing hash parameter")
		return
	}

	detail, err := s.gitService.GetCommitDetail(repoPath, hash)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, detail)
}

func (s *Server) handleGetUpdates(w http.ResponseWriter, r *http.Request) {
	repoPath := s.getActiveRepoPath()
	if repoPath == "" {
		writeJSON(w, http.StatusOK, &models.UpdatesResult{})
		return
	}

	q := r.URL.Query()
	period := q.Get("period")
	if period == "" {
		period = "7d"
	}

	var sinceTime, untilTime time.Time
	if sStr := q.Get("since"); sStr != "" {
		sinceTime, _ = time.Parse(time.RFC3339, sStr)
	}
	if uStr := q.Get("until"); uStr != "" {
		untilTime, _ = time.Parse(time.RFC3339, uStr)
	}

	limit := 200
	if l, err := strconv.Atoi(q.Get("limit")); err == nil && l > 0 {
		limit = l
	}

	query := models.TimeSliceQuery{
		Period:   period,
		Since:    sinceTime,
		Until:    untilTime,
		Author:   q.Get("author"),
		FilterAI: q.Get("ai"),
		Branch:   q.Get("branch"),
		Limit:    limit,
	}

	result, err := s.gitService.GetUpdates(repoPath, query)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleSetTheme(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Theme string `json:"theme"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid JSON")
		return
	}
	if body.Theme != "system" && body.Theme != "dark" && body.Theme != "light" {
		body.Theme = "system"
	}
	s.cfgManager.SetTheme(body.Theme)
	writeJSON(w, http.StatusOK, map[string]string{"theme": body.Theme})
}
