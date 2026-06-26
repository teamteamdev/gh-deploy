#!/usr/bin/env bash
#
# gh-deploy installer.
#
# Usage:
#   curl https://url-to/install.sh | sudo bash
#
set -euo pipefail

REPO="teamteamdev/gh-deploy"
BIN_DIR="/usr/local/bin"
CONFIG_DIR="/etc/gh-deploy"
UNIT_PATH="/etc/systemd/system/gh-deploy.service"
USER="gh-deploy"
GROUP="gh-deploy"

err() { echo "error: $*" >&2; exit 1; }

if [ "$(id -u)" -ne 0 ]; then
    err "this installer must be run as root (try: curl ... | sudo bash)"
fi

case "$(uname -m)" in
    x86_64|amd64) ARCH="amd64" ;;
    aarch64|arm64) ARCH="arm64" ;;
    *) err "unsupported architecture: $(uname -m)" ;;
esac

echo "Detecting latest release of $REPO..."
TAG="$(curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" \
    | grep -m1 '"tag_name"' \
    | sed -E 's/.*"tag_name"[[:space:]]*:[[:space:]]*"([^"]+)".*/\1/')"
[ -n "$TAG" ] || err "could not determine latest release tag"
echo "Latest release: $TAG"

ASSET="gh-deploy-linux-${ARCH}.tar.gz"
URL="https://github.com/${REPO}/releases/download/${TAG}/${ASSET}"

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

echo "Downloading $URL..."
curl -fsSL "$URL" -o "$TMP/${ASSET}"
tar -xzf "$TMP/${ASSET}" -C "$TMP"

# Create system group and user (idempotent).
if ! getent group "$GROUP" >/dev/null; then
    echo "Creating group $GROUP..."
    groupadd --system "$GROUP"
fi
if ! getent passwd "$USER" >/dev/null; then
    echo "Creating user $USER..."
    useradd --system --no-create-home --gid "$GROUP" "$USER"
fi

echo "Installing binary to ${BIN_DIR}/gh-deploy..."
install -m 0755 "$TMP/gh-deploy" "${BIN_DIR}/gh-deploy"

echo "Installing systemd unit to ${UNIT_PATH}..."
install -m 0644 "$TMP/gh-deploy.service" "$UNIT_PATH"

echo "Creating configuration directory ${CONFIG_DIR}..."
mkdir -p "$CONFIG_DIR"
chown "root:${GROUP}" "$CONFIG_DIR"
chmod 0755 "$CONFIG_DIR"

systemctl daemon-reload

cat <<EOF

gh-deploy installed successfully.

To register a GitHub App and generate the configuration, run:

  sudo gh-deploy setup --bind '<bind-address>' --public-url '<your-public-url>'

Run “gh-deploy setup --help” for all options and more information.

EOF
