// Package ui serves a loopback, read-only view of saved runs.
package ui

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/evolvectl/evolvectl/internal/domain"
	"github.com/evolvectl/evolvectl/internal/report"
	"github.com/evolvectl/evolvectl/internal/state"
)

// Server is a read-only localhost dashboard.
type Server struct {
	Store *state.Store
}

// Handler returns the HTTP handler. It rejects non-loopback Host values and non-GET methods.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.page)
	mux.HandleFunc("/runs", s.page)
	mux.HandleFunc("/runs/", s.page)
	mux.HandleFunc("/dependencies", s.page)
	mux.HandleFunc("/copybara", s.page)
	mux.HandleFunc("/changes", s.page)
	mux.HandleFunc("/validations", s.page)
	mux.HandleFunc("/settings", s.page)
	mux.HandleFunc("/api/v1/runs", s.apiRuns)
	mux.HandleFunc("/api/v1/runs/", s.apiRun)
	mux.HandleFunc("/api/v1/dependencies", s.apiDeps)
	mux.HandleFunc("/api/v1/events", s.apiEvents)
	mux.HandleFunc("/api/v1/copybara/workflows", s.apiCopybara)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !loopbackHost(r.Host) {
			http.Error(w, "host is not loopback", http.StatusForbidden)
			return
		}
		if origin := r.Header.Get("Origin"); origin != "" && !loopbackOrigin(origin) {
			http.Error(w, "origin is not loopback", http.StatusForbidden)
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "read-only dashboard", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; script-src 'unsafe-inline'; base-uri 'none'; form-action 'none'")
		mux.ServeHTTP(w, r)
	})
}

// Listen starts on a loopback address. Port 0 selects a free port.
func Listen(host, port string) (net.Listener, string, error) {
	if host == "" {
		host = "127.0.0.1"
	}
	if !isLoopbackName(host) {
		return nil, "", fmt.Errorf("refusing to bind %s; use 127.0.0.1 or localhost", host)
	}
	if port == "" {
		port = "0"
	}
	ln, err := net.Listen("tcp", net.JoinHostPort(host, port))
	if err != nil {
		return nil, "", err
	}
	return ln, ln.Addr().String(), nil
}

func (s *Server) page(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/runs/")
	if r.URL.Path == "/runs" || r.URL.Path == "/" || id == "" || id == "/runs" {
		runs, _ := s.Store.ListReports()
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(w, "<!DOCTYPE html><html lang=\"en\"><head><meta charset=\"utf-8\"><title>Evolvectl runs</title></head><body><h1>Runs</h1><ul>")
		if len(runs) == 0 {
			fmt.Fprintf(w, "<li>No runs yet. Run evolvectl scan or evolvectl upgrade.</li>")
		}
		for _, run := range runs {
			fmt.Fprintf(w, "<li><a href=\"/runs/%s\">%s</a> %s %s:%s</li>", templateEscape(run.ID), templateEscape(run.ID), templateEscape(run.Outcome), templateEscape(run.Target.Ecosystem), templateEscape(run.Target.Name))
		}
		fmt.Fprintf(w, "</ul><p>Read-only. Provider %s is whatever the serve process inherited; it is not saved for other terminals.</p></body></html>", templateEscape(r.Header.Get("X-Evolvectl-Provider")))
		return
	}
	id = strings.Trim(id, "/")
	rep, err := s.Store.LoadReport(id)
	if err != nil {
		http.Error(w, "run not found", http.StatusNotFound)
		return
	}
	body, err := report.HTML(rep)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(body)
}

func (s *Server) apiRuns(w http.ResponseWriter, r *http.Request) {
	runs, err := s.Store.ListReports()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	type row struct {
		ID, Outcome, Ecosystem, Name, To string
		Created                          time.Time
	}
	out := []row{}
	for _, run := range runs {
		out = append(out, row{run.ID, run.Outcome, run.Target.Ecosystem, run.Target.Name, run.Target.To, run.CreatedAt})
	}
	writeJSON(w, map[string]any{"runs": out})
}

func (s *Server) apiRun(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/v1/runs/")
	rep, err := s.Store.LoadReport(id)
	if err != nil {
		http.Error(w, "run not found", http.StatusNotFound)
		return
	}
	writeJSON(w, rep)
}

func (s *Server) apiDeps(w http.ResponseWriter, r *http.Request) {
	rep, err := s.pick(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	eco := r.URL.Query().Get("ecosystem")
	q := strings.ToLower(r.URL.Query().Get("q"))
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}
	size, _ := strconv.Atoi(r.URL.Query().Get("page_size"))
	if size < 1 || size > 500 {
		size = 50
	}
	var filtered []domain.Dependency
	for _, d := range rep.Inventory.Dependencies {
		if eco != "" && d.Ecosystem != eco {
			continue
		}
		if q != "" && !strings.Contains(strings.ToLower(d.Name), q) {
			continue
		}
		filtered = append(filtered, d)
	}
	start := (page - 1) * size
	if start > len(filtered) {
		start = len(filtered)
	}
	end := start + size
	if end > len(filtered) {
		end = len(filtered)
	}
	writeJSON(w, map[string]any{
		"run_id": rep.ID, "page": page, "page_size": size, "total": len(filtered),
		"data_as_of": rep.DataAsOf, "dependencies": filtered[start:end],
	})
}

func (s *Server) apiEvents(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("run_id")
	ev, err := s.Store.Events(id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok || r.URL.Query().Get("stream") != "1" {
		writeJSON(w, map[string]any{"events": ev})
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	for _, e := range ev {
		b, _ := json.Marshal(e)
		fmt.Fprintf(w, "data: %s\n\n", b)
		flusher.Flush()
	}
}

func (s *Server) apiCopybara(w http.ResponseWriter, r *http.Request) {
	rep, err := s.pick(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	writeJSON(w, rep.Copybara)
}

func (s *Server) pick(r *http.Request) (*domain.RunReport, error) {
	id := r.URL.Query().Get("run_id")
	if id == "" {
		runs, err := s.Store.ListReports()
		if err != nil || len(runs) == 0 {
			return nil, fmt.Errorf("no runs")
		}
		return runs[0], nil
	}
	return s.Store.LoadReport(id)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}

func loopbackHost(host string) bool {
	h, _, err := net.SplitHostPort(host)
	if err != nil {
		h = host
	}
	return isLoopbackName(h)
}

func loopbackOrigin(origin string) bool {
	origin = strings.TrimPrefix(origin, "http://")
	origin = strings.TrimPrefix(origin, "https://")
	if i := strings.Index(origin, "/"); i >= 0 {
		origin = origin[:i]
	}
	return loopbackHost(origin)
}

func isLoopbackName(h string) bool {
	if h == "localhost" {
		return true
	}
	ip := net.ParseIP(h)
	return ip != nil && ip.IsLoopback()
}

func templateEscape(s string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&#34;")
	return r.Replace(s)
}
