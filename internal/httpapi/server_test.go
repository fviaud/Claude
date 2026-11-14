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
	items  map[int64]catalogue.Item
	cats   map[int64]catalogue.Category
	nextID int64
}

func newFakeStore() *fakeStore {
	return &fakeStore{items: map[int64]catalogue.Item{}, cats: map[int64]catalogue.Category{}}
}

func (f *fakeStore) check(it catalogue.Item) error {
	for _, o := range f.items {
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

func (f *fakeStore) Create(_ context.Context, it catalogue.Item) (catalogue.Item, error) {
	if err := f.check(it); err != nil {
		return catalogue.Item{}, err
	}
	f.nextID++
	it.ID = f.nextID
	f.items[it.ID] = it
	return it, nil
}

func (f *fakeStore) Get(_ context.Context, id int64) (catalogue.Item, error) {
	it, ok := f.items[id]
	if !ok {
		return catalogue.Item{}, catalogue.ErrNotFound
	}
	return it, nil
}

func (f *fakeStore) filter(keep func(catalogue.Item) bool, limit, offset int) []catalogue.Item {
	var out []catalogue.Item
	for _, it := range f.items {
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

func (f *fakeStore) List(_ context.Context, limit, offset int) ([]catalogue.Item, error) {
	return f.filter(func(catalogue.Item) bool { return true }, limit, offset), nil
}

func (f *fakeStore) ListByCategory(_ context.Context, cat int64, limit, offset int) ([]catalogue.Item, error) {
	return f.filter(func(it catalogue.Item) bool { return it.CategoryID != nil && *it.CategoryID == cat }, limit, offset), nil
}

func (f *fakeStore) Update(_ context.Context, it catalogue.Item) (catalogue.Item, error) {
	if _, ok := f.items[it.ID]; !ok {
		return catalogue.Item{}, catalogue.ErrNotFound
	}
	if err := f.check(it); err != nil {
		return catalogue.Item{}, err
	}
	f.items[it.ID] = it
	return it, nil
}

func (f *fakeStore) Delete(_ context.Context, id int64) error {
	if _, ok := f.items[id]; !ok {
		return catalogue.ErrNotFound
	}
	delete(f.items, id)
	return nil
}

func (f *fakeStore) AdjustStock(_ context.Context, id int64, delta int32) (int32, error) {
	it, ok := f.items[id]
	if !ok {
		return 0, catalogue.ErrNotFound
	}
	if it.Stock+delta < 0 {
		return 0, catalogue.ErrInsufficientStock
	}
	it.Stock += delta
	f.items[id] = it
	return it.Stock, nil
}

func (f *fakeStore) CreateCategory(_ context.Context, name string) (catalogue.Category, error) {
	for _, c := range f.cats {
		if c.Name == name {
			return catalogue.Category{}, catalogue.ErrDuplicateCategory
		}
	}
	f.nextID++
	c := catalogue.Category{ID: f.nextID, Name: name}
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

func TestItemsLifecycle(t *testing.T) {
	c := newClient(t, nil)

	var cat categoryResponse
	c.do("POST", "/categories", `{"name":"Meubles"}`, http.StatusCreated, &cat)
	c.do("POST", "/categories", `{"name":"Meubles"}`, http.StatusConflict, nil)
	c.do("POST", "/categories", `{"name":"  "}`, http.StatusUnprocessableEntity, nil)

	var created itemResponse
	c.do("POST", "/items",
		`{"reference":"C1","name":"Chaise","price_cents":4999,"stock":3,"category_id":`+itoa(cat.ID)+`}`,
		http.StatusCreated, &created)
	if created.ID == 0 || created.Stock != 3 || created.CategoryID == nil || *created.CategoryID != cat.ID {
		t.Fatalf("created = %+v", created)
	}
	c.do("POST", "/items", `{"reference":"C1","name":"Autre"}`, http.StatusConflict, nil)
	c.do("POST", "/items", `{"reference":"T1","name":"Table"}`, http.StatusCreated, nil)
	c.do("POST", "/items", `{"reference":"X","name":"X","category_id":999}`, http.StatusUnprocessableEntity, nil)

	path := "/items/" + itoa(created.ID)
	var got itemResponse
	c.do("GET", path, "", http.StatusOK, &got)
	if got.Name != "Chaise" {
		t.Fatalf("got = %+v", got)
	}

	var list itemListResponse
	c.do("GET", "/items", "", http.StatusOK, &list)
	if len(list.Items) != 2 || list.Limit != defaultLimit {
		t.Fatalf("list = %+v", list)
	}
	c.do("GET", "/items?category_id="+itoa(cat.ID), "", http.StatusOK, &list)
	if len(list.Items) != 1 || list.Items[0].ID != created.ID {
		t.Fatalf("list by category = %+v", list)
	}
	c.do("GET", "/items?limit=1&offset=1", "", http.StatusOK, &list)
	if len(list.Items) != 1 || list.Items[0].Name != "Table" {
		t.Fatalf("paginated list = %+v", list)
	}

	var updated itemResponse
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
		{"GET", "/items/abc", "", http.StatusBadRequest},
		{"GET", "/items/0", "", http.StatusBadRequest},
		{"GET", "/items?limit=-1", "", http.StatusBadRequest},
		{"GET", "/items?category_id=x", "", http.StatusBadRequest},
		{"POST", "/items", `not json`, http.StatusBadRequest},
		{"POST", "/items", `{"reference":"A","name":"A","unknown":1}`, http.StatusBadRequest},
		{"POST", "/items", `{"name":"A"}`, http.StatusUnprocessableEntity},
		{"POST", "/items", `{"reference":"A","name":"A","price_cents":-1}`, http.StatusUnprocessableEntity},
		{"POST", "/items", `{"reference":"A","name":"A","stock":-1}`, http.StatusUnprocessableEntity},
		{"PUT", "/items/42", `{"reference":"A","name":"A"}`, http.StatusNotFound},
		{"POST", "/items/42/stock", `{}`, http.StatusUnprocessableEntity},
		{"POST", "/items/42/stock", `{"delta":0}`, http.StatusUnprocessableEntity},
		{"POST", "/items/42/stock", `{"delta":99999999999}`, http.StatusUnprocessableEntity},
		{"POST", "/items/42/stock", `{"delta":1}`, http.StatusNotFound},
		{"DELETE", "/items/42", "", http.StatusNotFound},
		{"PATCH", "/items/42", "", http.StatusMethodNotAllowed},
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
	c.do("GET", "/items", "", http.StatusUnauthorized, nil)
	c.do("POST", "/items", `{"reference":"A","name":"A"}`, http.StatusUnauthorized, nil)
	c.do("GET", "/does-not-exist", "", http.StatusUnauthorized, nil)

	c.key = "wrong"
	c.do("GET", "/items", "", http.StatusUnauthorized, nil)

	c.key = "second-key" // every configured key is accepted
	c.do("GET", "/items", "", http.StatusOK, nil)
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
		req, _ := http.NewRequest("GET", c.srv.URL+"/items", nil)
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
