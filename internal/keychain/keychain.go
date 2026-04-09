package keychain

import (
	"fmt"

	"github.com/zalando/go-keyring"
)

const (
	acctClientID  = "client_id"
	acctClientSec = "client_secret"
)

func Store(service, clientID, clientSecret string) error {
	_ = Delete(service)
	if err := keyring.Set(service, acctClientID, clientID); err != nil {
		return fmt.Errorf("storing client_id: %w", err)
	}
	if err := keyring.Set(service, acctClientSec, clientSecret); err != nil {
		return fmt.Errorf("storing client_secret: %w", err)
	}
	return nil
}

func Load(service string) (string, string, error) {
	clientID, err := keyring.Get(service, acctClientID)
	if err != nil {
		return "", "", fmt.Errorf("loading client_id: %w", err)
	}
	clientSecret, err := keyring.Get(service, acctClientSec)
	if err != nil {
		return "", "", fmt.Errorf("loading client_secret: %w", err)
	}
	return clientID, clientSecret, nil
}

func Delete(service string) error {
	_ = keyring.Delete(service, acctClientID)
	_ = keyring.Delete(service, acctClientSec)
	return nil
}
