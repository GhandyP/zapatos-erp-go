package domain

import "fmt"

type ValidationError struct{ Message string }

func (e ValidationError) Error() string { return e.Message }

type NotFoundError struct {
	Entity string
	ID     string
}

func (e NotFoundError) Error() string { return fmt.Sprintf("%s not found: %s", e.Entity, e.ID) }
