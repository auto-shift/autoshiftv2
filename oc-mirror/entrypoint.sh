#!/bin/bash

# Setup authentication if pull-secret.txt exists
if [ -f "/workspace/pull-secret.txt" ]; then
    echo "Setting up container registry authentication..."
    
    # Use standard XDG location that oc-mirror expects
    AUTH_DIR="$XDG_RUNTIME_DIR/containers"
    AUTH_FILE="$AUTH_DIR/auth.json"
    
    # Create auth directory
    mkdir -p "$AUTH_DIR"
    
    # Copy and format pull secret
    if cat /workspace/pull-secret.txt | jq . > "$AUTH_FILE" 2>/dev/null; then
        echo "✅ Authentication configured at $AUTH_FILE"
        chmod 600 "$AUTH_FILE"
        # Note: Don't set REGISTRY_AUTH_FILE env var as it causes oc-mirror v2 parsing issues
        # oc-mirror will use default location or --authfile flag
    else
        echo "⚠️  Warning: Failed to configure authentication (invalid JSON in pull-secret.txt)"
    fi
fi

# Generate ImageSet configuration if requested
if [ "$1" = "--generate-imageset" ] || [ "$1" = "generate-imageset" ]; then
    echo "🔄 Generating ImageSet configuration from AutoShift values..."
    cd /workspace
    
    if [ -f "autoshift/values.hub.yaml" ]; then
        ./scripts/generate-imageset-config.sh \
            values.hub.yaml \
            --output imageset-autoshift.yaml
        
        if [ -f "imageset-autoshift.yaml" ]; then
            echo "✅ Generated imageset-autoshift.yaml"
            echo ""
            echo "📋 Next steps:"
            echo "1. Review imageset-autoshift.yaml"
            echo "2. Run: oc-mirror -c imageset-autoshift.yaml file://mirror --v2"
            echo "3. Or run: oc-mirror -c imageset-autoshift.yaml docker://your-registry --v2"
            echo ""
        else
            echo "❌ Failed to generate ImageSet configuration"
            exit 1
        fi
    else
        echo "❌ AutoShift values file not found: autoshift/values.hub.yaml"
        exit 1
    fi
    
    # Don't continue to oc-mirror
    exit 0
fi

# If no special command, run the provided arguments
if [ $# -eq 0 ]; then
    # Default to bash for interactive use
    exec /bin/bash
elif [ "$1" = "bash" ] || [ "$1" = "/bin/bash" ]; then
    # Run bash with any additional arguments
    exec "$@"
else
    # Run oc-mirror with provided arguments
    exec /usr/local/bin/oc-mirror "$@"
fi