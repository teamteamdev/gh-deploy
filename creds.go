package main

import (
	"context"
	"fmt"
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

func fetchAccessToken(installationID int64) (string, error) {
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
