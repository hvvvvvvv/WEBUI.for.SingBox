package runtime

import (
	"errors"
	"net/url"
)

type redactedRequestError struct {
	cause   error
	message string
}

func (e *redactedRequestError) Error() string { return e.message }
func (e *redactedRequestError) Unwrap() error { return e.cause }

func redactRequestURL(err error) error {
	var request *url.Error
	if !errors.As(err, &request) {
		return err
	}
	copy := *request
	copy.URL = "[redacted URL]"
	if parsed, parseErr := url.Parse(request.URL); parseErr == nil && parsed.Host != "" {
		// Userinfo, paths and queries may all carry subscription credentials.
		copy.URL = (&url.URL{Scheme: parsed.Scheme, Host: parsed.Host}).String()
	}
	copy.Err = redactRequestURL(request.Err)
	return &redactedRequestError{cause: err, message: copy.Error()}
}
