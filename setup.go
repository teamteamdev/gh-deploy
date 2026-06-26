package main

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"os"
	"os/user"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/google/go-github/v57/github"
	"github.com/spf13/cobra"
)

type setupOptions struct {
	bind        string
	tlsCert     string
	tlsKey      string
	publicURL   string
	githubOrg   string
	configDir   string
	configChgrp string
}

func newSetupCmd() *cobra.Command {
	opts := &setupOptions{}

	cmd := &cobra.Command{
		Use:   "setup",
		Short: "Register a GitHub App and generate the configuration",
		Long: "Launches a one-shot web server that walks you through GitHub's App " +
			"Manifest flow, then writes config.toml, key.pem and webhook-secret.",
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSetup(opts)
		},
	}

	flags := cmd.Flags()
	flags.StringVar(&opts.bind, "bind", "", "address to listen on, e.g. [::]:8443 (required)")
	flags.StringVar(&opts.tlsCert, "tls-cert", "", "path to TLS certificate file")
	flags.StringVar(&opts.tlsKey, "tls-key", "", "path to TLS private key file")
	flags.StringVar(&opts.publicURL, "public-url", "", "public https:// URL of this server (required)")
	flags.StringVar(&opts.githubOrg, "github-org", "", "create the app under this organization instead of your account")
	flags.StringVar(&opts.configDir, "config", "/etc/gh-deploy", "directory to write configuration into")
	flags.StringVar(&opts.configChgrp, "config-chgrp", "gh-deploy", "group to assign to the generated files")

	cmd.MarkFlagRequired("bind")
	cmd.MarkFlagRequired("public-url")

	return cmd
}

func runSetup(opts *setupOptions) error {
	opts.publicURL = strings.TrimRight(opts.publicURL, "/")

	if !strings.HasPrefix(opts.publicURL, "https://") {
		return fmt.Errorf("--public-url must be an https:// URL")
	}

	if (opts.tlsCert == "") != (opts.tlsKey == "") {
		return fmt.Errorf("--tls-cert and --tls-key must be provided together")
	}

	gid, err := checkPermissions(opts.configDir, opts.configChgrp)
	if err != nil {
		return err
	}

	hostname, err := os.Hostname()
	if err != nil {
		return fmt.Errorf("failed to determine hostname: %w", err)
	}

	manifest, err := buildManifest(opts.publicURL, hostname)
	if err != nil {
		return err
	}

	formURL := "https://github.com/settings/apps/new"
	if opts.githubOrg != "" {
		formURL = fmt.Sprintf("https://github.com/organizations/%s/settings/apps/new", opts.githubOrg)
	}

	listener, err := listen(opts.bind)
	if err != nil {
		return fmt.Errorf("failed to create listener: %w", err)
	}
	defer listener.Close()

	done := make(chan error, 1)

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		renderManifestForm(w, formURL, manifest)
	})
	mux.HandleFunc("/setup", func(w http.ResponseWriter, r *http.Request) {
		code := r.URL.Query().Get("code")
		if code == "" {
			http.Error(w, "Missing code parameter", http.StatusBadRequest)
			return
		}

		accountName, err := completeSetup(r.Context(), code, opts, gid)
		if err != nil {
			log.Printf("Setup failed: %v", err)
			http.Error(w, fmt.Sprintf("Setup failed: %v", err), http.StatusInternalServerError)
			done <- err
			return
		}

		renderSuccess(w)
		printInstructions(opts.configDir, accountName)
		done <- nil
	})

	srv := &http.Server{Handler: mux}

	go func() {
		var err error
		if opts.tlsCert != "" {
			cert, lerr := tls.LoadX509KeyPair(opts.tlsCert, opts.tlsKey)
			if lerr != nil {
				done <- fmt.Errorf("failed to load TLS certificate: %w", lerr)
				return
			}
			srv.TLSConfig = &tls.Config{Certificates: []tls.Certificate{cert}}
			err = srv.ServeTLS(listener, "", "")
		} else {
			err = srv.Serve(listener)
		}
		if err != nil && err != http.ErrServerClosed {
			done <- fmt.Errorf("server failed: %w", err)
		}
	}()

	log.Printf("Open %s in your browser to continue setup.", opts.publicURL)

	err = <-done
	srv.Shutdown(context.Background())
	return err
}

// checkPermissions verifies the current user can create files in configDir and
// is able to assign them to the chgrp group. It returns the target group's GID.
func checkPermissions(configDir, chgrp string) (int, error) {
	group, err := user.LookupGroup(chgrp)
	if err != nil {
		return 0, fmt.Errorf("group %q not found: %w (is gh-deploy installed?)", chgrp, err)
	}
	gid, err := strconv.Atoi(group.Gid)
	if err != nil {
		return 0, fmt.Errorf("invalid gid for group %q: %w", chgrp, err)
	}

	if err := checkWritable(configDir); err != nil {
		return 0, fmt.Errorf("cannot create files in %s: %w\nTry re-running with sudo.", configDir, err)
	}

	if os.Geteuid() != 0 {
		if err := checkGroupMembership(group.Gid); err != nil {
			return 0, err
		}
	}

	return gid, nil
}

func checkWritable(dir string) error {
	f, err := os.CreateTemp(dir, ".gh-deploy-setup-*")
	if err != nil {
		return err
	}
	f.Close()
	return os.Remove(f.Name())
}

func checkGroupMembership(gid string) error {
	current, err := user.Current()
	if err != nil {
		return fmt.Errorf("failed to determine current user: %w", err)
	}
	gids, err := current.GroupIds()
	if err != nil {
		return fmt.Errorf("failed to list group membership: %w", err)
	}
	if slices.Contains(gids, gid) {
		return nil
	}
	return fmt.Errorf("user %s is not a member of the target group; re-run with sudo", current.Username)
}

func buildManifest(publicURL, hostname string) (string, error) {
	manifest := map[string]any{
		"name":            fmt.Sprintf("gh-deploy for %s", hostname),
		"url":             publicURL,
		"hook_attributes": map[string]string{"url": publicURL},
		"redirect_url":    publicURL + "/setup",
		"callback_urls":   []string{publicURL + "/setup"},
		"public":          false,
		"default_events":  []string{"push"},
		"default_permissions": map[string]string{
			"contents": "read",
			"checks":   "write",
		},
	}

	data, err := json.Marshal(manifest)
	if err != nil {
		return "", fmt.Errorf("failed to encode manifest: %w", err)
	}
	return string(data), nil
}

var manifestFormTmpl = template.Must(template.New("manifest").Parse(`<!DOCTYPE html>
<html>
<head><title>gh-deploy setup</title></head>
<body onload="document.forms[0].submit()">
  <form action="{{.URL}}" method="post">
    <input type="hidden" name="manifest" value="{{.Manifest}}">
    <noscript><button type="submit">Continue to GitHub</button></noscript>
  </form>
  <p>Redirecting you to GitHub to create the app&hellip;</p>
</body>
</html>`))

func renderManifestForm(w http.ResponseWriter, url, manifest string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	manifestFormTmpl.Execute(w, struct {
		URL      string
		Manifest string
	}{URL: url, Manifest: manifest})
}

func renderSuccess(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(`<!DOCTYPE html>
<html>
<head><title>gh-deploy setup</title></head>
<body>
  <h1>Installation successful!</h1>
  <p>The GitHub App has been created and your configuration was written.
  You can close this window and return to the terminal for the next steps.</p>
</body>
</html>`))
}

func completeSetup(ctx context.Context, code string, opts *setupOptions, gid int) (string, error) {
	cfg, _, err := github.NewClient(nil).Apps.CompleteAppManifest(ctx, code)
	if err != nil {
		return "", fmt.Errorf("failed to complete app manifest: %w", err)
	}

	uid := os.Geteuid()

	keyPath := filepath.Join(opts.configDir, "key.pem")
	if err := writeOwnedFile(keyPath, []byte(cfg.GetPEM()), 0640, uid, gid); err != nil {
		return "", err
	}

	secretPath := filepath.Join(opts.configDir, "webhook-secret")
	if err := writeOwnedFile(secretPath, []byte(cfg.GetWebhookSecret()), 0640, uid, gid); err != nil {
		return "", err
	}

	configPath := filepath.Join(opts.configDir, "config.toml")
	contents := renderConfig(opts, cfg.GetClientID(), keyPath, secretPath)
	if err := writeOwnedFile(configPath, []byte(contents), 0644, uid, gid); err != nil {
		return "", err
	}

	accountName := opts.githubOrg
	if accountName == "" {
		accountName = cfg.GetOwner().GetLogin()
	}
	return accountName, nil
}

func writeOwnedFile(path string, data []byte, mode os.FileMode, uid, gid int) error {
	if err := os.WriteFile(path, data, mode); err != nil {
		return fmt.Errorf("failed to write %s: %w", path, err)
	}
	if err := os.Chmod(path, mode); err != nil {
		return fmt.Errorf("failed to set permissions on %s: %w", path, err)
	}
	if err := os.Chown(path, uid, gid); err != nil {
		return fmt.Errorf("failed to set ownership on %s: %w", path, err)
	}
	return nil
}

func renderConfig(opts *setupOptions, clientID, keyPath, secretPath string) string {
	var b strings.Builder

	fmt.Fprintf(&b, "# Server configuration\n")
	fmt.Fprintf(&b, "bind = %q\n\n", opts.bind)

	if opts.tlsCert != "" {
		fmt.Fprintf(&b, "[tls]\n")
		fmt.Fprintf(&b, "cert_file = %q\n", opts.tlsCert)
		fmt.Fprintf(&b, "key_file = %q\n\n", opts.tlsKey)
	}

	fmt.Fprintf(&b, "[github_app]\n")
	fmt.Fprintf(&b, "client_id = %q\n", clientID)
	fmt.Fprintf(&b, "private_key_file = %q\n", keyPath)
	fmt.Fprintf(&b, "webhook_secret_file = %q\n\n", secretPath)

	fmt.Fprintf(&b, "# Repositories and branches to deploy\n")
	fmt.Fprintf(&b, "# [[projects]]\n")
	fmt.Fprintf(&b, "# repository = \"owner/repo\"\n")
	fmt.Fprintf(&b, "# branch = \"main\"\n")
	fmt.Fprintf(&b, "# path = \"/var/www/repo\"\n")
	fmt.Fprintf(&b, "# command = \"make deploy\"\n")
	fmt.Fprintf(&b, "# timeout = 300\n")

	return b.String()
}

func printInstructions(configDir, accountName string) {
	if accountName == "" {
		accountName = "your-account"
	}
	fmt.Printf(`
Installation successful! Run `+"`systemctl enable --now gh-deploy`"+` to start the app.

Now you can add repositories to %s/config.toml! Example:
  [[projects]]
  repository = "%s/my-first-repo"
  branch = "main"
  path = "/opt/my-first-repo"
  command = "./deploy.sh"
`, configDir, accountName)
}
