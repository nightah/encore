package blobstore

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"slices"
	"strings"

	"github.com/studio-b12/gowebdav"

	"github.com/nightah/encore/internal/config"
)

var _ Store = (*WebDAV)(nil)

// NewWebDAV returns a store for cfg. It does not contact the server; use Probe for that.
func NewWebDAV(cfg config.StorageWebDAV) (*WebDAV, error) {
	u, err := url.Parse(cfg.URL)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return nil, errors.New("webdav: url must be an http or https URL")
	}

	if u.User != nil {
		return nil, errors.New("webdav: put credentials in username and password, not the URL")
	}

	prefix, err := cleanPrefix(cfg.Prefix)
	if err != nil {
		return nil, fmt.Errorf("webdav: %w", err)
	}

	transport, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		return nil, errors.New("webdav: unexpected default transport")
	}

	transport = transport.Clone()

	if u.Scheme == "https" {
		if transport.TLSClientConfig, err = tlsConfig(cfg.CABundle, cfg.InsecureSkipVerify); err != nil {
			return nil, fmt.Errorf("webdav: %w", err)
		}
	}

	w := &WebDAV{url: strings.TrimSuffix(cfg.URL, "/") + "/", prefix: prefix, transport: transport}

	if cfg.Username != "" || cfg.Password != "" {
		w.auth = "Basic " + base64.StdEncoding.EncodeToString([]byte(cfg.Username+":"+cfg.Password))
	}

	return w, nil
}

// client returns a gowebdav client whose requests are bound to ctx. gowebdav has no context support, so each operation gets its own lightweight client with a transport that attaches ctx to every request it sends.
func (w *WebDAV) client(ctx context.Context) *gowebdav.Client {
	c := gowebdav.NewAuthClient(w.url, gowebdav.NewEmptyAuth())
	c.SetTransport(ctxTransport{ctx: ctx, base: w.transport})

	if w.auth != "" {
		c.SetHeader("Authorization", w.auth)
	}

	return c
}

func (t ctxTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	return t.base.RoundTrip(r.WithContext(t.ctx))
}

func (w *WebDAV) remotePath(key string) string { return "/" + joinPrefix(w.prefix, key) }

// Put implements Store. An unknown size is spooled to a temporary file first, because many WebDAV servers reject chunked uploads.
func (w *WebDAV) Put(ctx context.Context, key string, r io.Reader, size int64) error {
	if err := ValidateKey(key); err != nil {
		return err
	}

	if size < 0 {
		f, n, err := spool(ctx, r)
		if err != nil {
			return fmt.Errorf("uploading %s: %w", key, err)
		}

		defer func() {
			_ = f.Close()
			_ = os.Remove(f.Name())
		}()

		r, size = f, n
	}

	if err := w.client(ctx).WriteStreamWithLength(w.remotePath(key), ctxReader{ctx, r}, size, 0); err != nil {
		return fmt.Errorf("uploading %s: %w", key, describeWebDAV(ctx, err))
	}

	return nil
}

func spool(ctx context.Context, r io.Reader) (*os.File, int64, error) {
	f, err := os.CreateTemp("", "encore-upload-")
	if err != nil {
		return nil, 0, fmt.Errorf("spooling: %w", err)
	}

	n, err := io.Copy(f, ctxReader{ctx, r})
	if err == nil {
		_, err = f.Seek(0, io.SeekStart)
	}

	if err != nil {
		_ = f.Close()
		_ = os.Remove(f.Name())

		return nil, 0, fmt.Errorf("spooling: %w", err)
	}

	return f, n, nil
}

// Get implements Store.
func (w *WebDAV) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	if err := ValidateKey(key); err != nil {
		return nil, err
	}

	rc, err := w.client(ctx).ReadStream(w.remotePath(key))
	if err != nil {
		// 405 is the answer to GET on a collection, which is not an object.
		if gowebdav.IsErrNotFound(err) || gowebdav.IsErrCode(err, http.StatusMethodNotAllowed) {
			return nil, fmt.Errorf("%w: %s", ErrNotFound, key)
		}

		return nil, fmt.Errorf("downloading %s: %w", key, describeWebDAV(ctx, err))
	}

	return rc, nil
}

// Delete implements Store.
func (w *WebDAV) Delete(ctx context.Context, key string) error {
	if err := ValidateKey(key); err != nil {
		return err
	}

	if err := w.client(ctx).Remove(w.remotePath(key)); err != nil && !gowebdav.IsErrNotFound(err) {
		return fmt.Errorf("deleting %s: %w", key, describeWebDAV(ctx, err))
	}

	return nil
}

// List implements Store.
func (w *WebDAV) List(ctx context.Context, prefix string) ([]Object, error) {
	if err := validatePrefix(prefix); err != nil {
		return nil, err
	}

	c := w.client(ctx)

	start := ""
	if i := strings.LastIndexByte(prefix, '/'); i >= 0 {
		start = prefix[:i+1]
	}

	var out []Object

	if err := w.walk(ctx, c, start, prefix, &out); err != nil {
		return nil, fmt.Errorf("listing %q: %w", prefix, err)
	}

	slices.SortFunc(out, func(a, b Object) int { return strings.Compare(a.Key, b.Key) })

	return out, nil
}

// walk lists dir (a key prefix ending in "/", or "" for the root) and recurses into collections that could hold keys starting with prefix.
func (w *WebDAV) walk(ctx context.Context, c *gowebdav.Client, dir, prefix string, out *[]Object) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	entries, err := c.ReadDir(w.remotePath(dir))
	if err != nil {
		if gowebdav.IsErrNotFound(err) {
			return nil
		}

		return describeWebDAV(ctx, err)
	}

	for _, e := range entries {
		key := dir + e.Name()

		if e.IsDir() {
			sub := key + "/"
			if strings.HasPrefix(sub, prefix) || strings.HasPrefix(prefix, sub) {
				if err := w.walk(ctx, c, sub, prefix, out); err != nil {
					return err
				}
			}

			continue
		}

		if strings.HasPrefix(key, prefix) {
			*out = append(*out, Object{Key: key, Size: e.Size(), Modified: e.ModTime().UTC()})
		}
	}

	return nil
}

// Probe implements Store.
func (w *WebDAV) Probe(ctx context.Context) error {
	return probe(ctx, w)
}

// describeWebDAV turns gowebdav's bare status codes into something a user can act on.
func describeWebDAV(ctx context.Context, err error) error {
	if ctxErr := ctx.Err(); ctxErr != nil {
		return fmt.Errorf("%w: %w", ctxErr, err)
	}

	switch {
	case gowebdav.IsErrCode(err, http.StatusUnauthorized):
		return fmt.Errorf("%w (401: check the username and app password)", err)
	case gowebdav.IsErrCode(err, http.StatusForbidden):
		return fmt.Errorf("%w (403: the account cannot write here)", err)
	case gowebdav.IsErrCode(err, http.StatusMethodNotAllowed):
		return fmt.Errorf("%w (405: the URL may not be a WebDAV collection)", err)
	case gowebdav.IsErrCode(err, http.StatusInsufficientStorage):
		return fmt.Errorf("%w (507: the server is out of quota)", err)
	default:
		return err
	}
}
