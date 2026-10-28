package auth

import (
	"bytes"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExportedTOTP(t *testing.T) {
	secret, uri, err := NewTOTPKey("admin", bytes.NewReader([]byte("0123456789abcdefghij")))
	require.NoError(t, err)
	assert.Equal(t, "GAYTEMZUGU3DOOBZMFRGGZDFMZTWQ2LK", secret)
	assert.Equal(t, "otpauth://totp/Encore:admin?algorithm=SHA1&digits=6&issuer=Encore&period=30&secret=GAYTEMZUGU3DOOBZMFRGGZDFMZTWQ2LK", uri)

	now := time.Unix(1111111111, 0)

	code, err := TOTPCode(rfcSecret, now)
	require.NoError(t, err)
	assert.Equal(t, "050471", code)

	step, ok := TOTPStep(rfcSecret, code, now)
	assert.True(t, ok)
	assert.Equal(t, now.Unix()/totpPeriod, step)

	_, ok = TOTPStep(rfcSecret, "000000", now)
	assert.False(t, ok)
}
