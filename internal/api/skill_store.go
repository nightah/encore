package api

import (
	"context"
	"encoding/json"
	"errors"

	"golang.org/x/oauth2"

	"github.com/nightah/encore/internal/secrets"
	"github.com/nightah/encore/internal/smapi/lwa"
)

var _ lwa.TokenStore = secretTokenStore{}

// Load implements lwa.TokenStore.
func (s secretTokenStore) Load(ctx context.Context) (*oauth2.Token, error) {
	b, err := s.sec.Get(ctx, lwaTokenRef)
	if errors.Is(err, secrets.ErrNotFound) {
		return nil, nil //nolint:nilnil // documented TokenStore contract: nothing stored is not an error
	}

	if err != nil {
		return nil, err
	}
	defer clear(b)

	var tok oauth2.Token
	if err := json.Unmarshal(b, &tok); err != nil {
		return nil, err
	}

	return &tok, nil
}

// Save implements lwa.TokenStore.
func (s secretTokenStore) Save(ctx context.Context, tok *oauth2.Token) error {
	b, err := json.Marshal(tok) //nolint:gosec // serialised only to be encrypted by internal/secrets
	if err != nil {
		return err
	}
	defer clear(b)

	return s.sec.Put(ctx, lwaTokenRef, b)
}

func (s secretTokenStore) forget(ctx context.Context) error {
	return s.sec.Delete(ctx, lwaTokenRef)
}
