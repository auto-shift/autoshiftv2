#!/bin/bash
set -e

echo "🔐 Setting up secrets for oc-mirror pipelines..."

# Check if namespaces exist
echo "📋 Checking namespaces..."
oc get namespace oc-mirror-pipeline >/dev/null 2>&1 || {
    echo "❌ Namespace oc-mirror-pipeline not found. Apply infrastructure.yaml first."
    exit 1
}
oc get namespace oc-mirror >/dev/null 2>&1 || {
    echo "❌ Namespace oc-mirror not found. Apply infrastructure.yaml first."
    exit 1
}

# 1. Create registry authentication secret for Quay (build pipeline)
echo "🏗️ Creating Quay registry authentication secret..."
if oc get secret quay-registry-auth -n oc-mirror-pipeline >/dev/null 2>&1; then
    echo "✅ Secret quay-registry-auth already exists"
else
    read -p "Enter Quay.io username: " QUAY_USERNAME
    read -s -p "Enter Quay.io password/token: " QUAY_PASSWORD
    echo ""

    oc create secret docker-registry quay-registry-auth \
        --docker-server=quay.io \
        --docker-username="$QUAY_USERNAME" \
        --docker-password="$QUAY_PASSWORD" \
        --namespace=oc-mirror-pipeline

    echo "✅ Created quay-registry-auth secret"
fi

# 2. Create OpenShift internal registry authentication secret (build pipeline)
echo "🏗️ Creating OpenShift internal registry authentication secret..."
if oc get secret openshift-registry-auth -n oc-mirror-pipeline >/dev/null 2>&1; then
    echo "✅ Secret openshift-registry-auth already exists"
else
    oc create secret docker-registry openshift-registry-auth \
        --docker-server=image-registry.openshift-image-registry.svc:5000 \
        --docker-username=unused \
        --docker-password="$(oc whoami -t)" \
        --namespace=oc-mirror-pipeline

    echo "✅ Created openshift-registry-auth secret"
fi

# 3. Create pull secret for oc-mirror workloads (deployment pipeline)
echo "🚀 Creating oc-mirror pull secret..."
if oc get secret oc-mirror-pull-secret -n oc-mirror-pipeline >/dev/null 2>&1; then
    echo "✅ Secret oc-mirror-pull-secret already exists"
else
    # Check if pull-secret.json exists
    if [[ -f "$(pwd)/pull-secret.json" ]]; then
        echo "📁 Using existing pull-secret.json"
        oc create secret generic oc-mirror-pull-secret \
            --from-file=.dockerconfigjson="$(pwd)/pull-secret.json" \
            --type=kubernetes.io/dockerconfigjson \
            --namespace=oc-mirror-pipeline
    else
        echo "⚠️  pull-secret.json not found. Please create it first with registry credentials."
        echo "📝 Example pull-secret.json structure:"
        cat << 'EOF'
{
  "auths": {
    "quay.io": {
      "auth": "base64(username:password)",
      "email": "your-email@example.com"
    },
    "your-target-registry.com": {
      "auth": "base64(username:password)",
      "email": "your-email@example.com"
    }
  }
}
EOF
        exit 1
    fi

    echo "✅ Created oc-mirror-pull-secret secret"
fi

# 4. Copy pull secret to oc-mirror namespace for workloads
echo "📋 Copying pull secret to oc-mirror namespace..."
if oc get secret oc-mirror-pull-secret -n oc-mirror >/dev/null 2>&1; then
    echo "✅ Pull secret already exists in oc-mirror namespace"
else
    oc get secret oc-mirror-pull-secret -n oc-mirror-pipeline -o yaml | \
        sed 's/namespace: oc-mirror-pipeline/namespace: oc-mirror/' | \
        oc apply -f -

    echo "✅ Copied pull secret to oc-mirror namespace"
fi

echo ""
echo "🎉 All secrets created successfully!"
echo ""
echo "📋 Summary of created secrets:"
echo "  oc-mirror-pipeline namespace:"
echo "    - quay-registry-auth (for build pipeline)"
echo "    - openshift-registry-auth (for build pipeline)"
echo "    - oc-mirror-pull-secret (for deployment pipeline)"
echo "  oc-mirror namespace:"
echo "    - oc-mirror-pull-secret (for oc-mirror workloads)"
echo ""
echo "🚀 Ready to deploy pipelines!"