#!/bin/bash
# Upload tar content from disk to registry - AutoShift Enhanced
# Cache will be created fresh on this host - no cache transfer needed!

set -e

# Default configuration
CONFIG_FILE="${CONFIG_FILE:-imageset-config.yaml}"
CONTENT_DIR="${CONTENT_DIR:-content}"
CACHE_DIR="${CACHE_DIR:-${XDG_CACHE_HOME:-$HOME/.cache}}"
TARGET_REGISTRY="${TARGET_REGISTRY:-localhost:8443}"

# Parse command line arguments
while [[ $# -gt 0 ]]; do
    case $1 in
        -c|--config)
            CONFIG_FILE="$2"
            shift 2
            ;;
        --content-dir)
            CONTENT_DIR="$2"
            shift 2
            ;;
        --cache-dir)
            CACHE_DIR="$2"
            shift 2
            ;;
        -r|--registry)
            TARGET_REGISTRY="$2"
            shift 2
            ;;
        --dry-run)
            DRY_RUN="--dry-run"
            shift
            ;;
        -h|--help)
            echo "Usage: $0 [OPTIONS]"
            echo ""
            echo "Upload mirrored content from disk to disconnected registry"
            echo ""
            echo "Options:"
            echo "  -c, --config FILE       ImageSet configuration file (default: imageset-config.yaml)"
            echo "  --content-dir DIR       Content source directory (default: content)"
            echo "  --cache-dir DIR         Cache directory (default: \$XDG_CACHE_HOME or ~/.cache)"
            echo "  -r, --registry HOST     Target registry (default: localhost:8443)"
            echo "  --dry-run              Show what would be uploaded without transferring"
            echo "  -h, --help             Show this help message"
            echo ""
            echo "Environment Variables:"
            echo "  CONFIG_FILE            Default configuration file"
            echo "  CONTENT_DIR            Default content directory"
            echo "  CACHE_DIR              Default cache directory"
            echo "  TARGET_REGISTRY        Default target registry"
            echo ""
            echo "Examples:"
            echo "  $0                                         # Use defaults (localhost:8443)"
            echo "  $0 -r registry.example.com:443           # Custom registry"
            echo "  $0 -c imageset-autoshift.yaml            # Use AutoShift config"
            echo "  $0 --dry-run                              # Preview upload operations"
            exit 0
            ;;
        *)
            echo "Unknown option: $1"
            echo "Use --help for usage information"
            exit 1
            ;;
    esac
done

# Validate configuration file exists
if [[ ! -f "$CONFIG_FILE" ]]; then
    echo "❌ ERROR: Configuration file not found: $CONFIG_FILE"
    echo ""
    echo "Available ImageSet configurations:"
    find . -maxdepth 2 -name "imageset*.yaml" -type f 2>/dev/null || echo "  No imageset configurations found"
    exit 1
fi

# Validate content directory exists
if [[ ! -d "$CONTENT_DIR" ]]; then
    echo "❌ ERROR: Content directory not found: $CONTENT_DIR"
    echo ""
    echo "💡 Content directory should contain mirrored data from mirror-to-disk.sh"
    echo "   Available directories:"
    find . -maxdepth 2 -type d -name "*content*" 2>/dev/null || echo "  No content directories found"
    exit 1
fi

# Check for mirrored content
if [[ ! -d "$CONTENT_DIR/working-dir" ]]; then
    echo "❌ ERROR: No mirrored content found in $CONTENT_DIR"
    echo "💡 Run mirror-to-disk.sh first to create mirrored content"
    exit 1
fi

echo "📤 Uploading content from disk to registry..."
echo "📋 Config: $CONFIG_FILE"
echo "📁 Source: $CONTENT_DIR"
echo "🎯 Registry: $TARGET_REGISTRY"
echo "💾 Cache: $CACHE_DIR"
if [[ -n "$DRY_RUN" ]]; then
    echo "🔍 Mode: DRY RUN (no actual upload)"
fi
echo ""

# Show content summary
echo "📊 Content Summary:"
echo "   Size: $(du -sh "$CONTENT_DIR" 2>/dev/null | cut -f1 || echo 'Unknown')"
if [[ -d "$CONTENT_DIR/working-dir/.history" ]]; then
    echo "   Mirror operations: $(ls -1 "$CONTENT_DIR/working-dir/.history" | wc -l)"
    echo "   Latest operation: $(ls -1t "$CONTENT_DIR/working-dir/.history" | head -1 2>/dev/null || echo 'None')"
fi
echo ""

# Create cache directory if it doesn't exist
mkdir -p "$CACHE_DIR"

# Upload content to registry
oc-mirror -c "$CONFIG_FILE" \
    --from file://"$CONTENT_DIR" \
    docker://"$TARGET_REGISTRY" \
    --v2 \
    --cache-dir "$CACHE_DIR" \
    $DRY_RUN

echo ""
if [[ -n "$DRY_RUN" ]]; then
    echo "✅ Dry run completed successfully!"
    echo "🔍 Review the output above to see what would be uploaded"
else
    echo "✅ Upload to registry completed successfully!"
    echo "🗃️ Content uploaded to: $TARGET_REGISTRY"
    echo "💾 Local cache created at: $CACHE_DIR"
    echo "🌐 Registry accessible at: https://$TARGET_REGISTRY"
fi

echo ""
echo "💡 Next steps:"
if [[ -n "$DRY_RUN" ]]; then
    echo "   • Run without --dry-run to perform actual upload"
    echo "   • Verify registry authentication is configured"
else
    echo "   • Verify content: Browse https://$TARGET_REGISTRY"
    echo "   • Configure OpenShift clusters to use this registry"
    echo "   • Apply IDMS/ITMS configurations for cluster access"
    echo "   • Test image pulls: podman pull $TARGET_REGISTRY/openshift/release:latest"
fi
echo "   • Monitor registry storage: Check registry disk usage"
echo "   • Backup registry data: Consider registry backup procedures"