package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/gofrs/flock"
)

type WebhookPayload struct {
	Repository struct {
		FullName string `json:"full_name"`
	} `json:"repository"`
	Ref          string `json:"ref"`
	Installation struct {
		ID int64 `json:"id"`
	} `json:"installation"`
}

func handleWebhook(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if r.Header.Get("X-GitHub-Event") != "push" {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("Unsupported event type"))
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		log.Printf("Error reading request body: %v", err)
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}

	signature := r.Header.Get("X-Hub-Signature-256")

	if !verifySignature(body, signature, config.GitHubApp.webhookSecret) {
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
	for _, repo := range config.Projects {
		if repo.Repository == payload.Repository.FullName && repo.Branch == branch {
			repoConfig = repo
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
	token, err := getAccessToken(payload.Installation.ID)
	if err != nil {
		log.Printf("Error getting credentials: %v", err)
		return
	}

	lockPath := filepath.Join(filepath.Dir(repoConfig.Path), "."+filepath.Base(repoConfig.Path)+".lock")
	lock := flock.New(lockPath)
	locked, err := lock.TryLock()
	if err != nil {
		log.Printf("Failed to acquire lock: %v", err)
		return
	}
	if !locked {
		log.Printf("Waiting for lock on %s", repoConfig.Path)
		err := lock.Lock()
		if err != nil {
			log.Printf("Failed to acquire lock: %v", err)
			return
		}
	}
	defer lock.Unlock()

	started := time.Now()

	if err := deploy(token, repoConfig); err != nil {
		log.Printf("Error processing repository: %v", err)
		return
	}

	log.Printf("Webhook for %s#%s finished in %v", repoConfig.Repository, repoConfig.Branch, time.Since(started))
}
