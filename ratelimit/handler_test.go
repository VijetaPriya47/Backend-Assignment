package ratelimit

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func postRequest(t *testing.T, h *Handler, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/request", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.HandleRequest(w, req)
	return w
}

func TestHandleRequest_Accept(t *testing.T) {
	h := NewHandler(NewStore())
	w := postRequest(t, h, `{"user_id":"alice","payload":{"x":1}}`)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var resp acceptedResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("response is not valid JSON: %v", err)
	}
	if resp.Status != "accepted" {
		t.Errorf("expected status=accepted, got %q", resp.Status)
	}
	if resp.UserID != "alice" {
		t.Errorf("expected user_id=alice, got %q", resp.UserID)
	}
}

func TestHandleRequest_PayloadBoolTrue(t *testing.T) {
	h := NewHandler(NewStore())
	w := postRequest(t, h, `{"user_id":"alice","payload":true}`)
	if w.Code != http.StatusCreated {
		t.Errorf("payload=true should be valid, got %d", w.Code)
	}
}

func TestHandleRequest_PayloadZero(t *testing.T) {
	h := NewHandler(NewStore())
	w := postRequest(t, h, `{"user_id":"alice","payload":0}`)
	if w.Code != http.StatusCreated {
		t.Errorf("payload=0 should be valid, got %d", w.Code)
	}
}

func TestHandleRequest_PayloadFalse(t *testing.T) {
	h := NewHandler(NewStore())
	w := postRequest(t, h, `{"user_id":"alice","payload":false}`)
	if w.Code != http.StatusCreated {
		t.Errorf("payload=false should be valid, got %d", w.Code)
	}
}

func TestHandleRequest_RateLimit(t *testing.T) {
	h := NewHandler(NewStore())
	for i := 0; i < MaxRequests; i++ {
		w := postRequest(t, h, `{"user_id":"alice","payload":1}`)
		if w.Code != http.StatusCreated {
			t.Fatalf("request %d: expected 201, got %d", i+1, w.Code)
		}
	}
	w := postRequest(t, h, `{"user_id":"alice","payload":1}`)
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429 on 6th request, got %d", w.Code)
	}
	var errR struct{ Error string `json:"error"` }
	if err := json.Unmarshal(w.Body.Bytes(), &errR); err != nil {
		t.Fatal("429 body is not valid JSON")
	}
	if errR.Error == "" {
		t.Error("429 response must include an error message")
	}
}

func TestHandleRequest_MissingUserID(t *testing.T) {
	h := NewHandler(NewStore())
	w := postRequest(t, h, `{"payload":{"x":1}}`)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for missing user_id, got %d", w.Code)
	}
}

func TestHandleRequest_EmptyUserID(t *testing.T) {
	h := NewHandler(NewStore())
	w := postRequest(t, h, `{"user_id":"   ","payload":1}`)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for whitespace-only user_id, got %d", w.Code)
	}
}

func TestHandleRequest_MissingPayload(t *testing.T) {
	h := NewHandler(NewStore())
	w := postRequest(t, h, `{"user_id":"alice"}`)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for missing payload, got %d", w.Code)
	}
}

func TestHandleRequest_NullPayload(t *testing.T) {
	h := NewHandler(NewStore())
	w := postRequest(t, h, `{"user_id":"alice","payload":null}`)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for null payload, got %d", w.Code)
	}
}

func TestHandleRequest_InvalidJSON(t *testing.T) {
	h := NewHandler(NewStore())
	w := postRequest(t, h, `not-json`)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid JSON, got %d", w.Code)
	}
}

func TestHandleStats_Empty(t *testing.T) {
	h := NewHandler(NewStore())
	req := httptest.NewRequest(http.MethodGet, "/stats", nil)
	w := httptest.NewRecorder()
	h.HandleStats(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp statsResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("response is not valid JSON: %v", err)
	}
	if resp.Users == nil {
		t.Error("users field must not be null on empty store")
	}
}

func TestHandleStats_ShowsCorrectCountsAfterRequests(t *testing.T) {
	h := NewHandler(NewStore())

	// alice: 3 accepted, then fill + 1 extra to get a rejection
	for i := 0; i < 3; i++ {
		postRequest(t, h, `{"user_id":"alice","payload":1}`)
	}
	for i := 0; i < MaxRequests+1; i++ {
		postRequest(t, h, `{"user_id":"alice","payload":1}`)
	}
	// bob: 3 accepted, no rejections
	for i := 0; i < 3; i++ {
		postRequest(t, h, `{"user_id":"bob","payload":1}`)
	}

	req := httptest.NewRequest(http.MethodGet, "/stats", nil)
	w := httptest.NewRecorder()
	h.HandleStats(w, req)

	var resp statsResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("response is not valid JSON: %v", err)
	}

	if resp.Users["alice"].AcceptedInWindow != MaxRequests {
		t.Errorf("alice: expected %d accepted, got %d", MaxRequests, resp.Users["alice"].AcceptedInWindow)
	}
	if resp.Users["alice"].RejectedTotal < 1 {
		t.Errorf("alice: expected at least 1 rejection, got %d", resp.Users["alice"].RejectedTotal)
	}
	if resp.Users["bob"].AcceptedInWindow != 3 {
		t.Errorf("bob: expected 3 accepted, got %d", resp.Users["bob"].AcceptedInWindow)
	}
}
