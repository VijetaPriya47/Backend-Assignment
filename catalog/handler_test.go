package catalog

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// callHandler routes a request directly to the appropriate handler method,
// injecting path values where needed. Avoids reimplementing a router.
func callHandler(t *testing.T, h *Handler, method, target, body string, pathValues map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	for k, v := range pathValues {
		req.SetPathValue(k, v)
	}
	w := httptest.NewRecorder()
	switch {
	case method == http.MethodPost && target == "/products":
		h.CreateProduct(w, req)
	case method == http.MethodGet && strings.HasPrefix(target, "/products") && !strings.Contains(target, "/media"):
		if id, ok := pathValues["id"]; ok && id != "" {
			h.GetProduct(w, req)
		} else {
			h.ListProducts(w, req)
		}
	case method == http.MethodPost && strings.Contains(target, "/media"):
		h.AddMedia(w, req)
	default:
		t.Fatalf("unhandled route: %s %s", method, target)
	}
	return w
}

func mustUnmarshal(t *testing.T, data []byte, v any) {
	t.Helper()
	if err := json.Unmarshal(data, v); err != nil {
		t.Fatalf("failed to unmarshal response: %v\nbody: %s", err, data)
	}
}

// ---- POST /products ----

func TestCreateProduct_Handler_Created(t *testing.T) {
	h := NewHandler(NewStore())
	w := callHandler(t, h, "POST", "/products", `{
		"name":"Widget A","sku":"SKU-001",
		"image_urls":["https://cdn.example.com/img-1.jpg"],
		"video_urls":["https://cdn.example.com/demo.mp4"]
	}`, nil)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body)
	}
	var p Product
	mustUnmarshal(t, w.Body.Bytes(), &p)
	if p.ID == "" {
		t.Error("id must be present in response")
	}
	if p.ImageCount != 1 || p.VideoCount != 1 {
		t.Errorf("wrong counts: image=%d video=%d", p.ImageCount, p.VideoCount)
	}
	if len(p.ImageURLs) != 1 {
		t.Errorf("expected 1 image URL in create response, got %d", len(p.ImageURLs))
	}
}

func TestCreateProduct_Handler_DuplicateSKU(t *testing.T) {
	h := NewHandler(NewStore())
	body := `{"name":"A","sku":"SKU-001"}`
	callHandler(t, h, "POST", "/products", body, nil)
	w := callHandler(t, h, "POST", "/products", body, nil)
	if w.Code != http.StatusConflict {
		t.Errorf("expected 409 for duplicate SKU, got %d", w.Code)
	}
}

func TestCreateProduct_Handler_EmptyName(t *testing.T) {
	h := NewHandler(NewStore())
	w := callHandler(t, h, "POST", "/products", `{"name":"","sku":"SKU-001"}`, nil)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for empty name, got %d", w.Code)
	}
}

func TestCreateProduct_Handler_EmptySKU(t *testing.T) {
	h := NewHandler(NewStore())
	w := callHandler(t, h, "POST", "/products", `{"name":"Widget","sku":""}`, nil)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for empty SKU, got %d", w.Code)
	}
}

func TestCreateProduct_Handler_InvalidURL(t *testing.T) {
	h := NewHandler(NewStore())
	w := callHandler(t, h, "POST", "/products",
		`{"name":"Widget","sku":"SKU-001","image_urls":["not-a-url"]}`, nil)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid URL, got %d", w.Code)
	}
}

func TestCreateProduct_Handler_TooManyURLs(t *testing.T) {
	h := NewHandler(NewStore())
	urls := make([]string, MaxURLsPerRequest+1)
	for i := range urls {
		urls[i] = `"https://cdn.example.com/img.jpg"`
	}
	body := `{"name":"Widget","sku":"SKU-001","image_urls":[` + strings.Join(urls, ",") + `]}`
	w := callHandler(t, h, "POST", "/products", body, nil)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for too many URLs, got %d", w.Code)
	}
}

func TestCreateProduct_Handler_InvalidJSON(t *testing.T) {
	h := NewHandler(NewStore())
	w := callHandler(t, h, "POST", "/products", `not-json`, nil)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid JSON, got %d", w.Code)
	}
}

// ---- GET /products ----

func TestListProducts_Handler_Empty(t *testing.T) {
	h := NewHandler(NewStore())
	req := httptest.NewRequest("GET", "/products", nil)
	w := httptest.NewRecorder()
	h.ListProducts(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp listResponse
	mustUnmarshal(t, w.Body.Bytes(), &resp)
	if resp.Total != 0 {
		t.Errorf("expected total=0, got %d", resp.Total)
	}
}

func TestListProducts_Handler_NoURLsInResponse(t *testing.T) {
	h := NewHandler(NewStore())
	callHandler(t, h, "POST", "/products", `{
		"name":"Widget","sku":"SKU-001",
		"image_urls":["https://cdn.example.com/img-1.jpg","https://cdn.example.com/img-2.jpg"],
		"video_urls":["https://cdn.example.com/demo.mp4"]
	}`, nil)

	req := httptest.NewRequest("GET", "/products", nil)
	w := httptest.NewRecorder()
	h.ListProducts(w, req)

	if strings.Contains(w.Body.String(), "cdn.example.com") {
		t.Error("list response must not contain image/video URLs")
	}
	var resp listResponse
	mustUnmarshal(t, w.Body.Bytes(), &resp)
	if resp.Products[0].ImageCount != 2 {
		t.Errorf("expected image_count=2 in list, got %d", resp.Products[0].ImageCount)
	}
}

func TestListProducts_Handler_Pagination(t *testing.T) {
	h := NewHandler(NewStore())
	for i := 0; i < 15; i++ {
		callHandler(t, h, "POST", "/products",
			`{"name":"P","sku":"S`+string(rune('A'+i))+`"}`, nil)
	}

	req := httptest.NewRequest("GET", "/products?limit=5&offset=10", nil)
	w := httptest.NewRecorder()
	h.ListProducts(w, req)

	var resp listResponse
	mustUnmarshal(t, w.Body.Bytes(), &resp)
	if len(resp.Products) != 5 {
		t.Errorf("expected 5 items, got %d", len(resp.Products))
	}
	if resp.Total != 15 {
		t.Errorf("expected total=15, got %d", resp.Total)
	}
	if resp.Offset != 10 {
		t.Errorf("expected offset=10, got %d", resp.Offset)
	}
}

func TestListProducts_Handler_LimitClamped(t *testing.T) {
	h := NewHandler(NewStore())
	req := httptest.NewRequest("GET", "/products?limit=9999", nil)
	w := httptest.NewRecorder()
	h.ListProducts(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("overlarge limit should be clamped, not rejected; got %d", w.Code)
	}
	var resp listResponse
	mustUnmarshal(t, w.Body.Bytes(), &resp)
	if resp.Limit != maxLimit {
		t.Errorf("expected limit clamped to %d, got %d", maxLimit, resp.Limit)
	}
}

func TestListProducts_Handler_ZeroLimit(t *testing.T) {
	h := NewHandler(NewStore())
	req := httptest.NewRequest("GET", "/products?limit=0", nil)
	w := httptest.NewRecorder()
	h.ListProducts(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for limit=0, got %d", w.Code)
	}
}

func TestListProducts_Handler_NegativeOffset(t *testing.T) {
	h := NewHandler(NewStore())
	req := httptest.NewRequest("GET", "/products?offset=-1", nil)
	w := httptest.NewRecorder()
	h.ListProducts(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for negative offset, got %d", w.Code)
	}
}

func TestListProducts_Handler_InvalidLimit(t *testing.T) {
	h := NewHandler(NewStore())
	req := httptest.NewRequest("GET", "/products?limit=abc", nil)
	w := httptest.NewRecorder()
	h.ListProducts(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid limit, got %d", w.Code)
	}
}

// ---- GET /products/{id} ----

func TestGetProduct_Handler_Found(t *testing.T) {
	h := NewHandler(NewStore())
	cw := callHandler(t, h, "POST", "/products", `{
		"name":"Widget","sku":"SKU-001",
		"image_urls":["https://cdn.example.com/img-1.jpg"]
	}`, nil)
	var created Product
	mustUnmarshal(t, cw.Body.Bytes(), &created)

	w := callHandler(t, h, "GET", "/products/"+created.ID, "", map[string]string{"id": created.ID})
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var got Product
	mustUnmarshal(t, w.Body.Bytes(), &got)
	if len(got.ImageURLs) != 1 {
		t.Errorf("expected 1 image URL on detail, got %d", len(got.ImageURLs))
	}
}

func TestGetProduct_Handler_NotFound(t *testing.T) {
	h := NewHandler(NewStore())
	w := callHandler(t, h, "GET", "/products/prod_999999", "", map[string]string{"id": "prod_999999"})
	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w.Code)
	}
}

// ---- POST /products/{id}/media ----

func TestAddMedia_Handler_OK(t *testing.T) {
	h := NewHandler(NewStore())
	cw := callHandler(t, h, "POST", "/products", `{"name":"W","sku":"SKU-1"}`, nil)
	var created Product
	mustUnmarshal(t, cw.Body.Bytes(), &created)

	w := callHandler(t, h, "POST", "/products/"+created.ID+"/media",
		`{"image_urls":["https://cdn.example.com/img-1.jpg"]}`,
		map[string]string{"id": created.ID})
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body)
	}
	var updated Product
	mustUnmarshal(t, w.Body.Bytes(), &updated)
	if updated.ImageCount != 1 {
		t.Errorf("expected image_count=1, got %d", updated.ImageCount)
	}
}

func TestAddMedia_Handler_EmptyBody(t *testing.T) {
	h := NewHandler(NewStore())
	cw := callHandler(t, h, "POST", "/products", `{"name":"W","sku":"SKU-1"}`, nil)
	var created Product
	mustUnmarshal(t, cw.Body.Bytes(), &created)

	w := callHandler(t, h, "POST", "/products/"+created.ID+"/media", `{}`,
		map[string]string{"id": created.ID})
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for empty media body, got %d", w.Code)
	}
}

func TestAddMedia_Handler_NotFound(t *testing.T) {
	h := NewHandler(NewStore())
	w := callHandler(t, h, "POST", "/products/ghost/media",
		`{"image_urls":["https://cdn.example.com/x.jpg"]}`,
		map[string]string{"id": "ghost"})
	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w.Code)
	}
}

func TestAddMedia_Handler_InvalidURL(t *testing.T) {
	h := NewHandler(NewStore())
	cw := callHandler(t, h, "POST", "/products", `{"name":"W","sku":"SKU-1"}`, nil)
	var created Product
	mustUnmarshal(t, cw.Body.Bytes(), &created)

	w := callHandler(t, h, "POST", "/products/"+created.ID+"/media",
		`{"image_urls":["not-a-url"]}`,
		map[string]string{"id": created.ID})
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid URL, got %d", w.Code)
	}
}
