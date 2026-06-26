# gh-deploy

🚀 Tool to deploy code from GitHub repositories to your servers.

Uses webhooks instead of GitHub Actions: you don’t need to store your private keys in GitHub infrastructure or pay for Actions.

## Requisites

You need a server that can accept incoming connections from the internet and have a stable DNS name or IP address.

The tool now supports only operating systems running Linux. Create an issue if you want to use it on another platform.

It's preferred to use HTTPS. Get a free certificate from [Let’s Encrypt](https://letsencrypt.org/getting-started/). You can also setup reverse proxy (for example, [Nginx](https://docs.nginx.com/nginx/admin-guide/web-server/reverse-proxy/)).

## Installation

Download and unpack the tool:

```bash
curl -fsSL https://teamteamdev.github.io/gh-deploy/install.sh | sudo bash
```

This script:
- downloads a version for your server
- unpacks `gh-deploy` tool into `/usr/local/bin`
- sets up systemd unit
- creates `gh-deploy` user and group that will run the tool

Alternatively, you can download [latest release](https://github.com/teamteamdev/gh-deploy/releases) and put the binary in a preferred place.

## Configuration

Set up your reverse proxy if any. You will need:
- **public address** — URL at which the service will be available from internet,
- **listen address** — port or UNIX socket that this tool will listen to.

For example, if your server has public IP address `192.0.2.4`, and you don't use reverse proxy, you may use `http://192.0.2.4` as your public address, and `0.0.0.0:80` as your listen address. If you have reverse proxy on the same server that serves domain `https://example.org` to port `12345`, you may use `https://example.org` as your public address, and `127.0.0.1:12345` as your listen address.

Run the configuration master:

```bash
sudo gh-deploy setup --bind listen-address --public-url https://public-address [--github-org name]
```

See `gh-deploy setup --help` for all options.

Follow the generated link in your browser. Confirm app creation in GitHub. After that, the tool will generate its own configuration files in `/etc/gh-deploy` directory.

<details>

<summary>Configure manually</summary>

Create a GitHub App in your personal account on an organization that owns repositories you want to deploy. Set the following options:
- Webhook URL: specify your public address here
- Webhook events: `push`
- Permissions: `contents:read` (Repository → Contents → Read-only), `checks:write` (Repository → Checks → Read and write)

Acquire client ID webhook secret, private key (PEM file). Install the application on your own account.

Put private key in `/etc/gh-deploy/key.pem` and webhook secret in `/etc/gh-deploy/webhook-secret` file. Change permissions to `640` and group ownership to the group of user that runs the tool.

</details>

<details>

<summary>For NixOS users</summary>

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

</details>

### Add repositories

In the end of `/etc/gh-deploy/gh-deploy.yml`, for each repository and branch you want to deploy, add the following block:

```toml
# Repositories and branches to deploy
[[projects]]
repository = "owner/repo"
branch = "main"           # Branch to watch for changes
path = "/var/www/repo"    # This folder should be writable by user running the gh-deploy
command = "make deploy"   # Optional: command to run after update
timeout = 300             # in seconds, default is 120
```

If you are adding repositories to an already running instance of the tool, run `sudo systemctl reload gh-deploy` to apply the changes.

### Launch

Run

```bash
sudo systemctl enable --now gh-deploy
```

This starts the tool and schedules it to start on each subsequent system startup.

## Upgrade

Re-run the installation script.

## Uninstall

Go to GitHub settings → Developer settings → GitHub Apps and delete the app.

Run these commands as root:

```bash
systemctl disable --now gh-deploy
rm /usr/local/bin/gh-deploy /etc/systemd/system/gh-deploy.service
rm -rf /etc/gh-deploy
```

## License

Contents of this repository are available under [MIT License](LICENSE).
