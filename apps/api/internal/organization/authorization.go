package organization

func canUpdateOrganization(role Role) bool { return role == RoleOwner || role == RoleAdmin }
func canManageMembers(role Role) bool      { return role == RoleOwner || role == RoleAdmin }
func canAssignRole(actor, target Role) bool {
	if !validRole(target) || !canManageMembers(actor) {
		return false
	}
	if target == RoleOwner {
		return actor == RoleOwner
	}
	return true
}
