package handlers

import (
	"net/http"

	"github.com/MohakGupta2004/vipin-lms-backend/internal/lib/utils"
	"github.com/MohakGupta2004/vipin-lms-backend/internal/middleware"
	"github.com/MohakGupta2004/vipin-lms-backend/internal/service"
)

const (
	defaultUsersLimit = 50
	maxUsersLimit     = 200
)

type UserHandler struct {
	userService *service.UserService
}

func NewUserHandler(userService *service.UserService) *UserHandler {
	return &UserHandler{
		userService: userService,
	}
}

// ListUsers godoc
//
//	@Summary		List users
//	@Description	Admin only. Returns active users sorted by name, for the instructor picker (role=instructor) and the student picker (role=student). Never includes password data.
//	@Tags			users
//	@Produce		json
//	@Param			role	query		string											false	"student, instructor or admin (default: all)"
//	@Param			limit	query		int												false	"Max users to return (default 50, max 200)"
//	@Param			offset	query		int												false	"Number of users to skip (default 0)"
//	@Success		200		{object}	utils.JSONResponse{data=[]models.UserSummary}	"Users"
//	@Failure		400		{object}	utils.JSONResponse								"Invalid role, limit or offset"
//	@Failure		401		{object}	utils.JSONResponse								"Not logged in"
//	@Failure		403		{object}	utils.JSONResponse								"Not an admin"
//	@Failure		500		{object}	utils.JSONResponse								"Internal server error"
//	@Router			/users [get]
func (h *UserHandler) ListUsers(w http.ResponseWriter, r *http.Request) {
	user, ok := middleware.UserFromContext(r.Context())
	if !ok {
		utils.WriteJSONResponse(w, http.StatusUnauthorized, "unauthorized access")
		return
	}

	limit, err := intQuery(r, "limit", defaultUsersLimit)
	if err != nil || limit < 1 || limit > maxUsersLimit {
		utils.WriteJSONResponse(w, http.StatusBadRequest, "limit must be between 1 and 200")
		return
	}
	offset, err := intQuery(r, "offset", 0)
	if err != nil || offset < 0 {
		utils.WriteJSONResponse(w, http.StatusBadRequest, "offset must be 0 or more")
		return
	}

	users, err := h.userService.ListUsers(r.Context(), user, r.URL.Query().Get("role"), limit, offset)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	utils.WriteJSONResponse(w, http.StatusOK, users)
}
