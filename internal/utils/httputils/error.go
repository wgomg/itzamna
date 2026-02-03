package httputils

import "net/http"

type HTTPError struct {
	Code    int
	Message string
}

func (e *HTTPError) Error() string {
	return e.Message
}

func HandleError(w http.ResponseWriter, err error) {
	if httpErr, ok := err.(*HTTPError); ok {
		JSONError(w, httpErr.Code, httpErr.Message)
	} else {
		JSONError(w, http.StatusInternalServerError, "Internal server error")
	}
}
