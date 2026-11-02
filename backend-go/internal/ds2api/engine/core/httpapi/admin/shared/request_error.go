package shared

import "errors"

type requestError struct {
	detail string
}

func (e *requestError) Error() string {
	return e.detail
}

func newRequestError(detail string) error {
	return &requestError{detail: detail}
}

func NewRequestError(detail string) error {
	return newRequestError(detail)
}

func requestErrorDetail(err error) (string, bool) {
	var reqErr *requestError
	if errors.As(err, &reqErr) {
		return reqErr.detail, true
	}
	return "", false
}

func RequestErrorDetail(err error) (string, bool) {
	return requestErrorDetail(err)
}
