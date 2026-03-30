package scheduler

import "errors"

var (
	ErrNotFound         = errors.New("task not found")
	ErrCannotBeCanceled = errors.New("task cannot be canceled")
)
