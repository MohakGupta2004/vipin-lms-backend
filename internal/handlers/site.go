package handlers

import (
	"net/http"
	"strconv"

	"github.com/MohakGupta2004/vipin-lms-backend/internal/lib/utils"
	"github.com/MohakGupta2004/vipin-lms-backend/internal/middleware"
	"github.com/MohakGupta2004/vipin-lms-backend/internal/service"
)

const (
	defaultSiteDays  = 30
	defaultSiteLimit = 50
)

type SiteHandler struct {
	siteService *service.SiteService
}

func NewSiteHandler(siteService *service.SiteService) *SiteHandler {
	return &SiteHandler{siteService: siteService}
}

// GetSiteOverview godoc
//
//	@Summary		Website overview
//	@Description	Admin only. Aggregated website visitor, pageview and lead metrics for a UTC day window ending now.
//	@Tags			admin-site
//	@Produce		json
//	@Param			days	query		int	false	"Window size in days (default 30, min 1, max 365)"
//	@Success		200		{object}	utils.JSONResponse{data=models.SiteOverview}	"Overview"
//	@Failure		400		{object}	utils.JSONResponse						"Invalid days"
//	@Failure		401		{object}	utils.JSONResponse						"Not logged in"
//	@Failure		403		{object}	utils.JSONResponse						"Not an admin"
//	@Failure		500		{object}	utils.JSONResponse						"Internal server error"
//	@Router			/admin/site/overview [get]
func (h *SiteHandler) GetSiteOverview(w http.ResponseWriter, r *http.Request) {
	user, ok := middleware.UserFromContext(r.Context())
	if !ok {
		utils.WriteJSONResponse(w, http.StatusUnauthorized, "unauthorized access")
		return
	}

	days, err := intQuery(r, "days", defaultSiteDays)
	if err != nil {
		utils.WriteJSONResponse(w, http.StatusBadRequest, "days must be a number")
		return
	}
	overview, err := h.siteService.GetOverview(r.Context(), user, days)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	utils.WriteJSONResponse(w, http.StatusOK, overview)
}

// ListSiteLeads godoc
//
//	@Summary		List website leads
//	@Description	Admin only. Newest first. Optional filters: kind, course slug and q (name/email/company).
//	@Tags			admin-site
//	@Produce		json
//	@Param			kind	query		string	false	"masterclass, diagnostic, contact, firms, newsletter or interest"
//	@Param			course	query		string	false	"Only leads containing this course slug"
//	@Param			q		query		string	false	"Case-insensitive search on name/email/company"
//	@Param			limit	query		int		false	"Max results (default 50, max 200)"
//	@Param			offset	query		int		false	"Number to skip (default 0)"
//	@Success		200		{object}	utils.JSONResponse{data=models.SiteLeadsPage}	"Leads"
//	@Failure		400		{object}	utils.JSONResponse						"Invalid kind, limit or offset"
//	@Failure		401		{object}	utils.JSONResponse						"Not logged in"
//	@Failure		403		{object}	utils.JSONResponse						"Not an admin"
//	@Failure		500		{object}	utils.JSONResponse						"Internal server error"
//	@Router			/admin/site/leads [get]
func (h *SiteHandler) ListSiteLeads(w http.ResponseWriter, r *http.Request) {
	user, ok := middleware.UserFromContext(r.Context())
	if !ok {
		utils.WriteJSONResponse(w, http.StatusUnauthorized, "unauthorized access")
		return
	}

	limit, err := intQuery(r, "limit", defaultSiteLimit)
	if err != nil {
		utils.WriteJSONResponse(w, http.StatusBadRequest, "limit must be a number")
		return
	}
	offset, err := intQuery(r, "offset", 0)
	if err != nil {
		utils.WriteJSONResponse(w, http.StatusBadRequest, "offset must be a number")
		return
	}
	query := r.URL.Query()
	page, err := h.siteService.ListLeads(r.Context(), user, service.SiteLeadsFilter{
		Kind:   query.Get("kind"),
		Course: query.Get("course"),
		Q:      query.Get("q"),
		Limit:  limit,
		Offset: offset,
	})
	if err != nil {
		writeServiceError(w, err)
		return
	}
	utils.WriteJSONResponse(w, http.StatusOK, page)
}

// ListSiteLeadEmails godoc
//
//	@Summary		List website lead emails
//	@Description	Admin only. Distinct lower-cased emails for one lead kind, optionally filtered by course slug.
//	@Tags			admin-site
//	@Produce		json
//	@Param			kind	query		string	true	"masterclass, diagnostic, contact, firms, newsletter or interest"
//	@Param			course	query		string	false	"Only leads containing this course slug"
//	@Success		200		{object}	utils.JSONResponse{data=models.SiteLeadEmails}	"Emails"
//	@Failure		400		{object}	utils.JSONResponse						"Invalid or missing kind"
//	@Failure		401		{object}	utils.JSONResponse						"Not logged in"
//	@Failure		403		{object}	utils.JSONResponse						"Not an admin"
//	@Failure		500		{object}	utils.JSONResponse						"Internal server error"
//	@Router			/admin/site/leads/emails [get]
func (h *SiteHandler) ListSiteLeadEmails(w http.ResponseWriter, r *http.Request) {
	user, ok := middleware.UserFromContext(r.Context())
	if !ok {
		utils.WriteJSONResponse(w, http.StatusUnauthorized, "unauthorized access")
		return
	}

	query := r.URL.Query()
	result, err := h.siteService.ListLeadEmails(r.Context(), user, query.Get("kind"), query.Get("course"))
	if err != nil {
		writeServiceError(w, err)
		return
	}
	utils.WriteJSONResponse(w, http.StatusOK, result)
}

// ListSiteLeadPhones godoc
//
//	@Summary		List website lead phones
//	@Description	Admin only. One row per distinct phone with newest lead's name/email, optionally filtered by kind and course slug.
//	@Tags			admin-site
//	@Produce		json
//	@Param			kind	query		string	false	"masterclass, diagnostic, contact, firms, newsletter or interest"
//	@Param			course	query		string	false	"Only leads containing this course slug"
//	@Success		200		{object}	utils.JSONResponse{data=models.SiteLeadPhones}	"Phones"
//	@Failure		400		{object}	utils.JSONResponse						"Invalid kind"
//	@Failure		401		{object}	utils.JSONResponse						"Not logged in"
//	@Failure		403		{object}	utils.JSONResponse						"Not an admin"
//	@Failure		500		{object}	utils.JSONResponse						"Internal server error"
//	@Router			/admin/site/leads/phones [get]
func (h *SiteHandler) ListSiteLeadPhones(w http.ResponseWriter, r *http.Request) {
	user, ok := middleware.UserFromContext(r.Context())
	if !ok {
		utils.WriteJSONResponse(w, http.StatusUnauthorized, "unauthorized access")
		return
	}

	query := r.URL.Query()
	result, err := h.siteService.ListLeadPhones(r.Context(), user, query.Get("kind"), query.Get("course"))
	if err != nil {
		writeServiceError(w, err)
		return
	}
	utils.WriteJSONResponse(w, http.StatusOK, result)
}

// ListSiteVisitors godoc
//
//	@Summary		List website visitors
//	@Description	Admin only. Ordered by lastSeenAt desc. returning=true keeps only visitors with visitCount > 1.
//	@Tags			admin-site
//	@Produce		json
//	@Param			returning	query		bool	false	"Only returning visitors"
//	@Param			limit		query		int		false	"Max results (default 50, max 200)"
//	@Param			offset		query		int		false	"Number to skip (default 0)"
//	@Success		200			{object}	utils.JSONResponse{data=models.SiteVisitorsPage}	"Visitors"
//	@Failure		400			{object}	utils.JSONResponse							"Invalid returning, limit or offset"
//	@Failure		401			{object}	utils.JSONResponse							"Not logged in"
//	@Failure		403			{object}	utils.JSONResponse							"Not an admin"
//	@Failure		500			{object}	utils.JSONResponse							"Internal server error"
//	@Router				/admin/site/visitors [get]
func (h *SiteHandler) ListSiteVisitors(w http.ResponseWriter, r *http.Request) {
	user, ok := middleware.UserFromContext(r.Context())
	if !ok {
		utils.WriteJSONResponse(w, http.StatusUnauthorized, "unauthorized access")
		return
	}

	limit, err := intQuery(r, "limit", defaultSiteLimit)
	if err != nil {
		utils.WriteJSONResponse(w, http.StatusBadRequest, "limit must be a number")
		return
	}
	offset, err := intQuery(r, "offset", 0)
	if err != nil {
		utils.WriteJSONResponse(w, http.StatusBadRequest, "offset must be a number")
		return
	}

	returningOnly := false
	if value := r.URL.Query().Get("returning"); value != "" {
		parsed, err := strconv.ParseBool(value)
		if err != nil {
			utils.WriteJSONResponse(w, http.StatusBadRequest, "returning must be true or false")
			return
		}
		returningOnly = parsed
	}

	page, err := h.siteService.ListVisitors(r.Context(), user, service.SiteVisitorsFilter{
		Returning: returningOnly,
		Limit:     limit,
		Offset:    offset,
	})
	if err != nil {
		writeServiceError(w, err)
		return
	}
	utils.WriteJSONResponse(w, http.StatusOK, page)
}

// ListSiteVisitorEvents godoc
//
//	@Summary		List one visitor's events
//	@Description	Admin only. Newest first. Helps inspect one visitor journey.
//	@Tags			admin-site
//	@Produce		json
//	@Param			id		path		string	true	"Visitor ID"
//	@Param			limit	query		int		false	"Max events (default 100, max 500)"
//	@Success		200		{object}	utils.JSONResponse{data=[]models.SiteEvent}	"Events"
//	@Failure		400		{object}	utils.JSONResponse						"Invalid limit"
//	@Failure		401		{object}	utils.JSONResponse						"Not logged in"
//	@Failure		403		{object}	utils.JSONResponse						"Not an admin"
//	@Failure		404		{object}	utils.JSONResponse						"Visitor not found"
//	@Failure		500		{object}	utils.JSONResponse						"Internal server error"
//	@Router			/admin/site/visitors/{id}/events [get]
func (h *SiteHandler) ListSiteVisitorEvents(w http.ResponseWriter, r *http.Request) {
	user, ok := middleware.UserFromContext(r.Context())
	if !ok {
		utils.WriteJSONResponse(w, http.StatusUnauthorized, "unauthorized access")
		return
	}

	limit, err := intQuery(r, "limit", 100)
	if err != nil {
		utils.WriteJSONResponse(w, http.StatusBadRequest, "limit must be a number")
		return
	}
	events, err := h.siteService.ListVisitorEvents(r.Context(), user, r.PathValue("id"), limit)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	utils.WriteJSONResponse(w, http.StatusOK, events)
}
