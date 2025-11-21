#!/bin/bash
# Create all AutoShift repositories in Quay.io
# Usage: ./create-quay-repos.sh <quay-token> <organization>

set -e

QUAY_TOKEN="${1:-}"
ORG="${2:-autoshift}"

if [ -z "$QUAY_TOKEN" ]; then
    echo "Usage: $0 <quay-token> [organization]"
    echo ""
    echo "Get your token from: https://quay.io/organization/$ORG?tab=applications"
    echo "Create an OAuth Application with 'Create Repositories' permission"
    exit 1
fi

API_URL="https://quay.io/api/v1"

echo "Creating repositories in organization: $ORG"
echo ""

# Discover all charts
BOOTSTRAP_CHARTS=$(find . -maxdepth 2 -name Chart.yaml -not -path "./policies/*" -not -path "./autoshift/*" -exec dirname {} \; | xargs -n1 basename)
POLICY_CHARTS=$(find policies -maxdepth 2 -name Chart.yaml -exec dirname {} \; | xargs -n1 basename)

# Function to create repository
create_repo() {
    local repo_name=$1
    echo -n "Creating $repo_name... "

    response=$(curl -s -X POST \
        "${API_URL}/repository" \
        -H "Authorization: Bearer ${QUAY_TOKEN}" \
        -H "Content-Type: application/json" \
        -d '{
            "repository": "'${repo_name}'",
            "visibility": "public",
            "namespace": "'${ORG}'",
            "description": "AutoShift Helm Chart: '${repo_name}'",
            "repo_kind": "image"
        }')

    if echo "$response" | grep -q '"name"'; then
        echo "✓ Created"
    elif echo "$response" | grep -q "already exists"; then
        echo "⊙ Already exists"
    else
        echo "✗ Failed"
        echo "Response: $response"
    fi
}

# Create bootstrap charts (in bootstrap/ namespace)
echo "Bootstrap charts:"
for chart in $BOOTSTRAP_CHARTS; do
    create_repo "bootstrap/$chart"
done

echo ""
echo "Main chart:"
create_repo "autoshift"

echo ""
echo "Policy charts (in policies/ namespace):"
for chart in $POLICY_CHARTS; do
    create_repo "policies/$chart"
done

echo ""
echo "========================================="
echo "Repository creation complete!"
echo "========================================="
echo ""
echo "Verify at: https://quay.io/organization/$ORG"
