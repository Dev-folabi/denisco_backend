// Package storage wraps ImageKit, the media host for product images.
//
// Uploads never pass through the API: the admin asks for a short-lived
// signature, uploads straight to ImageKit, and sends back the resulting file
// metadata. That keeps large image bodies off a 2 GB VPS and keeps the private
// key on the server.
package storage

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Dev-folabi/denisco_backend/internal/platform/config"
	"github.com/Dev-folabi/denisco_backend/internal/shared/apperror"
)

// signatureTTL is how long an upload signature stays valid. ImageKit caps the
// expiry at one hour ahead; a short window limits how long a leaked signature
// is useful.
const signatureTTL = 10 * time.Minute

// ErrNotConfigured is returned when ImageKit credentials are absent.
var ErrNotConfigured = apperror.New(http.StatusServiceUnavailable, "MEDIA_UPLOAD_UNAVAILABLE", "Image uploads are not configured.")

// deleteTimeout bounds a media-deletion call. It runs while an admin waits on
// a product delete, so it fails fast rather than holding the request open.
const deleteTimeout = 10 * time.Second

// managementAPI is ImageKit's file-management base URL.
const managementAPI = "https://api.imagekit.io/v1/files"

// ImageKit issues upload signatures, validates the metadata that comes back
// and deletes files whose product has been removed.
type ImageKit struct {
	publicKey   string
	privateKey  string
	urlEndpoint string
	client      *http.Client
}

// NewImageKit builds the client from configuration. Missing credentials are
// not fatal: the API still runs, and only the upload endpoint reports that the
// integration is unavailable.
func NewImageKit(cfg config.ImageKitConfig) *ImageKit {
	return &ImageKit{
		publicKey:   cfg.PublicKey,
		privateKey:  cfg.PrivateKey,
		urlEndpoint: strings.TrimRight(cfg.URLEndpoint, "/"),
		client:      &http.Client{Timeout: deleteTimeout},
	}
}

// Configured reports whether uploads can be signed.
func (k *ImageKit) Configured() bool {
	return k.publicKey != "" && k.privateKey != "" && k.urlEndpoint != ""
}

// UploadSignature is the credential set the admin client sends to ImageKit.
type UploadSignature struct {
	Token       string `json:"token"`
	Expire      int64  `json:"expire"`
	Signature   string `json:"signature"`
	PublicKey   string `json:"public_key"`
	URLEndpoint string `json:"url_endpoint"`
	UploadURL   string `json:"upload_url"`
	Folder      string `json:"folder"`
}

// NewUploadSignature issues a short-lived signature for a direct upload.
//
// ImageKit's scheme: the signature is HMAC-SHA1 of (token + expiry) keyed with
// the private key, which therefore never leaves the server.
func (k *ImageKit) NewUploadSignature(folder string) (*UploadSignature, error) {
	if !k.Configured() {
		return nil, ErrNotConfigured
	}

	token, err := randomToken()
	if err != nil {
		return nil, apperror.Internal.Wrap(err)
	}

	expire := time.Now().Add(signatureTTL).Unix()

	mac := hmac.New(sha1.New, []byte(k.privateKey))
	mac.Write([]byte(token + fmt.Sprint(expire)))

	return &UploadSignature{
		Token:       token,
		Expire:      expire,
		Signature:   hex.EncodeToString(mac.Sum(nil)),
		PublicKey:   k.publicKey,
		URLEndpoint: k.urlEndpoint,
		UploadURL:   "https://upload.imagekit.io/api/v1/files/upload",
		Folder:      folder,
	}, nil
}

// ValidateURL checks that an image URL actually points at this account's
// ImageKit endpoint, so a product cannot be made to reference an arbitrary
// host through the admin API.
func (k *ImageKit) ValidateURL(url string) error {
	trimmed := strings.TrimSpace(url)
	if trimmed == "" {
		return apperror.Validation.WithDetails(map[string]any{"images": "an image URL is required"})
	}

	// Without a configured endpoint there is nothing to check against, so any
	// https URL is accepted and the deployment is responsible for the rest.
	if k.urlEndpoint == "" {
		if !strings.HasPrefix(trimmed, "https://") {
			return apperror.Validation.WithDetails(map[string]any{"images": "must be served over HTTPS"})
		}
		return nil
	}

	if !strings.HasPrefix(trimmed, k.urlEndpoint+"/") {
		return apperror.Validation.WithDetails(map[string]any{
			"images": "must be an image uploaded to the configured media library",
		})
	}

	return nil
}

// DeleteFile removes a file from the media library by its ImageKit file id.
//
// A file that is already gone is not an error: deleting a product must not
// fail because its image was tidied up by hand first. Nor is a missing
// configuration — a deployment without ImageKit credentials still deletes
// products, it just has no media to clean up.
func (k *ImageKit) DeleteFile(ctx context.Context, fileID string) error {
	id := strings.TrimSpace(fileID)
	if id == "" || !k.Configured() {
		return nil
	}

	ctx, cancel := context.WithTimeout(ctx, deleteTimeout)
	defer cancel()

	request, err := http.NewRequestWithContext(ctx, http.MethodDelete, managementAPI+"/"+url.PathEscape(id), nil)
	if err != nil {
		return apperror.Internal.Wrap(err)
	}

	// ImageKit authenticates management calls with the private key as the
	// HTTP Basic username and an empty password.
	request.SetBasicAuth(k.privateKey, "")

	response, err := k.client.Do(request)
	if err != nil {
		return apperror.New(http.StatusBadGateway, "MEDIA_DELETE_FAILED",
			"The product image could not be removed from the media library.").Wrap(err)
	}
	defer func() {
		_, _ = io.Copy(io.Discard, response.Body)
		_ = response.Body.Close()
	}()

	switch {
	case response.StatusCode == http.StatusNoContent || response.StatusCode == http.StatusOK:
		return nil
	case response.StatusCode == http.StatusNotFound:
		// Already deleted.
		return nil
	default:
		body, _ := io.ReadAll(io.LimitReader(response.Body, 512))
		return apperror.New(http.StatusBadGateway, "MEDIA_DELETE_FAILED",
			"The product image could not be removed from the media library.").
			Wrap(fmt.Errorf("storage: imagekit delete returned %d: %s", response.StatusCode, strings.TrimSpace(string(body))))
	}
}

// randomToken returns the unique token that ties a signature to one upload.
func randomToken() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("storage: generate token: %w", err)
	}
	return hex.EncodeToString(buf), nil
}
