#!/usr/bin/env bash
set -euo pipefail

usage() {
    cat <<EOF
Usage: $0 <version>

Examples:
  $0 0.1.0
  $0
EOF
}

while getopts ":h" opt; do
    case $opt in
        h) usage; exit 0 ;;
        *) usage; exit 1 ;;
    esac
done
shift $((OPTIND - 1))

INPUT="${1:-}"

for cmd in gh git; do
    if ! command -v "$cmd" >/dev/null 2>&1; then
        echo "Error: $cmd is required"
        exit 1
    fi
done

if ! gh auth status >/dev/null 2>&1; then
    echo "Error: gh must be authenticated"
    exit 1
fi

BRANCH=$(git rev-parse --abbrev-ref HEAD)
if [[ "$BRANCH" != "main" ]]; then
    echo "Error: must be on main branch (currently on $BRANCH)"
    exit 1
fi

REPO_ROOT=$(git rev-parse --show-toplevel)
cd "$REPO_ROOT"

if ! git diff --quiet || ! git diff --cached --quiet; then
    echo "Error: working tree must be clean"
    exit 1
fi

git fetch origin main --tags --quiet
LOCAL=$(git rev-parse HEAD)
REMOTE=$(git rev-parse origin/main)
if [[ "$LOCAL" != "$REMOTE" ]]; then
    echo "Error: local main is not up to date with origin/main"
    echo "  local:  $LOCAL"
    echo "  remote: $REMOTE"
    exit 1
fi

PREV=$(git tag -l 'v[0-9]*.[0-9]*.[0-9]*' | sort -V | tail -n1)

if [[ -z "$INPUT" ]]; then
    echo ""
    if [[ -n "$PREV" ]]; then
        echo "Current release: ${PREV}"
    else
        echo "Current release: (none)"
    fi
    echo ""
    read -rp "Enter new version: " INPUT
fi

if [[ -z "$INPUT" ]]; then
    echo "Error: version cannot be empty"
    exit 1
fi

INPUT="${INPUT#v}"
if [[ ! "$INPUT" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
    echo "Error: version must be a valid semver (e.g. 0.1.0)"
    exit 1
fi

VERSION="v${INPUT}"
IMAGE="ghcr.io/gaucho-racing/vault-k8s-operator"

if git tag -l "$VERSION" | grep -q "^${VERSION}$"; then
    echo "Error: tag $VERSION already exists"
    exit 1
fi

if git ls-remote --exit-code --tags origin "refs/tags/${VERSION}" >/dev/null 2>&1; then
    echo "Error: remote tag $VERSION already exists"
    exit 1
fi

if gh release view "$VERSION" >/dev/null 2>&1; then
    echo "Error: release $VERSION already exists"
    exit 1
fi

echo ""
echo "=== Release Summary ==="
if [[ -n "$PREV" ]]; then
    echo "  Previous release: ${PREV}"
else
    echo "  Previous release: (none)"
fi
echo "  Version:     ${VERSION}"
echo "  Commit:      $(git rev-parse --short HEAD)"
echo "  Branch:      main"
echo ""
echo "  Files to update:"
echo "    config/manager/manager.yaml"
echo ""
echo "  Docker image that will be tagged:"
echo "    ${IMAGE}:${INPUT}"
echo ""
read -rp "Proceed? (y/N) " CONFIRM
if [[ "$CONFIRM" != "y" && "$CONFIRM" != "Y" ]]; then
    echo "Aborted."
    exit 0
fi

sed -i '' "s#image: ${IMAGE}:.*#image: ${IMAGE}:${INPUT}#" "${REPO_ROOT}/config/manager/manager.yaml"

if git diff --quiet -- config/manager/manager.yaml; then
    if ! grep -Fq "image: ${IMAGE}:${INPUT}" "${REPO_ROOT}/config/manager/manager.yaml"; then
        echo "Error: failed to update config/manager/manager.yaml image"
        exit 1
    fi
    echo "config/manager/manager.yaml already references ${IMAGE}:${INPUT}"
else
    git add config/manager/manager.yaml
    git commit -m "release: vault-k8s-operator ${VERSION}"
    git push origin main
fi

LOCAL=$(git rev-parse HEAD)

gh release create "$VERSION" \
    --target "$LOCAL" \
    --title "$VERSION" \
    --generate-notes \
    --fail-on-no-commits

echo ""
echo "Done. ${VERSION} released. The operator workflow will publish ${IMAGE}:${INPUT} shortly."
