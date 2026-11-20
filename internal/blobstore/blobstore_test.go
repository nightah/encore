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

	testCases := []struct {
		name     string
		have     string
		expected string
		err      error
	}{
		{name: "ShouldKeepEmpty", have: "", expected: ""},
		{name: "ShouldCleanTheRootToEmpty", have: "/", expected: ""},
		{name: "ShouldKeepAPlainPrefix", have: "prod", expected: "prod"},
		{name: "ShouldTrimSlashes", have: "/prod/", expected: "prod"},
		{name: "ShouldTrimATrailingSlashFromANestedPrefix", have: "a/b/", expected: "a/b"},
		{name: "ShouldRejectAnEmptySegment", have: "a//b", err: ErrInvalidKey},
		{name: "ShouldRejectAParentSegment", have: "a/../b", err: ErrInvalidKey},
		{name: "ShouldRejectACurrentSegment", have: "./a", err: ErrInvalidKey},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			actual, err := cleanPrefix(tc.have)

			if tc.err != nil {
				require.ErrorIs(t, err, tc.err)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tc.expected, actual)
		})
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
	testCases := []struct {
		name        string
		store       probeStore
		err         string
		cleansUp    bool
		leavesProbe bool
	}{
		{name: "ShouldReportAFailedWrite", store: probeStore{putErr: assert.AnError}, err: "cannot write"},
		{name: "ShouldReportAFailedRead", store: probeStore{getErr: assert.AnError}, err: "cannot read it back", cleansUp: true},
		{name: "ShouldReportAFailedStream", store: probeStore{readErr: assert.AnError}, err: "probe: reading", cleansUp: true},
		{name: "ShouldReportACorruptReadBack", store: probeStore{corrupt: true}, err: "differ from the", cleansUp: true},
		{name: "ShouldReportAFailedDelete", store: probeStore{deleteErr: assert.AnError}, err: "cannot delete it", cleansUp: true, leavesProbe: true},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			local, err := NewLocal(t.TempDir())
			require.NoError(t, err)

			s := tc.store
			s.Local = local

			err = probe(t.Context(), &s)
			require.ErrorContains(t, err, tc.err)
			assert.True(t, strings.HasPrefix(err.Error(), "probe: "), err)

			if tc.cleansUp {
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

			if tc.leavesProbe {
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
