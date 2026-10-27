package basepath

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestClean(t *testing.T) {
	for in, want := range map[string]string{"": "/", "/": "/", "/encore": "/encore/", "encore/": "/encore/", "/media/encore/": "/media/encore/"} {
		assert.Equal(t, want, Clean(in), in)
	}
}

func TestContext(t *testing.T) {
	assert.Equal(t, "/", From(t.Context()))
	assert.Equal(t, "/encore/", From(With(t.Context(), "/encore")))
}
