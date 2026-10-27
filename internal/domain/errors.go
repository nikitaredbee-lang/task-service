package domain

import "errors"

// Базовые виды ошибок. По ним HTTP слой выбирает код ответа.
var (
	ErrNotFound     = errors.New("not found")
	ErrForbidden    = errors.New("forbidden")
	ErrConflict     = errors.New("conflict")
	ErrValidation   = errors.New("validation error")
	ErrUnauthorized = errors.New("unauthorized")
)

// Error это ошибка с сообщением, которое можно показать клиенту.
// Через errors.Is она сводится к одному из базовых видов выше.
type Error struct {
	kind error
	msg  string
}

func (e *Error) Error() string { return e.msg }
func (e *Error) Unwrap() error { return e.kind }

func NotFound(msg string) error     { return &Error{kind: ErrNotFound, msg: msg} }
func Forbidden(msg string) error    { return &Error{kind: ErrForbidden, msg: msg} }
func Conflict(msg string) error     { return &Error{kind: ErrConflict, msg: msg} }
func Invalid(msg string) error      { return &Error{kind: ErrValidation, msg: msg} }
func Unauthorized(msg string) error { return &Error{kind: ErrUnauthorized, msg: msg} }
