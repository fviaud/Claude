// Package httpapi exposes the catalogue over a JSON HTTP API.
package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/fviaud/claude/internal/catalogue"
)

// Store is the subset of catalogue.Repository the API needs. It lets tests
// use an in-memory fake.
type Store interface {
	CreateProduct(ctx context.Context, p catalogue.Product) (catalogue.Product, error)
	GetProduct(ctx context.Context, id int64) (catalogue.Product, error)
	ListProducts(ctx context.Context, limit, offset int) ([]catalogue.Product, error)
	ListProductsByCategory(ctx context.Context, categoryID int64, limit, offset int) ([]catalogue.Product, error)
	UpdateProduct(ctx context.Context, p catalogue.Product) (catalogue.Product, error)
	DeleteProduct(ctx context.Context, id int64) error
	AdjustStock(ctx context.Context, id int64, delta int32) (int32, error)
	CreateCategory(ctx context.Context, name string) (catalogue.Category, error)
	ListCategories(ctx context.Context) ([]catalogue.Category, error)
	DeleteCategory(ctx context.Context, id int64) error
}

// Server handles the HTTP requests.
type Server struct {
	store  Store
	ping   func(context.Context) error
	logger *slog.Logger
	keys   apiKeys
	mux    *http.ServeMux
}

// New returns a Server. ping checks the database for /healthz, which stays
// public; every other route requires one of apiKeys. It returns an error if
// apiKeys is empty, so the API is never exposed without protection by mistake.
func New(store Store, ping func(context.Context) error, logger *slog.Logger, apiKeys []string) (*Server, error) {
	if len(apiKeys) == 0 {
		return nil, fmt.Errorf("httpapi: at least one API key is required")
	}
	s := &Server{store: store, ping: ping, logger: logger, keys: newAPIKeys(apiKeys), mux: http.NewServeMux()}

	s.mux.HandleFunc("GET /healthz", s.health)

	protected := http.NewServeMux()
	for _, rt := range s.protectedRoutes() {
		protected.HandleFunc(rt.pattern, rt.handler)
	}

	// Every path other than /healthz goes through the key check first, so an
	// unauthenticated client cannot even probe which routes exist.
	s.mux.Handle("/", s.requireAPIKey(protected))

	return s, nil
}

type route struct {
	pattern string
	handler http.HandlerFunc
}

// protectedRoutes lists the routes that require an API key. Keep
// docs/openapi.yaml in sync: TestOpenAPIRoutesMatch fails otherwise.
func (s *Server) protectedRoutes() []route {
	return []route{
		{"GET /products", s.listProducts},
		{"POST /products", s.createProduct},
		{"GET /products/{id}", s.getProduct},
		{"PUT /products/{id}", s.updateProduct},
		{"DELETE /products/{id}", s.deleteProduct},
		{"POST /products/{id}/stock", s.adjustStock},
		{"GET /categories", s.listCategories},
		{"POST /categories", s.createCategory},
		{"DELETE /categories/{id}", s.deleteCategory},
	}
}

// ServeHTTP logs each request and dispatches it.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
	s.mux.ServeHTTP(rec, r)
	s.logger.Info("request",
		"method", r.Method, "path", r.URL.Path,
		"status", rec.status, "duration", time.Since(start))
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	if err := s.ping(r.Context()); err != nil {
		s.logger.Error("health check failed", "err", err)
		writeError(w, http.StatusServiceUnavailable, "database unavailable")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// writeJSON writes v as JSON with the given status code.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

type errorResponse struct {
	Error string `json:"error"`
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, errorResponse{Error: msg})
}

// storeError converts a catalogue error to an HTTP response. Unknown errors
// are logged and reported as 500 without leaking details.
func (s *Server) storeError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, catalogue.ErrProductNotFound):
		writeError(w, http.StatusNotFound, "product not found")
	case errors.Is(err, catalogue.ErrDuplicateReference):
		writeError(w, http.StatusConflict, "reference already exists")
	case errors.Is(err, catalogue.ErrDuplicateCategory):
		writeError(w, http.StatusConflict, "category already exists")
	case errors.Is(err, catalogue.ErrInsufficientStock):
		writeError(w, http.StatusConflict, "insufficient stock")
	case errors.Is(err, catalogue.ErrCategoryNotFound):
		writeError(w, http.StatusNotFound, "category not found")
	default:
		s.logger.Error("store error", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
	}
}

// decode reads a JSON body into v, rejecting unknown fields and bodies
// larger than 1 MiB.
func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body: "+err.Error())
		return false
	}
	return true
}
