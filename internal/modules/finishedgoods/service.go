package finishedgoods

import (
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"

	"zapatos-erp-go/internal/domain"
	"zapatos-erp-go/internal/store"
)

type StockAdjustment struct {
	ID    string `json:"id"`
	Delta int    `json:"delta"`
	Note  string `json:"note"`
	At    string `json:"at"`
}

type FinishedProductVariant struct {
	ID          string            `json:"id"`
	Style       string            `json:"style"`
	Size        string            `json:"size"`
	Color       string            `json:"color"`
	Stock       int               `json:"stock"`
	Dimensions  domain.Dimensions `json:"dimensions"`
	Adjustments []StockAdjustment `json:"adjustments,omitempty"`
}

type Service struct {
	mu   sync.Mutex
	repo store.Repository[FinishedProductVariant]
}

func New(repo store.Repository[FinishedProductVariant]) *Service { return &Service{repo: repo} }

func (s *Service) List() ([]FinishedProductVariant, error) {
	items, err := s.repo.List()
	if err != nil {
		return nil, err
	}
	for i := range items {
		items[i] = normalize(items[i])
	}
	return items, nil
}

func (s *Service) Get(id string) (FinishedProductVariant, error) {
	item, ok, err := s.repo.FindByID(id)
	if err != nil {
		return FinishedProductVariant{}, err
	}
	if !ok {
		return FinishedProductVariant{}, domain.NotFoundError{Entity: "FinishedProductVariant", ID: id}
	}
	return normalize(item), nil
}

func (s *Service) Upsert(variant FinishedProductVariant) (FinishedProductVariant, error) {
	if strings.TrimSpace(variant.Style) == "" || strings.TrimSpace(variant.Size) == "" || strings.TrimSpace(variant.Color) == "" {
		return FinishedProductVariant{}, domain.ValidationError{Message: "Variant requires style, size and color"}
	}
	if variant.Stock < 0 {
		return FinishedProductVariant{}, domain.ValidationError{Message: "stock cannot be negative"}
	}
	if err := variant.Dimensions.Validate(); err != nil {
		return FinishedProductVariant{}, err
	}
	if strings.TrimSpace(variant.ID) == "" {
		id, err := createVariantID(variant.Style, variant.Size, variant.Color)
		if err != nil {
			return FinishedProductVariant{}, err
		}
		variant.ID = id
	}
	variant = normalize(variant)
	return s.repo.Save(variant)
}

func (s *Service) AdjustStock(id string, delta int, note string) (FinishedProductVariant, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if delta == 0 {
		return FinishedProductVariant{}, domain.ValidationError{Message: "delta must be a non-zero number"}
	}
	current, err := s.Get(id)
	if err != nil {
		return FinishedProductVariant{}, err
	}
	next := current.Stock + delta
	if next < 0 {
		return FinishedProductVariant{}, domain.ValidationError{Message: "stock cannot be negative"}
	}
	current.Stock = next
	current.Adjustments = append(current.Adjustments, StockAdjustment{
		ID:    fmt.Sprintf("adj_%d", time.Now().UnixNano()),
		Delta: delta,
		Note:  strings.TrimSpace(note),
		At:    time.Now().UTC().Format(time.RFC3339Nano),
	})
	return s.repo.Save(current)
}

func normalize(variant FinishedProductVariant) FinishedProductVariant {
	if variant.Adjustments == nil {
		variant.Adjustments = []StockAdjustment{}
	}
	return variant
}

func createVariantID(style, size, color string) (string, error) {
	slug := slugify(style) + "-" + slugify(size) + "-" + slugify(color)
	slug = strings.Trim(slug, "-")
	if slug == "" {
		return "", domain.ValidationError{Message: "Variant requires style, size and color"}
	}
	return slug, nil
}

var slugRe = regexp.MustCompile(`[^a-z0-9]+`)

func slugify(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = slugRe.ReplaceAllString(value, "-")
	return strings.Trim(value, "-")
}
