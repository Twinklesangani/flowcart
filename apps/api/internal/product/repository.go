package product

import (
	"context"
	"errors"
	"flowcart/apps/api/internal/pagination"
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

const productColumns = `id, organization_id, sku, name, description, is_active, unit_price_minor, currency_code, created_at, updated_at`

func (r *Repository) Create(ctx context.Context, organizationID uuid.UUID, input CreateInput) (Product, error) {
	var item Product
	err := r.pool.QueryRow(ctx, `INSERT INTO products (organization_id, sku, name, description, is_active, unit_price_minor, currency_code) VALUES ($1,$2,$3,$4,COALESCE($5,TRUE),$6,$7) RETURNING `+productColumns, organizationID, input.SKU, input.Name, input.Description, input.IsActive, input.UnitPriceMinor, input.CurrencyCode).Scan(&item.ID, &item.OrganizationID, &item.SKU, &item.Name, &item.Description, &item.IsActive, &item.UnitPriceMinor, &item.CurrencyCode, &item.CreatedAt, &item.UpdatedAt)
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
		if err := rows.Scan(&item.ID, &item.OrganizationID, &item.SKU, &item.Name, &item.Description, &item.IsActive, &item.UnitPriceMinor, &item.CurrencyCode, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan product: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list products: %w", err)
	}
	return items, nil
}

func (r *Repository) ListPage(ctx context.Context, organizationID uuid.UUID, limit int, cursor pagination.Cursor) (ListPage, error) {
	where := "WHERE organization_id=$1"
	args := []any{organizationID, limit + 1}
	if cursor.ID != uuid.Nil {
		where += " AND (created_at,id)<($2,$3)"
		args = []any{organizationID, cursor.CreatedAt, cursor.ID, limit + 1}
	}
	rows, err := r.pool.Query(ctx, `SELECT `+productColumns+` FROM products `+where+` ORDER BY created_at DESC,id DESC LIMIT $`+fmt.Sprint(len(args)), args...)
	if err != nil {
		return ListPage{}, fmt.Errorf("list products: %w", err)
	}
	defer rows.Close()
	items := make([]Product, 0, limit+1)
	for rows.Next() {
		var item Product
		if err := rows.Scan(&item.ID, &item.OrganizationID, &item.SKU, &item.Name, &item.Description, &item.IsActive, &item.UnitPriceMinor, &item.CurrencyCode, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return ListPage{}, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return ListPage{}, err
	}
	page := ListPage{Products: items}
	if len(items) > limit {
		page.Products = items[:limit]
		page.HasMore = true
		last := page.Products[len(page.Products)-1]
		page.NextCursor = pagination.EncodeCursor(pagination.Cursor{OrganizationID: organizationID, CreatedAt: last.CreatedAt, ID: last.ID})
	}
	return page, nil
}
func (r *Repository) Get(ctx context.Context, organizationID, id uuid.UUID) (Product, error) {
	var item Product
	err := r.pool.QueryRow(ctx, `SELECT `+productColumns+` FROM products WHERE organization_id=$1 AND id=$2`, organizationID, id).Scan(&item.ID, &item.OrganizationID, &item.SKU, &item.Name, &item.Description, &item.IsActive, &item.UnitPriceMinor, &item.CurrencyCode, &item.CreatedAt, &item.UpdatedAt)
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
	err := r.pool.QueryRow(ctx, `UPDATE products SET sku=COALESCE($3,sku), name=COALESCE($4,name), description=COALESCE($5,description), is_active=COALESCE($6,is_active), unit_price_minor=COALESCE($7,unit_price_minor), currency_code=COALESCE($8,currency_code), updated_at=NOW() WHERE organization_id=$1 AND id=$2 RETURNING `+productColumns, organizationID, id, patch.SKU, patch.Name, patch.Description, patch.IsActive, patch.UnitPriceMinor, patch.CurrencyCode).Scan(&item.ID, &item.OrganizationID, &item.SKU, &item.Name, &item.Description, &item.IsActive, &item.UnitPriceMinor, &item.CurrencyCode, &item.CreatedAt, &item.UpdatedAt)
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
