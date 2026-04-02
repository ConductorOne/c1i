package config

import "fmt"

func BaseURL(tenant string) string {
	return fmt.Sprintf("https://%s.conductor.one", tenant)
}

func KeychainService(tenant string) string {
	return fmt.Sprintf("c1i/%s", tenant)
}
