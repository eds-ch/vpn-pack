package service

// Error types — protocol-agnostic, no net/http.

type ErrorKind int

const (
	ErrValidation ErrorKind = iota
	ErrUpstream
	ErrInternal
	ErrPrecondition
	ErrNotFound
	ErrUnavailable
)

type Error struct {
	Kind    ErrorKind
	Message string
	Cause   error
}

func (e *Error) Error() string { return e.Message }

func (e *Error) Unwrap() error { return e.Cause }

func validationError(msg string) *Error {
	return &Error{Kind: ErrValidation, Message: msg}
}

func upstreamError(msg string, cause error) *Error {
	return &Error{Kind: ErrUpstream, Message: msg, Cause: cause}
}

func internalError(msg string, cause ...error) *Error {
	e := &Error{Kind: ErrInternal, Message: msg}
	if len(cause) > 0 {
		e.Cause = cause[0]
	}
	return e
}

func preconditionError(msg string) *Error {
	return &Error{Kind: ErrPrecondition, Message: msg}
}

func notFoundError(msg string) *Error {
	return &Error{Kind: ErrNotFound, Message: msg}
}

const ErrMsgIntegrationKeyRequired = "Integration API key required before activating Tailscale"
