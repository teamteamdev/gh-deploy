package main

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/gofrs/flock"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/go-github/v57/github"
)

type Config struct {
	Bind string `toml:"bind"`
	Port int    `toml:"port"`
	TLS  *struct {
		CertFile string `toml:"cert_file"`
		KeyFile  string `toml:"key_file"`
	} `toml:"tls"`
	GitHubApp struct {
		ClientID      string `toml:"client_id"`
		PEMKeyPath    string `toml:"pem_key_path"`
		WebhookSecret string `toml:"webhook_secret"`
	} `toml:"github_app"`
	GitLFS bool         `toml:"git_lfs"`
	Repos  []RepoConfig `toml:"repos"`
}

type RepoConfig struct {
	Repository string `toml:"full_name"`
	Branch     string `toml:"branch"`
	ClonePath  string `toml:"clone_path"`
	Command    string `toml:"command"`
	Timeout    int    `toml:"timeout"`
}

type WebhookPayload struct {
	Repository struct {
		FullName string `json:"full_name"`
	} `json:"repository"`
	Ref          string `json:"ref"`
	Installation struct {
		ID int64 `json:"id"`
	} `json:"installation"`
}

var config Config

func main() {
	if len(os.Args) != 2 {
		log.Fatal("Usage: ./webhook-server config.toml")
	}

	configPath := os.Args[1]
	if err := loadConfig(configPath); err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	http.HandleFunc("/", handleWebhook)

	addr := fmt.Sprintf("%s:%d", config.Bind, config.Port)
	log.Printf("Starting webhook server on %s", addr)

	var err error
	if config.TLS != nil {
		err = http.ListenAndServeTLS(addr, config.TLS.CertFile, config.TLS.KeyFile, nil)
	} else {
		err = http.ListenAndServe(addr, nil)
	}

	if err != nil {
		log.Fatalf("Server failed to start: %v", err)
	}
}

func loadConfig(path string) error {
	_, err := toml.DecodeFile(path, &config)
	if err != nil {
		return fmt.Errorf("failed to decode TOML config: %w", err)
	}

	if config.Bind == "" {
		config.Bind = "[::]"
	}

	// Weird way to set default values
	for i, repo := range config.Repos {
		if repo.Timeout == 0 {
			config.Repos[i].Timeout = 120
		}
	}

	return nil
}

func handleWebhook(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		log.Printf("Error reading request body: %v", err)
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}

	signature := r.Header.Get("X-Hub-Signature-256")
	if !verifySignature(body, signature, config.GitHubApp.WebhookSecret) {
		log.Printf("Invalid webhook signature")
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	var payload WebhookPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		log.Printf("Error parsing webhook payload: %v", err)
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}

	branch := strings.TrimPrefix(payload.Ref, "refs/heads/")

	var repoConfig *RepoConfig
	for _, repo := range config.Repos {
		if repo.Repository == payload.Repository.FullName && repo.Branch == branch {
			repoConfig = &repo
			break
		}
	}

	if repoConfig == nil {
		log.Printf("No configuration found for repo: %s, branch: %s", payload.Repository.FullName, branch)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("No configuration found"))
		return
	}

	log.Printf("Processing repo %s#%s", repoConfig.Repository, repoConfig.Branch)

	go processWebhook(payload, *repoConfig)

	w.WriteHeader(http.StatusOK)
}

func verifySignature(payload []byte, signature, secret string) bool {
	if signature == "" {
		return false
	}

	signature = strings.TrimPrefix(signature, "sha256=")

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	expectedMAC := mac.Sum(nil)
	expectedSignature := hex.EncodeToString(expectedMAC)

	return hmac.Equal([]byte(signature), []byte(expectedSignature))
}

func processWebhook(payload WebhookPayload, repoConfig RepoConfig) {
	started := time.Now()

	// Get GitHub App installation token
	token, err := getAccessToken(payload.Installation.ID)
	if err != nil {
		log.Printf("Error getting credentials: %v", err)
		return
	}

	lockPath := filepath.Join(filepath.Dir(repoConfig.ClonePath), "."+filepath.Base(repoConfig.ClonePath)+".lock")
	lock := flock.New(lockPath)
	locked, err := lock.TryLock()
	if err != nil {
		log.Printf("Failed to acquire lock: %v", err)
		return
	}
	if !locked {
		log.Printf("Waiting for lock on %s", repoConfig.ClonePath)
		err := lock.Lock()
		if err != nil {
			log.Printf("Failed to acquire lock: %v", err)
			return
		}
	}
	defer lock.Unlock()

	if err := processRepository(token, repoConfig); err != nil {
		log.Printf("Error processing repository: %v", err)
		return
	}

	log.Printf("Webhook for %s#%s finished in %v", repoConfig.Repository, repoConfig.Branch, time.Since(started))
}

func getAccessToken(installationID int64) (string, error) {
	keyBytes, err := os.ReadFile(config.GitHubApp.PEMKeyPath)
	if err != nil {
		return "", fmt.Errorf("failed to read PEM key: %w", err)
	}

	key, err := jwt.ParseRSAPrivateKeyFromPEM(keyBytes)
	if err != nil {
		return "", fmt.Errorf("failed to parse PEM key: %w", err)
	}

	now := time.Now()
	claims := jwt.MapClaims{
		"iat": now.Unix(),
		"exp": now.Add(10 * time.Minute).Unix(),
		"iss": config.GitHubApp.ClientID,
	}

	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	tokenString, err := token.SignedString(key)
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

func processRepository(token string, repoConfig RepoConfig) error {
	cloneURL := fmt.Sprintf("https://x-access-token:%s@github.com/%s.git", token, repoConfig.Repository)
	branchRef := fmt.Sprintf("refs/heads/%s", repoConfig.Branch)

	repoExists := true
	if _, err := os.Stat(repoConfig.ClonePath); os.IsNotExist(err) {
		repoExists = false
	} else if err != nil {
		return fmt.Errorf("failed to check repository path: %w", err)
	}

	if !repoExists {
		log.Printf("Cloning repository %s#%s to %s", repoConfig.Repository, repoConfig.Branch, repoConfig.ClonePath)
		if err := execGit("", "clone", cloneURL, repoConfig.ClonePath, "-b", repoConfig.Branch); err != nil {
			return err
		}
		if config.GitLFS {
			if err := execGit(repoConfig.ClonePath, "lfs", "install", "--local"); err != nil {
				return err
			}
		}
	} else {
		log.Printf("Pulling changes from %s#%s to %s", repoConfig.Repository, repoConfig.Branch, repoConfig.ClonePath)

		if err := execGit(repoConfig.ClonePath, "set-url", "origin", cloneURL); err != nil {
			return err
		}

		if err := execGit(repoConfig.ClonePath, "fetch", "origin", branchRef); err != nil {
			return err
		}
	}

	if config.GitLFS {
		if err := execGit(repoConfig.ClonePath, "lfs", "fetch", "origin", branchRef); err != nil {
			return err
		}
	}

	if repoExists {
		if err := execGit(repoConfig.ClonePath, "checkout", "-B", repoConfig.Branch, fmt.Sprintf("origin/%s", repoConfig.Branch)); err != nil {
			return err
		}
	}

	if config.GitLFS {
		if err := execGit(repoConfig.ClonePath, "lfs", "checkout"); err != nil {
			return err
		}
	}

	if repoConfig.Command != "" {
		log.Printf("Deploying using: %s", repoConfig.Command)
		if err := executeCommand(repoConfig); err != nil {
			return fmt.Errorf("failed to execute deploy command: %w", err)
		}
	}

	return nil
}

func execGit(path string, args ...string) error {
	cmd := exec.Command("git", args...)
	if path != "" {
		cmd.Dir = path
	}
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	return cmd.Run()
}

func executeCommand(repoConfig RepoConfig) error {
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(repoConfig.Timeout)*time.Second)
	defer cancel()

	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "sh"
	}
	cmd := exec.CommandContext(ctx, shell, "-c", repoConfig.Command)
	cmd.Dir = repoConfig.ClonePath
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	return cmd.Run()
}
