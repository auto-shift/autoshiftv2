#!/bin/bash
# Print AutoShift values from the cluster resources oc-mirror v2 writes, ready to paste into a
# clusterset or cluster values file:
#
#   config.update-service           from updateService.yaml (written when graph: true)
#   config.disconnected.releaseSignatures
#                                   from signature-configmap.json, one entry per release digest
#
# Usage: ./scripts/oc-mirror-to-values.sh <oc-mirror working-dir>/cluster-resources
#
# Release versions are read from the signature file names under <working-dir>/signatures when that
# directory exists. An entry without one still works; version is only for people reading the list.
#
# Requires jq and yq. Runs on macOS (bash 3.2, BSD tools) and RHEL.

set -euo pipefail

usage() {
    echo "Usage: $0 <oc-mirror working-dir>/cluster-resources" >&2
    exit 1
}

[[ $# -eq 1 ]] || usage
resources_dir="${1%/}"
[[ -d "$resources_dir" ]] || {
    echo "Error: $resources_dir is not a directory" >&2
    exit 1
}

for tool in jq yq; do
    if ! command -v "$tool" >/dev/null 2>&1; then
        echo "Error: $tool is required" >&2
        exit 1
    fi
done

update_service_file="$resources_dir/updateService.yaml"
signature_file="$resources_dir/signature-configmap.json"
signatures_dir="$(dirname "$resources_dir")/signatures"

if [[ ! -f "$update_service_file" && ! -f "$signature_file" ]]; then
    echo "Error: found neither updateService.yaml nor signature-configmap.json in $resources_dir" >&2
    exit 1
fi

# Version for a digest, from a signature file named <version>-<arch>-sha256-<hex>. Empty when the
# signatures directory or a matching file is absent.
version_for() {
    local hex="$1" match name
    [[ -d "$signatures_dir" ]] || return 0
    match=$(find "$signatures_dir" -type f -name "*-sha256-$hex*" 2>/dev/null | head -1)
    [[ -n "$match" ]] || return 0
    name=$(basename "$match")
    name="${name%%-sha256-*}"
    echo "${name%-*}"
}

echo "config:"

if [[ -f "$update_service_file" ]]; then
    releases=$(yq -r 'select(.kind == "UpdateService") | .spec.releases // ""' "$update_service_file" | head -1)
    graph_image=$(yq -r 'select(.kind == "UpdateService") | .spec.graphDataImage // ""' "$update_service_file" | head -1)
    if [[ -z "$releases" || -z "$graph_image" ]]; then
        echo "Error: $update_service_file has no UpdateService with spec.releases and spec.graphDataImage" >&2
        exit 1
    fi
    echo "  update-service:"
    echo "    releases: '$releases'"
    echo "    graphDataImage: '$graph_image'"
else
    echo "# No updateService.yaml in $resources_dir; set graph: true in the ImageSetConfiguration to get one." >&2
fi

if [[ -f "$signature_file" ]]; then
    # One line per signature: <hex> <index> <base64>, sorted by digest then index.
    entries=$(jq -r '
        .binaryData // {} | to_entries[]
        | (.key | capture("^sha256-(?<hex>[a-f0-9]{64})-(?<n>[0-9]+)$")) as $k
        | "\($k.hex) \($k.n) \(.value)"' "$signature_file" | sort -k1,1 -k2,2n)
    if [[ -z "$entries" ]]; then
        echo "Error: $signature_file has no binaryData keys of the form sha256-<hex>-<n>" >&2
        exit 1
    fi
    echo "  disconnected:"
    echo "    releaseSignatures:"
    previous=""
    while read -r hex _ signature; do
        if [[ "$hex" != "$previous" ]]; then
            version=$(version_for "$hex")
            if [[ -n "$version" ]]; then
                echo "      - version: '$version'"
                echo "        digest: 'sha256:$hex'"
            else
                echo "      - digest: 'sha256:$hex'"
            fi
            echo "        signatures:"
            previous="$hex"
        fi
        echo "          - '$signature'"
    done <<< "$entries"
else
    echo "# No signature-configmap.json in $resources_dir." >&2
fi
