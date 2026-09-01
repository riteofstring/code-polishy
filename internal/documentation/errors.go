package versioneddocs

import (
	"errors"
	"fmt"
	"strings"
)

type ErrorKind string

const (
	ErrorInvalidCatalog ErrorKind = "invalid-catalog"
	ErrorUnavailable    ErrorKind = "unavailable"
	ErrorUnknown        ErrorKind = "unknown"
	ErrorAmbiguous      ErrorKind = "ambiguous"
	ErrorInvalidQuery   ErrorKind = "invalid-query"
)

type Error struct {
	Kind        ErrorKind
	Subject     string
	Suggestions []string
	Cause       error
}

func (failure *Error) Error() string {
	if failure == nil {
		return ""
	}
	prefix := string(failure.Kind)
	switch failure.Kind {
	case ErrorInvalidCatalog:
		prefix = "invalid documentation catalog"
	case ErrorUnavailable:
		prefix = "documentation unavailable"
	case ErrorUnknown:
		prefix = fmt.Sprintf("unknown documentation topic %q", failure.Subject)
	case ErrorAmbiguous:
		prefix = fmt.Sprintf("ambiguous documentation topic %q", failure.Subject)
	case ErrorInvalidQuery:
		prefix = "invalid documentation query"
	}
	if failure.Cause != nil {
		prefix += ": " + failure.Cause.Error()
	}
	if len(failure.Suggestions) == 1 {
		prefix += fmt.Sprintf("; did you mean %q?", failure.Suggestions[0])
	} else if len(failure.Suggestions) > 1 {
		prefix += "; valid matches: " + strings.Join(failure.Suggestions, ", ")
	}
	return prefix
}

func (failure *Error) Unwrap() error {
	if failure == nil {
		return nil
	}
	return failure.Cause
}

func IsKind(err error, kind ErrorKind) bool {
	var failure *Error
	return errors.As(err, &failure) && failure.Kind == kind
}

func failure(kind ErrorKind, subject string, cause error) error {
	return &Error{Kind: kind, Subject: subject, Cause: cause}
}
