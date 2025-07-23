package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"time"
)

func deploy(token string, repoConfig RepoConfig) error {
	cloneURL := fmt.Sprintf("https://github.com/%s.git", repoConfig.Repository)
	branchRef := fmt.Sprintf("refs/heads/%s", repoConfig.Branch)

	repoExists := true
	if _, err := os.Stat(repoConfig.Path); os.IsNotExist(err) {
		repoExists = false
	} else if err != nil {
		return fmt.Errorf("failed to check repository path: %w", err)
	}

	git := GitClient{
		Token: token,
		Path:  repoConfig.Path,
	}

	if !repoExists {
		log.Printf("Cloning repository %s#%s to %s", repoConfig.Repository, repoConfig.Branch, repoConfig.Path)

		if err := os.MkdirAll(repoConfig.Path, 0755); err != nil {
			return err
		}

		if err := git.Exec("clone", cloneURL, repoConfig.Path, "-b", repoConfig.Branch); err != nil {
			return err
		}
		if config.GitLFS {
			if err := git.Exec("lfs", "install", "--local"); err != nil {
				return err
			}
		}
	} else {
		log.Printf("Pulling changes from %s#%s to %s", repoConfig.Repository, repoConfig.Branch, repoConfig.Path)

		if err := git.Exec("remote", "set-url", "origin", cloneURL); err != nil {
			return err
		}

		if err := git.Exec("fetch", "origin", branchRef); err != nil {
			return err
		}
	}

	if config.GitLFS {
		if err := git.Exec("lfs", "fetch", "origin", branchRef); err != nil {
			return err
		}
	}

	if repoExists {
		if err := git.Exec("checkout", "-B", repoConfig.Branch, fmt.Sprintf("origin/%s", repoConfig.Branch)); err != nil {
			return err
		}
	}

	if config.GitLFS {
		if err := git.Exec("lfs", "checkout"); err != nil {
			return err
		}
	}

	if repoConfig.Command != "" {
		log.Printf("Deploying using: %s", repoConfig.Command)
		if err := executePostCommand(repoConfig); err != nil {
			return fmt.Errorf("failed to execute deploy command: %w", err)
		}
	}

	return nil
}

type GitClient struct {
	Token string
	Path  string
}

func (c *GitClient) Exec(command string, args ...string) error {
	// Git doesn't have a convenient way to pass the credentials, so we pass Bash function fake one-time credential helper.
	// See `man git` about `git -c` global flag; and `man gitcredentials` about helpers.

	args = append([]string{
		"-c", fmt.Sprintf("credential.helper=!f(){ echo username=x-access-token;echo password='%s';echo; };f", c.Token),
		command,
	}, args...)

	cmd := exec.Command("git", args...)
	cmd.Dir = c.Path
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	return cmd.Run()
}

func executePostCommand(repoConfig RepoConfig) error {
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(repoConfig.Timeout)*time.Second)
	defer cancel()

	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/sh"
	}
	cmd := exec.CommandContext(ctx, shell, "-c", repoConfig.Command)
	cmd.Dir = repoConfig.Path
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	return cmd.Run()
}
