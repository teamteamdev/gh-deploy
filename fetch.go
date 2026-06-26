package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

func newFetchCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "fetch <config.toml>",
		Short:        "Manually fetch and deploy the project",
		Long:         `Manually run the deployment for the project that owns the current directory.`,
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			configPath = args[0]
			return runFetch()
		},
	}
}

func runFetch() error {
	if err := loadConfig(true); err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	root, err := findRoot()
	if err != nil {
		return err
	}

	var repoConfig *RepoConfig
	for _, repo := range config.Projects {
		if repo.Path == root {
			repoConfig = repo
			break
		}
	}

	if repoConfig == nil {
		return fmt.Errorf("Deploy configuration was not found for the current directory.")
	}

	token, err := getRepositoryToken(repoConfig.Repository)
	if err != nil {
		return fmt.Errorf("failed to get credentials: %w", err)
	}

	return deploy(token, *repoConfig)
}

// findRoot returns the git repository root for the current working directory,
// falling back to the working directory itself when it is not inside a repo.
func findRoot() (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("failed to determine current directory: %w", err)
	}

	out, err := exec.Command("git", "-C", cwd, "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return filepath.Abs(cwd)
	}

	return strings.TrimSpace(string(out)), nil
}
