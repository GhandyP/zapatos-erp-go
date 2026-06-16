package rawmaterials

import (
	"zapatos-erp-go/internal/domain"
	"zapatos-erp-go/internal/store"
)

type RawMaterial struct {
	ID         string            `json:"id"`
	Name       string            `json:"name"`
	Unit       string            `json:"unit"`
	MinStock   int               `json:"minStock"`
	Dimensions domain.Dimensions `json:"dimensions"`
}

type Service struct{ repo store.Repository[RawMaterial] }

func New(repo store.Repository[RawMaterial]) *Service { return &Service{repo: repo} }

func (s *Service) List() ([]RawMaterial, error) { return s.repo.List() }

func (s *Service) Get(id string) (RawMaterial, error) {
	item, ok, err := s.repo.FindByID(id)
	if err != nil {
		return RawMaterial{}, err
	}
	if !ok {
		return RawMaterial{}, domain.NotFoundError{Entity: "RawMaterial", ID: id}
	}
	return item, nil
}

func (s *Service) Upsert(material RawMaterial) (RawMaterial, error) {
	if material.ID == "" || material.Name == "" {
		return RawMaterial{}, domain.ValidationError{Message: "Raw material requires id and name"}
	}
	if material.MinStock < 0 {
		return RawMaterial{}, domain.ValidationError{Message: "minStock cannot be negative"}
	}
	if err := material.Dimensions.Validate(); err != nil {
		return RawMaterial{}, err
	}
	return s.repo.Save(material)
}

func (s *Service) Remove(id string) error {
	deleted, err := s.repo.Delete(id)
	if err != nil {
		return err
	}
	if !deleted {
		return domain.NotFoundError{Entity: "RawMaterial", ID: id}
	}
	return nil
}
