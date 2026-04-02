package keychain

import (
	"fmt"
	"os/exec"
	"strings"
)

const (
	acctClientID  = "client_id"
	acctClientSec = "client_secret"
)

func Store(service, clientID, clientSecret string) error {
	_ = Delete(service)

	if err := setItem(service, acctClientID, clientID); err != nil {
		return fmt.Errorf("storing client_id: %w", err)
	}
	if err := setItem(service, acctClientSec, clientSecret); err != nil {
		return fmt.Errorf("storing client_secret: %w", err)
	}
	return nil
}

func Load(service string) (clientID, clientSecret string, err error) {
	clientID, err = getItem(service, acctClientID)
	if err != nil {
		return "", "", fmt.Errorf("loading client_id: %w", err)
	}
	clientSecret, err = getItem(service, acctClientSec)
	if err != nil {
		return "", "", fmt.Errorf("loading client_secret: %w", err)
	}
	return clientID, clientSecret, nil
}

func Delete(service string) error {
	_ = deleteItem(service, acctClientID)
	_ = deleteItem(service, acctClientSec)
	return nil
}

func setItem(service, account, value string) error {
	cmd := exec.Command("/usr/bin/security", "add-generic-password",
		"-s", service,
		"-a", account,
		"-w", value,
		"-U",
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("security add-generic-password: %s: %w", strings.TrimSpace(string(out)), err)
	}
	return nil
}

func getItem(service, account string) (string, error) {
	cmd := exec.Command("/usr/bin/security", "find-generic-password",
		"-s", service,
		"-a", account,
		"-w",
	)
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("no keychain entry for %s/%s", service, account)
	}
	return strings.TrimSpace(string(out)), nil
}

func deleteItem(service, account string) error {
	cmd := exec.Command("/usr/bin/security", "delete-generic-password",
		"-s", service,
		"-a", account,
	)
	_ = cmd.Run()
	return nil
}
