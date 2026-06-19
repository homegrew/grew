#!/bin/bash
# Build the Homegrew macOS installer package and disk image.
#
# Usage:
#   ./tools/build-installer.sh --binary <path> [--version <ver>] [--output-dir <dir>]
#
# Options:
#   --binary      Path to the universal grew binary to package (required)
#   --version     Version string for the pkg (default: git describe --tags --always)
#   --output-dir  Directory where artifacts are written (default: dist/)
#
# Outputs:
#   <output-dir>/Homegrew Installer.pkg
#   <output-dir>/Homegrew.dmg
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"

# Defaults
BINARY_PATH=""
VERSION=""
OUTPUT_DIR="${REPO_ROOT}/dist"

# Parse arguments
while [[ $# -gt 0 ]]; do
    case "$1" in
        --binary)
            BINARY_PATH="$2"
            shift 2
            ;;
        --version)
            VERSION="$2"
            shift 2
            ;;
        --output-dir)
            OUTPUT_DIR="$2"
            shift 2
            ;;
        *)
            echo "Unknown argument: $1" >&2
            exit 1
            ;;
    esac
done

if [ -z "${BINARY_PATH}" ]; then
    echo "error: --binary <path> is required" >&2
    exit 1
fi

if [ ! -f "${BINARY_PATH}" ]; then
    echo "error: binary not found: ${BINARY_PATH}" >&2
    exit 1
fi

# Derive version from git if not provided
if [ -z "$VERSION" ]; then
    VERSION=$(git -C "${REPO_ROOT}" describe --tags --always 2>/dev/null || echo "dev")
fi

echo "==> Building Homegrew installer version ${VERSION}"
echo "==> Binary: ${BINARY_PATH}"
echo "==> Output directory: ${OUTPUT_DIR}"

mkdir -p "${OUTPUT_DIR}"

# Stage the pkg payload: binary lands at /private/tmp/homegrew-setup/grew
PAYLOAD_ROOT="${OUTPUT_DIR}/installer-root"
BINARY_STAGING="${PAYLOAD_ROOT}/private/tmp/homegrew-setup"
rm -rf "${PAYLOAD_ROOT}"
mkdir -p "${BINARY_STAGING}"
cp "${BINARY_PATH}" "${BINARY_STAGING}/grew"
chmod 755 "${BINARY_STAGING}/grew"

# Stage the pkg scripts directory
SCRIPTS_DIR="${OUTPUT_DIR}/installer-scripts"
rm -rf "${SCRIPTS_DIR}"
mkdir -p "${SCRIPTS_DIR}"
cp "${SCRIPT_DIR}/installer/postinstall" "${SCRIPTS_DIR}/postinstall"
chmod 755 "${SCRIPTS_DIR}/postinstall"

# Build the component package
PKG_PATH="${OUTPUT_DIR}/Homegrew Installer.pkg"
echo "==> Building pkg..."
pkgbuild \
    --root "${PAYLOAD_ROOT}" \
    --scripts "${SCRIPTS_DIR}" \
    --identifier com.homegrew.grew.installer \
    --version "${VERSION}" \
    --install-location / \
    "${PKG_PATH}"

# Clean up staging directories
rm -rf "${PAYLOAD_ROOT}" "${SCRIPTS_DIR}"

# Create the disk image
DMG_STAGING="${OUTPUT_DIR}/dmg-staging"
rm -rf "${DMG_STAGING}"
mkdir -p "${DMG_STAGING}"
cp "${PKG_PATH}" "${DMG_STAGING}/Homegrew Installer.pkg"

DMG_PATH="${OUTPUT_DIR}/Homegrew.dmg"
echo "==> Creating disk image..."
hdiutil create \
    -volname "Homegrew" \
    -srcfolder "${DMG_STAGING}" \
    -ov \
    -format UDZO \
    "${DMG_PATH}"

rm -rf "${DMG_STAGING}"

echo ""
echo "==> Done."
echo "    Package:    ${PKG_PATH}"
echo "    Disk image: ${DMG_PATH}"
