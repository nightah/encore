package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAlexaVoiceOptions(t *testing.T) {
	cfg, err := Load(Options{Environ: environ()})
	require.NoError(t, err)
	assert.False(t, cfg.Alexa.ShutdownQuits)
	assert.Equal(t, DefaultQueueLimit, cfg.Alexa.QueueLimit)
	assert.True(t, cfg.Alexa.AutoDeploy, "skill changes deploy by themselves unless turned off")
	assert.InDelta(t, DefaultAutoDeployMinutes, cfg.Alexa.AutoDeployMinutes, 0)

	cfg, err = Load(Options{Environ: environ("ENCORE_ALEXA_SHUTDOWN_QUITS=true", "ENCORE_ALEXA_QUEUE_LIMIT=250", "ENCORE_ALEXA_AUTO_DEPLOY=false", "ENCORE_ALEXA_AUTO_DEPLOY_MINUTES=2.5")})
	require.NoError(t, err)
	assert.True(t, cfg.Alexa.ShutdownQuits)
	assert.Equal(t, 250, cfg.Alexa.QueueLimit)
	assert.False(t, cfg.Alexa.AutoDeploy)
	assert.InDelta(t, 2.5, cfg.Alexa.AutoDeployMinutes, 0)

	_, err = Load(Options{Environ: environ("ENCORE_ALEXA_QUEUE_LIMIT=0")})
	require.ErrorContains(t, err, "alexa.queue_limit")

	for _, n := range []string{"0", "0.5", "1441"} {
		_, err = Load(Options{Environ: environ("ENCORE_ALEXA_AUTO_DEPLOY_MINUTES=" + n)})
		require.ErrorContains(t, err, "alexa.auto_deploy_minutes", n)
	}
}

func TestKodiShutdownQuitsOverride(t *testing.T) {
	path := filepath.Join(t.TempDir(), "encore.yml")
	require.NoError(t, os.WriteFile(path, []byte(`
alexa:
  shutdown_quits: true
kodi:
  - name: Lounge
    url: http://kodi:8080/jsonrpc
    shutdown_quits: false
  - name: Study
    url: http://study:8080/jsonrpc
`), 0o600))

	cfg, err := Load(Options{File: path, Environ: environ()})
	require.NoError(t, err)
	require.Len(t, cfg.Kodi, 2)
	require.NotNil(t, cfg.Kodi[0].ShutdownQuits)
	assert.False(t, *cfg.Kodi[0].ShutdownQuits)
	assert.Nil(t, cfg.Kodi[1].ShutdownQuits, "unset means use alexa.shutdown_quits")
}
