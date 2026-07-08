package apperror

// DomainError is implemented by typed service-layer errors with stable API codes.
type DomainError interface {
	error
	Code() string
	Message() string
	Details() any
}
