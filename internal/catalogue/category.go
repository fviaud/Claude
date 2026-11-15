package catalogue

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
)

// Category groups products. Deleting a category leaves its products
// without a category.
type Category struct {
	ID        int64
	Name      string
	CreatedAt time.Time
}

// CreateCategory inserts a new category.
func (r *Repository) CreateCategory(ctx context.Context, name string) (Category, error) {
	var c Category
	err := r.pool.QueryRow(ctx,
		"INSERT INTO categories (name) VALUES ($1) RETURNING id, name, created_at",
		name).Scan(&c.ID, &c.Name, &c.CreatedAt)
	if err != nil {
		return Category{}, translate("create category", err)
	}
	return c, nil
}

// ListCategories returns all categories ordered by name.
func (r *Repository) ListCategories(ctx context.Context) ([]Category, error) {
	rows, err := r.pool.Query(ctx, "SELECT id, name, created_at FROM categories ORDER BY name")
	if err != nil {
		return nil, translate("list categories", err)
	}
	cats, err := pgx.CollectRows(rows, pgx.RowToStructByPos[Category])
	if err != nil {
		return nil, translate("list categories", err)
	}
	return cats, nil
}

// DeleteCategory removes a category. Its products are kept, without a category.
func (r *Repository) DeleteCategory(ctx context.Context, id int64) error {
	tag, err := r.pool.Exec(ctx, "DELETE FROM categories WHERE id = $1", id)
	if err != nil {
		return translate("delete category", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrCategoryNotFound
	}
	return nil
}
