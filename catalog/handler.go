package catalog

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/source-asia-backend/internal/httputil"
)

const (
	defaultLimit = 20
	maxLimit     = 100
)

// Handler wires the catalog Store to HTTP endpoints.
type Handler struct {
	store *Store
}

// NewHandler returns a Handler backed by the given Store.
func NewHandler(store *Store) *Handler {
	return &Handler{store: store}
}

// createProductBody is the expected JSON shape for POST /products.
type createProductBody struct {
	Name      string   `json:"name"`
	SKU       string   `json:"sku"`
	ImageURLs []string `json:"image_urls"`
	VideoURLs []string `json:"video_urls"`
}

// addMediaBody is the expected JSON shape for POST /products/{id}/media.
type addMediaBody struct {
	ImageURLs []string `json:"image_urls"`
	VideoURLs []string `json:"video_urls"`
}

// listItem is the lightweight product shape returned by GET /products.
// Intentionally omits ImageURLs and VideoURLs to prevent loading all media
// on list queries.
type listItem struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	SKU        string    `json:"sku"`
	ImageCount int       `json:"image_count"`
	VideoCount int       `json:"video_count"`
	CreatedAt  string    `json:"created_at"`
}

// listResponse is the paginated envelope returned by GET /products.
type listResponse struct {
	Products []listItem `json:"products"`
	Total    int        `json:"total"`
	Offset   int        `json:"offset"`
	Limit    int        `json:"limit"`
}

// CreateProduct handles POST /products.
// Returns 201 Created, 409 Conflict on duplicate SKU, 400 on bad input.
func (h *Handler) CreateProduct(w http.ResponseWriter, r *http.Request) {
	var body createProductBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		httputil.BadRequest(w, "invalid JSON body")
		return
	}

	if err := validateNonEmpty(body.Name, "name"); err != nil {
		httputil.BadRequest(w, err.Error())
		return
	}
	if err := validateNonEmpty(body.SKU, "sku"); err != nil {
		httputil.BadRequest(w, err.Error())
		return
	}
	if err := validateURLSlice(body.ImageURLs, "image_urls"); err != nil {
		httputil.BadRequest(w, err.Error())
		return
	}
	if err := validateURLSlice(body.VideoURLs, "video_urls"); err != nil {
		httputil.BadRequest(w, err.Error())
		return
	}

	product, err := h.store.CreateProduct(body.Name, body.SKU, body.ImageURLs, body.VideoURLs)
	if err != nil {
		if errors.Is(err, ErrDuplicateSKU) {
			httputil.WriteJSON(w, http.StatusConflict,
				httputil.ErrResp{Error: "a product with this SKU already exists"})
			return
		}
		httputil.InternalError(w)
		return
	}

	httputil.WriteJSON(w, http.StatusCreated, product)
}

// ListProducts handles GET /products.
// Never loads media URLs — only returns ProductMeta fields.
func (h *Handler) ListProducts(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	limit := defaultLimit
	if l := q.Get("limit"); l != "" {
		v, err := strconv.Atoi(l)
		if err != nil || v < 1 {
			httputil.BadRequest(w, "limit must be a positive integer")
			return
		}
		if v > maxLimit {
			v = maxLimit
		}
		limit = v
	}

	offset := 0
	if o := q.Get("offset"); o != "" {
		v, err := strconv.Atoi(o)
		if err != nil || v < 0 {
			httputil.BadRequest(w, "offset must be a non-negative integer")
			return
		}
		offset = v
	}

	metas, total := h.store.ListProducts(offset, limit)

	items := make([]listItem, len(metas))
	for i, m := range metas {
		items[i] = listItem{
			ID:         m.ID,
			Name:       m.Name,
			SKU:        m.SKU,
			ImageCount: m.ImageCount,
			VideoCount: m.VideoCount,
			CreatedAt:  m.CreatedAt.UTC().Format("2006-01-02T15:04:05Z"),
		}
	}

	httputil.WriteJSON(w, http.StatusOK, listResponse{
		Products: items,
		Total:    total,
		Offset:   offset,
		Limit:    limit,
	})
}

// GetProduct handles GET /products/{id}.
func (h *Handler) GetProduct(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	product := h.store.GetProduct(id)
	if product == nil {
		httputil.NotFound(w, "product not found")
		return
	}
	httputil.WriteJSON(w, http.StatusOK, product)
}

// AddMedia handles POST /products/{id}/media.
func (h *Handler) AddMedia(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	var body addMediaBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		httputil.BadRequest(w, "invalid JSON body")
		return
	}

	if len(body.ImageURLs) == 0 && len(body.VideoURLs) == 0 {
		httputil.BadRequest(w, "at least one of image_urls or video_urls must be provided and non-empty")
		return
	}
	if err := validateURLSlice(body.ImageURLs, "image_urls"); err != nil {
		httputil.BadRequest(w, err.Error())
		return
	}
	if err := validateURLSlice(body.VideoURLs, "video_urls"); err != nil {
		httputil.BadRequest(w, err.Error())
		return
	}

	product, err := h.store.AddMedia(id, body.ImageURLs, body.VideoURLs)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			httputil.NotFound(w, "product not found")
			return
		}
		httputil.InternalError(w)
		return
	}

	httputil.WriteJSON(w, http.StatusOK, product)
}
