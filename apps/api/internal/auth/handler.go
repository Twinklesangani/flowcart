package auth

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"
)

const refreshCookieName = "flowcart_refresh"

type Handler struct {
	service      *Service
	secureCookie bool
}

func NewHandler(service *Service, secureCookie bool) *Handler {
	return &Handler{service: service, secureCookie: secureCookie}
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
	var input registerRequest
	if !decodeJSON(writer, request, &input) {
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
	var input loginRequest
	if !decodeJSON(writer, request, &input) {
		return
	}
	result, err := handler.service.Login(request.Context(), input.Email, input.Password)
	if err != nil {
		handler.respondServiceError(writer, err)
		return
	}
	handler.writeAuthResponse(writer, http.StatusOK, result)
}

func (handler *Handler) Refresh(writer http.ResponseWriter, request *http.Request) {
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
	if cookie, err := request.Cookie(refreshCookieName); err == nil {
		_ = handler.service.Logout(request.Context(), cookie.Value)
	}
	handler.clearRefreshCookie(writer)
	writer.WriteHeader(http.StatusNoContent)
}

func (handler *Handler) Me(writer http.ResponseWriter, request *http.Request) {
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
	decoder := json.NewDecoder(request.Body)
	if err := decoder.Decode(target); err != nil {
		writeError(writer, http.StatusBadRequest, "invalid_request", "Request body must be valid JSON.")
		return false
	}
	return true
}
func (handler *Handler) respondServiceError(writer http.ResponseWriter, err error) {
	switch {
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
