// Author: Eryk Kulikowski @ KU Leuven (2026). Apache 2.0 License

package types

import "errors"

// UnrecoverableError marks a job error as permanent: retrying cannot succeed
// until an operator fixes the underlying problem (e.g., a source data object
// whose replica cannot be opened). The job loop fails such jobs immediately
// instead of exhausting the retry budget.
type UnrecoverableError struct {
	Err error
}

func (e *UnrecoverableError) Error() string {
	return "unrecoverable error detected: " + e.Err.Error()
}

func (e *UnrecoverableError) Unwrap() error {
	return e.Err
}

func NewUnrecoverableError(err error) error {
	return &UnrecoverableError{Err: err}
}

func IsUnrecoverable(err error) bool {
	var target *UnrecoverableError
	return errors.As(err, &target)
}
