package httpapi

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/getkin/kin-openapi/routers"
	"github.com/getkin/kin-openapi/routers/gorillamux"
)

const specPath = "../../docs/openapi.yaml"

func loadSpec(t *testing.T) *openapi3.T {
	t.Helper()
	loader := openapi3.NewLoader()
	doc, err := loader.LoadFromFile(specPath)
	if err != nil {
		t.Fatalf("load %s: %v", specPath, err)
	}
	if err := doc.Validate(context.Background()); err != nil {
		t.Fatalf("%s is not a valid OpenAPI document: %v", specPath, err)
	}
	return doc
}

// TestOpenAPIRoutesMatch checks that the spec documents exactly the routes
// the server registers, no more and no less.
func TestOpenAPIRoutesMatch(t *testing.T) {
	doc := loadSpec(t)

	var inSpec []string
	for path, item := range doc.Paths.Map() {
		for method := range item.Operations() {
			inSpec = append(inSpec, method+" "+path)
		}
	}

	inCode := []string{"GET /healthz"}
	s := &Server{}
	for _, rt := range s.protectedRoutes() {
		inCode = append(inCode, rt.pattern)
	}

	sort.Strings(inSpec)
	sort.Strings(inCode)
	if strings.Join(inSpec, "\n") != strings.Join(inCode, "\n") {
		t.Errorf("routes differ between %s and the server\nspec:\n  %s\ncode:\n  %s",
			specPath, strings.Join(inSpec, "\n  "), strings.Join(inCode, "\n  "))
	}
}

// specRecorder wraps the server and validates every request and response
// against the spec. It records validation errors instead of failing so the
// test can report them all.
type specRecorder struct {
	t      *testing.T
	router routers.Router
	next   http.Handler
}

func (v specRecorder) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	r.Body = io.NopCloser(bytes.NewReader(body))

	rec := httptest.NewRecorder()
	v.next.ServeHTTP(rec, r)

	route, params, err := v.router.FindRoute(r)
	if err != nil {
		v.t.Errorf("%s %s: not in spec: %v", r.Method, r.URL.Path, err)
	} else {
		reqInput := &openapi3filter.RequestValidationInput{
			Request: r.Clone(r.Context()), PathParams: params, Route: route,
			Options: &openapi3filter.Options{AuthenticationFunc: openapi3filter.NoopAuthenticationFunc},
		}
		reqInput.Request.Body = io.NopCloser(bytes.NewReader(body))
		respInput := &openapi3filter.ResponseValidationInput{
			RequestValidationInput: reqInput,
			Status:                 rec.Code,
			Header:                 rec.Header(),
			Body:                   io.NopCloser(bytes.NewReader(rec.Body.Bytes())),
			Options:                &openapi3filter.Options{IncludeResponseStatus: true},
		}
		if err := openapi3filter.ValidateResponse(r.Context(), respInput); err != nil {
			v.t.Errorf("%s %s -> %d: response does not match spec: %v\nbody: %s",
				r.Method, r.URL.Path, rec.Code, err, rec.Body.String())
		}
	}

	for k, vals := range rec.Header() {
		w.Header()[k] = vals
	}
	w.WriteHeader(rec.Code)
	_, _ = w.Write(rec.Body.Bytes())
}

// TestOpenAPIResponsesMatch replays realistic traffic, including error
// cases, and checks every response (status, headers, JSON body) against the
// spec.
func TestOpenAPIResponsesMatch(t *testing.T) {
	doc := loadSpec(t)
	doc.Servers = nil // match requests whatever the test server's address
	router, err := gorillamux.NewRouter(doc)
	if err != nil {
		t.Fatal(err)
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	h, err := New(newFakeStore(), func(context.Context) error { return nil }, logger, []string{testKey})
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(specRecorder{t: t, router: router, next: h})
	defer srv.Close()
	c := &apiClient{t: t, srv: srv, key: testKey}

	c.do("GET", "/healthz", "", http.StatusOK, nil)

	c.do("POST", "/categories", `{"name":"Meubles"}`, http.StatusCreated, nil)
	c.do("POST", "/categories", `{"name":"Meubles"}`, http.StatusConflict, nil)
	c.do("POST", "/categories", `{"name":""}`, http.StatusUnprocessableEntity, nil)
	c.do("GET", "/categories", "", http.StatusOK, nil)

	c.do("POST", "/items", `{"reference":"C1","name":"Chaise","price_cents":4999,"stock":5,"category_id":1}`, http.StatusCreated, nil)
	c.do("POST", "/items", `{"reference":"L1","name":"Lampe"}`, http.StatusCreated, nil)
	c.do("POST", "/items", `{"reference":"C1","name":"Doublon"}`, http.StatusConflict, nil)
	c.do("POST", "/items", `{"reference":"X","name":"X","category_id":99}`, http.StatusUnprocessableEntity, nil)
	c.do("POST", "/items", `{"reference":"X"}`, http.StatusUnprocessableEntity, nil)
	c.do("POST", "/items", `not json`, http.StatusBadRequest, nil)

	c.do("GET", "/items", "", http.StatusOK, nil)
	c.do("GET", "/items?limit=1&offset=1&category_id=1", "", http.StatusOK, nil)
	c.do("GET", "/items?limit=-1", "", http.StatusBadRequest, nil)
	c.do("GET", "/items/2", "", http.StatusOK, nil)
	c.do("GET", "/items/99", "", http.StatusNotFound, nil)
	c.do("GET", "/items/abc", "", http.StatusBadRequest, nil)

	c.do("PUT", "/items/2", `{"reference":"L1","name":"Lampe","category_id":null}`, http.StatusOK, nil)
	c.do("PUT", "/items/99", `{"reference":"Z","name":"Z"}`, http.StatusNotFound, nil)
	c.do("PUT", "/items/2", `{"reference":"C1","name":"Lampe"}`, http.StatusConflict, nil)

	c.do("POST", "/items/1/stock", `{"delta":-2}`, http.StatusOK, nil)
	c.do("POST", "/items/1/stock", `{"delta":-10}`, http.StatusConflict, nil)
	c.do("POST", "/items/1/stock", `{"delta":0}`, http.StatusUnprocessableEntity, nil)
	c.do("POST", "/items/99/stock", `{"delta":1}`, http.StatusNotFound, nil)

	c.do("DELETE", "/items/2", "", http.StatusNoContent, nil)
	c.do("DELETE", "/items/2", "", http.StatusNotFound, nil)
	c.do("DELETE", "/categories/1", "", http.StatusNoContent, nil)
	c.do("DELETE", "/categories/1", "", http.StatusNotFound, nil)

	c.key = ""
	c.do("GET", "/items", "", http.StatusUnauthorized, nil)
	c.do("DELETE", "/categories/1", "", http.StatusUnauthorized, nil)
}
