package handlers

import (
	"errors"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"strconv"
	"time"

	"github.com/MohakGupta2004/vipin-lms-backend/internal/lib/utils"
	"github.com/MohakGupta2004/vipin-lms-backend/internal/middleware"
	"github.com/MohakGupta2004/vipin-lms-backend/internal/service"
	"github.com/MohakGupta2004/vipin-lms-backend/internal/storage"
)

const (
	maxNoteRequestBytes = storage.MaxPDFSize + 1<<20 // PDF plus room for the text fields
	noteFormMemory      = 1 << 20                    // bigger parts spill to disk
	noteTransferTimeout = 2 * time.Minute            // server-wide timeouts are too short for a 25 MB file
	defaultNoteLimit    = 50
	maxNoteLimit        = 100
)

type CourseNoteHandler struct {
	noteService *service.CourseNoteService
}

func NewCourseNoteHandler(noteService *service.CourseNoteService) *CourseNoteHandler {
	return &CourseNoteHandler{
		noteService: noteService,
	}
}

// UploadNote godoc
//
//	@Summary		Upload a course note
//	@Description	Instructor uploads a PDF note to a course they teach. Max 25 MB.
//	@Tags			notes
//	@Accept			multipart/form-data
//	@Produce		json
//	@Param			id			path		string										true	"Course ID"
//	@Param			title		formData	string										true	"Note title (max 200 characters)"
//	@Param			description	formData	string										false	"Short description (max 2000 characters)"
//	@Param			file		formData	file										true	"PDF file"
//	@Success		201			{object}	utils.JSONResponse{data=models.CourseNote}	"Note uploaded"
//	@Failure		400			{object}	utils.JSONResponse							"Invalid form, file is not a PDF, or too large"
//	@Failure		401			{object}	utils.JSONResponse							"Not logged in"
//	@Failure		403			{object}	utils.JSONResponse							"Not the instructor of this course"
//	@Failure		404			{object}	utils.JSONResponse							"Course not found"
//	@Failure		500			{object}	utils.JSONResponse							"Internal server error"
//	@Router			/courses/{id}/notes [post]
func (h *CourseNoteHandler) UploadNote(w http.ResponseWriter, r *http.Request) {
	user, ok := middleware.UserFromContext(r.Context())
	if !ok {
		utils.WriteJSONResponse(w, http.StatusUnauthorized, "unauthorized access")
		return
	}

	extendDeadlines(w)
	r.Body = http.MaxBytesReader(w, r.Body, maxNoteRequestBytes)
	if err := r.ParseMultipartForm(noteFormMemory); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			utils.WriteJSONResponse(w, http.StatusBadRequest, storage.ErrPDFTooLarge.Error())
			return
		}
		utils.WriteJSONResponse(w, http.StatusBadRequest, "malformed multipart form")
		return
	}
	defer r.MultipartForm.RemoveAll()

	file, header, err := r.FormFile("file")
	if err != nil {
		utils.WriteJSONResponse(w, http.StatusBadRequest, "file is required")
		return
	}
	defer file.Close()

	note, err := h.noteService.UploadNote(r.Context(), user, r.PathValue("id"),
		r.FormValue("title"), r.FormValue("description"), header.Filename, file)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	utils.WriteJSONResponse(w, http.StatusCreated, note)
}

// ListNotes godoc
//
//	@Summary		List a course's notes
//	@Description	Returns the PDF notes of a course, newest first. Only the course instructor and enrolled students can see them.
//	@Tags			notes
//	@Produce		json
//	@Param			id		path		string											true	"Course ID"
//	@Param			limit	query		int												false	"Max notes to return (default 50, max 100)"
//	@Param			offset	query		int												false	"Number of notes to skip (default 0)"
//	@Success		200		{object}	utils.JSONResponse{data=[]models.CourseNote}	"Notes"
//	@Failure		400		{object}	utils.JSONResponse								"Invalid limit or offset"
//	@Failure		401		{object}	utils.JSONResponse								"Not logged in"
//	@Failure		403		{object}	utils.JSONResponse								"Not enrolled in this course"
//	@Failure		404		{object}	utils.JSONResponse								"Course not found"
//	@Failure		500		{object}	utils.JSONResponse								"Internal server error"
//	@Router			/courses/{id}/notes [get]
func (h *CourseNoteHandler) ListNotes(w http.ResponseWriter, r *http.Request) {
	user, ok := middleware.UserFromContext(r.Context())
	if !ok {
		utils.WriteJSONResponse(w, http.StatusUnauthorized, "unauthorized access")
		return
	}

	limit, err := intQuery(r, "limit", defaultNoteLimit)
	if err != nil || limit < 1 || limit > maxNoteLimit {
		utils.WriteJSONResponse(w, http.StatusBadRequest, "limit must be between 1 and 100")
		return
	}
	offset, err := intQuery(r, "offset", 0)
	if err != nil || offset < 0 {
		utils.WriteJSONResponse(w, http.StatusBadRequest, "offset must be 0 or more")
		return
	}

	notes, err := h.noteService.ListNotes(r.Context(), user, r.PathValue("id"), limit, offset)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	utils.WriteJSONResponse(w, http.StatusOK, notes)
}

// DownloadNote godoc
//
//	@Summary		Download a note
//	@Description	Streams the PDF. Only the course instructor and enrolled students can download it.
//	@Tags			notes
//	@Produce		application/pdf
//	@Param			id	path		string				true	"Note ID"
//	@Success		200	{file}		file				"PDF file"
//	@Failure		401	{object}	utils.JSONResponse	"Not logged in"
//	@Failure		404	{object}	utils.JSONResponse	"Note not found, or not visible to this user"
//	@Failure		500	{object}	utils.JSONResponse	"Internal server error"
//	@Router			/notes/{id}/file [get]
func (h *CourseNoteHandler) DownloadNote(w http.ResponseWriter, r *http.Request) {
	user, ok := middleware.UserFromContext(r.Context())
	if !ok {
		utils.WriteJSONResponse(w, http.StatusUnauthorized, "unauthorized access")
		return
	}

	note, rc, err := h.noteService.OpenNote(r.Context(), user, r.PathValue("id"))
	if err != nil {
		writeServiceError(w, err)
		return
	}
	defer rc.Close()

	extendDeadlines(w)
	hdr := w.Header()
	hdr.Set("Content-Type", "application/pdf")
	hdr.Set("Content-Disposition", mime.FormatMediaType("inline", map[string]string{"filename": note.FileName}))
	hdr.Set("Content-Length", strconv.FormatInt(note.SizeBytes, 10))
	hdr.Set("Cache-Control", "private, no-store")
	hdr.Set("X-Content-Type-Options", "nosniff")
	hdr.Set("Content-Security-Policy", "sandbox")
	if _, err := io.Copy(w, rc); err != nil {
		// Headers are already sent, so all we can do is log.
		slog.Error("note download interrupted", "noteId", note.ID, "err", err)
	}
}

// DeleteNote godoc
//
//	@Summary		Delete a note
//	@Description	Instructor deletes a note they uploaded. The PDF is removed too.
//	@Tags			notes
//	@Produce		json
//	@Param			id	path		string							true	"Note ID"
//	@Success		200	{object}	utils.JSONResponse{data=string}	"Note deleted"
//	@Failure		401	{object}	utils.JSONResponse				"Not logged in"
//	@Failure		403	{object}	utils.JSONResponse				"Not an instructor"
//	@Failure		404	{object}	utils.JSONResponse				"Note not found"
//	@Failure		500	{object}	utils.JSONResponse				"Internal server error"
//	@Router			/notes/{id} [delete]
func (h *CourseNoteHandler) DeleteNote(w http.ResponseWriter, r *http.Request) {
	user, ok := middleware.UserFromContext(r.Context())
	if !ok {
		utils.WriteJSONResponse(w, http.StatusUnauthorized, "unauthorized access")
		return
	}

	if err := h.noteService.DeleteNote(r.Context(), user, r.PathValue("id")); err != nil {
		writeServiceError(w, err)
		return
	}
	utils.WriteJSONResponse(w, http.StatusOK, "note deleted")
}

// extendDeadlines lifts the server-wide read/write timeouts for this one file transfer.
// Errors are ignored: some ResponseWriters (tests) do not support deadlines.
func extendDeadlines(w http.ResponseWriter) {
	rc := http.NewResponseController(w)
	deadline := time.Now().Add(noteTransferTimeout)
	_ = rc.SetReadDeadline(deadline)
	_ = rc.SetWriteDeadline(deadline)
}
