package blobstore

import (
	"bytes"
	"context"
	"io"
	"path"
	"path/filepath"
	"strings"
	"testing"
	"testing/iotest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func FuzzValidateKey(f *testing.F) {
	for _, seed := range []string{"a", "a/b.c", "", "/a", "a/", "a//b", "..", "a/../b", "./a", `a\b`, "a\x00b", "\xff", "with space/ü"} {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, key string) {
		if err := ValidateKey(key); err != nil {
			require.ErrorIs(t, err, ErrInvalidKey)

			return
		}

		assert.True(t, filepath.IsLocal(key), "%q must stay inside the store root", key)
		assert.Equal(t, path.Clean(key), key, "%q must already be clean", key)
		assert.NotContains(t, key, `\`)
	})
}

func TestJoinAndCleanPrefix(t *testing.T) {
	assert.Equal(t, "k", joinPrefix("", "k"))
	assert.Equal(t, "p/k", joinPrefix("p", "k"))

	for in, want := range map[string]string{"": "", "/": "", "prod": "prod", "/prod/": "prod", "a/b/": "a/b"} {
		got, err := cleanPrefix(in)
		require.NoError(t, err, in)
		assert.Equal(t, want, got, in)
	}

	for _, bad := range []string{"a//b", "a/../b", "./a"} {
		_, err := cleanPrefix(bad)
		require.ErrorIs(t, err, ErrInvalidKey, bad)
	}
}

// probeStore wraps a Local store and breaks one step of the probe.
type probeStore struct {
	*Local

	putErr, getErr, readErr, deleteErr error
	corrupt                            bool
	deleted                            []string
}

func (p *probeStore) Put(ctx context.Context, key string, r io.Reader, size int64) error {
	if p.putErr != nil {
		return p.putErr
	}

	return p.Local.Put(ctx, key, r, size)
}

func (p *probeStore) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	switch {
	case p.getErr != nil:
		return nil, p.getErr
	case p.readErr != nil:
		return io.NopCloser(iotest.ErrReader(p.readErr)), nil
	case p.corrupt:
		return io.NopCloser(strings.NewReader("something else")), nil
	}

	return p.Local.Get(ctx, key)
}

func (p *probeStore) Delete(ctx context.Context, key string) error {
	p.deleted = append(p.deleted, key)

	if p.deleteErr != nil {
		return p.deleteErr
	}

	return p.Local.Delete(ctx, key)
}

func TestProbeReportsTheFailingStep(t *testing.T) {
	for name, tt := range map[string]struct {
		store       probeStore
		want        string
		cleansUp    bool
		leavesProbe bool
	}{
		"write":   {store: probeStore{putErr: assert.AnError}, want: "cannot write"},
		"read":    {store: probeStore{getErr: assert.AnError}, want: "cannot read it back", cleansUp: true},
		"stream":  {store: probeStore{readErr: assert.AnError}, want: "probe: reading", cleansUp: true},
		"corrupt": {store: probeStore{corrupt: true}, want: "differ from the", cleansUp: true},
		"delete":  {store: probeStore{deleteErr: assert.AnError}, want: "cannot delete it", cleansUp: true, leavesProbe: true},
	} {
		t.Run(name, func(t *testing.T) {
			local, err := NewLocal(t.TempDir())
			require.NoError(t, err)

			s := tt.store
			s.Local = local

			err = probe(t.Context(), &s)
			require.ErrorContains(t, err, tt.want)
			assert.True(t, strings.HasPrefix(err.Error(), "probe: "), err)

			if tt.cleansUp {
				require.Len(t, s.deleted, 1, "the probe object is deleted after a failure")
				assert.True(t, strings.HasPrefix(s.deleted[0], probeKeyPrefix))
			} else {
				assert.Empty(t, s.deleted)
			}

			if !s.corrupt {
				require.ErrorIs(t, err, assert.AnError, "the cause is wrapped")
			}

			objs, err := local.List(t.Context(), "")
			require.NoError(t, err)

			if tt.leavesProbe {
				assert.Len(t, objs, 1)
			} else {
				assert.Empty(t, objs, "no probe object is left behind")
			}
		})
	}
}

func TestProbeUsesAFreshKeyEachTime(t *testing.T) {
	local, err := NewLocal(t.TempDir())
	require.NoError(t, err)

	s := &probeStore{Local: local}
	require.NoError(t, probe(t.Context(), s))
	require.NoError(t, probe(t.Context(), s))
	require.Len(t, s.deleted, 2)
	assert.NotEqual(t, s.deleted[0], s.deleted[1])
}

func TestCtxReaderStopsOnCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	r := ctxReader{ctx: ctx, r: bytes.NewReader([]byte("abcdef"))}

	buf := make([]byte, 3)
	n, err := r.Read(buf)
	require.NoError(t, err)
	assert.Equal(t, "abc", string(buf[:n]))

	cancel()

	_, err = r.Read(buf)
	require.ErrorIs(t, err, context.Canceled)
}
