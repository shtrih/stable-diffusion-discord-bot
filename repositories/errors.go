package repositories

import "fmt"

type NotFoundError struct {
	entityName string
}

func NewNotFoundError(entityName string) *NotFoundError {
	return &NotFoundError{entityName: entityName}
}

func (e *NotFoundError) Error() string {
	return fmt.Sprintf("%s not found", e.entityName)
}

func (e *NotFoundError) Is(err error) bool {
	_, ok := err.(*NotFoundError)
	return ok
}
