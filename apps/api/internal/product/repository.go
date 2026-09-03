package product

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrNotFound = errors.New("product not found")
var ErrSKUTaken = errors.New("sku taken")

type repository interface {
	Create(context.Context, uuid.UUID, CreateInput) (Product, error)
	List(context.Context, uuid.UUID) ([]Product, error)
	Get(context.Context, uuid.UUID, uuid.UUID) (Product, error)
	Update(context.Context, uuid.UUID, uuid.UUID, PatchInput) (Product, error)
}

type Repository struct{ pool *pgxpool.Pool }

func NewRepository(pool *pgxpool.Pool) *Repository { return &Repository{pool: pool} }

const productColumns = `id, organization_id, sku, name, description, is_active, created_at, updated_at`

func (r *Repository) Create(ctx context.Context, organizationID uuid.UUID, input CreateInput) (Product, error) {
	var item Product
	err := r.pool.QueryRow(ctx, `INSERT INTO products (organization_id, sku, name, description, is_active) VALUES ($1,$2,$3,$4,COALESCE($5,TRUE)) RETURNING `+productColumns, organizationID, input.SKU, input.Name, input.Description, input.IsActive).Scan(&item.ID, &item.OrganizationID, &item.SKU, &item.Name, &item.Description, &item.IsActive, &item.CreatedAt, &item.UpdatedAt)
	if isUniqueError(err) {
		return Product{}, ErrSKUTaken
	}
	if err != nil {
		return Product{}, fmt.Errorf("create product: %w", err)
	}
	return item, nil
}
func (r *Repository) List(ctx context.Context, organizationID uuid.UUID) ([]Product, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+productColumns+` FROM products WHERE organization_id=$1 ORDER BY name, sku`, organizationID)
	if err != nil {
		return nil, fmt.Errorf("list products: %w", err)
	}
	defer rows.Close()
	items := make([]Product, 0)
	for rows.Next() {
		var item Product
		if err := rows.Scan(&item.ID, &item.OrganizationID, &item.SKU, &item.Name, &item.Description, &item.IsActive, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan product: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list products: %w", err)
	}
	return items, nil
}
func (r *Repository) Get(ctx context.Context, organizationID, id uuid.UUID) (Product, error) {
	var item Product
	err := r.pool.QueryRow(ctx, `SELECT `+productColumns+` FROM products WHERE organization_id=$1 AND id=$2`, organizationID, id).Scan(&item.ID, &item.OrganizationID, &item.SKU, &item.Name, &item.Description, &item.IsActive, &item.CreatedAt, &item.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Product{}, ErrNotFound
	}
	if err != nil {
		return Product{}, fmt.Errorf("get product: %w", err)
	}
	return item, nil
}
func (r *Repository) Update(ctx context.Context, organizationID, id uuid.UUID, patch PatchInput) (Product, error) {
	var item Product
	err := r.pool.QueryRow(ctx, `UPDATE products SET sku=COALESCE($3,sku), name=COALESCE($4,name), description=COALESCE($5,description), is_active=COALESCE($6,is_active), updated_at=NOW() WHERE organization_id=$1 AND id=$2 RETURNING `+productColumns, organizationID, id, patch.SKU, patch.Name, patch.Description, patch.IsActive).Scan(&item.ID, &item.OrganizationID, &item.SKU, &item.Name, &item.Description, &item.IsActive, &item.CreatedAt, &item.UpdatedAt)
	if isUniqueError(err) {
		return Product{}, ErrSKUTaken
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return Product{}, ErrNotFound
	}
	if err != nil {
		return Product{}, fmt.Errorf("update product: %w", err)
	}
	return item, nil
}
func isUniqueError(err error) bool {
	var databaseError *pgconn.PgError
	return errors.As(err, &databaseError) && databaseError.Code == "23505"
}
