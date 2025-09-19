#!/bin/bash

# Parse arguments for values file
parse_values_file() {
    local values_file=""
    
    # Look for --values-file parameter
    while [[ $# -gt 0 ]]; do
        case $1 in
            --values-file)
                values_file="$2"
                shift 2
                ;;
            *)
                shift
                ;;
        esac
    done
    
    # Default locations to check in order of preference
    local default_locations=(
        "/config/values.yaml"                    # ConfigMap mount
        "/workspace/autoshift/values.hub.yaml"  # Git repository location
        "autoshift/values.hub.yaml"             # Local relative path
        "values.hub.yaml"                       # Current directory
    )
    
    # If values file was specified, check if it exists
    if [ -n "$values_file" ]; then
        if [ -f "$values_file" ]; then
            echo "$values_file"
            return 0
        else
            echo "❌ Specified values file not found: $values_file" >&2
            exit 1
        fi
    fi
    
    # Check default locations
    for location in "${default_locations[@]}"; do
        if [ -f "$location" ]; then
            echo "$location"
            return 0
        fi
    done
    
    echo "❌ No AutoShift values file found. Checked:" >&2
    printf "   %s\n" "${default_locations[@]}" >&2
    echo "💡 Use --values-file to specify a custom location" >&2
    exit 1
}

# Setup authentication from pull secret
setup_authentication() {
    echo "🔍 Setting up container registry authentication..."
    
    # Use standard XDG location that oc-mirror expects
    AUTH_DIR="$XDG_RUNTIME_DIR/containers"
    AUTH_FILE="$AUTH_DIR/auth.json"
    
    # Create auth directory
    mkdir -p "$AUTH_DIR"
    
    # Check for pull secret at standard location
    if [ -f "/workspace/pull-secret.txt" ]; then
        echo "📋 Found pull secret at /workspace/pull-secret.txt"
        if cat /workspace/pull-secret.txt | jq . > "$AUTH_FILE" 2>/dev/null; then
            echo "✅ Authentication configured successfully"
            chmod 600 "$AUTH_FILE"
        else
            echo "⚠️  Warning: Invalid JSON in pull secret"
        fi
    else
        echo "⚠️  No pull secret found at /workspace/pull-secret.txt"
        echo "📝 For podman/docker: -v /path/to/pull-secret.json:/workspace/pull-secret.txt:ro"
        echo "📝 For Kubernetes: Secret mounted at /workspace/pull-secret.txt"
        echo "ℹ️  oc-mirror will use default authentication if available"
    fi
    
    # Note: Don't set REGISTRY_AUTH_FILE env var as it causes oc-mirror v2 parsing issues
    # oc-mirror will use default location or --authfile flag
}

# Show available workflow commands
show_workflows() {
    echo "🔧 AutoShift oc-mirror Container - Available Workflows:"
    echo ""
    echo "📋 ImageSet Management:"
    echo "  generate-imageset [--values-file FILE] [options]        Generate ImageSet from AutoShift values"
    echo "  generate-delete-imageset [--values-file FILE] [options] Generate DeleteImageSet from AutoShift values"
    echo ""
    echo "🔄 Mirroring Workflows:"
    echo "  mirror-to-disk [--values-file FILE] [options]          Mirror registry content to disk (air-gapped)"
    echo "  disk-to-mirror [--values-file FILE] [options]          Upload disk content to registry"
    echo "  mirror-to-mirror [--values-file FILE] [options]        Direct registry-to-registry mirroring"
    echo ""
    echo "🗑️  Image Lifecycle Management:"
    echo "  delete-generate [--values-file FILE] [options]         Generate safe deletion plan for old images"
    echo "  delete-execute [--values-file FILE] [options]          Execute deletion plan (PERMANENT!)"
    echo ""
    echo "🚀 Combined Workflows:"
    echo "  workflow-to-disk [workflow-options] [mirror-options]        Values → ImageSet → Disk"
    echo "  workflow-from-disk [options]                                Disk → Registry"
    echo "  workflow-direct [workflow-options] [mirror-options]         Values → ImageSet → Registry"
    echo "  workflow-delete-generate [workflow-options] [delete-options] Values → DeleteImageSet → Plan"
    echo "  workflow-cleanup [options]                                  Generate delete plan from existing config"
    echo ""
    echo "🔧 Workflow Options (for combined workflows):"
    echo "  --values-file FILE        Use specific values file (default: values.hub.yaml)"
    echo "  --operators-only          Generate operators-only ImageSet"
    echo "  --openshift-only          Generate platform-only ImageSet"
    echo "  --openshift-version X.Y   Override OpenShift version"
    echo "  --delete-older-than SPAN  Delete versions older than timespan (e.g., 90d, 6m)"
    echo "  --clean-cache             Clean oc-mirror cache before operation"
    echo ""
    echo "⚙️  Container Operations:"
    echo "  workflows                         Show this help message"
    echo "  bash                             Interactive shell"
    echo "  [oc-mirror args]                 Pass through to oc-mirror directly"
    echo ""
    echo "💡 Use --help with any workflow command for detailed options"
    echo "   Example: mirror-to-disk --help"
}

# Generate ImageSet configuration if requested
if [ "$1" = "--generate-imageset" ] || [ "$1" = "generate-imageset" ]; then
    shift # Remove the command name
    setup_authentication
    echo "🔄 Generating ImageSet configuration from AutoShift values..."
    cd /workspace
    
    if [ -f "autoshift/values.hub.yaml" ]; then
        ./generate-imageset-config.sh \
            values.hub.yaml \
            --output imageset-autoshift.yaml \
            "$@"
        
        if [ -f "imageset-autoshift.yaml" ]; then
            echo "✅ Generated imageset-autoshift.yaml"
            echo ""
            echo "📋 Next steps:"
            echo "1. Review imageset-autoshift.yaml"
            echo "2. Run: mirror-to-disk -c imageset-autoshift.yaml"
            echo "3. Or run: mirror-to-mirror -c imageset-autoshift.yaml -r your-registry:443"
            echo "4. Or run: oc-mirror -c imageset-autoshift.yaml file://mirror --v2"
            echo ""
        else
            echo "❌ Failed to generate ImageSet configuration"
            exit 1
        fi
    else
        echo "❌ AutoShift values file not found: autoshift/values.hub.yaml"
        exit 1
    fi
    
    exit 0
fi

# Generate Delete ImageSet configuration if requested
if [ "$1" = "--generate-delete-imageset" ] || [ "$1" = "generate-delete-imageset" ]; then
    shift # Remove the command name
    setup_authentication
    echo "🗑️ Generating DeleteImageSet configuration from AutoShift values..."
    cd /workspace
    
    if [ -f "autoshift/values.hub.yaml" ]; then
        ./generate-imageset-config.sh \
            values.hub.yaml \
            --delete-mode \
            --output imageset-delete-autoshift.yaml \
            "$@"
        
        if [ -f "imageset-delete-autoshift.yaml" ]; then
            echo "✅ Generated imageset-delete-autoshift.yaml"
            echo ""
            echo "📋 Next steps:"
            echo "1. Review imageset-delete-autoshift.yaml"
            echo "2. Run: delete-generate -c imageset-delete-autoshift.yaml"
            echo "3. Review deletion plan, then run: delete-execute"
            echo "4. Or use: workflow-delete-generate for automated workflow"
            echo ""
        else
            echo "❌ Failed to generate DeleteImageSet configuration"
            exit 1
        fi
    else
        echo "❌ AutoShift values file not found: autoshift/values.hub.yaml"
        exit 1
    fi
    
    exit 0
fi

# Handle workflow commands
setup_authentication
cd /workspace

case "$1" in
    "workflows"|"--workflows"|"help"|"--help")
        show_workflows
        exit 0
        ;;
    "mirror-to-disk")
        shift
        VALUES_FILE=$(parse_values_file "$@")
        exec ./mirror-to-disk.sh --values-file "$VALUES_FILE" "$@"
        ;;
    "disk-to-mirror")
        shift
        VALUES_FILE=$(parse_values_file "$@")
        exec ./disk-to-mirror.sh --values-file "$VALUES_FILE" "$@"
        ;;
    "mirror-to-mirror")
        shift
        VALUES_FILE=$(parse_values_file "$@")
        exec ./mirror-to-mirror.sh --values-file "$VALUES_FILE" "$@"
        ;;
    "delete-generate")
        shift
        VALUES_FILE=$(parse_values_file "$@")
        exec ./delete-generate.sh --values-file "$VALUES_FILE" "$@"
        ;;
    "delete-execute")
        shift
        VALUES_FILE=$(parse_values_file "$@")
        exec ./delete-execute.sh --values-file "$VALUES_FILE" "$@"
        ;;
    "workflow-to-disk")
        # Combined workflow: generate imageset + mirror to disk
        shift # Remove workflow command
        
        # Find the values file
        VALUES_FILE=$(parse_values_file "$@")
        
        # Parse workflow-specific arguments
        IMAGESET_ARGS=""
        MIRROR_ARGS=""
        
        while [[ $# -gt 0 ]]; do
            case $1 in
                --values-file)
                    # Skip values file argument since we already parsed it
                    shift 2
                    ;;
                --operators-only|--openshift-only|--delete-mode)
                    IMAGESET_ARGS="$IMAGESET_ARGS $1"
                    shift
                    ;;
                --openshift-version|--min-version|--max-version|--output|--delete-older-than|--delete-keep-last)
                    IMAGESET_ARGS="$IMAGESET_ARGS $1 $2"
                    shift 2
                    ;;
                --clean-cache)
                    echo "🧹 Cleaning cache before mirror operation..."
                    rm -rf /workspace/cache/*
                    echo "✅ Cache cleaned"
                    shift
                    ;;
                *)
                    # Pass remaining args to mirror-to-disk
                    MIRROR_ARGS="$MIRROR_ARGS $1"
                    shift
                    ;;
            esac
        done
        
        echo "🚀 AutoShift Workflow: Values → ImageSet → Disk"
        echo "📋 Values file: $VALUES_FILE"
        [[ -n "$IMAGESET_ARGS" ]] && echo "⚙️  ImageSet args: $IMAGESET_ARGS"
        [[ -n "$MIRROR_ARGS" ]] && echo "⚙️  Mirror args: $MIRROR_ARGS"
        
        if [ -f "$VALUES_FILE" ]; then
            echo "1️⃣ Generating ImageSet configuration..."
            ./generate-imageset-config.sh "$VALUES_FILE" --output imageset-autoshift.yaml $IMAGESET_ARGS
            if [ -f "imageset-autoshift.yaml" ]; then
                echo "2️⃣ Mirroring to disk..."
                exec ./mirror-to-disk.sh -c imageset-autoshift.yaml $MIRROR_ARGS
            else
                echo "❌ Failed to generate ImageSet configuration"
                exit 1
            fi
        else
            echo "❌ AutoShift values file not found: autoshift/$VALUES_FILE"
            echo "💡 Available values files:"
            ls -1 autoshift/values*.yaml 2>/dev/null || echo "   No values files found"
            exit 1
        fi
        ;;
    "workflow-from-disk")
        # Combined workflow: disk to registry
        echo "🚀 AutoShift Workflow: Disk → Registry"
        shift
        exec ./disk-to-mirror.sh "$@"
        ;;
    "workflow-direct")
        # Combined workflow: generate imageset + direct mirror
        shift # Remove workflow command
        
        # Parse workflow-specific arguments
        VALUES_FILE="values.hub.yaml"
        IMAGESET_ARGS=""
        MIRROR_ARGS=""
        
        while [[ $# -gt 0 ]]; do
            case $1 in
                --values-file)
                    VALUES_FILE="$2"
                    shift 2
                    ;;
                --operators-only|--openshift-only|--delete-mode)
                    IMAGESET_ARGS="$IMAGESET_ARGS $1"
                    shift
                    ;;
                --openshift-version|--min-version|--max-version|--output|--delete-older-than|--delete-keep-last)
                    IMAGESET_ARGS="$IMAGESET_ARGS $1 $2"
                    shift 2
                    ;;
                --clean-cache)
                    echo "🧹 Cleaning cache before mirror operation..."
                    rm -rf /workspace/cache/*
                    echo "✅ Cache cleaned"
                    shift
                    ;;
                *)
                    # Pass remaining args to mirror-to-mirror
                    MIRROR_ARGS="$MIRROR_ARGS $1"
                    shift
                    ;;
            esac
        done
        
        echo "🚀 AutoShift Workflow: Values → ImageSet → Registry"
        echo "📋 Values file: $VALUES_FILE"
        [[ -n "$IMAGESET_ARGS" ]] && echo "⚙️  ImageSet args: $IMAGESET_ARGS"
        [[ -n "$MIRROR_ARGS" ]] && echo "⚙️  Mirror args: $MIRROR_ARGS"
        
        if [ -f "autoshift/$VALUES_FILE" ]; then
            echo "1️⃣ Generating ImageSet configuration..."
            ./generate-imageset-config.sh "$VALUES_FILE" --output imageset-autoshift.yaml $IMAGESET_ARGS
            if [ -f "imageset-autoshift.yaml" ]; then
                echo "2️⃣ Mirroring directly to registry..."
                exec ./mirror-to-mirror.sh -c imageset-autoshift.yaml $MIRROR_ARGS
            else
                echo "❌ Failed to generate ImageSet configuration"
                exit 1
            fi
        else
            echo "❌ AutoShift values file not found: autoshift/$VALUES_FILE"
            echo "💡 Available values files:"
            ls -1 autoshift/values*.yaml 2>/dev/null || echo "   No values files found"
            exit 1
        fi
        ;;
    "workflow-cleanup")
        # Combined workflow: generate delete config + cleanup
        echo "🚀 AutoShift Workflow: Generate Delete Plan"
        shift
        if [ -f "imageset-delete.yaml" ]; then
            exec ./delete-generate.sh -c imageset-delete.yaml "$@"
        else
            echo "❌ Delete configuration not found: imageset-delete.yaml"
            echo "💡 First generate delete config: generate-delete-imageset"
            exit 1
        fi
        ;;
    "workflow-delete-generate")
        # Combined workflow: values → delete imageset → delete plan
        shift # Remove workflow command
        
        # Parse workflow-specific arguments
        VALUES_FILE="values.hub.yaml"
        IMAGESET_ARGS="--delete-mode"  # Always include delete mode for this workflow
        DELETE_ARGS=""
        
        while [[ $# -gt 0 ]]; do
            case $1 in
                --values-file)
                    VALUES_FILE="$2"
                    shift 2
                    ;;
                --operators-only|--openshift-only)
                    IMAGESET_ARGS="$IMAGESET_ARGS $1"
                    shift
                    ;;
                --openshift-version|--min-version|--max-version|--output|--delete-older-than|--delete-keep-last)
                    IMAGESET_ARGS="$IMAGESET_ARGS $1 $2"
                    shift 2
                    ;;
                --clean-cache)
                    echo "🧹 Cleaning cache before delete operation..."
                    rm -rf /workspace/cache/*
                    echo "✅ Cache cleaned"
                    shift
                    ;;
                *)
                    # Pass remaining args to delete-generate
                    DELETE_ARGS="$DELETE_ARGS $1"
                    shift
                    ;;
            esac
        done
        
        echo "🚀 AutoShift Workflow: Values → DeleteImageSet → Delete Plan"
        echo "📋 Values file: $VALUES_FILE"
        [[ -n "$IMAGESET_ARGS" ]] && echo "⚙️  ImageSet args: $IMAGESET_ARGS"
        [[ -n "$DELETE_ARGS" ]] && echo "⚙️  Delete args: $DELETE_ARGS"
        
        if [ -f "autoshift/$VALUES_FILE" ]; then
            echo "1️⃣ Generating DeleteImageSet configuration..."
            ./generate-imageset-config.sh $VALUES_FILE --output imageset-delete-autoshift.yaml $IMAGESET_ARGS
            if [ -f "imageset-delete-autoshift.yaml" ]; then
                echo "2️⃣ Generating deletion plan..."
                exec ./delete-generate.sh -c imageset-delete-autoshift.yaml $DELETE_ARGS
            else
                echo "❌ Failed to generate DeleteImageSet configuration"
                exit 1
            fi
        else
            echo "❌ AutoShift values file not found: autoshift/$VALUES_FILE"
            echo "💡 Available values files:"
            ls -1 autoshift/values*.yaml 2>/dev/null || echo "   No values files found"
            exit 1
        fi
        ;;
    "generate-delete-imageset")
        shift
        # Parse values file argument or use default
        VALUES_FILE="values.hub.yaml"
        if [[ $# -gt 0 && ! "$1" =~ ^-- ]]; then
            VALUES_FILE="$1"
            shift
        fi
        exec ./generate-imageset-config.sh $VALUES_FILE --delete-mode "$@"
        ;;
esac

# Handle remaining cases
if [ $# -eq 0 ]; then
    # Default to showing workflows help
    show_workflows
    echo ""
    echo "🐚 Starting interactive shell..."
    exec /bin/bash
elif [ "$1" = "bash" ] || [ "$1" = "/bin/bash" ]; then
    # Run bash with any additional arguments
    exec "$@"
else
    # Run oc-mirror with provided arguments
    echo "🔧 Running oc-mirror with arguments: $@"
    exec /usr/local/bin/oc-mirror "$@"
fi