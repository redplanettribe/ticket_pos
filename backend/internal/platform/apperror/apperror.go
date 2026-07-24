// Package apperror holds the domain-error contract shared by every domain
// module: the DomainError interface handlers map to HTTP, and the one
// implementation of it modules construct their typed errors from.
package apperror

// DomainError is implemented by typed service-layer errors with stable API codes.
type DomainError interface {
	error
	Code() string
	Message() string
	Details() any
}

// domainError is the single implementation of DomainError. It is unexported so
// the only way to build one is New, and so nothing can type-assert its way to a
// module-specific concrete type: the code string is the contract, and
// platform.WriteDomainError maps on that alone.
type domainError struct {
	code    string
	message string
	details any
}

func (e *domainError) Error() string   { return e.message }
func (e *domainError) Code() string    { return e.code }
func (e *domainError) Message() string { return e.message }
func (e *domainError) Details() any    { return e.details }

// New builds a typed domain error. Each domain module keeps ownership of its
// codes and messages in its own errors.go and calls this to construct them; the
// struct itself carries no domain knowledge and is not worth five copies.
func New(code, message string, details any) DomainError {
	return &domainError{code: code, message: message, details: details}
}
