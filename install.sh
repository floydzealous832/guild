#!/bin/sh
# install.sh — POSIX shell installer for guild
#
# Downloads the matching release tarball + checksums.txt from GitHub,
# verifies SHA256 BEFORE extraction, and installs the binary to
# ~/.local/bin/guild by default.
#
# Supported: darwin, linux × amd64, arm64.
# Usage:
#   install.sh [--version vX.Y.Z] [--prefix DIR] [--help]
# Environment overrides:
#   GUILD_VERSION         — same as --version
#   GUILD_INSTALL_PREFIX  — same as --prefix
#
# Signature (cosign) verification is deliberately NOT performed here;
# this script checks SHA256 only. Power users who want full supply-chain
# verification via Sigstore's keyless cosign should follow the
# `cosign verify-blob` steps documented in .goreleaser.yml and
# SECURITY.md, then extract/install manually.
#
# No telemetry, no phone-home. The only network calls are to
# api.github.com (to resolve the latest tag) and to
# github.com/mathomhaus/guild/releases/download/... (for the tarball
# and checksums.txt).

set -eu

# ─── constants ───────────────────────────────────────────────────────
REPO="mathomhaus/guild"
BIN_NAME="guild"
DEFAULT_PREFIX="${HOME}/.local/bin"

# ─── cleanup trap ────────────────────────────────────────────────────
TMPDIR_INSTALL=""
cleanup() {
    if [ -n "${TMPDIR_INSTALL}" ] && [ -d "${TMPDIR_INSTALL}" ]; then
        rm -rf "${TMPDIR_INSTALL}"
    fi
}
trap cleanup EXIT INT HUP TERM

# ─── helpers ─────────────────────────────────────────────────────────
err() {
    printf '%s\n' "error: $*" >&2
}

die() {
    err "$*"
    exit 1
}

info() {
    printf '%s\n' "$*"
}

usage() {
    cat <<'EOF'
install.sh — install guild (https://github.com/mathomhaus/guild)

Usage:
  install.sh [--version vX.Y.Z] [--prefix DIR] [--help]

Options:
  --version vX.Y.Z   Install a specific release tag. Defaults to the
                     latest release on GitHub.
  --prefix DIR       Install the binary to DIR/guild. Defaults to
                     ~/.local/bin.
  --help, -h         Show this help and exit.

Environment:
  GUILD_VERSION          Same as --version. Useful when the GitHub API
                         is rate-limited.
  GUILD_INSTALL_PREFIX   Same as --prefix.

Examples:
  install.sh
  install.sh --version v0.1.0
  install.sh --prefix /usr/local/bin
  GUILD_VERSION=v0.1.0 install.sh

After install, register guild with your MCP client:
  guild mcp install

For signature (cosign) verification before install, see SECURITY.md
and the cosign verify-blob snippet in .goreleaser.yml.
EOF
}

# Run either sha256sum (Linux) or shasum -a 256 (macOS). Print the hex
# digest for the single file argument. Aborts if neither is installed.
sha256_of() {
    _file="$1"
    if command -v sha256sum >/dev/null 2>&1; then
        sha256sum "${_file}" | awk '{print $1}'
    elif command -v shasum >/dev/null 2>&1; then
        shasum -a 256 "${_file}" | awk '{print $1}'
    else
        die "neither sha256sum nor shasum found in PATH; cannot verify download"
    fi
}

# Fetch $1 (URL) to $2 (path). Prefer curl, fall back to wget.
fetch() {
    _url="$1"
    _out="$2"
    if command -v curl >/dev/null 2>&1; then
        # -f: fail on HTTP errors (non-2xx). -sS: silent but show errors.
        # -L: follow redirects.
        curl -fsSL "${_url}" -o "${_out}"
    elif command -v wget >/dev/null 2>&1; then
        wget -qO "${_out}" "${_url}"
    else
        die "neither curl nor wget found in PATH; cannot download release"
    fi
}

# ─── argument parsing ────────────────────────────────────────────────
VERSION="${GUILD_VERSION:-}"
PREFIX="${GUILD_INSTALL_PREFIX:-${DEFAULT_PREFIX}}"

while [ $# -gt 0 ]; do
    case "$1" in
        --version)
            [ $# -ge 2 ] || die "--version requires a value (e.g. v0.1.0)"
            VERSION="$2"
            shift 2
            ;;
        --version=*)
            VERSION="${1#--version=}"
            shift
            ;;
        --prefix)
            [ $# -ge 2 ] || die "--prefix requires a directory"
            PREFIX="$2"
            shift 2
            ;;
        --prefix=*)
            PREFIX="${1#--prefix=}"
            shift
            ;;
        -h|--help)
            usage
            exit 0
            ;;
        *)
            err "unknown argument: $1"
            usage >&2
            exit 2
            ;;
    esac
done

# ─── platform detection ──────────────────────────────────────────────
OS_RAW="$(uname -s 2>/dev/null || echo unknown)"
case "${OS_RAW}" in
    Darwin) OS="darwin" ;;
    Linux)  OS="linux"  ;;
    *)
        die "unsupported OS: ${OS_RAW} (guild ships binaries for darwin and linux; use 'go install github.com/mathomhaus/guild/cmd/guild@latest' on others)"
        ;;
esac

ARCH_RAW="$(uname -m 2>/dev/null || echo unknown)"
case "${ARCH_RAW}" in
    x86_64|amd64)  ARCH="amd64" ;;
    aarch64|arm64) ARCH="arm64" ;;
    *)
        die "unsupported architecture: ${ARCH_RAW} (supported: x86_64/amd64, aarch64/arm64)"
        ;;
esac

# ─── version resolution ──────────────────────────────────────────────
if [ -z "${VERSION}" ]; then
    info "resolving latest release from github.com/${REPO}..."
    LATEST_URL="https://api.github.com/repos/${REPO}/releases/latest"
    TMPDIR_INSTALL="$(mktemp -d 2>/dev/null || mktemp -d -t guild-install)"
    API_BODY="${TMPDIR_INSTALL}/latest.json"

    if ! fetch "${LATEST_URL}" "${API_BODY}"; then
        die "failed to query GitHub API (${LATEST_URL}). If you are rate-limited, set GUILD_VERSION=vX.Y.Z or pass --version."
    fi

    # Extract "tag_name": "vX.Y.Z" without a JSON parser.
    # grep -o handles the line; sed strips the surrounding syntax.
    VERSION="$(grep -o '"tag_name"[[:space:]]*:[[:space:]]*"[^"]*"' "${API_BODY}" \
        | head -n1 \
        | sed -e 's/.*"tag_name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/')"

    if [ -z "${VERSION}" ]; then
        # Detect the GitHub rate-limit signal and give a useful hint.
        if grep -q 'API rate limit exceeded' "${API_BODY}" 2>/dev/null; then
            die "GitHub API rate limit exceeded. Set GUILD_VERSION=vX.Y.Z (or pass --version) to bypass the API."
        fi
        die "could not parse tag_name from GitHub API response. Set GUILD_VERSION=vX.Y.Z (or pass --version) to bypass the API."
    fi
else
    TMPDIR_INSTALL="$(mktemp -d 2>/dev/null || mktemp -d -t guild-install)"
fi

# Strip a leading 'v' for the tarball filename (goreleaser uses .Version
# — without the v — in name_template).
VERSION_NUM="${VERSION#v}"

# ─── download ────────────────────────────────────────────────────────
TARBALL="${BIN_NAME}_${VERSION_NUM}_${OS}_${ARCH}.tar.gz"
CHECKSUMS="checksums.txt"
BASE_URL="https://github.com/${REPO}/releases/download/${VERSION}"

info "downloading ${TARBALL} (${VERSION})..."
if ! fetch "${BASE_URL}/${TARBALL}" "${TMPDIR_INSTALL}/${TARBALL}"; then
    die "failed to download ${BASE_URL}/${TARBALL} — does that release exist for ${OS}/${ARCH}?"
fi

info "downloading ${CHECKSUMS}..."
if ! fetch "${BASE_URL}/${CHECKSUMS}" "${TMPDIR_INSTALL}/${CHECKSUMS}"; then
    die "failed to download ${BASE_URL}/${CHECKSUMS}"
fi

# ─── verify SHA256 BEFORE extracting ─────────────────────────────────
EXPECTED="$(awk -v name="${TARBALL}" '$2 == name {print $1; exit}' "${TMPDIR_INSTALL}/${CHECKSUMS}")"
if [ -z "${EXPECTED}" ]; then
    die "no checksum entry for ${TARBALL} in ${CHECKSUMS}"
fi

ACTUAL="$(sha256_of "${TMPDIR_INSTALL}/${TARBALL}")"
if [ "${EXPECTED}" != "${ACTUAL}" ]; then
    err "SHA256 mismatch for ${TARBALL}"
    err "  expected: ${EXPECTED}"
    err "  actual:   ${ACTUAL}"
    exit 1
fi
info "sha256 verified"

# ─── extract + install ───────────────────────────────────────────────
EXTRACT_DIR="${TMPDIR_INSTALL}/extract"
mkdir -p "${EXTRACT_DIR}"

if ! tar -xzf "${TMPDIR_INSTALL}/${TARBALL}" -C "${EXTRACT_DIR}"; then
    die "failed to extract ${TARBALL}"
fi

if [ ! -f "${EXTRACT_DIR}/${BIN_NAME}" ]; then
    die "binary '${BIN_NAME}' not found inside ${TARBALL}"
fi

# Create install dir if missing (mkdir -p is POSIX).
if [ ! -d "${PREFIX}" ]; then
    mkdir -p "${PREFIX}" || die "failed to create ${PREFIX}"
fi

INSTALL_PATH="${PREFIX}/${BIN_NAME}"
# Install via cp; use a temp name then mv for an atomic swap on the
# same filesystem. Falls back to plain cp if rename across FS fails.
cp "${EXTRACT_DIR}/${BIN_NAME}" "${INSTALL_PATH}.tmp" \
    || die "failed to copy binary to ${INSTALL_PATH}.tmp"
chmod +x "${INSTALL_PATH}.tmp"
mv "${INSTALL_PATH}.tmp" "${INSTALL_PATH}" \
    || die "failed to move binary into place at ${INSTALL_PATH}"

# ─── post-install ────────────────────────────────────────────────────
info ""
info "installed ${BIN_NAME} ${VERSION} → ${INSTALL_PATH}"

# PATH check: does `command -v guild` resolve to the install path?
# If not, warn. We also check for a literal path component match to
# cover the case where another guild binary is ahead of us in PATH.
ON_PATH=0
if RESOLVED="$(command -v "${BIN_NAME}" 2>/dev/null)" && [ "${RESOLVED}" = "${INSTALL_PATH}" ]; then
    ON_PATH=1
else
    # Walk PATH to see if PREFIX is present as a component.
    _oldifs="${IFS}"
    IFS=":"
    for _p in ${PATH}; do
        if [ "${_p}" = "${PREFIX}" ]; then
            ON_PATH=1
            break
        fi
    done
    IFS="${_oldifs}"
fi

if [ "${ON_PATH}" -ne 1 ]; then
    info ""
    info "note: ${PREFIX} is not in your PATH."
    info "      add this line to your shell profile (~/.zshrc, ~/.bashrc, ~/.profile):"
    info "        export PATH=\"${PREFIX}:\$PATH\""
fi

info ""
info "next step:"
info "  ${BIN_NAME} mcp install   # register guild with your MCP client"
