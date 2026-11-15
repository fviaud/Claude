// Package catalogue stores the catalogue in PostgreSQL: products and the
// categories that group them.
package catalogue

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	// ErrProductNotFound is returned when no product matches the given id.
	ErrProductNotFound = errors.New("catalogue: product not found")
	// ErrDuplicateReference is returned when a product reference is already used.
	ErrDuplicateReference = errors.New("catalogue: reference already exists")
	// ErrCategoryNotFound is returned when a category does not exist.
	ErrCategoryNotFound = errors.New("catalogue: category not found")
	// ErrDuplicateCategory is returned when a category name is already used.
	ErrDuplicateCategory = errors.New("catalogue: category already exists")
	// ErrInsufficientStock is returned when a stock change would make it negative.
	ErrInsufficientStock = errors.New("catalogue: insufficient stock")
)

// Product is one row of the products table. Prices are stored in cents to
// avoid floating-point rounding errors.
type Product struct {
	ID          int64
	Reference   string
	Name        string
	Description string
	PriceCents  int64
	// CategoryID is nil when the product has no category.
	CategoryID *int64
	// Stock is the quantity available; it can never be negative.
	Stock     int32
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Repository gives access to the products and categories tables.
type Repository struct {
	pool *pgxpool.Pool
}

// NewRepository returns a Repository backed by pool.
func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

const productColumns = "id, reference, name, description, price_cents, category_id, stock, created_at, updated_at"

func scanProduct(row pgx.Row) (Product, error) {
	var p Product
	err := row.Scan(&p.ID, &p.Reference, &p.Name, &p.Description,
		&p.PriceCents, &p.CategoryID, &p.Stock, &p.CreatedAt, &p.UpdatedAt)
	return p, err
}

// translate maps PostgreSQL errors to the package's errors, using the name
// of the constraint involved (see internal/database/migrations).
func translate(op string, err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrProductNotFound
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.ConstraintName {
		case "products_reference_key":
			return ErrDuplicateReference
		case "categories_name_key":
			return ErrDuplicateCategory
		case "products_category_id_fkey":
			return ErrCategoryNotFound
		case "products_stock_check":
			return ErrInsufficientStock
		}
	}
	return fmt.Errorf("catalogue: %s: %w", op, err)
}

// CreateProduct inserts a new product and returns it with its ID and
// timestamps set.
func (r *Repository) CreateProduct(ctx context.Context, p Product) (Product, error) {
	row := r.pool.QueryRow(ctx,
		`INSERT INTO products (reference, name, description, price_cents, category_id, stock)
		 VALUES ($1, $2, $3, $4, $5, $6)
		 RETURNING `+productColumns,
		p.Reference, p.Name, p.Description, p.PriceCents, p.CategoryID, p.Stock)
	created, err := scanProduct(row)
	if err != nil {
		return Product{}, translate("create product", err)
	}
	return created, nil
}

// GetProduct returns the product with the given id.
func (r *Repository) GetProduct(ctx context.Context, id int64) (Product, error) {
	row := r.pool.QueryRow(ctx, "SELECT "+productColumns+" FROM products WHERE id = $1", id)
	p, err := scanProduct(row)
	if err != nil {
		return Product{}, translate("get product", err)
	}
	return p, nil
}

// ListProducts returns products ordered by name, at most limit products
// starting at offset.
func (r *Repository) ListProducts(ctx context.Context, limit, offset int) ([]Product, error) {
	return r.queryProducts(ctx, "list products",
		"SELECT "+productColumns+" FROM products ORDER BY name, id LIMIT $1 OFFSET $2",
		limit, offset)
}

// ListProductsByCategory returns the products of a category ordered by
// name, at most limit products starting at offset.
func (r *Repository) ListProductsByCategory(ctx context.Context, categoryID int64, limit, offset int) ([]Product, error) {
	return r.queryProducts(ctx, "list products by category",
		"SELECT "+productColumns+" FROM products WHERE category_id = $1 ORDER BY name, id LIMIT $2 OFFSET $3",
		categoryID, limit, offset)
}

func (r *Repository) queryProducts(ctx context.Context, op, sql string, args ...any) ([]Product, error) {
	rows, err := r.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, translate(op, err)
	}
	products, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (Product, error) {
		return scanProduct(row)
	})
	if err != nil {
		return nil, translate(op, err)
	}
	return products, nil
}

// CountProducts returns the number of products.
func (r *Repository) CountProducts(ctx context.Context) (int64, error) {
	var n int64
	if err := r.pool.QueryRow(ctx, "SELECT count(*) FROM products").Scan(&n); err != nil {
		return 0, translate("count products", err)
	}
	return n, nil
}

// UpdateProduct replaces the editable fields of the product with p.ID and
// returns the updated product.
func (r *Repository) UpdateProduct(ctx context.Context, p Product) (Product, error) {
	row := r.pool.QueryRow(ctx,
		`UPDATE products
		 SET reference = $2, name = $3, description = $4, price_cents = $5,
		     category_id = $6, stock = $7, updated_at = now()
		 WHERE id = $1
		 RETURNING `+productColumns,
		p.ID, p.Reference, p.Name, p.Description, p.PriceCents, p.CategoryID, p.Stock)
	updated, err := scanProduct(row)
	if err != nil {
		return Product{}, translate("update product", err)
	}
	return updated, nil
}

// DeleteProduct removes the product with the given id.
func (r *Repository) DeleteProduct(ctx context.Context, id int64) error {
	tag, err := r.pool.Exec(ctx, "DELETE FROM products WHERE id = $1", id)
	if err != nil {
		return translate("delete product", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrProductNotFound
	}
	return nil
}

// AdjustStock adds delta (negative to remove) to the stock of the product
// with the given id, in a single atomic statement, and returns the new
// stock. It returns ErrInsufficientStock if the stock would become negative.
func (r *Repository) AdjustStock(ctx context.Context, id int64, delta int32) (int32, error) {
	var stock int32
	err := r.pool.QueryRow(ctx,
		`UPDATE products SET stock = stock + $2, updated_at = now()
		 WHERE id = $1
		 RETURNING stock`,
		id, delta).Scan(&stock)
	if err != nil {
		return 0, translate("adjust stock", err)
	}
	return stock, nil
}
