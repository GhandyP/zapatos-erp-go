package logistics

import (
	"strings"

	"zapatos-erp-go/internal/domain"
	"zapatos-erp-go/internal/store"
)

type StorageSlot struct {
	ID            string            `json:"id"`
	Name          string            `json:"name"`
	Zone          string            `json:"zone"`
	CapacityUnits int               `json:"capacityUnits"`
	Dimensions    domain.Dimensions `json:"dimensions"`
}

type Service struct{ repo store.Repository[StorageSlot] }

func New(repo store.Repository[StorageSlot]) *Service { return &Service{repo: repo} }

func (s *Service) List() ([]StorageSlot, error) { return s.repo.List() }

func (s *Service) Get(id string) (StorageSlot, error) {
	item, ok, err := s.repo.FindByID(id)
	if err != nil {
		return StorageSlot{}, err
	}
	if !ok {
		return StorageSlot{}, domain.NotFoundError{Entity: "StorageSlot", ID: id}
	}
	return item, nil
}

func (s *Service) Upsert(item StorageSlot) (StorageSlot, error) {
	if strings.TrimSpace(item.ID) == "" || strings.TrimSpace(item.Name) == "" || strings.TrimSpace(item.Zone) == "" {
		return StorageSlot{}, domain.ValidationError{Message: "Storage slot requires id, name and zone"}
	}
	if item.CapacityUnits <= 0 {
		return StorageSlot{}, domain.ValidationError{Message: "capacityUnits must be positive"}
	}
	if err := item.Dimensions.Validate(); err != nil {
		return StorageSlot{}, err
	}
	return s.repo.Save(item)
}

func (s *Service) Remove(id string) error {
	deleted, err := s.repo.Delete(id)
	if err != nil {
		return err
	}
	if !deleted {
		return domain.NotFoundError{Entity: "StorageSlot", ID: id}
	}
	return nil
}
