package httpapi

import (
	"errors"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/fviaud/claude/internal/catalogue"
)

const (
	defaultLimit = 50
	maxLimit     = 200
)

type itemResponse struct {
	ID          int64     `json:"id"`
	Reference   string    `json:"reference"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	PriceCents  int64     `json:"price_cents"`
	CategoryID  *int64    `json:"category_id"`
	Stock       int32     `json:"stock"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

func toItemResponse(it catalogue.Item) itemResponse {
	return itemResponse{
		ID: it.ID, Reference: it.Reference, Name: it.Name, Description: it.Description,
		PriceCents: it.PriceCents, CategoryID: it.CategoryID, Stock: it.Stock,
		CreatedAt: it.CreatedAt, UpdatedAt: it.UpdatedAt,
	}
}

// itemRequest is the body of POST /items and PUT /items/{id}.
type itemRequest struct {
	Reference   string `json:"reference"`
	Name        string `json:"name"`
	Description string `json:"description"`
	PriceCents  int64  `json:"price_cents"`
	CategoryID  *int64 `json:"category_id"`
	Stock       int32  `json:"stock"`
}

func (req itemRequest) validate() string {
	var problems []string
	if strings.TrimSpace(req.Reference) == "" {
		problems = append(problems, "reference is required")
	}
	if strings.TrimSpace(req.Name) == "" {
		problems = append(problems, "name is required")
	}
	if req.PriceCents < 0 {
		problems = append(problems, "price_cents must be >= 0")
	}
	if req.Stock < 0 {
		problems = append(problems, "stock must be >= 0")
	}
	return strings.Join(problems, "; ")
}

func (req itemRequest) toItem(id int64) catalogue.Item {
	return catalogue.Item{
		ID: id, Reference: strings.TrimSpace(req.Reference), Name: strings.TrimSpace(req.Name),
		Description: req.Description, PriceCents: req.PriceCents,
		CategoryID: req.CategoryID, Stock: req.Stock,
	}
}

type itemListResponse struct {
	Items  []itemResponse `json:"items"`
	Limit  int            `json:"limit"`
	Offset int            `json:"offset"`
}

// pathID parses the {id} path parameter. It writes a 400 and returns false
// when it is not a positive integer.
func pathID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, "id must be a positive integer")
		return 0, false
	}
	return id, true
}

// queryInt reads an optional non-negative integer query parameter.
func queryInt(r *http.Request, name string, def int) (int, error) {
	v := r.URL.Query().Get(name)
	if v == "" {
		return def, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 0 {
		return 0, errors.New(name + " must be a non-negative integer")
	}
	return n, nil
}

func (s *Server) listItems(w http.ResponseWriter, r *http.Request) {
	limit, err := queryInt(r, "limit", defaultLimit)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	limit = min(max(limit, 1), maxLimit)
	offset, err := queryInt(r, "offset", 0)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	var items []catalogue.Item
	if c := r.URL.Query().Get("category_id"); c != "" {
		categoryID, err := strconv.ParseInt(c, 10, 64)
		if err != nil || categoryID <= 0 {
			writeError(w, http.StatusBadRequest, "category_id must be a positive integer")
			return
		}
		items, err = s.store.ListByCategory(r.Context(), categoryID, limit, offset)
		if err != nil {
			s.storeError(w, err)
			return
		}
	} else {
		items, err = s.store.List(r.Context(), limit, offset)
		if err != nil {
			s.storeError(w, err)
			return
		}
	}

	resp := itemListResponse{Items: make([]itemResponse, 0, len(items)), Limit: limit, Offset: offset}
	for _, it := range items {
		resp.Items = append(resp.Items, toItemResponse(it))
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) createItem(w http.ResponseWriter, r *http.Request) {
	var req itemRequest
	if !decode(w, r, &req) {
		return
	}
	if msg := req.validate(); msg != "" {
		writeError(w, http.StatusUnprocessableEntity, msg)
		return
	}
	it, err := s.store.Create(r.Context(), req.toItem(0))
	if err != nil {
		s.itemWriteError(w, err)
		return
	}
	w.Header().Set("Location", "/items/"+strconv.FormatInt(it.ID, 10))
	writeJSON(w, http.StatusCreated, toItemResponse(it))
}

func (s *Server) getItem(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	it, err := s.store.Get(r.Context(), id)
	if err != nil {
		s.storeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toItemResponse(it))
}

func (s *Server) updateItem(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var req itemRequest
	if !decode(w, r, &req) {
		return
	}
	if msg := req.validate(); msg != "" {
		writeError(w, http.StatusUnprocessableEntity, msg)
		return
	}
	it, err := s.store.Update(r.Context(), req.toItem(id))
	if err != nil {
		s.itemWriteError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toItemResponse(it))
}

// itemWriteError handles errors from Create and Update: a missing category
// there is a problem with the request body, not a missing resource.
func (s *Server) itemWriteError(w http.ResponseWriter, err error) {
	if errors.Is(err, catalogue.ErrCategoryNotFound) {
		writeError(w, http.StatusUnprocessableEntity, "category_id does not exist")
		return
	}
	s.storeError(w, err)
}

func (s *Server) deleteItem(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if err := s.store.Delete(r.Context(), id); err != nil {
		s.storeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type stockRequest struct {
	Delta *int64 `json:"delta"`
}

type stockResponse struct {
	ID    int64 `json:"id"`
	Stock int32 `json:"stock"`
}

func (s *Server) adjustStock(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var req stockRequest
	if !decode(w, r, &req) {
		return
	}
	if req.Delta == nil || *req.Delta == 0 {
		writeError(w, http.StatusUnprocessableEntity, "delta is required and must not be 0")
		return
	}
	if *req.Delta > math.MaxInt32 || *req.Delta < math.MinInt32 {
		writeError(w, http.StatusUnprocessableEntity, "delta is out of range")
		return
	}
	stock, err := s.store.AdjustStock(r.Context(), id, int32(*req.Delta))
	if err != nil {
		s.storeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, stockResponse{ID: id, Stock: stock})
}

type categoryResponse struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
}

type categoryRequest struct {
	Name string `json:"name"`
}

func (s *Server) listCategories(w http.ResponseWriter, r *http.Request) {
	cats, err := s.store.ListCategories(r.Context())
	if err != nil {
		s.storeError(w, err)
		return
	}
	resp := make([]categoryResponse, 0, len(cats))
	for _, c := range cats {
		resp = append(resp, categoryResponse{ID: c.ID, Name: c.Name, CreatedAt: c.CreatedAt})
	}
	writeJSON(w, http.StatusOK, map[string]any{"categories": resp})
}

func (s *Server) createCategory(w http.ResponseWriter, r *http.Request) {
	var req categoryRequest
	if !decode(w, r, &req) {
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		writeError(w, http.StatusUnprocessableEntity, "name is required")
		return
	}
	c, err := s.store.CreateCategory(r.Context(), name)
	if err != nil {
		s.storeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, categoryResponse{ID: c.ID, Name: c.Name, CreatedAt: c.CreatedAt})
}

func (s *Server) deleteCategory(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if err := s.store.DeleteCategory(r.Context(), id); err != nil {
		s.storeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
