// Package transport exposes the auth module over HTTP: request decoding,
// cookie handling and route registration. It contains no business rules.
package transport

import (
	"net/http"
	"time"

	"github.com/Dev-folabi/denisco_backend/internal/platform/config"
)

// cookieManager issues and clears the refresh-token cookie.
//
// The refresh token is kept out of JavaScript's reach (HttpOnly) and scoped to
// the auth path, so it is never attached to ordinary API calls. SameSite plus
// the CSRF origin guard protects the cookie-authenticated endpoints.
type cookieManager struct {
	cfg config.AuthConfig
}

// newCookieManager returns a manager bound to the cookie configuration.
func newCookieManager(cfg config.AuthConfig) *cookieManager {
	return &cookieManager{cfg: cfg}
}

// set writes the refresh cookie with the session's expiry.
func (c *cookieManager) set(w http.ResponseWriter, token string, expiresAt time.Time) {
	http.SetCookie(w, &http.Cookie{
		Name:     c.cfg.RefreshCookieName,
		Value:    token,
		Path:     c.cfg.RefreshCookiePath,
		Domain:   c.cfg.CookieDomain,
		Expires:  expiresAt,
		MaxAge:   int(time.Until(expiresAt).Seconds()),
		Secure:   c.cfg.CookieSecure,
		HttpOnly: true,
		SameSite: c.cfg.CookieSameSite,
	})
}

// clear expires the refresh cookie on logout.
func (c *cookieManager) clear(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     c.cfg.RefreshCookieName,
		Value:    "",
		Path:     c.cfg.RefreshCookiePath,
		Domain:   c.cfg.CookieDomain,
		Expires:  time.Unix(0, 0),
		MaxAge:   -1,
		Secure:   c.cfg.CookieSecure,
		HttpOnly: true,
		SameSite: c.cfg.CookieSameSite,
	})
}

// read returns the refresh token from the request cookie.
func (c *cookieManager) read(r *http.Request) string {
	cookie, err := r.Cookie(c.cfg.RefreshCookieName)
	if err != nil {
		return ""
	}
	return cookie.Value
}
