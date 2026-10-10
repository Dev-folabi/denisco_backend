package httpx

import (
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"strconv"
	"strings"

	"github.com/Dev-folabi/denisco_backend/internal/shared/apperror"
)

// Pagination defaults and bounds, enforced so clients cannot request
// unbounded result sets.
const (
	DefaultPage  = 1
	DefaultLimit = 20
	MaxLimit     = 100
)

// Decode reads a JSON body into dst, rejecting unknown fields and bodies that
// are empty, malformed or oversized. The body size itself is capped by the
// body-limit middleware.
func Decode(r *http.Request, dst any) error {
	if r.Body == nil {
		return apperror.BadRequest.WithDetails(map[string]any{"body": "a JSON body is required"})
	}

	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(dst); err != nil {
		var maxBytes *http.MaxBytesError
		if errors.As(err, &maxBytes) {
			return apperror.PayloadLarge.Wrap(err)
		}

		var syntaxErr *json.SyntaxError
		var typeErr *json.UnmarshalTypeError
		switch {
		case strings.HasPrefix(err.Error(), "json: unknown field "):
			field := strings.Trim(strings.TrimPrefix(err.Error(), "json: unknown field "), `"`)
			return apperror.BadRequest.WithDetails(map[string]any{field: "is not a recognised field"}).Wrap(err)
		case errors.As(err, &syntaxErr):
			return apperror.BadRequest.WithDetails(map[string]any{"body": "malformed JSON"}).Wrap(err)
		case errors.As(err, &typeErr):
			return apperror.Validation.WithDetails(map[string]any{typeErr.Field: "has the wrong type"}).Wrap(err)
		case errors.Is(err, http.ErrBodyReadAfterClose):
			return apperror.BadRequest.Wrap(err)
		default:
			return apperror.BadRequest.WithDetails(map[string]any{"body": "could not be parsed"}).Wrap(err)
		}
	}

	// A single JSON value per request; trailing content is a client bug.
	if decoder.More() {
		return apperror.BadRequest.WithDetails(map[string]any{"body": "must contain a single JSON object"})
	}

	return nil
}

// MaxSearchLength bounds a search term.
//
// Search runs as a case-insensitive regular expression over a collection, so
// the term's length is work the server does per document. The terms a customer
// or an administrator actually types are short; a long one is either a mistake
// or an attempt to make one request expensive.
const MaxSearchLength = 100

// SearchParam reads the `search` query parameter, trimmed and bounded.
func SearchParam(r *http.Request) string {
	search := strings.TrimSpace(r.URL.Query().Get("search"))
	if len(search) > MaxSearchLength {
		return search[:MaxSearchLength]
	}
	return search
}

// QueryInt reads an integer query parameter, falling back to a default.
func QueryInt(r *http.Request, key string, fallback int) int {
	raw := strings.TrimSpace(r.URL.Query().Get(key))
	if raw == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}
	return parsed
}

// PageParams reads and clamps the pagination query parameters.
func PageParams(r *http.Request) (page, limit int) {
	page = QueryInt(r, "page", DefaultPage)
	if page < 1 {
		page = DefaultPage
	}

	limit = QueryInt(r, "limit", DefaultLimit)
	switch {
	case limit < 1:
		limit = DefaultLimit
	case limit > MaxLimit:
		limit = MaxLimit
	}
	return page, limit
}

// clientIPHeader is the one header ClientIP will believe, set once at
// startup. Empty — the default — means no header is trusted at all.
//
// It is deliberately not a list, and the unset default is deliberately the
// strict one. Anyone can send X-Forwarded-For, and Cloudflare appends the real
// address to whatever the client sent rather than replacing it, so trusting
// that header by default would let a caller choose which bucket the rate
// limiter counts it in — or whose address appears in the audit log.
var clientIPHeader string

// TrustClientIPHeader records the header the deployment's proxy sets with the
// real client address: `CF-Connecting-IP` behind Cloudflare, `X-Real-IP`
// behind a proxy that sets it from the connection it accepted. It is called
// once during startup, before the server begins serving.
//
// The header is only worth trusting when nothing can reach the API except the
// proxy — which is what binding the API to the internal network does.
func TrustClientIPHeader(header string) {
	clientIPHeader = http.CanonicalHeaderKey(strings.TrimSpace(header))
}

// ClientIP returns the address rate limiting and the audit log attribute a
// request to.
//
// With no trusted header configured it is the peer address, which cannot be
// forged. With one configured, that header wins when present, and the peer
// address remains the fallback for a request that arrived without it.
func ClientIP(r *http.Request) string {
	if clientIPHeader != "" {
		forwarded := strings.TrimSpace(r.Header.Get(clientIPHeader))

		// Only a single-address header is trusted. In a chain like
		// X-Forwarded-For, which entry is the client depends on how many
		// hops appended to it, and guessing wrong is how a forged entry gets
		// believed — so the configured header must be one the proxy writes
		// itself (`CF-Connecting-IP`, `X-Real-IP`), and a chain falls through
		// to the peer address instead.
		if forwarded != "" && !strings.Contains(forwarded, ",") {
			return forwarded
		}
	}

	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
