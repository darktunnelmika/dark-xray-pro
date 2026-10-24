package xrayconfig

import (
	"errors"

	"github.com/darktunnelmika/dark-xray-pro/internal/subscriptioncodec"
)

// The server profile and client codec deliberately share one typed grammar.
// Client options remain metadata: profileStream never emits them to a server.
type XHTTPOptions = subscriptioncodec.XHTTPOptions
type ClientOptions = subscriptioncodec.ClientOptions

const MaxConnectionPathBytes = subscriptioncodec.MaxConnectionPathBytes
const MaxXHTTPPaddingBytes = subscriptioncodec.MaxXHTTPPaddingBytes

// ProfileValidationError identifies a public field without exposing a value,
// private key, user credential or raw core error.
type ProfileValidationError struct {
	Field string
	Code  string
}

func (e *ProfileValidationError) Error() string { return e.Code }
func (e *ProfileValidationError) Unwrap() error { return ErrInvalid }

func connectionOptionError(section string, err error) error {
	var detail *subscriptioncodec.OptionError
	if errors.As(err, &detail) {
		return &ProfileValidationError{Field: "options." + section + "." + detail.Field, Code: detail.Code}
	}
	return ErrInvalid
}

func NormalizeXHTTPOptions(options *XHTTPOptions) (*XHTTPOptions, error) {
	o, err := subscriptioncodec.NormalizeXHTTPOptions(options)
	if err != nil {
		return nil, connectionOptionError("xhttp", err)
	}
	return o, nil
}

func NormalizeClientOptions(options *ClientOptions) (*ClientOptions, error) {
	o, err := subscriptioncodec.NormalizeClientOptions(options)
	if err != nil {
		return nil, connectionOptionError("client", err)
	}
	return o, nil
}

type xhttpSettings struct {
	Path          string `json:"path,omitempty"`
	Host          string `json:"host,omitempty"`
	Mode          string `json:"mode,omitempty"`
	XPaddingBytes string `json:"xPaddingBytes,omitempty"`
}
