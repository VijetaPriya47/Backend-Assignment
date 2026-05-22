package catalog

import (
	"errors"
	"sync"
	"testing"
)

// ---- CreateProduct ----

func TestCreateProduct_Basic(t *testing.T) {
	s := NewStore()
	p, err := s.CreateProduct("Widget A", "SKU-001",
		[]string{"https://cdn.example.com/img-1.jpg"},
		[]string{"https://cdn.example.com/demo.mp4"},
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.ID == "" {
		t.Error("product ID must not be empty")
	}
	if p.Name != "Widget A" || p.SKU != "SKU-001" {
		t.Errorf("wrong name/sku: %q %q", p.Name, p.SKU)
	}
	if p.ImageCount != 1 || p.VideoCount != 1 {
		t.Errorf("wrong counts: image=%d video=%d", p.ImageCount, p.VideoCount)
	}
	if len(p.ImageURLs) != 1 || len(p.VideoURLs) != 1 {
		t.Errorf("wrong URL slice lengths: image=%d video=%d", len(p.ImageURLs), len(p.VideoURLs))
	}
}

func TestCreateProduct_NilMediaBecomesEmptySlice(t *testing.T) {
	s := NewStore()
	p, err := s.CreateProduct("Bare", "SKU-BARE", nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.ImageURLs == nil || p.VideoURLs == nil {
		t.Error("image_urls and video_urls must be empty slices, not nil")
	}
}

func TestCreateProduct_DuplicateSKU(t *testing.T) {
	s := NewStore()
	s.CreateProduct("Widget A", "SKU-001", nil, nil) //nolint:errcheck
	_, err := s.CreateProduct("Widget B", "SKU-001", nil, nil)
	if !errors.Is(err, ErrDuplicateSKU) {
		t.Fatalf("expected ErrDuplicateSKU, got %v", err)
	}
}

func TestCreateProduct_ConcurrentDuplicateSKU(t *testing.T) {
	// Two goroutines racing to create the same SKU — exactly one must win.
	s := NewStore()
	var wg sync.WaitGroup
	wins := 0
	var mu sync.Mutex

	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := s.CreateProduct("P", "RACE-SKU", nil, nil)
			if err == nil {
				mu.Lock()
				wins++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()

	if wins != 1 {
		t.Errorf("expected exactly 1 winner on concurrent duplicate SKU, got %d", wins)
	}
}

func TestCreateProduct_SliceAliasing(t *testing.T) {
	s := NewStore()
	urls := []string{"https://cdn.example.com/img-1.jpg"}
	p, _ := s.CreateProduct("Widget", "SKU-1", urls, nil)

	// Mutating the caller's slice must not affect internal state.
	urls[0] = "https://evil.com/hacked"

	got := s.GetProduct(p.ID)
	if got.ImageURLs[0] != "https://cdn.example.com/img-1.jpg" {
		t.Errorf("internal store was mutated through caller slice: %s", got.ImageURLs[0])
	}
}

func TestCreateProduct_ReturnSliceAliasing(t *testing.T) {
	s := NewStore()
	p, _ := s.CreateProduct("Widget", "SKU-1",
		[]string{"https://cdn.example.com/img-1.jpg"}, nil)

	// Mutating the returned slice must not affect internal state.
	p.ImageURLs[0] = "https://evil.com/hacked"

	got := s.GetProduct(p.ID)
	if got.ImageURLs[0] != "https://cdn.example.com/img-1.jpg" {
		t.Errorf("internal store mutated through returned slice: %s", got.ImageURLs[0])
	}
}

func TestCreateProduct_IDsAreUnique(t *testing.T) {
	s := NewStore()
	seen := make(map[string]bool)
	for i := 0; i < 10; i++ {
		p, _ := s.CreateProduct("P", "SKU-"+string(rune('A'+i)), nil, nil)
		if seen[p.ID] {
			t.Fatalf("duplicate ID generated: %s", p.ID)
		}
		seen[p.ID] = true
	}
}

// ---- ListProducts ----

func TestListProducts_Empty(t *testing.T) {
	s := NewStore()
	items, total := s.ListProducts(0, 10)
	if total != 0 || len(items) != 0 {
		t.Errorf("expected empty list, got total=%d len=%d", total, len(items))
	}
}

func TestListProducts_Pagination(t *testing.T) {
	s := NewStore()
	for i := 0; i < 25; i++ {
		s.CreateProduct("P", "SKU-"+string(rune('A'+i%26))+string(rune('0'+i%10)), nil, nil) //nolint:errcheck
	}

	page1, total := s.ListProducts(0, 10)
	if total != 25 {
		t.Errorf("expected total=25, got %d", total)
	}
	if len(page1) != 10 {
		t.Errorf("expected 10 items on page 1, got %d", len(page1))
	}

	lastPage, _ := s.ListProducts(20, 10)
	if len(lastPage) != 5 {
		t.Errorf("expected 5 items on last page, got %d", len(lastPage))
	}
}

func TestListProducts_OffsetBeyondTotal(t *testing.T) {
	s := NewStore()
	s.CreateProduct("P", "SKU-1", nil, nil) //nolint:errcheck

	items, total := s.ListProducts(999, 10)
	if total != 1 || len(items) != 0 {
		t.Errorf("expected empty page: total=%d len=%d", total, len(items))
	}
}

func TestListProducts_NeverLoadsMediaURLs(t *testing.T) {
	s := NewStore()
	s.CreateProduct("Widget", "SKU-1", //nolint:errcheck
		[]string{"https://cdn.example.com/a.jpg", "https://cdn.example.com/b.jpg"},
		[]string{"https://cdn.example.com/v.mp4"},
	)

	items, _ := s.ListProducts(0, 10)
	if len(items) != 1 {
		t.Fatalf("expected 1 item")
	}
	// Counts come from meta, never from loading media.
	if items[0].ImageCount != 2 || items[0].VideoCount != 1 {
		t.Errorf("wrong counts: image=%d video=%d", items[0].ImageCount, items[0].VideoCount)
	}
}

// ---- GetProduct ----

func TestGetProduct_Found(t *testing.T) {
	s := NewStore()
	created, _ := s.CreateProduct("Widget", "SKU-1",
		[]string{"https://cdn.example.com/a.jpg"}, []string{})

	got := s.GetProduct(created.ID)
	if got == nil {
		t.Fatal("expected product, got nil")
	}
	if got.ID != created.ID {
		t.Errorf("ID mismatch")
	}
	if len(got.ImageURLs) != 1 {
		t.Errorf("expected 1 image URL, got %d", len(got.ImageURLs))
	}
}

func TestGetProduct_NotFound(t *testing.T) {
	s := NewStore()
	if s.GetProduct("nonexistent") != nil {
		t.Error("expected nil for unknown ID")
	}
}

// ---- AddMedia ----

func TestAddMedia_AppendsURLs(t *testing.T) {
	s := NewStore()
	created, _ := s.CreateProduct("Widget", "SKU-1",
		[]string{"https://cdn.example.com/img-1.jpg"}, []string{})

	updated, err := s.AddMedia(created.ID,
		[]string{"https://cdn.example.com/img-2.jpg"},
		[]string{"https://cdn.example.com/demo.mp4"},
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(updated.ImageURLs) != 2 || updated.ImageCount != 2 {
		t.Errorf("expected 2 images, got len=%d count=%d", len(updated.ImageURLs), updated.ImageCount)
	}
	if len(updated.VideoURLs) != 1 || updated.VideoCount != 1 {
		t.Errorf("expected 1 video, got len=%d count=%d", len(updated.VideoURLs), updated.VideoCount)
	}
}

func TestAddMedia_UpdatesMetaCountsVisibleInList(t *testing.T) {
	s := NewStore()
	created, _ := s.CreateProduct("Widget", "SKU-1", nil, nil)
	s.AddMedia(created.ID, []string{"https://cdn.example.com/img-1.jpg"}, nil) //nolint:errcheck

	items, _ := s.ListProducts(0, 10)
	if items[0].ImageCount != 1 {
		t.Errorf("meta image_count not updated: got %d", items[0].ImageCount)
	}
}

func TestAddMedia_WithNilURLs(t *testing.T) {
	s := NewStore()
	created, _ := s.CreateProduct("Widget", "SKU-1", nil, nil)
	// Passing nil for one of the arrays must not panic
	_, err := s.AddMedia(created.ID, []string{"https://cdn.example.com/a.jpg"}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestAddMedia_NotFound(t *testing.T) {
	s := NewStore()
	_, err := s.AddMedia("ghost", []string{"https://cdn.example.com/x.jpg"}, nil)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}
