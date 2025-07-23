# gh-deploy

🚀 Tool to deploy code from GitHub repositories to your servers.

## Installation

The preferred option is to use our interactive installer: https://teamteam.dev/gh-deploy/. It will guide you through GitHub App registration and creates required configuration files.

To install manually, create a GitHub App in your personal account on an organization that owns repositories you want to deploy. Set the following options:
- Webhook URL: provide URL that you’ll deploy this tool to
- Webhook events: `push`
- Permissions: `contents:read` (Repository → Contents → Read-only)

Acquire client ID webhook secret, private key (PEM file). Install the application on your own account.

Put private key in `/etc/gh-deploy/key.pem` and webhook secret in `/etc/gh-deploy/webhook-secret` file.

Then follow with one of the installation options:

### Systems that have systemd enabled

Run this command. It will download the latest release to `/usr/local/bin/gh-deploy`, create default config, install and start systemd unit.

Set `CLIENT_ID` variable to your GitHub App Client ID (required). Also it’s recommended (but not required) to set `APP_USER` to the name
of your Linux user that will pull the repositories and run the deploy scripts. It should have enough privileges to do that. If not provided,
the current user will be used.

```bash
curl https://teamteam.dev/gh-deploy/install.sh | sudo CLIENT_ID=<your-github-app-id> APP_USER=<username> bash -
```

Then update `/etc/gh-deploy/config.toml` to your preference and make the webhook available at URL specified when creating the app.
Use `systemctl restart gh-deploy` to restart app after configuration changes.

### NixOS

Use `flake.nix` that provides `gh-deploy` service. Example configuration:

```nix
services.gh-deploy = {
  enable = true;
  domain = "prod.teamteam.dev";
  gitLfs = true;
  githubApp = {
    clientId = "123456abcdef";
    privateKeyFile = "/etc/gh-deploy/key.pem";
    webhookSecretFile = "/etc/gh-deploy/webhook-secret";
  };
  projects = {
    gh-deploy = {
      repository = "teamteamdev/gh-deploy";
      branch = "pages";
      timeout = 300;
      command = ''
        systemctl restart nginx
      '';
    };
  };
};
```

> Note that NixOS module registers user and group `gh-deploy` to run the service, so change file ownership respectively.

### Other systems

You can fetch latest release from [Releases](https://github.com/teamteamdev/gh-deploy/releases) page.

To launch, use

```shell
gh-deploy path/to/config.toml
```

### Build from source

```shell
go build -o gh-deploy
```

## Configuration

See [config.example.toml](config.example.toml) for configuration examples.

Use `systemctl reload gh-deploy` to reload the configuration. Updating HTTP server settings (bind address, port or TLS) on-the-fly is not supported,
except for replacing TLS key pair.

## License

Contents of this repository are available under [MIT License](LICENSE).
