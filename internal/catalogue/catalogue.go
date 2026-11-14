// Package catalogue stores and reads catalogue items in PostgreSQL.
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
	// ErrNotFound is returned when no item matches the given id.
	ErrNotFound = errors.New("catalogue: item not found")
	// ErrDuplicateReference is returned when the reference is already used.
	ErrDuplicateReference = errors.New("catalogue: reference already exists")
)

// Item is one entry of the catalogue. Prices are stored in cents to avoid
// floating-point rounding errors.
type Item struct {
	ID          int64
	Reference   string
	Name        string
	Description string
	PriceCents  int64
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// Repository gives access to the catalogue table.
type Repository struct {
	pool *pgxpool.Pool
}

// NewRepository returns a Repository backed by pool.
func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

const columns = "id, reference, name, description, price_cents, created_at, updated_at"

func scanItem(row pgx.Row) (Item, error) {
	var it Item
	err := row.Scan(&it.ID, &it.Reference, &it.Name, &it.Description,
		&it.PriceCents, &it.CreatedAt, &it.UpdatedAt)
	return it, err
}

// translate maps PostgreSQL errors to the package's errors.
func translate(op string, err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" { // unique_violation
		return ErrDuplicateReference
	}
	return fmt.Errorf("catalogue: %s: %w", op, err)
}

// Create inserts a new item and returns it with its ID and timestamps set.
func (r *Repository) Create(ctx context.Context, it Item) (Item, error) {
	row := r.pool.QueryRow(ctx,
		`INSERT INTO catalogue (reference, name, description, price_cents)
		 VALUES ($1, $2, $3, $4)
		 RETURNING `+columns,
		it.Reference, it.Name, it.Description, it.PriceCents)
	created, err := scanItem(row)
	if err != nil {
		return Item{}, translate("create", err)
	}
	return created, nil
}

// Get returns the item with the given id.
func (r *Repository) Get(ctx context.Context, id int64) (Item, error) {
	row := r.pool.QueryRow(ctx, "SELECT "+columns+" FROM catalogue WHERE id = $1", id)
	it, err := scanItem(row)
	if err != nil {
		return Item{}, translate("get", err)
	}
	return it, nil
}

// List returns items ordered by name, at most limit items starting at offset.
func (r *Repository) List(ctx context.Context, limit, offset int) ([]Item, error) {
	rows, err := r.pool.Query(ctx,
		"SELECT "+columns+" FROM catalogue ORDER BY name, id LIMIT $1 OFFSET $2",
		limit, offset)
	if err != nil {
		return nil, translate("list", err)
	}
	items, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (Item, error) {
		return scanItem(row)
	})
	if err != nil {
		return nil, translate("list", err)
	}
	return items, nil
}

// Count returns the number of items in the catalogue.
func (r *Repository) Count(ctx context.Context) (int64, error) {
	var n int64
	if err := r.pool.QueryRow(ctx, "SELECT count(*) FROM catalogue").Scan(&n); err != nil {
		return 0, translate("count", err)
	}
	return n, nil
}

// Update replaces the editable fields of the item with it.ID and returns the
// updated item.
func (r *Repository) Update(ctx context.Context, it Item) (Item, error) {
	row := r.pool.QueryRow(ctx,
		`UPDATE catalogue
		 SET reference = $2, name = $3, description = $4, price_cents = $5, updated_at = now()
		 WHERE id = $1
		 RETURNING `+columns,
		it.ID, it.Reference, it.Name, it.Description, it.PriceCents)
	updated, err := scanItem(row)
	if err != nil {
		return Item{}, translate("update", err)
	}
	return updated, nil
}

// Delete removes the item with the given id.
func (r *Repository) Delete(ctx context.Context, id int64) error {
	tag, err := r.pool.Exec(ctx, "DELETE FROM catalogue WHERE id = $1", id)
	if err != nil {
		return translate("delete", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
