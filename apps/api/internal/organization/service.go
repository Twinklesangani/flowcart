package organization

import (
	"context"
	"errors"
	"flowcart/apps/api/internal/pagination"
	"strings"

	"github.com/google/uuid"
)

type repository interface {
	CreateWithOwner(context.Context, Organization, uuid.UUID) (Organization, error)
	FindMembership(context.Context, uuid.UUID, uuid.UUID) (OrganizationMembership, error)
	ListForUser(context.Context, uuid.UUID, int) ([]OrganizationSummary, error)
	Find(context.Context, uuid.UUID) (Organization, error)
	Update(context.Context, uuid.UUID, string, string) (Organization, error)
	ListMembers(context.Context, uuid.UUID, int) ([]Member, error)
	FindUserByEmail(context.Context, string) (uuid.UUID, error)
	AddMember(context.Context, uuid.UUID, uuid.UUID, Role) error
	ChangeRole(context.Context, uuid.UUID, uuid.UUID, Role) error
	RemoveMember(context.Context, uuid.UUID, uuid.UUID) error
}

var ErrInvalidInput = errors.New("invalid input")

type Service struct{ repository repository }

func NewService(r repository) *Service { return &Service{repository: r} }
func (s *Service) Create(ctx context.Context, userID uuid.UUID, name, slug string) (Organization, error) {
	name = strings.TrimSpace(name)
	slug = normalizeSlug(slug)
	if !validName(name) || !validSlug(slug) {
		return Organization{}, ErrInvalidInput
	}
	return s.repository.CreateWithOwner(ctx, Organization{ID: uuid.New(), Name: name, Slug: slug}, userID)
}
func (s *Service) List(ctx context.Context, userID uuid.UUID, limit int) ([]OrganizationSummary, error) {
	if limit < 1 || limit > pagination.MaxLimit {
		return nil, ErrInvalidInput
	}
	return s.repository.ListForUser(ctx, userID, limit)
}
func (s *Service) Membership(ctx context.Context, organizationID, userID uuid.UUID) (OrganizationMembership, error) {
	return s.repository.FindMembership(ctx, organizationID, userID)
}
func (s *Service) Get(ctx context.Context, userID, organizationID uuid.UUID) (Organization, error) {
	if _, err := s.repository.FindMembership(ctx, organizationID, userID); err != nil {
		if errors.Is(err, ErrNotFound) {
			return Organization{}, ErrNotFound
		}
		return Organization{}, err
	}
	return s.repository.Find(ctx, organizationID)
}
func (s *Service) Update(ctx context.Context, userID, organizationID uuid.UUID, name, slug string) (Organization, error) {
	membership, err := s.repository.FindMembership(ctx, organizationID, userID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return Organization{}, ErrNotFound
		}
		return Organization{}, err
	}
	if !canUpdateOrganization(membership.Role) {
		return Organization{}, ErrForbidden
	}
	name = strings.TrimSpace(name)
	slug = normalizeSlug(slug)
	if !validName(name) || !validSlug(slug) {
		return Organization{}, ErrInvalidInput
	}
	return s.repository.Update(ctx, organizationID, name, slug)
}
func (s *Service) Members(ctx context.Context, userID, organizationID uuid.UUID, limit int) ([]Member, error) {
	if _, err := s.repository.FindMembership(ctx, organizationID, userID); err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if limit < 1 || limit > pagination.MaxLimit {
		return nil, ErrInvalidInput
	}
	return s.repository.ListMembers(ctx, organizationID, limit)
}
func (s *Service) AddMember(ctx context.Context, userID, organizationID uuid.UUID, email string, role Role) error {
	if !validRole(role) {
		return ErrInvalidRole
	}
	membership, err := s.repository.FindMembership(ctx, organizationID, userID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return ErrNotFound
		}
		return err
	}
	if !canAssignRole(membership.Role, role) {
		return ErrForbidden
	}
	memberID, err := s.repository.FindUserByEmail(ctx, strings.ToLower(strings.TrimSpace(email)))
	if err != nil {
		return err
	}
	return s.repository.AddMember(ctx, organizationID, memberID, role)
}
func (s *Service) ChangeRole(ctx context.Context, userID, organizationID, targetID uuid.UUID, role Role) error {
	membership, err := s.repository.FindMembership(ctx, organizationID, userID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return ErrNotFound
		}
		return err
	}
	if !canAssignRole(membership.Role, role) {
		return ErrForbidden
	}
	target, err := s.repository.FindMembership(ctx, organizationID, targetID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return ErrMemberNotFound
		}
		return err
	}
	if membership.Role == RoleAdmin && target.Role == RoleOwner {
		return ErrForbidden
	}
	return s.repository.ChangeRole(ctx, organizationID, targetID, role)
}
func (s *Service) RemoveMember(ctx context.Context, userID, organizationID, targetID uuid.UUID) error {
	membership, err := s.repository.FindMembership(ctx, organizationID, userID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return ErrNotFound
		}
		return err
	}
	if !canManageMembers(membership.Role) {
		return ErrForbidden
	}
	target, err := s.repository.FindMembership(ctx, organizationID, targetID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return ErrMemberNotFound
		}
		return err
	}
	if target.Role == RoleOwner && membership.Role != RoleOwner {
		return ErrForbidden
	}
	return s.repository.RemoveMember(ctx, organizationID, targetID)
}

var ErrForbidden = errors.New("forbidden")
var ErrInvalidRole = errors.New("invalid role")
