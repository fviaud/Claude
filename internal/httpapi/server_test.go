package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"

	"github.com/fviaud/claude/internal/catalogue"
)

// fakeStore is an in-memory Store that mimics the repository's errors.
type fakeStore struct {
	products      map[int64]catalogue.Product
	cats          map[int64]catalogue.Category
	nextProductID int64 // separate sequences, as in PostgreSQL
	nextCatID     int64
}

func newFakeStore() *fakeStore {
	return &fakeStore{products: map[int64]catalogue.Product{}, cats: map[int64]catalogue.Category{}}
}

func (f *fakeStore) check(it catalogue.Product) error {
	for _, o := range f.products {
		if o.Reference == it.Reference && o.ID != it.ID {
			return catalogue.ErrDuplicateReference
		}
	}
	if it.CategoryID != nil {
		if _, ok := f.cats[*it.CategoryID]; !ok {
			return catalogue.ErrCategoryNotFound
		}
	}
	return nil
}

func (f *fakeStore) CreateProduct(_ context.Context, it catalogue.Product) (catalogue.Product, error) {
	if err := f.check(it); err != nil {
		return catalogue.Product{}, err
	}
	f.nextProductID++
	it.ID = f.nextProductID
	f.products[it.ID] = it
	return it, nil
}

func (f *fakeStore) GetProduct(_ context.Context, id int64) (catalogue.Product, error) {
	it, ok := f.products[id]
	if !ok {
		return catalogue.Product{}, catalogue.ErrProductNotFound
	}
	return it, nil
}

func (f *fakeStore) filter(keep func(catalogue.Product) bool, limit, offset int) []catalogue.Product {
	var out []catalogue.Product
	for _, it := range f.products {
		if keep(it) {
			out = append(out, it)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	if offset >= len(out) {
		return nil
	}
	return out[offset:min(offset+limit, len(out))]
}

func (f *fakeStore) ListProducts(_ context.Context, limit, offset int) ([]catalogue.Product, error) {
	return f.filter(func(catalogue.Product) bool { return true }, limit, offset), nil
}

func (f *fakeStore) ListProductsByCategory(_ context.Context, cat int64, limit, offset int) ([]catalogue.Product, error) {
	return f.filter(func(it catalogue.Product) bool { return it.CategoryID != nil && *it.CategoryID == cat }, limit, offset), nil
}

func (f *fakeStore) UpdateProduct(_ context.Context, it catalogue.Product) (catalogue.Product, error) {
	if _, ok := f.products[it.ID]; !ok {
		return catalogue.Product{}, catalogue.ErrProductNotFound
	}
	if err := f.check(it); err != nil {
		return catalogue.Product{}, err
	}
	f.products[it.ID] = it
	return it, nil
}

func (f *fakeStore) DeleteProduct(_ context.Context, id int64) error {
	if _, ok := f.products[id]; !ok {
		return catalogue.ErrProductNotFound
	}
	delete(f.products, id)
	return nil
}

func (f *fakeStore) AdjustStock(_ context.Context, id int64, delta int32) (int32, error) {
	it, ok := f.products[id]
	if !ok {
		return 0, catalogue.ErrProductNotFound
	}
	if it.Stock+delta < 0 {
		return 0, catalogue.ErrInsufficientStock
	}
	it.Stock += delta
	f.products[id] = it
	return it.Stock, nil
}

func (f *fakeStore) CreateCategory(_ context.Context, name string) (catalogue.Category, error) {
	for _, c := range f.cats {
		if c.Name == name {
			return catalogue.Category{}, catalogue.ErrDuplicateCategory
		}
	}
	f.nextCatID++
	c := catalogue.Category{ID: f.nextCatID, Name: name}
	f.cats[c.ID] = c
	return c, nil
}

func (f *fakeStore) ListCategories(context.Context) ([]catalogue.Category, error) {
	var out []catalogue.Category
	for _, c := range f.cats {
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (f *fakeStore) DeleteCategory(_ context.Context, id int64) error {
	if _, ok := f.cats[id]; !ok {
		return catalogue.ErrCategoryNotFound
	}
	delete(f.cats, id)
	return nil
}

const testKey = "test-key"

type apiClient struct {
	t   *testing.T
	srv *httptest.Server
	key string // sent as X-API-Key when not empty
}

func newClient(t *testing.T, ping func(context.Context) error) *apiClient {
	t.Helper()
	if ping == nil {
		ping = func(context.Context) error { return nil }
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	h, err := New(newFakeStore(), ping, logger, []string{testKey, "second-key"})
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return &apiClient{t: t, srv: srv, key: testKey}
}

// do sends a request and checks the status code. When out is non-nil the
// JSON response body is decoded into it.
func (c *apiClient) do(method, path, body string, wantStatus int, out any) {
	c.t.Helper()
	req, err := http.NewRequest(method, c.srv.URL+path, strings.NewReader(body))
	if err != nil {
		c.t.Fatal(err)
	}
	if c.key != "" {
		req.Header.Set("X-API-Key", c.key)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != wantStatus {
		c.t.Fatalf("%s %s: status = %d, want %d; body = %s", method, path, resp.StatusCode, wantStatus, raw)
	}
	if out != nil {
		if err := json.Unmarshal(raw, out); err != nil {
			c.t.Fatalf("%s %s: decode %s: %v", method, path, raw, err)
		}
	}
}

func TestHealth(t *testing.T) {
	newClient(t, nil).do("GET", "/healthz", "", http.StatusOK, nil)
	newClient(t, func(context.Context) error { return errors.New("down") }).
		do("GET", "/healthz", "", http.StatusServiceUnavailable, nil)
}

func TestProductsLifecycle(t *testing.T) {
	c := newClient(t, nil)

	var cat categoryResponse
	c.do("POST", "/categories", `{"name":"Meubles"}`, http.StatusCreated, &cat)
	c.do("POST", "/categories", `{"name":"Meubles"}`, http.StatusConflict, nil)
	c.do("POST", "/categories", `{"name":"  "}`, http.StatusUnprocessableEntity, nil)

	var created productResponse
	c.do("POST", "/products",
		`{"reference":"C1","name":"Chaise","price_cents":4999,"stock":3,"category_id":`+itoa(cat.ID)+`}`,
		http.StatusCreated, &created)
	if created.ID == 0 || created.Stock != 3 || created.CategoryID == nil || *created.CategoryID != cat.ID {
		t.Fatalf("created = %+v", created)
	}
	c.do("POST", "/products", `{"reference":"C1","name":"Autre"}`, http.StatusConflict, nil)
	c.do("POST", "/products", `{"reference":"T1","name":"Table"}`, http.StatusCreated, nil)
	c.do("POST", "/products", `{"reference":"X","name":"X","category_id":999}`, http.StatusUnprocessableEntity, nil)

	path := "/products/" + itoa(created.ID)
	var got productResponse
	c.do("GET", path, "", http.StatusOK, &got)
	if got.Name != "Chaise" {
		t.Fatalf("got = %+v", got)
	}

	var list productListResponse
	c.do("GET", "/products", "", http.StatusOK, &list)
	if len(list.Products) != 2 || list.Limit != defaultLimit {
		t.Fatalf("list = %+v", list)
	}
	c.do("GET", "/products?category_id="+itoa(cat.ID), "", http.StatusOK, &list)
	if len(list.Products) != 1 || list.Products[0].ID != created.ID {
		t.Fatalf("list by category = %+v", list)
	}
	c.do("GET", "/products?limit=1&offset=1", "", http.StatusOK, &list)
	if len(list.Products) != 1 || list.Products[0].Name != "Table" {
		t.Fatalf("paginated list = %+v", list)
	}

	var updated productResponse
	c.do("PUT", path, `{"reference":"C1","name":"Chaise pliante","price_cents":3999,"stock":3}`, http.StatusOK, &updated)
	if updated.Name != "Chaise pliante" || updated.CategoryID != nil {
		t.Fatalf("updated = %+v", updated)
	}

	var stock stockResponse
	c.do("POST", path+"/stock", `{"delta":-2}`, http.StatusOK, &stock)
	if stock.Stock != 1 {
		t.Fatalf("stock = %+v, want 1", stock)
	}
	c.do("POST", path+"/stock", `{"delta":-2}`, http.StatusConflict, nil)

	c.do("DELETE", path, "", http.StatusNoContent, nil)
	c.do("GET", path, "", http.StatusNotFound, nil)
	c.do("DELETE", "/categories/"+itoa(cat.ID), "", http.StatusNoContent, nil)
	c.do("DELETE", "/categories/"+itoa(cat.ID), "", http.StatusNotFound, nil)
}

func TestBadRequests(t *testing.T) {
	c := newClient(t, nil)
	tests := []struct {
		method, path, body string
		want               int
	}{
		{"GET", "/products/abc", "", http.StatusBadRequest},
		{"GET", "/products/0", "", http.StatusBadRequest},
		{"GET", "/products?limit=-1", "", http.StatusBadRequest},
		{"GET", "/products?category_id=x", "", http.StatusBadRequest},
		{"POST", "/products", `not json`, http.StatusBadRequest},
		{"POST", "/products", `{"reference":"A","name":"A","unknown":1}`, http.StatusBadRequest},
		{"POST", "/products", `{"name":"A"}`, http.StatusUnprocessableEntity},
		{"POST", "/products", `{"reference":"A","name":"A","price_cents":-1}`, http.StatusUnprocessableEntity},
		{"POST", "/products", `{"reference":"A","name":"A","stock":-1}`, http.StatusUnprocessableEntity},
		{"PUT", "/products/42", `{"reference":"A","name":"A"}`, http.StatusNotFound},
		{"POST", "/products/42/stock", `{}`, http.StatusUnprocessableEntity},
		{"POST", "/products/42/stock", `{"delta":0}`, http.StatusUnprocessableEntity},
		{"POST", "/products/42/stock", `{"delta":99999999999}`, http.StatusUnprocessableEntity},
		{"POST", "/products/42/stock", `{"delta":1}`, http.StatusNotFound},
		{"DELETE", "/products/42", "", http.StatusNotFound},
		{"PATCH", "/products/42", "", http.StatusMethodNotAllowed},
	}
	for _, tt := range tests {
		t.Run(tt.method+" "+tt.path+" "+tt.body, func(t *testing.T) {
			c.t = t
			var e errorResponse
			if tt.want == http.StatusMethodNotAllowed {
				c.do(tt.method, tt.path, tt.body, tt.want, nil)
				return
			}
			c.do(tt.method, tt.path, tt.body, tt.want, &e)
			if e.Error == "" {
				t.Errorf("missing error message")
			}
		})
	}
}

func itoa(n int64) string {
	b, _ := json.Marshal(n)
	return string(b)
}

func TestNewRequiresAPIKey(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	if _, err := New(newFakeStore(), nil, logger, nil); err == nil {
		t.Fatal("New without keys: want error, got nil")
	}
}

func TestAPIKey(t *testing.T) {
	c := newClient(t, nil)

	c.key = ""
	c.do("GET", "/healthz", "", http.StatusOK, nil) // public
	c.do("GET", "/products", "", http.StatusUnauthorized, nil)
	c.do("POST", "/products", `{"reference":"A","name":"A"}`, http.StatusUnauthorized, nil)
	c.do("GET", "/does-not-exist", "", http.StatusUnauthorized, nil)

	c.key = "wrong"
	c.do("GET", "/products", "", http.StatusUnauthorized, nil)

	c.key = "second-key" // every configured key is accepted
	c.do("GET", "/products", "", http.StatusOK, nil)
	c.do("GET", "/does-not-exist", "", http.StatusNotFound, nil)

	for _, tt := range []struct {
		header string
		want   int
	}{
		{"Bearer " + testKey, http.StatusOK},
		{"bearer " + testKey, http.StatusOK},
		{"Bearer wrong", http.StatusUnauthorized},
		{"Basic " + testKey, http.StatusUnauthorized},
		{testKey, http.StatusUnauthorized},
	} {
		req, _ := http.NewRequest("GET", c.srv.URL+"/products", nil)
		req.Header.Set("Authorization", tt.header)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != tt.want {
			t.Errorf("Authorization %q: status = %d, want %d", tt.header, resp.StatusCode, tt.want)
		}
		if tt.want == http.StatusUnauthorized && resp.Header.Get("WWW-Authenticate") == "" {
			t.Errorf("Authorization %q: missing WWW-Authenticate header", tt.header)
		}
	}
}

func TestParseAPIKeys(t *testing.T) {
	got := ParseAPIKeys(" a, ,b ,, c")
	if strings.Join(got, "|") != "a|b|c" {
		t.Fatalf("ParseAPIKeys = %q", got)
	}
	if got := ParseAPIKeys("  "); len(got) != 0 {
		t.Fatalf("ParseAPIKeys(blank) = %q, want empty", got)
	}
}
