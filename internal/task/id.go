package task

import "github.com/google/uuid"

func NewID() (string, error) {
	value, err := uuid.NewV7()
	if err != nil {
		return "", err
	}

	return value.String(), nil
}

func IsUUIDv7(value string) bool {
	parsed, err := uuid.Parse(value)
	return err == nil && parsed.Version() == 7 && parsed.String() == value
}
