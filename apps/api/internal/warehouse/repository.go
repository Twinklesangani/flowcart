package warehouse

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrNotFound = errors.New("warehouse not found")
var ErrCodeTaken = errors.New("warehouse code taken")

type repository interface {
	Create(context.Context, uuid.UUID, CreateInput) (Warehouse, error)
	List(context.Context, uuid.UUID) ([]Warehouse, error)
	Get(context.Context, uuid.UUID, uuid.UUID) (Warehouse, error)
	Update(context.Context, uuid.UUID, uuid.UUID, PatchInput) (Warehouse, error)
}

type Repository struct{ pool *pgxpool.Pool }

func NewRepository(pool *pgxpool.Pool) *Repository { return &Repository{pool: pool} }

const warehouseColumns = `id, organization_id, code, name, address_line1, address_line2, city, state, postal_code, country_code, is_active, created_at, updated_at`

func (r *Repository) Create(ctx context.Context, organizationID uuid.UUID, input CreateInput) (Warehouse, error) {
	var item Warehouse
	err := r.pool.QueryRow(ctx, `INSERT INTO warehouses (organization_id, code, name, address_line1, address_line2, city, state, postal_code, country_code, is_active) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,COALESCE($10,TRUE)) RETURNING `+warehouseColumns, organizationID, input.Code, input.Name, input.AddressLine1, input.AddressLine2, input.City, input.State, input.PostalCode, input.CountryCode, input.IsActive).Scan(&item.ID, &item.OrganizationID, &item.Code, &item.Name, &item.AddressLine1, &item.AddressLine2, &item.City, &item.State, &item.PostalCode, &item.CountryCode, &item.IsActive, &item.CreatedAt, &item.UpdatedAt)
	if isUniqueError(err) {
		return Warehouse{}, ErrCodeTaken
	}
	if err != nil {
		return Warehouse{}, fmt.Errorf("create warehouse: %w", err)
	}
	return item, nil
}
func (r *Repository) List(ctx context.Context, organizationID uuid.UUID) ([]Warehouse, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+warehouseColumns+` FROM warehouses WHERE organization_id=$1 ORDER BY name, code`, organizationID)
	if err != nil {
		return nil, fmt.Errorf("list warehouses: %w", err)
	}
	defer rows.Close()
	items := make([]Warehouse, 0)
	for rows.Next() {
		var item Warehouse
		if err := rows.Scan(&item.ID, &item.OrganizationID, &item.Code, &item.Name, &item.AddressLine1, &item.AddressLine2, &item.City, &item.State, &item.PostalCode, &item.CountryCode, &item.IsActive, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan warehouse: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list warehouses: %w", err)
	}
	return items, nil
}
func (r *Repository) Get(ctx context.Context, organizationID, id uuid.UUID) (Warehouse, error) {
	var item Warehouse
	err := r.pool.QueryRow(ctx, `SELECT `+warehouseColumns+` FROM warehouses WHERE organization_id=$1 AND id=$2`, organizationID, id).Scan(&item.ID, &item.OrganizationID, &item.Code, &item.Name, &item.AddressLine1, &item.AddressLine2, &item.City, &item.State, &item.PostalCode, &item.CountryCode, &item.IsActive, &item.CreatedAt, &item.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Warehouse{}, ErrNotFound
	}
	if err != nil {
		return Warehouse{}, fmt.Errorf("get warehouse: %w", err)
	}
	return item, nil
}
func (r *Repository) Update(ctx context.Context, organizationID, id uuid.UUID, patch PatchInput) (Warehouse, error) {
	var item Warehouse
	err := r.pool.QueryRow(ctx, `UPDATE warehouses SET code=COALESCE($3,code), name=COALESCE($4,name), address_line1=COALESCE($5,address_line1), address_line2=COALESCE($6,address_line2), city=COALESCE($7,city), state=COALESCE($8,state), postal_code=COALESCE($9,postal_code), country_code=COALESCE($10,country_code), is_active=COALESCE($11,is_active), updated_at=NOW() WHERE organization_id=$1 AND id=$2 RETURNING `+warehouseColumns, organizationID, id, patch.Code, patch.Name, patch.AddressLine1, patch.AddressLine2, patch.City, patch.State, patch.PostalCode, patch.CountryCode, patch.IsActive).Scan(&item.ID, &item.OrganizationID, &item.Code, &item.Name, &item.AddressLine1, &item.AddressLine2, &item.City, &item.State, &item.PostalCode, &item.CountryCode, &item.IsActive, &item.CreatedAt, &item.UpdatedAt)
	if isUniqueError(err) {
		return Warehouse{}, ErrCodeTaken
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return Warehouse{}, ErrNotFound
	}
	if err != nil {
		return Warehouse{}, fmt.Errorf("update warehouse: %w", err)
	}
	return item, nil
}
func isUniqueError(err error) bool {
	var databaseError *pgconn.PgError
	return errors.As(err, &databaseError) && databaseError.Code == "23505"
}
