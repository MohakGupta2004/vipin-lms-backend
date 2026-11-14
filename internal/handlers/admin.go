package handlers

import (
	"net/http"

	"github.com/MohakGupta2004/vipin-lms-backend/internal/lib/utils"
	"github.com/MohakGupta2004/vipin-lms-backend/internal/middleware"
	"github.com/MohakGupta2004/vipin-lms-backend/internal/service"
)

const (
	defaultAdminDays  = 3
	defaultAdminLimit = 50
)

type AdminHandler struct {
	adminService *service.AdminService
}

func NewAdminHandler(adminService *service.AdminService) *AdminHandler {
	return &AdminHandler{adminService: adminService}
}

// ListAdminPeople godoc
//
//	@Summary		List students for the admin
//	@Description	Admin only. Students of one view: new, never_logged_in, not_enrolled or recently_enrolled.
//	@Description	Every person carries all of their enrollments, newest first.
//	@Tags			admin
//	@Produce		json
//	@Param			view	query		string									true	"new, never_logged_in, not_enrolled or recently_enrolled"
//	@Param			days	query		int										false	"Window size in days (default 3, min 1, max 30)"
//	@Param			limit	query		int										false	"Max students to return (default 50, max 200)"
//	@Param			offset	query		int										false	"Number of students to skip (default 0)"
//	@Success		200		{object}	utils.JSONResponse{data=models.PeoplePage}	"Students"
//	@Failure		400		{object}	utils.JSONResponse						"Invalid view, days, limit or offset"
//	@Failure		401		{object}	utils.JSONResponse						"Not logged in"
//	@Failure		403		{object}	utils.JSONResponse						"Not an admin"
//	@Failure		500		{object}	utils.JSONResponse						"Internal server error"
//	@Router			/admin/people [get]
func (h *AdminHandler) ListAdminPeople(w http.ResponseWriter, r *http.Request) {
	user, ok := middleware.UserFromContext(r.Context())
	if !ok {
		utils.WriteJSONResponse(w, http.StatusUnauthorized, "unauthorized access")
		return
	}

	days, err := intQuery(r, "days", defaultAdminDays)
	if err != nil {
		utils.WriteJSONResponse(w, http.StatusBadRequest, "days must be a number")
		return
	}
	limit, err := intQuery(r, "limit", defaultAdminLimit)
	if err != nil {
		utils.WriteJSONResponse(w, http.StatusBadRequest, "limit must be a number")
		return
	}
	offset, err := intQuery(r, "offset", 0)
	if err != nil {
		utils.WriteJSONResponse(w, http.StatusBadRequest, "offset must be a number")
		return
	}

	page, err := h.adminService.ListPeople(r.Context(), user, service.PeopleFilter{
		View:   r.URL.Query().Get("view"),
		Days:   days,
		Limit:  limit,
		Offset: offset,
	})
	if err != nil {
		writeServiceError(w, err)
		return
	}
	utils.WriteJSONResponse(w, http.StatusOK, page)
}

// GetAdminDashboard godoc
//
//	@Summary		Admin dashboard
//	@Description	Admin only. Sign-ups, logins, enrollments and website numbers of the last days, plus the newest sign-ups and leads.
//	@Tags			admin
//	@Produce		json
//	@Param			days	query		int	false	"Window size in days (default 3, min 1, max 30)"
//	@Success		200		{object}	utils.JSONResponse{data=models.AdminDashboard}	"Dashboard"
//	@Failure		400		{object}	utils.JSONResponse							"Invalid days"
//	@Failure		401		{object}	utils.JSONResponse							"Not logged in"
//	@Failure		403		{object}	utils.JSONResponse							"Not an admin"
//	@Failure		500		{object}	utils.JSONResponse							"Internal server error"
//	@Router			/admin/dashboard [get]
func (h *AdminHandler) GetAdminDashboard(w http.ResponseWriter, r *http.Request) {
	user, ok := middleware.UserFromContext(r.Context())
	if !ok {
		utils.WriteJSONResponse(w, http.StatusUnauthorized, "unauthorized access")
		return
	}

	days, err := intQuery(r, "days", defaultAdminDays)
	if err != nil {
		utils.WriteJSONResponse(w, http.StatusBadRequest, "days must be a number")
		return
	}
	dashboard, err := h.adminService.GetDashboard(r.Context(), user, days)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	utils.WriteJSONResponse(w, http.StatusOK, dashboard)
}
