package catalogue_test

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/fviaud/claude/internal/catalogue"
	"github.com/fviaud/claude/internal/database"
)

// newRepo connects to TEST_DATABASE_URL, applies migrations and empties the
// catalogue table. The test is skipped when the variable is not set.
func newRepo(t *testing.T) *catalogue.Repository {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	ctx := context.Background()
	pool, err := database.Connect(ctx, url)
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	t.Cleanup(pool.Close)
	if _, err := database.Migrate(ctx, pool); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	if _, err := pool.Exec(ctx, "TRUNCATE catalogue RESTART IDENTITY"); err != nil {
		t.Fatalf("truncate: %v", err)
	}
	return catalogue.NewRepository(pool)
}

func TestRepositoryCRUD(t *testing.T) {
	repo := newRepo(t)
	ctx := context.Background()

	created, err := repo.Create(ctx, catalogue.Item{
		Reference: "REF-001", Name: "Chaise", Description: "En bois", PriceCents: 4999,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.ID == 0 || created.CreatedAt.IsZero() {
		t.Fatalf("Create: ID or CreatedAt not set: %+v", created)
	}

	if _, err := repo.Create(ctx, catalogue.Item{Reference: "REF-001", Name: "Autre"}); !errors.Is(err, catalogue.ErrDuplicateReference) {
		t.Fatalf("Create duplicate: error = %v, want ErrDuplicateReference", err)
	}

	got, err := repo.Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Name != "Chaise" || got.PriceCents != 4999 {
		t.Fatalf("Get = %+v", got)
	}

	if _, err := repo.Create(ctx, catalogue.Item{Reference: "REF-002", Name: "Armoire", PriceCents: 29900}); err != nil {
		t.Fatalf("Create second: %v", err)
	}
	items, err := repo.List(ctx, 10, 0)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(items) != 2 || items[0].Name != "Armoire" {
		t.Fatalf("List = %+v, want 2 items starting with Armoire", items)
	}
	if n, err := repo.Count(ctx); err != nil || n != 2 {
		t.Fatalf("Count = %d, %v; want 2", n, err)
	}

	got.PriceCents = 3999
	updated, err := repo.Update(ctx, got)
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.PriceCents != 3999 || !updated.UpdatedAt.After(created.UpdatedAt) {
		t.Fatalf("Update = %+v", updated)
	}

	if err := repo.Delete(ctx, created.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := repo.Get(ctx, created.ID); !errors.Is(err, catalogue.ErrNotFound) {
		t.Fatalf("Get after delete: error = %v, want ErrNotFound", err)
	}
	if err := repo.Delete(ctx, created.ID); !errors.Is(err, catalogue.ErrNotFound) {
		t.Fatalf("Delete twice: error = %v, want ErrNotFound", err)
	}
	if _, err := repo.Update(ctx, catalogue.Item{ID: 9999, Reference: "X", Name: "X"}); !errors.Is(err, catalogue.ErrNotFound) {
		t.Fatalf("Update missing: error = %v, want ErrNotFound", err)
	}
}

func TestRepositoryRejectsNegativePrice(t *testing.T) {
	repo := newRepo(t)
	_, err := repo.Create(context.Background(), catalogue.Item{Reference: "NEG", Name: "X", PriceCents: -1})
	if err == nil {
		t.Fatal("Create with negative price: want error, got nil")
	}
}
