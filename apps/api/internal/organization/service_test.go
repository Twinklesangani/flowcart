package organization

import (
	"context"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

type fakeRepository struct {
	organizations []Organization
	listError     error
	listResult    []OrganizationSummary
	membershipErr error
	memberships   map[[2]uuid.UUID]OrganizationMembership
	users         map[string]uuid.UUID
	created       bool
	updated       bool
}

func newFakeRepository() *fakeRepository {
	return &fakeRepository{memberships: make(map[[2]uuid.UUID]OrganizationMembership), users: make(map[string]uuid.UUID)}
}

func (r *fakeRepository) CreateWithOwner(_ context.Context, organization Organization, userID uuid.UUID) (Organization, error) {
	r.created = true
	r.organizations = append(r.organizations, organization)
	r.memberships[[2]uuid.UUID{organization.ID, userID}] = OrganizationMembership{OrganizationID: organization.ID, UserID: userID, Role: RoleOwner, CreatedAt: time.Now()}
	return organization, nil
}
func (r *fakeRepository) FindMembership(_ context.Context, organizationID, userID uuid.UUID) (OrganizationMembership, error) {
	if r.membershipErr != nil {
		return OrganizationMembership{}, r.membershipErr
	}
	membership, ok := r.memberships[[2]uuid.UUID{organizationID, userID}]
	if !ok {
		return OrganizationMembership{}, ErrNotFound
	}
	return membership, nil
}
func (r *fakeRepository) ListForUser(_ context.Context, userID uuid.UUID) ([]OrganizationSummary, error) {
	if r.listError != nil {
		return nil, r.listError
	}
	if r.listResult != nil {
		return r.listResult, nil
	}
	result := make([]OrganizationSummary, 0)
	for _, organization := range r.organizations {
		if membership, err := r.FindMembership(context.Background(), organization.ID, userID); err == nil {
			result = append(result, OrganizationSummary{ID: organization.ID, Name: organization.Name, Slug: organization.Slug, Role: membership.Role})
		}
	}
	return result, nil
}

func TestListHandlerUsesServiceSummariesWithoutMembershipLookup(t *testing.T) {
	organizationID := uuid.New()
	repository := newFakeRepository()
	repository.listResult = []OrganizationSummary{{ID: organizationID, Name: "A", Slug: "a", Role: RoleViewer}}
	repository.membershipErr = errors.New("membership lookup should not happen")
	handler := NewHandler(NewService(repository))
	request := httptest.NewRequest("GET", "/api/v1/organizations", strings.NewReader(""))
	response := httptest.NewRecorder()

	handler.List(response, request)
	if response.Code != 200 || !strings.Contains(response.Body.String(), `"role":"viewer"`) {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
}
func (r *fakeRepository) Find(_ context.Context, id uuid.UUID) (Organization, error) {
	for _, organization := range r.organizations {
		if organization.ID == id {
			return organization, nil
		}
	}
	return Organization{}, ErrNotFound
}
func (r *fakeRepository) Update(_ context.Context, id uuid.UUID, name, slug string) (Organization, error) {
	r.updated = true
	return Organization{ID: id, Name: name, Slug: slug}, nil
}
func (r *fakeRepository) ListMembers(_ context.Context, id uuid.UUID) ([]Member, error) {
	result := make([]Member, 0)
	for key, membership := range r.memberships {
		if key[0] == id {
			result = append(result, Member{UserID: key[1], Role: membership.Role})
		}
	}
	return result, nil
}
func (r *fakeRepository) FindUserByEmail(_ context.Context, email string) (uuid.UUID, error) {
	id, ok := r.users[email]
	if !ok {
		return uuid.Nil, ErrUserNotFound
	}
	return id, nil
}
func (r *fakeRepository) AddMember(_ context.Context, organizationID, userID uuid.UUID, role Role) error {
	key := [2]uuid.UUID{organizationID, userID}
	if _, ok := r.memberships[key]; ok {
		return ErrDuplicateMembership
	}
	r.memberships[key] = OrganizationMembership{OrganizationID: organizationID, UserID: userID, Role: role}
	return nil
}
func (r *fakeRepository) ChangeRole(_ context.Context, organizationID, userID uuid.UUID, role Role) error {
	key := [2]uuid.UUID{organizationID, userID}
	membership, ok := r.memberships[key]
	if !ok {
		return ErrMemberNotFound
	}
	membership.Role = role
	r.memberships[key] = membership
	return nil
}
func (r *fakeRepository) RemoveMember(_ context.Context, organizationID, userID uuid.UUID) error {
	key := [2]uuid.UUID{organizationID, userID}
	if _, ok := r.memberships[key]; !ok {
		return ErrMemberNotFound
	}
	delete(r.memberships, key)
	return nil
}

func TestCreateCreatesOwnerMembership(t *testing.T) {
	repository := newFakeRepository()
	service := NewService(repository)
	userID := uuid.New()
	organization, err := service.Create(context.Background(), userID, " Example Company ", " Example-Company ")
	if err != nil {
		t.Fatal(err)
	}
	if !repository.created {
		t.Fatal("organization was not created")
	}
	membership, err := repository.FindMembership(context.Background(), organization.ID, userID)
	if err != nil || membership.Role != RoleOwner {
		t.Fatalf("owner membership = %+v, err = %v", membership, err)
	}
	if organization.Slug != "example-company" {
		t.Fatalf("slug = %q", organization.Slug)
	}
}

func TestTenantIsolationListsOnlyMembershipsAndRejectsNonMember(t *testing.T) {
	repository := newFakeRepository()
	service := NewService(repository)
	userA, userB := uuid.New(), uuid.New()
	organizationA := Organization{ID: uuid.New(), Name: "A", Slug: "a"}
	organizationB := Organization{ID: uuid.New(), Name: "B", Slug: "b"}
	repository.organizations = []Organization{organizationA, organizationB}
	repository.memberships[[2]uuid.UUID{organizationA.ID, userA}] = OrganizationMembership{OrganizationID: organizationA.ID, UserID: userA, Role: RoleOwner}
	repository.memberships[[2]uuid.UUID{organizationB.ID, userB}] = OrganizationMembership{OrganizationID: organizationB.ID, UserID: userB, Role: RoleOwner}
	organizations, err := service.List(context.Background(), userA)
	if err != nil || len(organizations) != 1 || organizations[0].ID != organizationA.ID {
		t.Fatalf("user A organizations = %+v, err = %v", organizations, err)
	}
	if organizations[0].Role != RoleOwner {
		t.Fatalf("user A role = %q", organizations[0].Role)
	}
	if _, err := service.Get(context.Background(), userA, organizationB.ID); err != ErrNotFound {
		t.Fatalf("cross-tenant get error = %v", err)
	}
	if _, err := service.Update(context.Background(), userB, organizationA.ID, "no", "no"); err != ErrNotFound {
		t.Fatalf("non-member update error = %v", err)
	}
}

func TestMembershipRepositoryErrorsArePreserved(t *testing.T) {
	repository := newFakeRepository()
	repository.membershipErr = errors.New("database unavailable")
	service := NewService(repository)
	userID, organizationID := uuid.New(), uuid.New()

	checks := []struct {
		name string
		call func() error
	}{
		{"get", func() error { _, err := service.Get(context.Background(), userID, organizationID); return err }},
		{"update", func() error {
			_, err := service.Update(context.Background(), userID, organizationID, "Company", "company")
			return err
		}},
		{"members", func() error { _, err := service.Members(context.Background(), userID, organizationID); return err }},
		{"add member", func() error {
			return service.AddMember(context.Background(), userID, organizationID, "user@example.com", RoleViewer)
		}},
		{"change role", func() error {
			return service.ChangeRole(context.Background(), userID, organizationID, uuid.New(), RoleViewer)
		}},
		{"remove member", func() error { return service.RemoveMember(context.Background(), userID, organizationID, uuid.New()) }},
	}
	for _, check := range checks {
		t.Run(check.name, func(t *testing.T) {
			if err := check.call(); !errors.Is(err, repository.membershipErr) {
				t.Fatalf("error = %v, want repository error", err)
			}
		})
	}
}

func TestRoleAuthorization(t *testing.T) {
	if canUpdateOrganization(RoleWarehouseManager) || canUpdateOrganization(RoleViewer) {
		t.Fatal("unexpected organization update capabilities")
	}
	if canAssignRole(RoleAdmin, RoleOwner) {
		t.Fatal("admin promoted owner")
	}
	if !canAssignRole(RoleOwner, RoleOwner) {
		t.Fatal("owner could not assign owner")
	}
}

func TestAdminCannotModifyOwnerOrBecomeOwner(t *testing.T) {
	repository := newFakeRepository()
	service := NewService(repository)
	organizationID, adminID, ownerID := uuid.New(), uuid.New(), uuid.New()
	repository.memberships[[2]uuid.UUID{organizationID, adminID}] = OrganizationMembership{OrganizationID: organizationID, UserID: adminID, Role: RoleAdmin}
	repository.memberships[[2]uuid.UUID{organizationID, ownerID}] = OrganizationMembership{OrganizationID: organizationID, UserID: ownerID, Role: RoleOwner}
	if err := service.ChangeRole(context.Background(), adminID, organizationID, ownerID, RoleAdmin); err != ErrForbidden {
		t.Fatalf("admin owner change error = %v", err)
	}
	if err := service.ChangeRole(context.Background(), adminID, organizationID, adminID, RoleOwner); err != ErrForbidden {
		t.Fatalf("admin promotion error = %v", err)
	}
}
