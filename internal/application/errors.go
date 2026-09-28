package application

import (
	"errors"
	"fmt"
)

var (
	ErrInvalidInput     = errors.New("invalid input")
	ErrBusinessRejected = errors.New("business operation rejected")
)

func invalidInput(message string) error {
	return fmt.Errorf("%w: %s", ErrInvalidInput, message)
}

func businessRejected(message string) error {
	return fmt.Errorf("%w: %s", ErrBusinessRejected, message)
}
