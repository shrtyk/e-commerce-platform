package http

import (
	"errors"
	"net/http"

	"github.com/go-chi/render"
	"github.com/go-playground/validator/v10"
	openapi_types "github.com/oapi-codegen/runtime/types"

	commonerrors "github.com/shrtyk/e-commerce-platform/internal/common/errors"
	"github.com/shrtyk/e-commerce-platform/internal/common/transport"
	"github.com/shrtyk/e-commerce-platform/internal/identity-svc/internal/adapters/inbound/http/dto"
	"github.com/shrtyk/e-commerce-platform/internal/identity-svc/internal/core/ports/outbound"
	"github.com/shrtyk/e-commerce-platform/internal/identity-svc/internal/core/service/auth"
)

type IdentityHandler struct {
	dto.Unimplemented

	authService *auth.AuthService
	validator   *validator.Validate
}

func NewIdentityHandler(authService *auth.AuthService) *IdentityHandler {
	return &IdentityHandler{
		authService: authService,
		validator:   validator.New(validator.WithRequiredStructEnabled()),
	}
}

func (h *IdentityHandler) Healthz(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}

func (h *IdentityHandler) HandleOpenAPIError(w http.ResponseWriter, r *http.Request, _ error) {
	h.writeError(w, r, commonerrors.BadRequest("invalid_request", "invalid request parameters"))
}

func (h *IdentityHandler) RegisterUser(w http.ResponseWriter, r *http.Request) {
	var request dto.RegisterRequest
	if err := render.DecodeJSON(r.Body, &request); err != nil {
		h.writeError(w, r, commonerrors.BadRequest("invalid_request", "invalid request body"))
		return
	}
	if err := h.validator.Struct(request); err != nil {
		h.writeError(w, r, commonerrors.BadRequest("invalid_request", "invalid request body"))
		return
	}

	result, err := h.authService.RegisterUser(r.Context(), auth.RegisterUserInput{
		Email:       string(request.Email),
		Password:    request.Password,
		DisplayName: request.DisplayName,
	})
	if err != nil {
		h.writeError(w, r, mapAuthError(err))
		return
	}

	render.Status(r, http.StatusCreated)
	render.JSON(w, r, dto.AuthTokensResponse{
		AccessToken:  result.AccessToken,
		RefreshToken: result.RefreshToken,
	})
}

func (h *IdentityHandler) RegisterAdmin(w http.ResponseWriter, r *http.Request) {
	var request dto.RegisterRequest
	if err := render.DecodeJSON(r.Body, &request); err != nil {
		h.writeError(w, r, commonerrors.BadRequest("invalid_request", "invalid request body"))
		return
	}
	if err := h.validator.Struct(request); err != nil {
		h.writeError(w, r, commonerrors.BadRequest("invalid_request", "invalid request body"))
		return
	}

	result, err := h.authService.RegisterAdmin(r.Context(), auth.RegisterUserInput{
		Email:       string(request.Email),
		Password:    request.Password,
		DisplayName: request.DisplayName,
	})
	if err != nil {
		h.writeError(w, r, mapAuthError(err))
		return
	}

	render.Status(r, http.StatusCreated)
	render.JSON(w, r, dto.AuthTokensResponse{
		AccessToken:  result.AccessToken,
		RefreshToken: result.RefreshToken,
	})
}

func (h *IdentityHandler) LoginUser(w http.ResponseWriter, r *http.Request) {
	var request dto.LoginRequest
	if err := render.DecodeJSON(r.Body, &request); err != nil {
		h.writeError(w, r, commonerrors.BadRequest("invalid_request", "invalid request body"))
		return
	}
	if err := h.validator.Struct(request); err != nil {
		h.writeError(w, r, commonerrors.BadRequest("invalid_request", "invalid request body"))
		return
	}

	result, err := h.authService.LoginUser(r.Context(), auth.LoginUserInput{
		Email:    string(request.Email),
		Password: request.Password,
	})
	if err != nil {
		h.writeError(w, r, mapAuthError(err))
		return
	}

	render.Status(r, http.StatusOK)
	render.JSON(w, r, dto.AuthTokensResponse{
		AccessToken:  result.AccessToken,
		RefreshToken: result.RefreshToken,
	})
}

func (h *IdentityHandler) RefreshToken(w http.ResponseWriter, r *http.Request) {
	var request dto.RefreshTokenRequest
	if err := render.DecodeJSON(r.Body, &request); err != nil {
		h.writeError(w, r, commonerrors.BadRequest("invalid_request", "invalid request body"))
		return
	}
	if err := h.validator.Struct(request); err != nil {
		h.writeError(w, r, commonerrors.BadRequest("invalid_request", "invalid request body"))
		return
	}

	result, err := h.authService.RefreshToken(r.Context(), auth.RefreshTokenInput{
		RefreshToken: request.RefreshToken,
	})
	if err != nil {
		h.writeError(w, r, mapAuthError(err))
		return
	}

	render.Status(r, http.StatusOK)
	render.JSON(w, r, dto.AuthTokensResponse{
		AccessToken:  result.AccessToken,
		RefreshToken: result.RefreshToken,
	})
}

func (h *IdentityHandler) GetMyProfile(w http.ResponseWriter, r *http.Request) {
	claims, ok := transport.ClaimsFromContext(r.Context())
	if !ok {
		h.writeError(w, r, commonerrors.Unauthorized("unauthorized", "unauthorized"))
		return
	}

	result, err := h.authService.GetMyProfile(r.Context(), claims.UserID)
	if err != nil {
		h.writeError(w, r, mapProfileError(err))
		return
	}

	render.Status(r, http.StatusOK)
	render.JSON(w, r, dto.UserProfile{
		UserId:      result.UserID,
		Email:       openapi_types.Email(result.Email),
		DisplayName: result.DisplayName,
		Role:        dto.UserProfileRole(result.Role),
		Status:      dto.UserProfileStatus(result.Status),
	})
}

func (h *IdentityHandler) UpdateMyProfile(w http.ResponseWriter, r *http.Request) {
	claims, ok := transport.ClaimsFromContext(r.Context())
	if !ok {
		h.writeError(w, r, commonerrors.Unauthorized("unauthorized", "unauthorized"))
		return
	}

	var request dto.UpdateProfileRequest
	if err := render.DecodeJSON(r.Body, &request); err != nil {
		h.writeError(w, r, commonerrors.BadRequest("invalid_request", "invalid request body"))
		return
	}
	if err := h.validator.Struct(request); err != nil {
		h.writeError(w, r, commonerrors.BadRequest("invalid_request", "invalid request body"))
		return
	}

	result, err := h.authService.UpdateMyProfile(r.Context(), claims.UserID, auth.UpdateProfileInput{
		DisplayName: request.DisplayName,
	})
	if err != nil {
		h.writeError(w, r, mapProfileError(err))
		return
	}

	render.Status(r, http.StatusOK)
	render.JSON(w, r, dto.UserProfile{
		UserId:      result.UserID,
		Email:       openapi_types.Email(result.Email),
		DisplayName: result.DisplayName,
		Role:        dto.UserProfileRole(result.Role),
		Status:      dto.UserProfileStatus(result.Status),
	})
}

func (h *IdentityHandler) writeError(w http.ResponseWriter, r *http.Request, err error) {
	httpErr := commonerrors.FromError(err)
	commonerrors.WriteJSON(w, httpErr, transport.RequestIDFromContext(r.Context()))
}

func mapAuthError(err error) error {
	switch {
	case errors.Is(err, auth.ErrInvalidRegisterInput):
		return commonerrors.BadRequest("invalid_request", "invalid register input")
	case errors.Is(err, auth.ErrEmailAlreadyRegistered):
		return commonerrors.Conflict("email_already_registered", "email already registered")
	case errors.Is(err, auth.ErrInvalidCredentials):
		return commonerrors.Unauthorized("invalid_credentials", "invalid credentials")
	case errors.Is(err, auth.ErrInvalidRefreshToken):
		return commonerrors.Unauthorized("invalid_refresh_token", "invalid refresh token")
	default:
		return commonerrors.InternalError("internal_error")
	}
}

func mapProfileError(err error) error {
	switch {
	case errors.Is(err, auth.ErrProfileUpdateFailed):
		return commonerrors.BadRequest("invalid_request", "invalid profile input")
	case errors.Is(err, outbound.ErrUserNotFound):
		return commonerrors.NotFound("user_not_found", "user not found")
	default:
		return commonerrors.InternalError("internal_error")
	}
}
