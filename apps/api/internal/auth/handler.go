package auth

import (
	"encoding/json"
	"errors"
	"flowcart/apps/api/internal/httpboundary"
	"flowcart/apps/api/internal/ratelimit"
	"net/http"
	"time"
)

const refreshCookieName = "flowcart_refresh"

type Handler struct {
	service             *Service
	secureCookie        bool
	registrationEnabled bool
	limits              authLimits
	origins             httpboundary.OriginPolicy
}

func NewHandler(service *Service, secureCookie bool, policies ...httpboundary.OriginPolicy) *Handler {
	return NewHandlerForEnvironment(service, secureCookie, true, policies...)
}

func NewHandlerForEnvironment(service *Service, secureCookie, registrationEnabled bool, policies ...httpboundary.OriginPolicy) *Handler {
	origins, _ := httpboundary.NewOriginPolicy("", secureCookie)
	if len(policies) > 0 {
		origins = policies[0]
	}
	return &Handler{service: service, secureCookie: secureCookie, registrationEnabled: registrationEnabled, limits: newAuthLimits(nil), origins: origins}
}

type registerRequest struct {
	Email     string `json:"email"`
	Password  string `json:"password"`
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
}
type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}
type authResponse struct {
	User        safeUser `json:"user"`
	AccessToken string   `json:"access_token"`
}
type safeUser struct {
	ID        string `json:"id"`
	Email     string `json:"email"`
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
}
type errorResponse struct {
	Error errorBody `json:"error"`
}
type errorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (handler *Handler) Register(writer http.ResponseWriter, request *http.Request) {
	if !handler.origins.AllowAuthMutation(writer, request) {
		return
	}
	if !handler.registrationEnabled {
		writeError(writer, http.StatusNotFound, "not_found", "Not found.")
		return
	}
	var input registerRequest
	if !decodeJSON(writer, request, &input) {
		return
	}
	if !handler.limits.register.AllowHTTP(writer, ratelimit.ClientIP(request)) {
		return
	}
	result, err := handler.service.Register(request.Context(), input.Email, input.Password, input.FirstName, input.LastName)
	if err != nil {
		handler.respondServiceError(writer, err)
		return
	}
	handler.writeAuthResponse(writer, http.StatusCreated, result)
}

func (handler *Handler) Login(writer http.ResponseWriter, request *http.Request) {
	if !handler.origins.AllowAuthMutation(writer, request) {
		return
	}
	var input loginRequest
	if !decodeJSON(writer, request, &input) {
		return
	}
	if !handler.limits.login.AllowHTTP(writer, ratelimit.ClientIP(request)) {
		return
	}
	var accountTicket *ratelimit.Ticket
	if email, normalizeErr := normalizeEmail(input.Email); normalizeErr == nil {
		var retry time.Duration
		accountTicket, retry = handler.limits.account.Take(email)
		if accountTicket == nil {
			ratelimit.Reject(writer, retry)
			return
		}
	}
	result, err := handler.service.Login(request.Context(), input.Email, input.Password)
	if accountTicket != nil && !errors.Is(err, ErrInvalidCredentials) {
		accountTicket.Refund()
	}
	if err != nil {
		handler.respondServiceError(writer, err)
		return
	}
	handler.writeAuthResponse(writer, http.StatusOK, result)
}

func (handler *Handler) Refresh(writer http.ResponseWriter, request *http.Request) {
	if !handler.origins.AllowAuthMutation(writer, request) {
		return
	}
	if !handler.limits.refresh.AllowHTTP(writer, ratelimit.ClientIP(request)) {
		return
	}
	cookie, err := request.Cookie(refreshCookieName)
	if err != nil {
		handler.respondServiceError(writer, ErrInvalidCredentials)
		return
	}
	result, err := handler.service.Refresh(request.Context(), cookie.Value)
	if err != nil {
		handler.respondServiceError(writer, err)
		return
	}
	handler.writeAuthResponse(writer, http.StatusOK, result)
}

func (handler *Handler) Logout(writer http.ResponseWriter, request *http.Request) {
	if !handler.origins.AllowAuthMutation(writer, request) {
		return
	}
	if cookie, err := request.Cookie(refreshCookieName); err == nil {
		if err := handler.service.Logout(request.Context(), cookie.Value); err != nil {
			handler.respondServiceError(writer, err)
			return
		}
	}
	handler.clearRefreshCookie(writer)
	writer.WriteHeader(http.StatusNoContent)
}

func (handler *Handler) Me(writer http.ResponseWriter, request *http.Request) {
	writer.Header().Set("Cache-Control", "no-store")
	userID, ok := UserIDFromContext(request.Context())
	if !ok {
		writeUnauthorized(writer)
		return
	}
	user, err := handler.service.CurrentUser(request.Context(), userID)
	if err != nil {
		handler.respondServiceError(writer, err)
		return
	}
	writeJSON(writer, http.StatusOK, safeUserFrom(user))
}

func (handler *Handler) writeAuthResponse(writer http.ResponseWriter, status int, result AuthResult) {
	http.SetCookie(writer, &http.Cookie{Name: refreshCookieName, Value: result.RefreshToken, Path: "/", HttpOnly: true, Secure: handler.secureCookie, SameSite: http.SameSiteLaxMode, Expires: result.ExpiresAt, MaxAge: int(time.Until(result.ExpiresAt).Seconds())})
	writeJSON(writer, status, authResponse{User: safeUserFrom(result.User), AccessToken: result.AccessToken})
}

func (handler *Handler) clearRefreshCookie(writer http.ResponseWriter) {
	http.SetCookie(writer, &http.Cookie{Name: refreshCookieName, Value: "", Path: "/", HttpOnly: true, Secure: handler.secureCookie, SameSite: http.SameSiteLaxMode, MaxAge: -1, Expires: time.Unix(1, 0)})
}
func safeUserFrom(user User) safeUser {
	return safeUser{ID: user.ID.String(), Email: user.Email, FirstName: user.FirstName, LastName: user.LastName}
}

func decodeJSON(writer http.ResponseWriter, request *http.Request, target any) bool {
	return httpboundary.Decode(writer, request, target, httpboundary.AuthBodyLimit)
}
func (handler *Handler) respondServiceError(writer http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrHashCapacity):
		ratelimit.Reject(writer, time.Second)
	case errors.Is(err, ErrInvalidInput):
		writeError(writer, http.StatusBadRequest, "invalid_request", "Request data is invalid.")
	case errors.Is(err, ErrDuplicateEmail):
		writeError(writer, http.StatusConflict, "email_taken", "An account with this email already exists.")
	case errors.Is(err, ErrInvalidCredentials):
		writeError(writer, http.StatusUnauthorized, "invalid_credentials", "Invalid email or password.")
	default:
		writeError(writer, http.StatusInternalServerError, "internal_error", "An unexpected error occurred.")
	}
}
func writeJSON(writer http.ResponseWriter, status int, value any) {
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(value)
}
func writeError(writer http.ResponseWriter, status int, code, message string) {
	writeJSON(writer, status, errorResponse{Error: errorBody{Code: code, Message: message}})
}
