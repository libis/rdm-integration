// Author: Eryk Kulikowski @ KU Leuven (2026). Apache 2.0 License

package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strconv"

	"integration/app/core/reauth"
)

const maxBufferedErrorBody = 64 * 1024

// safeErrorResponses is the final response boundary for the API. Error bodies
// are replaced with a fixed message; the only exception is a validated Globus
// reauthorization challenge, which is reduced to its allow-listed fields.
func safeErrorResponses(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		wrapped := &errorResponseWriter{ResponseWriter: w}
		next.ServeHTTP(wrapped, r)
		wrapped.finish()
	})
}

type errorResponseWriter struct {
	http.ResponseWriter
	status    int
	errorBody bytes.Buffer
	tooLarge  bool
}

func (w *errorResponseWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}

func (w *errorResponseWriter) WriteHeader(status int) {
	if w.status != 0 {
		return
	}
	w.status = status
	if status < http.StatusBadRequest {
		w.ResponseWriter.WriteHeader(status)
	}
}

func (w *errorResponseWriter) Write(body []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
		w.ResponseWriter.WriteHeader(http.StatusOK)
	}
	if w.status < http.StatusBadRequest {
		return w.ResponseWriter.Write(body)
	}
	if w.errorBody.Len()+len(body) > maxBufferedErrorBody {
		w.tooLarge = true
		return len(body), nil
	}
	return w.errorBody.Write(body)
}

func (w *errorResponseWriter) finish() {
	if w.status == 0 {
		w.ResponseWriter.WriteHeader(http.StatusOK)
		return
	}
	if w.status < http.StatusBadRequest {
		return
	}

	body, contentType := safeErrorBody(w.status, w.errorBody.Bytes(), w.tooLarge)
	header := w.ResponseWriter.Header()
	header.Del("Content-Length")
	header.Set("Content-Type", contentType)
	w.ResponseWriter.WriteHeader(w.status)
	_, _ = w.ResponseWriter.Write(body)
}

func safeErrorBody(status int, body []byte, tooLarge bool) ([]byte, string) {
	if status == http.StatusUnauthorized && !tooLarge {
		var payload struct {
			Reauth *reauth.Error `json:"reauth"`
		}
		if json.Unmarshal(body, &payload) == nil {
			if safe := reauth.Sanitize(payload.Reauth); safe != nil &&
				(len(safe.RequiredScopes) > 0 || len(safe.RequiredDomains) > 0) {
				safeBody, err := json.Marshal(struct {
					Reauth *reauth.Error `json:"reauth"`
				}{Reauth: safe})
				if err == nil {
					return safeBody, "application/json"
				}
			}
		}
	}
	return []byte(strconv.Itoa(status) + " - request failed"), "text/plain; charset=utf-8"
}
