#!/usr/bin/env bash
# Faithful CI reproduction for OCPBUGS-106186
#
# Mirrors ALL test images from quay.io/openshift/community-e2e-images
# to a local TLS registry, exactly as bare-metal CI does.
# This is 55 images mirrored concurrently via `oc image mirror -f <mapping>`.
#
# STDOUT: readable progress/status
# STDERR: oc image mirror debug output + registry logs
#
# Usage:
#   ./repro-ci.sh                # single run, see everything
#   ./repro-ci.sh 20             # 20 iterations, stop on first trigger
#   ./repro-ci.sh 20 2>debug.log # progress on terminal, debug to file
#
# Requirements: podman, openssl, oc
# No cluster needed.

set -euo pipefail

REGISTRY_PORT="${REGISTRY_PORT:-5000}"
REGISTRY_DIR="$(mktemp -d)"
CERT_DIR="${REGISTRY_DIR}/certs"
REGISTRY_NAME="repro-registry-ci"
DEST="localhost:${REGISTRY_PORT}/test/e2e"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
RUN_COUNT="${1:-1}"

cleanup() {
    echo "Cleaning up..."
    podman rm -f "${REGISTRY_NAME}" 2>/dev/null || true
    rm -rf "${REGISTRY_DIR}"
}
trap cleanup EXIT

start_registry() {
    podman rm -f "${REGISTRY_NAME}" 2>/dev/null || true
    podman run -d --name "${REGISTRY_NAME}" \
        -p "${REGISTRY_PORT}:5000" \
        -e REGISTRY_HTTP_TLS_CERTIFICATE=/certs/cert.pem \
        -e REGISTRY_HTTP_TLS_KEY=/certs/key.pem \
        -e REGISTRY_LOG_LEVEL=debug \
        -e REGISTRY_HTTP_HTTP2_DISABLED=false \
        -v "${CERT_DIR}:/certs:z" \
        quay.io/libpod/registry:2.8 >/dev/null
    sleep 2
}

echo "Generating TLS cert..."
mkdir -p "${CERT_DIR}"
openssl req -x509 -newkey rsa:4096 -keyout "${CERT_DIR}/key.pem" \
    -out "${CERT_DIR}/cert.pem" -days 1 -nodes \
    -subj "/CN=localhost" \
    -addext "subjectAltName=DNS:localhost,IP:127.0.0.1" 2>/dev/null

echo "Starting registry:2.8 on localhost:${REGISTRY_PORT} (TLS + HTTP/2)..."
start_registry

# Build the mirror mapping: community-e2e-images -> local registry
# This matches exactly what CI does: mirror FROM the community mirror
# TO a local registry, not from upstream sources.
MAPPING="${REGISTRY_DIR}/mirror-mapping.txt"

GEN_FILE="${SCRIPT_DIR}/../../../redhat/repos/liouk/origin/test/extended/util/image/zz_generated.txt"
if [[ ! -f "${GEN_FILE}" ]]; then
    GEN_FILE=$(find /home/liouk -path "*/test/extended/util/image/zz_generated.txt" -type f 2>/dev/null | head -1)
fi

if [[ -z "${GEN_FILE}" || ! -f "${GEN_FILE}" ]]; then
    echo "ERROR: Could not find zz_generated.txt"
    exit 1
fi

while IFS=' ' read -r upstream mirror; do
    [[ "${upstream}" == \#* ]] && continue
    [[ -z "${upstream}" ]] && continue
    tag="${mirror##*:}"
    echo "${mirror} ${DEST}:${tag}"
done < "${GEN_FILE}" > "${MAPPING}"

IMAGE_COUNT=$(grep -c . "${MAPPING}" 2>/dev/null || echo 0)
echo "Mirror mapping: ${IMAGE_COUNT} images (community-e2e-images -> localhost:${REGISTRY_PORT})"

if [[ "${IMAGE_COUNT}" -eq 0 ]]; then
    echo "ERROR: Empty mirror mapping"
    exit 1
fi

TRIGGERED=0
for i in $(seq 1 "${RUN_COUNT}"); do
    echo ""
    echo "=== Run ${i}/${RUN_COUNT} ==="

    if [[ ${i} -gt 1 ]]; then
        echo "Restarting registry..."
        start_registry
    fi

    # all oc output goes to STDERR; capture exit code without tripping set -e
    OC_EXIT=0
    { oc image mirror \
        -f "${MAPPING}" \
        --insecure=true; } >&2 2>&1 || OC_EXIT=$?

    # Check registry logs for the specific failure signature
    REGISTRY_OUTPUT=$(podman logs "${REGISTRY_NAME}" 2>&1)
    MATCHES=$(echo "${REGISTRY_OUTPUT}" | grep -iE "CANCEL|rst_stream|wrong.offset|copied=2097153" || true)

    if [[ -n "${MATCHES}" ]]; then
        echo ""
        echo ">>> BUG TRIGGERED on run ${i}!"
        echo ">>> Registry log matches:"
        echo "${MATCHES}" | head -5
        # Also dump full registry logs to STDERR for analysis
        echo "### registry logs (run ${i}) ###" >&2
        echo "${REGISTRY_OUTPUT}" >&2
        TRIGGERED=1
    fi

    if [[ ${OC_EXIT} -ne 0 ]]; then
        echo ">>> oc image mirror FAILED (exit ${OC_EXIT}) on run ${i}"
        # Dump filtered registry errors to STDERR
        echo "### registry error logs (run ${i}) ###" >&2
        echo "${REGISTRY_OUTPUT}" | grep -iE "error|cancel|rst_stream|wrong.offset|disconnect" >&2 || true
        TRIGGERED=1
    else
        echo "Run ${i}: mirror succeeded"
    fi

    if [[ ${TRIGGERED} -eq 1 ]]; then
        echo ""
        echo "Bug reproduced!"
        exit 0
    fi
done

echo ""
echo "Bug did NOT trigger in ${RUN_COUNT} runs"
echo "Try: ./repro-ci.sh 20"
