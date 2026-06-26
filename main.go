package main

import (
	"crypto/tls"
	"fmt"
	"log"
	"net/http"

	"github.com/spf13/cobra"
)

func init() {
	log.SetFlags(0)
}

func newRootCmd() *cobra.Command {
	rootCmd := &cobra.Command{
		Use:   "gh-deploy <config.toml>",
		Short: "GitHub webhook handler for secure repository deployment.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			configPath = args[0]
			return runServer()
		},
		SilenceUsage: true,
	}

	rootCmd.AddCommand(newSetupCmd())

	return rootCmd
}

func main() {
	if err := newRootCmd().Execute(); err != nil {
		log.Fatal(err)
	}
}

func runServer() error {
	if err := loadConfig(true); err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	handleConfigReload()

	mux := http.NewServeMux()
	mux.HandleFunc("/", handleWebhook)

	_, addr := parseBind(config.Bind)
	log.Printf("Starting gh-deploy on %s", addr)

	listener, err := listen(config.Bind)
	if err != nil {
		return fmt.Errorf("failed to create listener: %w", err)
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
		return fmt.Errorf("server failed to start: %w", err)
	}

	return nil
}
