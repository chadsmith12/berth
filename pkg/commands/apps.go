package commands

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/chadsmith12/berth/pkg/coolify"
	"github.com/chadsmith12/berth/pkg/output"
)

// resolveAppUUID takes --uuid, then BERTH_UUID, then the repository's
// environment registry. Every command that acts on a linked application
// resolves it the same way; nothing is inferred.
func resolveAppUUID(sess *Session, flagUUID, envName string) (string, error) {
	if flagUUID != "" {
		return flagUUID, nil
	}
	if v := strings.TrimSpace(os.Getenv("BERTH_UUID")); v != "" {
		return v, nil
	}
	if sess.Repo != nil {
		if env, ok := sess.Repo.EnvironmentFor(envName); ok && env.UUID != "" {
			return env.UUID, nil
		}
	}
	return "", output.Usage(fmt.Errorf("no application linked to %q — pass --uuid, set BERTH_UUID, or link one with berth launch", envName))
}

// classifyAPIError maps Coolify failures that mean authentication or scoping
// (401/403, and 404 — a mis-scoped token 404s across teams) to auth errors.
func classifyAPIError(err error) error {
	var apiErr *coolify.ApiError
	if errors.As(err, &apiErr) && (apiErr.Status == 401 || apiErr.Status == 403 || apiErr.Status == 404) {
		return output.Auth(err)
	}
	return err
}

// appState strips Coolify's health suffix: "running:unknown" is the state
// "running".
func appState(status string) string {
	state, _, _ := strings.Cut(status, ":")
	return state
}

// sessionEnv is the environment a command acts on: --env when given, else
// the sanctioned default.
func sessionEnv(sess *Session) string {
	if sess.Env != "" {
		return sess.Env
	}
	return "production"
}
