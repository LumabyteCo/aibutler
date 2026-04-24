package vault

import (
	"context"
	"fmt"
	"os"
	"strings"
)

// envVault implements Vault using environment variables.
// For CI/container use ONLY. Security is degraded.
type envVault struct {
	prefix string // e.g., "AIBUTLER_"
}

func newEnvVault() *envVault {
	return &envVault{prefix: "AIBUTLER_"}
}

func (v *envVault) Store(_ context.Context, cred Credential) error {
	key := v.envKey(cred.Key)
	return os.Setenv(key, string(cred.Value))
}

func (v *envVault) Get(_ context.Context, key string) (Credential, error) {
	envKey := v.envKey(key)
	val, ok := os.LookupEnv(envKey)
	if !ok {
		return Credential{}, ErrNotFound
	}
	return Credential{
		Key:   key,
		Type:  CredAPIKey,
		Value: []byte(val),
	}, nil
}

func (v *envVault) Delete(_ context.Context, key string) error {
	return os.Unsetenv(v.envKey(key))
}

func (v *envVault) List(_ context.Context) ([]string, error) {
	var keys []string
	for _, env := range os.Environ() {
		if strings.HasPrefix(env, v.prefix) {
			parts := strings.SplitN(env, "=", 2)
			key := strings.ToLower(strings.TrimPrefix(parts[0], v.prefix))
			keys = append(keys, key)
		}
	}
	return keys, nil
}

func (v *envVault) Has(_ context.Context, key string) (bool, error) {
	_, ok := os.LookupEnv(v.envKey(key))
	return ok, nil
}

func (v *envVault) HealthCheck(_ context.Context) error {
	return nil
}

func (v *envVault) envKey(key string) string {
	return fmt.Sprintf("%s%s", v.prefix, strings.ToUpper(strings.ReplaceAll(key, ".", "_")))
}
