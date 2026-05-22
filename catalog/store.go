package catalog

import (
	"errors"
	"fmt"
	"sync"
	"time"
)

// Sentinel errors returned by Store methods.
var (
	ErrDuplicateSKU = errors.New("duplicate SKU")
	ErrNotFound     = errors.New("product not found")
)

// ProductMeta holds lightweight fields returned in list responses.
// Kept separate from media so list queries never touch URL slices.
type ProductMeta struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	SKU        string    `json:"sku"`
	ImageCount int       `json:"image_count"`
	VideoCount int       `json:"video_count"`
	CreatedAt  time.Time `json:"created_at"`
}

// ProductMedia holds the full URL arrays for a product.
// Only loaded by GetProduct (detail endpoint).
type ProductMedia struct {
	ImageURLs []string
	VideoURLs []string
}

// Product is the full product representation returned on detail and create responses.
type Product struct {
	ProductMeta
	ImageURLs []string `json:"image_urls"`
	VideoURLs []string `json:"video_urls"`
}

// Store is the in-memory product catalog.
//
// Data model:
//
//	metas  map[id]*ProductMeta   — lightweight; used by list queries only
//	media  map[id]*ProductMedia  — URL arrays; only loaded on detail queries
//	skuIdx map[sku]string        — O(1) duplicate SKU detection on create
//	order  []string              — insertion-ordered IDs for stable pagination
//
// A single RWMutex guards all four maps. Reads use RLock; writes use Lock.
// Entries are never deleted so there is no risk of dangling pointer access
// after releasing the lock.
type Store struct {
	mu     sync.RWMutex
	metas  map[string]*ProductMeta
	media  map[string]*ProductMedia
	skuIdx map[string]string
	order  []string
	nextID int
}

// NewStore returns an initialised, empty Store.
func NewStore() *Store {
	return &Store{
		metas:  make(map[string]*ProductMeta),
		media:  make(map[string]*ProductMedia),
		skuIdx: make(map[string]string),
	}
}

func (s *Store) generateID() string {
	s.nextID++
	return fmt.Sprintf("prod_%06d", s.nextID)
}

// copyStrings returns a new slice with the same contents as src.
// Ensures internal state cannot be mutated through caller-held references.
func copyStrings(src []string) []string {
	if len(src) == 0 {
		return []string{}
	}
	dst := make([]string, len(src))
	copy(dst, src)
	return dst
}

// CreateProduct adds a new product and returns the full representation.
// Returns ErrDuplicateSKU if the SKU is already in use.
func (s *Store) CreateProduct(name, sku string, imageURLs, videoURLs []string) (*Product, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.skuIdx[sku]; exists {
		return nil, ErrDuplicateSKU
	}

	id := s.generateID()

	// Copy caller slices so the store owns its data independently.
	imgs := copyStrings(imageURLs)
	vids := copyStrings(videoURLs)

	meta := &ProductMeta{
		ID:         id,
		Name:       name,
		SKU:        sku,
		ImageCount: len(imgs),
		VideoCount: len(vids),
		CreatedAt:  time.Now(),
	}

	s.metas[id] = meta
	s.media[id] = &ProductMedia{ImageURLs: imgs, VideoURLs: vids}
	s.skuIdx[sku] = id
	s.order = append(s.order, id)

	// Return copies so the caller cannot mutate internal state.
	return &Product{
		ProductMeta: *meta,
		ImageURLs:   copyStrings(imgs),
		VideoURLs:   copyStrings(vids),
	}, nil
}

// ListProducts returns a page of lightweight ProductMeta values.
// Media URLs are never loaded — this is O(limit) regardless of how many
// URLs each product has stored.
func (s *Store) ListProducts(offset, limit int) ([]*ProductMeta, int) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	total := len(s.order)
	if offset >= total {
		return []*ProductMeta{}, total
	}

	end := offset + limit
	if end > total {
		end = total
	}

	page := make([]*ProductMeta, end-offset)
	for i, id := range s.order[offset:end] {
		cp := *s.metas[id] // value copy — caller gets its own struct
		page[i] = &cp
	}
	return page, total
}

// GetProduct returns the full product (meta + media) or nil if not found.
func (s *Store) GetProduct(id string) *Product {
	s.mu.RLock()
	defer s.mu.RUnlock()

	meta, ok := s.metas[id]
	if !ok {
		return nil
	}
	med := s.media[id]
	metaCp := *meta

	return &Product{
		ProductMeta: metaCp,
		ImageURLs:   copyStrings(med.ImageURLs),
		VideoURLs:   copyStrings(med.VideoURLs),
	}
}

// AddMedia appends URLs to an existing product.
// Returns ErrNotFound if the product does not exist.
func (s *Store) AddMedia(id string, imageURLs, videoURLs []string) (*Product, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	meta, ok := s.metas[id]
	if !ok {
		return nil, ErrNotFound
	}
	med := s.media[id]

	med.ImageURLs = append(med.ImageURLs, imageURLs...)
	med.VideoURLs = append(med.VideoURLs, videoURLs...)
	meta.ImageCount = len(med.ImageURLs)
	meta.VideoCount = len(med.VideoURLs)

	metaCp := *meta
	return &Product{
		ProductMeta: metaCp,
		ImageURLs:   copyStrings(med.ImageURLs),
		VideoURLs:   copyStrings(med.VideoURLs),
	}, nil
}
