package provider

import (
	"context"
	"errors"
)

type ErrorCode string

const (
	Unsupported  ErrorCode = "unsupported"
	NeedsSignIn  ErrorCode = "signIn"
	Denied       ErrorCode = "permission"
	Expired      ErrorCode = "expired"
	Network      ErrorCode = "network"
	RateLimited  ErrorCode = "rateLimited"
	Canceled     ErrorCode = "canceled"
	Timeout      ErrorCode = "timeout"
	PluginFailed ErrorCode = "plugin"
)

type Error struct {
	Code    ErrorCode
	Message string
	Cause   error
}

func (e *Error) Error() string { return "source request: " + string(e.Code) }
func (e *Error) Unwrap() error { return e.Cause }

func MessageOf(err error) string {
	var failure *Error
	if errors.As(err, &failure) {
		return failure.Message
	}
	return ""
}

func CodeOf(err error) ErrorCode {
	switch {
	case errors.Is(err, context.Canceled):
		return Canceled
	case errors.Is(err, context.DeadlineExceeded):
		return Timeout
	}
	var failure *Error
	if errors.As(err, &failure) {
		return failure.Code
	}
	return PluginFailed
}
