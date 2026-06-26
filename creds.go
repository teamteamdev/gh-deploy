package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/go-github/v57/github"
	"github.com/kofalt/go-memoize"
	"github.com/patrickmn/go-cache"
)

var (
	tokenCache = memoize.NewMemoizer(
		10*time.Minute,
		cache.NoExpiration,
	)
)

// appJWT mints a short-lived JWT that authenticates as the GitHub App itself.
func appJWT() (string, error) {
	now := time.Now()
	claims := jwt.MapClaims{
		"iat": now.Unix(),
		"exp": now.Add(10 * time.Minute).Unix(),
		"iss": config.GitHubApp.ClientID,
	}

	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	tokenString, err := token.SignedString(config.GitHubApp.privateKey)
	if err != nil {
		return "", fmt.Errorf("failed to sign JWT: %w", err)
	}

	return tokenString, nil
}

func fetchAccessToken(installationID int64) (string, error) {
	tokenString, err := appJWT()
	if err != nil {
		return "", err
	}

	client := github.NewClient(nil).WithAuthToken(tokenString)

	installationToken, _, err := client.Apps.CreateInstallationToken(
		context.Background(),
		installationID,
		&github.InstallationTokenOptions{},
	)
	if err != nil {
		return "", fmt.Errorf("failed to create installation token: %w", err)
	}

	return installationToken.GetToken(), nil
}

func getAccessToken(installationID int64) (string, error) {
	fetch := func() (string, error) {
		return fetchAccessToken(installationID)
	}

	token, err, _ := memoize.Call(tokenCache, fmt.Sprintf("fetch-access-token-%d", installationID), fetch)
	return token, err
}

// getRepositoryToken resolves the App installation that owns the given
// "owner/repo" repository and returns an access token for it. This is used by
// the manual fetch command, which has no webhook payload to read the
// installation ID from.
func getRepositoryToken(repository string) (string, error) {
	owner, repo, found := strings.Cut(repository, "/")
	if !found {
		return "", fmt.Errorf("invalid repository name %q, expected \"owner/repo\"", repository)
	}

	tokenString, err := appJWT()
	if err != nil {
		return "", err
	}

	client := github.NewClient(nil).WithAuthToken(tokenString)

	installation, _, err := client.Apps.FindRepositoryInstallation(context.Background(), owner, repo)
	if err != nil {
		return "", fmt.Errorf("failed to find installation for %s: %w", repository, err)
	}

	return getAccessToken(installation.GetID())
}
