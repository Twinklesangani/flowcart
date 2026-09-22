package organization

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestProductionMemberAdditionIsUnavailable(t *testing.T) {
	handler := NewHandlerForEnvironment(nil, false)
	request := httptest.NewRequest(http.MethodPost, "/organizations/test/members", strings.NewReader(`{"email":"member@example.com","role":"viewer"}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.AddMember(response, request)
	if response.Code != http.StatusNotFound {
		t.Fatalf("member addition status=%d, want 404", response.Code)
	}
}
