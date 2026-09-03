package organization

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrNotFound = errors.New("organization not found")
var ErrMemberNotFound = errors.New("member not found")
var ErrUserNotFound = errors.New("user not found")
var ErrSlugTaken = errors.New("slug taken")
var ErrDuplicateMembership = errors.New("duplicate membership")
var ErrLastOwner = errors.New("last owner")

type Repository struct{ pool *pgxpool.Pool }

func NewRepository(pool *pgxpool.Pool) *Repository { return &Repository{pool: pool} }

func (r *Repository) CreateWithOwner(ctx context.Context, organization Organization, userID uuid.UUID) (Organization, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return Organization{}, fmt.Errorf("begin organization creation: %w", err)
	}
	defer tx.Rollback(ctx)
	err = tx.QueryRow(ctx, `INSERT INTO organizations (id, name, slug) VALUES ($1, $2, $3) RETURNING id, name, slug`, organization.ID, organization.Name, organization.Slug).Scan(&organization.ID, &organization.Name, &organization.Slug)
	if err != nil {
		if isUniqueError(err) {
			return Organization{}, ErrSlugTaken
		}
		return Organization{}, fmt.Errorf("create organization: %w", err)
	}
	_, err = tx.Exec(ctx, `INSERT INTO organization_members (organization_id, user_id, role) VALUES ($1, $2, 'owner')`, organization.ID, userID)
	if err != nil {
		return Organization{}, fmt.Errorf("create organization owner: %w", err)
	}
	if err = tx.Commit(ctx); err != nil {
		return Organization{}, fmt.Errorf("commit organization creation: %w", err)
	}
	return organization, nil
}
func (r *Repository) FindMembership(ctx context.Context, organizationID, userID uuid.UUID) (OrganizationMembership, error) {
	var m OrganizationMembership
	err := r.pool.QueryRow(ctx, `SELECT organization_id, user_id, role, created_at FROM organization_members WHERE organization_id=$1 AND user_id=$2`, organizationID, userID).Scan(&m.OrganizationID, &m.UserID, &m.Role, &m.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return m, ErrNotFound
	}
	if err != nil {
		return m, fmt.Errorf("find membership: %w", err)
	}
	return m, nil
}
func (r *Repository) ListForUser(ctx context.Context, userID uuid.UUID) ([]OrganizationSummary, error) {
	rows, err := r.pool.Query(ctx, `SELECT o.id, o.name, o.slug, m.role FROM organizations o JOIN organization_members m ON m.organization_id=o.id WHERE m.user_id=$1 ORDER BY o.name`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []OrganizationSummary
	for rows.Next() {
		var o OrganizationSummary
		if err := rows.Scan(&o.ID, &o.Name, &o.Slug, &o.Role); err != nil {
			return nil, err
		}
		result = append(result, o)
	}
	return result, rows.Err()
}
func (r *Repository) Find(ctx context.Context, id uuid.UUID) (Organization, error) {
	var o Organization
	err := r.pool.QueryRow(ctx, `SELECT id,name,slug FROM organizations WHERE id=$1`, id).Scan(&o.ID, &o.Name, &o.Slug)
	if errors.Is(err, pgx.ErrNoRows) {
		return o, ErrNotFound
	}
	return o, err
}
func (r *Repository) Update(ctx context.Context, id uuid.UUID, name, slug string) (Organization, error) {
	var o Organization
	err := r.pool.QueryRow(ctx, `UPDATE organizations SET name=$2,slug=$3,updated_at=NOW() WHERE id=$1 RETURNING id,name,slug`, id, name, slug).Scan(&o.ID, &o.Name, &o.Slug)
	if isUniqueError(err) {
		return o, ErrSlugTaken
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return o, ErrNotFound
	}
	return o, err
}
func (r *Repository) ListMembers(ctx context.Context, id uuid.UUID) ([]Member, error) {
	rows, err := r.pool.Query(ctx, `SELECT u.id,u.email,COALESCE(u.first_name,''),COALESCE(u.last_name,''),m.role,m.created_at FROM organization_members m JOIN users u ON u.id=m.user_id WHERE m.organization_id=$1 ORDER BY u.email`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []Member
	for rows.Next() {
		var m Member
		if err := rows.Scan(&m.UserID, &m.Email, &m.FirstName, &m.LastName, &m.Role, &m.CreatedAt); err != nil {
			return nil, err
		}
		result = append(result, m)
	}
	return result, rows.Err()
}
func (r *Repository) FindUserByEmail(ctx context.Context, email string) (uuid.UUID, error) {
	var id uuid.UUID
	err := r.pool.QueryRow(ctx, `SELECT id FROM users WHERE LOWER(email)=LOWER($1)`, email).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return id, ErrUserNotFound
	}
	return id, err
}
func (r *Repository) AddMember(ctx context.Context, id, userID uuid.UUID, role Role) error {
	_, err := r.pool.Exec(ctx, `INSERT INTO organization_members (organization_id,user_id,role) VALUES ($1,$2,$3)`, id, userID, role)
	if isUniqueError(err) {
		return ErrDuplicateMembership
	}
	return err
}
func (r *Repository) ChangeRole(ctx context.Context, id, userID uuid.UUID, role Role) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err := lockOrganization(ctx, tx, id); err != nil {
		return err
	}
	var current Role
	err = tx.QueryRow(ctx, `SELECT role FROM organization_members WHERE organization_id=$1 AND user_id=$2 FOR UPDATE`, id, userID).Scan(&current)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrMemberNotFound
	}
	if err != nil {
		return err
	}
	if current == RoleOwner && role != RoleOwner {
		ownerCount, err := lockOwnerCount(ctx, tx, id)
		if err != nil {
			return err
		}
		if ownerCount <= 1 {
			return ErrLastOwner
		}
	}
	_, err = tx.Exec(ctx, `UPDATE organization_members SET role=$3 WHERE organization_id=$1 AND user_id=$2`, id, userID, role)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}
func (r *Repository) RemoveMember(ctx context.Context, id, userID uuid.UUID) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err := lockOrganization(ctx, tx, id); err != nil {
		return err
	}
	var role Role
	err = tx.QueryRow(ctx, `SELECT role FROM organization_members WHERE organization_id=$1 AND user_id=$2 FOR UPDATE`, id, userID).Scan(&role)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrMemberNotFound
	}
	if err != nil {
		return err
	}
	if role == RoleOwner {
		ownerCount, err := lockOwnerCount(ctx, tx, id)
		if err != nil {
			return err
		}
		if ownerCount <= 1 {
			return ErrLastOwner
		}
	}
	_, err = tx.Exec(ctx, `DELETE FROM organization_members WHERE organization_id=$1 AND user_id=$2`, id, userID)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func lockOrganization(ctx context.Context, tx pgx.Tx, organizationID uuid.UUID) error {
	var id uuid.UUID
	err := tx.QueryRow(ctx, `SELECT id FROM organizations WHERE id=$1 FOR UPDATE`, organizationID).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	return err
}

func lockOwnerCount(ctx context.Context, tx pgx.Tx, organizationID uuid.UUID) (int, error) {
	rows, err := tx.Query(ctx, `SELECT id FROM organization_members WHERE organization_id=$1 AND role='owner' FOR UPDATE`, organizationID)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		count++
	}
	return count, rows.Err()
}
func isUniqueError(err error) bool {
	var e *pgconn.PgError
	return errors.As(err, &e) && e.Code == "23505"
}
func normalizeSlug(value string) string { return strings.ToLower(strings.TrimSpace(value)) }
func validSlug(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-') {
			return false
		}
	}
	return true
}
func validName(value string) bool { return len(strings.TrimSpace(value)) >= 2 && len(value) <= 255 }
func now() time.Time              { return time.Now() }
