package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
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

func init() {
	log.SetFlags(0)
}

func main() {
	if len(os.Args) != 2 {
		log.Fatal("Usage: ./gh-deploy config.toml")
	}

	configPath = os.Args[1]
	if err := loadConfig(true); err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	handleConfigReload()

	mux := http.NewServeMux()
	mux.HandleFunc("/", handleWebhook)

	proto := "tcp"
	addr := config.Bind
	if strings.HasPrefix(config.Bind, "unix:") {
		proto = "unix"
		addr = strings.TrimPrefix(config.Bind, "unix:")
	}

	log.Printf("Starting gh-deploy on %s", addr)

	listener, err := getListener(proto, addr)
	if err != nil {
		log.Fatalf("Failed to create listener: %v", err)
	}
	defer listener.Close()

	var tlsConfig *tls.Config
	if config.TLS != nil {
		tlsConfig = &tls.Config{
			GetCertificate: func(*tls.ClientHelloInfo) (*tls.Certificate, error) {
				return &config.TLS.certificate, nil
			},
		}
	}

	srv := &http.Server{
		Addr:      addr,
		Handler:   mux,
		TLSConfig: tlsConfig,
	}

	if config.TLS != nil {
		err = srv.ServeTLS(listener, "", "")
	} else {
		err = srv.Serve(listener)
	}

	if err != nil {
		log.Fatalf("Server failed to start: %v", err)
	}
}

func getListener(proto, addr string) (net.Listener, error) {
	if proto == "unix" {
		if err := os.Remove(addr); err != nil && !os.IsNotExist(err) {
			return nil, fmt.Errorf("cannot delete existing file: %w", err)
		}

		if err := os.MkdirAll(filepath.Dir(addr), 0755); err != nil {
			return nil, fmt.Errorf("cannot create directory for socket: %w", err)
		}
	}

	listener, err := net.Listen(proto, addr)
	if err != nil {
		return nil, err
	}

	if proto == "unix" {
		if err := os.Chmod(addr, 0777); err != nil {
			listener.Close()
			return nil, fmt.Errorf("cannot set permissions for socket: %w", err)
		}
	}

	return listener, nil
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
