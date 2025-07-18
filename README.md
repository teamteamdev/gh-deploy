# gh-deploy

🚀 Tool to deploy code from GitHub repositories to your servers.

## Installation

The preferred option is to use our interactive installer: https://teamteam.dev/gh-deploy/. It will guide you through GitHub App registration and creates required configuration files.

To install manually, create a GitHub App in your personal account on an organization that owns repositories you want to deploy. Set the following options:
- Webhook URL: provide URL that you’ll deploy this tool to
- Webhook events: `push`
- Permissions: `contents:read` (Repository → Contents → Read-only)

Acquire client ID webhook secret, private key (PEM file). Install the application on your own account.

Then follow with one of the installation options:

### Systems that have systemd enabled

Decide which user will be running the tool. The following instructions suppose this user will be named `user` and has default group named `usergroup`. Execute these commands:

```bash
sudo install -Ddm 755 -o root -g usergroup /etc/gh-deploy
```

Then create files `/etc/gh-deploy/key.pem` and `/etc/gh-deploy/webhook-secret` with PEM file and webhook secret respectively. Then:

```bash
sudo chmod 640 /etc/gh-deploy/key.pem /etc/gh-deploy/webhook-secret
sudo chown root:usergroup /etc/gh-deploy/key.pem /etc/gh-deploy/webhook-secret

# This will download the latest release to /usr/local/bin/gh-deploy, create default config, install and start systemd unit.
curl https://teamteam.dev/gh-deploy/install.sh | sudo APP_ID=<your-github-app-id> USER=user bash -
```

Then update `/etc/gh-deploy/config.toml` to your preference and make the webhook available at URL specified before.

### NixOS

Use `flake.nix` that provides `gh-deploy` service.

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

## License

Contents of this repository are available under [MIT License](LICENSE).
