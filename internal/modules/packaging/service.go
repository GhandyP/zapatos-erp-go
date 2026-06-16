package packaging

import (
	"strings"

	"zapatos-erp-go/internal/domain"
	"zapatos-erp-go/internal/store"
)

type PackagingSpec struct {
	ID            string            `json:"id"`
	Name          string            `json:"name"`
	TransportMode string            `json:"transportMode"`
	MaxUnits      int               `json:"maxUnits"`
	Dimensions    domain.Dimensions `json:"dimensions"`
}

type Service struct {
	repo store.Repository[PackagingSpec]
}

func New(repo store.Repository[PackagingSpec]) *Service { return &Service{repo: repo} }

func (s *Service) List() ([]PackagingSpec, error) { return s.repo.List() }

func (s *Service) Get(id string) (PackagingSpec, error) {
	item, ok, err := s.repo.FindByID(id)
	if err != nil {
		return PackagingSpec{}, err
	}
	if !ok {
		return PackagingSpec{}, domain.NotFoundError{Entity: "PackagingSpec", ID: id}
	}
	return item, nil
}

func (s *Service) Upsert(item PackagingSpec) (PackagingSpec, error) {
	if strings.TrimSpace(item.ID) == "" || strings.TrimSpace(item.Name) == "" || strings.TrimSpace(item.TransportMode) == "" {
		return PackagingSpec{}, domain.ValidationError{Message: "Packaging spec requires id, name and transportMode"}
	}
	if item.MaxUnits <= 0 {
		return PackagingSpec{}, domain.ValidationError{Message: "maxUnits must be positive"}
	}
	if err := item.Dimensions.Validate(); err != nil {
		return PackagingSpec{}, err
	}
	return s.repo.Save(item)
}

func (s *Service) Remove(id string) error {
	deleted, err := s.repo.Delete(id)
	if err != nil {
		return err
	}
	if !deleted {
		return domain.NotFoundError{Entity: "PackagingSpec", ID: id}
	}
	return nil
}
