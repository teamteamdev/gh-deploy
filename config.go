package main

import (
	"crypto/rsa"
	"crypto/tls"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"

	"github.com/BurntSushi/toml"
	"github.com/golang-jwt/jwt/v5"
)

type Config struct {
	Bind string `toml:"bind"`
	TLS  *struct {
		CertFile    string `toml:"cert_file"`
		KeyFile     string `toml:"key_file"`
		certificate tls.Certificate
	} `toml:"tls"`
	GitHubApp struct {
		ClientID          string `toml:"client_id"`
		PrivateKeyFile    string `toml:"private_key_file"`
		WebhookSecretFile string `toml:"webhook_secret_file"`
		privateKey        *rsa.PrivateKey
		webhookSecret     string
	} `toml:"github_app"`
	GitLFS   bool          `toml:"git_lfs"`
	Projects []*RepoConfig `toml:"projects"`
}

type RepoConfig struct {
	Repository string `toml:"repository"`
	Branch     string `toml:"branch"`
	Path       string `toml:"path"`
	Command    string `toml:"command"`
	Timeout    int    `toml:"timeout"`
}

var (
	config      Config
	configPath  string
	configMutex = sync.Mutex{}
)

func loadConfig(initial bool) error {
	var newConfig Config
	_, err := toml.DecodeFile(configPath, &newConfig)
	if err != nil {
		return fmt.Errorf("failed to decode TOML config: %w", err)
	}

	if newConfig.Bind == "" {
		return fmt.Errorf("configuration 'bind' parameter is required")
	}

	// Validate projects one by one
	for _, repo := range newConfig.Projects {
		repo.Path, err = filepath.Abs(repo.Path)

		if err != nil {
			return fmt.Errorf("failed to resolve path: %w", err)
		}

		if repo.Timeout == 0 {
			repo.Timeout = 120
		}
	}

	if newConfig.TLS != nil {
		newConfig.TLS.certificate, err = tls.LoadX509KeyPair(os.ExpandEnv(newConfig.TLS.CertFile), os.ExpandEnv(newConfig.TLS.KeyFile))
		if err != nil {
			return fmt.Errorf("failed to load TLS certificate: %w", err)
		}
	}

	keyBytes, err := os.ReadFile(os.ExpandEnv(newConfig.GitHubApp.PrivateKeyFile))
	if err != nil {
		return fmt.Errorf("failed to read PEM key: %w", err)
	}

	newConfig.GitHubApp.privateKey, err = jwt.ParseRSAPrivateKeyFromPEM(keyBytes)
	if err != nil {
		return fmt.Errorf("failed to parse PEM key: %w", err)
	}

	webhookSecretBytes, err := os.ReadFile(os.ExpandEnv(newConfig.GitHubApp.WebhookSecretFile))
	if err != nil {
		return fmt.Errorf("failed to read webhook secret: %w", err)
	}

	newConfig.GitHubApp.webhookSecret = strings.TrimSpace(string(webhookSecretBytes))

	if !initial && ((config.TLS == nil) != (newConfig.TLS == nil) || config.Bind != newConfig.Bind) {
		log.Printf("Warning: Changing bind address or toggling TLS requires a restart of the server, updates to these settings are ignored")
	}

	config = newConfig

	return nil
}

func handleConfigReload() {
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGHUP)

	go func() {
		for range sigChan {
			configMutex.Lock()
			log.Printf("Received SIGHUP, reloading configuration...")

			if err := loadConfig(false); err != nil {
				log.Fatalf("Failed to reload config: %v", err)
			}

			log.Printf("Configuration reloaded successfully")

			tokenCache.Storage.Flush()
			configMutex.Unlock()
		}
	}()
}
