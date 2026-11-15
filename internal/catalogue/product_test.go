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
// tables. The test is skipped when the variable is not set.
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
	if _, err := pool.Exec(ctx, "TRUNCATE products, categories RESTART IDENTITY"); err != nil {
		t.Fatalf("truncate: %v", err)
	}
	return catalogue.NewRepository(pool)
}

func TestRepositoryCRUD(t *testing.T) {
	repo := newRepo(t)
	ctx := context.Background()

	created, err := repo.CreateProduct(ctx, catalogue.Product{
		Reference: "REF-001", Name: "Chaise", Description: "En bois", PriceCents: 4999,
	})
	if err != nil {
		t.Fatalf("CreateProduct: %v", err)
	}
	if created.ID == 0 || created.CreatedAt.IsZero() {
		t.Fatalf("CreateProduct: ID or CreatedAt not set: %+v", created)
	}

	if _, err := repo.CreateProduct(ctx, catalogue.Product{Reference: "REF-001", Name: "Autre"}); !errors.Is(err, catalogue.ErrDuplicateReference) {
		t.Fatalf("CreateProduct duplicate: error = %v, want ErrDuplicateReference", err)
	}

	got, err := repo.GetProduct(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetProduct: %v", err)
	}
	if got.Name != "Chaise" || got.PriceCents != 4999 {
		t.Fatalf("GetProduct = %+v", got)
	}

	if _, err := repo.CreateProduct(ctx, catalogue.Product{Reference: "REF-002", Name: "Armoire", PriceCents: 29900}); err != nil {
		t.Fatalf("CreateProduct second: %v", err)
	}
	products, err := repo.ListProducts(ctx, 10, 0)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(products) != 2 || products[0].Name != "Armoire" {
		t.Fatalf("ListProducts = %+v, want 2 products starting with Armoire", products)
	}
	if n, err := repo.CountProducts(ctx); err != nil || n != 2 {
		t.Fatalf("CountProducts = %d, %v; want 2", n, err)
	}

	got.PriceCents = 3999
	updated, err := repo.UpdateProduct(ctx, got)
	if err != nil {
		t.Fatalf("UpdateProduct: %v", err)
	}
	if updated.PriceCents != 3999 || !updated.UpdatedAt.After(created.UpdatedAt) {
		t.Fatalf("UpdateProduct = %+v", updated)
	}

	if err := repo.DeleteProduct(ctx, created.ID); err != nil {
		t.Fatalf("DeleteProduct: %v", err)
	}
	if _, err := repo.GetProduct(ctx, created.ID); !errors.Is(err, catalogue.ErrProductNotFound) {
		t.Fatalf("GetProduct after delete: error = %v, want ErrProductNotFound", err)
	}
	if err := repo.DeleteProduct(ctx, created.ID); !errors.Is(err, catalogue.ErrProductNotFound) {
		t.Fatalf("DeleteProduct twice: error = %v, want ErrProductNotFound", err)
	}
	if _, err := repo.UpdateProduct(ctx, catalogue.Product{ID: 9999, Reference: "X", Name: "X"}); !errors.Is(err, catalogue.ErrProductNotFound) {
		t.Fatalf("UpdateProduct missing: error = %v, want ErrProductNotFound", err)
	}
}

func TestRepositoryRejectsNegativePrice(t *testing.T) {
	repo := newRepo(t)
	_, err := repo.CreateProduct(context.Background(), catalogue.Product{Reference: "NEG", Name: "X", PriceCents: -1})
	if err == nil {
		t.Fatal("Create with negative price: want error, got nil")
	}
}

func TestRepositoryCategories(t *testing.T) {
	repo := newRepo(t)
	ctx := context.Background()

	meubles, err := repo.CreateCategory(ctx, "Meubles")
	if err != nil {
		t.Fatalf("CreateCategory: %v", err)
	}
	if _, err := repo.CreateCategory(ctx, "Meubles"); !errors.Is(err, catalogue.ErrDuplicateCategory) {
		t.Fatalf("CreateCategory duplicate: error = %v, want ErrDuplicateCategory", err)
	}
	if _, err := repo.CreateCategory(ctx, "Déco"); err != nil {
		t.Fatalf("CreateCategory: %v", err)
	}
	cats, err := repo.ListCategories(ctx)
	if err != nil || len(cats) != 2 || cats[0].Name != "Déco" {
		t.Fatalf("ListCategories = %+v, %v", cats, err)
	}

	chaise, err := repo.CreateProduct(ctx, catalogue.Product{Reference: "C1", Name: "Chaise", CategoryID: &meubles.ID})
	if err != nil {
		t.Fatalf("CreateProduct with category: %v", err)
	}
	if _, err := repo.CreateProduct(ctx, catalogue.Product{Reference: "L1", Name: "Lampe"}); err != nil {
		t.Fatalf("CreateProduct without category: %v", err)
	}
	missing := int64(9999)
	if _, err := repo.CreateProduct(ctx, catalogue.Product{Reference: "X1", Name: "X", CategoryID: &missing}); !errors.Is(err, catalogue.ErrCategoryNotFound) {
		t.Fatalf("CreateProduct with missing category: error = %v, want ErrCategoryNotFound", err)
	}

	products, err := repo.ListProductsByCategory(ctx, meubles.ID, 10, 0)
	if err != nil || len(products) != 1 || products[0].ID != chaise.ID {
		t.Fatalf("ListProductsByCategory = %+v, %v", products, err)
	}

	if err := repo.DeleteCategory(ctx, meubles.ID); err != nil {
		t.Fatalf("DeleteCategory: %v", err)
	}
	got, err := repo.GetProduct(ctx, chaise.ID)
	if err != nil || got.CategoryID != nil {
		t.Fatalf("GetProduct after DeleteCategory = %+v, %v; want CategoryID nil", got, err)
	}
	if err := repo.DeleteCategory(ctx, meubles.ID); !errors.Is(err, catalogue.ErrCategoryNotFound) {
		t.Fatalf("DeleteCategory twice: error = %v, want ErrCategoryNotFound", err)
	}
}

func TestRepositoryStock(t *testing.T) {
	repo := newRepo(t)
	ctx := context.Background()

	it, err := repo.CreateProduct(ctx, catalogue.Product{Reference: "S1", Name: "Tabouret", Stock: 5})
	if err != nil {
		t.Fatalf("CreateProduct: %v", err)
	}
	if it.Stock != 5 {
		t.Fatalf("CreateProduct: Stock = %d, want 5", it.Stock)
	}

	if s, err := repo.AdjustStock(ctx, it.ID, 3); err != nil || s != 8 {
		t.Fatalf("AdjustStock(+3) = %d, %v; want 8", s, err)
	}
	if s, err := repo.AdjustStock(ctx, it.ID, -8); err != nil || s != 0 {
		t.Fatalf("AdjustStock(-8) = %d, %v; want 0", s, err)
	}
	if _, err := repo.AdjustStock(ctx, it.ID, -1); !errors.Is(err, catalogue.ErrInsufficientStock) {
		t.Fatalf("AdjustStock(-1) at 0: error = %v, want ErrInsufficientStock", err)
	}
	if _, err := repo.AdjustStock(ctx, 9999, 1); !errors.Is(err, catalogue.ErrProductNotFound) {
		t.Fatalf("AdjustStock missing product: error = %v, want ErrProductNotFound", err)
	}
	if _, err := repo.CreateProduct(ctx, catalogue.Product{Reference: "S2", Name: "X", Stock: -1}); !errors.Is(err, catalogue.ErrInsufficientStock) {
		t.Fatalf("CreateProduct with negative stock: error = %v, want ErrInsufficientStock", err)
	}
}
