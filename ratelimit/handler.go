package ratelimit

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/source-asia-backend/internal/httputil"
)

// Handler wires the Store to HTTP endpoints.
type Handler struct {
	store *Store
}

// NewHandler returns a Handler backed by the given Store.
func NewHandler(store *Store) *Handler {
	return &Handler{store: store}
}

// requestBody is the expected JSON shape for POST /request.
type requestBody struct {
	UserID  string          `json:"user_id"`
	Payload json.RawMessage `json:"payload"`
}

// acceptedResponse is returned on a successful POST /request.
type acceptedResponse struct {
	Status  string          `json:"status"`
	UserID  string          `json:"user_id"`
	Payload json.RawMessage `json:"payload"`
	Message string          `json:"message"`
}

// statsResponse is returned by GET /stats.
type statsResponse struct {
	Users map[string]UserStats `json:"users"`
}

// HandleRequest handles POST /request.
// Returns 201 on acceptance, 429 when rate-limited, 400 on bad input.
func (h *Handler) HandleRequest(w http.ResponseWriter, r *http.Request) {
	var body requestBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		httputil.BadRequest(w, "invalid JSON body")
		return
	}

	if strings.TrimSpace(body.UserID) == "" {
		httputil.BadRequest(w, "user_id is required and must be non-empty")
		return
	}

	if len(body.Payload) == 0 || string(body.Payload) == "null" {
		httputil.BadRequest(w, "payload is required")
		return
	}

	if h.store.TryAccept(body.UserID) {
		httputil.WriteJSON(w, http.StatusCreated, acceptedResponse{
			Status:  "accepted",
			UserID:  body.UserID,
			Payload: body.Payload,
			Message: "request accepted",
		})
		return
	}

	httputil.WriteJSON(w, http.StatusTooManyRequests, httputil.ErrResp{
		Error: "rate limit exceeded: maximum 5 requests per minute per user",
	})
}

// HandleStats handles GET /stats.
func (h *Handler) HandleStats(w http.ResponseWriter, r *http.Request) {
	httputil.WriteJSON(w, http.StatusOK, statsResponse{Users: h.store.AllStats()})
}
