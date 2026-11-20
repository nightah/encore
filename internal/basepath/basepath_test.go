package basepath

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestClean(t *testing.T) {
	testCases := []struct {
		name     string
		have     string
		expected string
	}{
		{name: "ShouldCleanEmptyToRoot", have: "", expected: "/"},
		{name: "ShouldKeepRoot", have: "/", expected: "/"},
		{name: "ShouldAddTrailingSlash", have: "/encore", expected: "/encore/"},
		{name: "ShouldAddLeadingSlash", have: "encore/", expected: "/encore/"},
		{name: "ShouldKeepCleanNestedPath", have: "/media/encore/", expected: "/media/encore/"},
		{name: "ShouldCollapseRepeatedSlashes", have: "//encore//", expected: "/encore/"},
		{name: "ShouldCollapseSlashesToRoot", have: "///", expected: "/"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, Clean(tc.have))
		})
	}
}

func TestContext(t *testing.T) {
	assert.Equal(t, "/", From(t.Context()))
	assert.Equal(t, "/encore/", From(With(t.Context(), "/encore")))
}
