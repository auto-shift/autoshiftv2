#!/bin/bash
# Mirror content directly from registry to registry (semi-connected flow) - AutoShift Enhanced
# Bypasses disk storage - direct registry-to-registry mirroring

set -e

# Default configuration
CONFIG_FILE="${CONFIG_FILE:-imageset-config.yaml}"
WORKSPACE_DIR="${WORKSPACE_DIR:-workspace}"
CACHE_DIR="${CACHE_DIR:-${XDG_CACHE_HOME:-$HOME/.cache}}"
TARGET_REGISTRY="${TARGET_REGISTRY:-localhost:8443}"
SINCE_FLAG=""
INCREMENTAL_MODE="false"
AUTO_SINCE="false"

# Parse command line arguments
while [[ $# -gt 0 ]]; do
    case $1 in
        -c|--config)
            CONFIG_FILE="$2"
            shift 2
            ;;
        --workspace-dir)
            WORKSPACE_DIR="$2"
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
        --since)
            SINCE_FLAG="--since $2"
            INCREMENTAL_MODE="true"
            shift 2
            ;;
        --incremental)
            INCREMENTAL_MODE="true"
            AUTO_SINCE="true"
            shift
            ;;
        --dry-run)
            DRY_RUN="--dry-run"
            shift
            ;;
        -h|--help)
            echo "Usage: $0 [OPTIONS]"
            echo ""
            echo "Mirror content directly from source registry to target registry (semi-connected)"
            echo ""
            echo "Options:"
            echo "  -c, --config FILE       ImageSet configuration file (default: imageset-config.yaml)"
            echo "  --workspace-dir DIR     Workspace directory (default: workspace)"
            echo "  --cache-dir DIR         Cache directory (default: \$XDG_CACHE_HOME or ~/.cache)"
            echo "  -r, --registry HOST     Target registry (default: localhost:8443)"
            echo "  --since DATE            Mirror only changes since date (YYYY-MM-DD or ISO format)"
            echo "  --incremental           Auto-detect last mirror date from .history files"
            echo "  --dry-run              Show what would be mirrored without transferring"
            echo "  -h, --help             Show this help message"
            echo ""
            echo "Environment Variables:"
            echo "  CONFIG_FILE            Default configuration file"
            echo "  WORKSPACE_DIR          Default workspace directory"
            echo "  CACHE_DIR              Default cache directory"
            echo "  TARGET_REGISTRY        Default target registry"
            echo ""
            echo "Examples:"
            echo "  $0 -r my-registry.com/mirror        # Mirror to registry"
            echo "  $0 --since 2025-09-01               # Mirror changes since September 1st"
            echo "  $0 --incremental                    # Auto-detect since date from last mirror"
            echo ""
            echo "Examples:"
            echo "  $0                                         # Use defaults (localhost:8443)"
            echo "  $0 -r registry.example.com:443           # Custom target registry"
            echo "  $0 -c imageset-autoshift.yaml            # Use AutoShift config"
            echo "  $0 --dry-run                              # Preview mirror operations"
            echo ""
            echo "Network Requirements:"
            echo "  • Access to source registries (registry.redhat.io, quay.io, etc.)"
            echo "  • Access to target registry ($TARGET_REGISTRY)"
            echo "  • Valid authentication for both source and target registries"
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
    echo ""
    echo "💡 To generate an AutoShift ImageSet configuration:"
    echo "   ./generate-imageset-config.sh values.hub.yaml --output imageset-autoshift.yaml"
    exit 1
fi

echo "🔄 Mirroring content directly to registry..."
echo "🎯 Target: docker://$TARGET_REGISTRY (direct registry)"
echo "📋 Config: $CONFIG_FILE"
echo "📁 Workspace: $WORKSPACE_DIR"
echo "💾 Cache: $CACHE_DIR"
if [[ -n "$DRY_RUN" ]]; then
    echo "🔍 Mode: DRY RUN (no actual mirroring)"
fi
echo ""

# Auto-detect --since flag from .history files if requested
if [[ "$AUTO_SINCE" == "true" || "$INCREMENTAL_MODE" == "true" && -z "$SINCE_FLAG" ]]; then
    HISTORY_DIR="$WORKSPACE_DIR/working-dir/.history"
    if [[ -d "$HISTORY_DIR" ]]; then
        # Find the most recent .history file
        LATEST_HISTORY=$(find "$HISTORY_DIR" -name ".history-*" -type f 2>/dev/null | sort | tail -1)
        if [[ -n "$LATEST_HISTORY" ]]; then
            # Extract date from filename: .history-2025-09-23T14:32:31Z
            HISTORY_DATE=$(basename "$LATEST_HISTORY" | sed 's/\.history-//' | cut -d'T' -f1)
            SINCE_FLAG="--since $HISTORY_DATE"
            echo "🔍 Auto-detected incremental mode from history: $HISTORY_DATE"
            echo "📂 History file: $LATEST_HISTORY"
            echo "📅 Since: $HISTORY_DATE"
        else
            echo "ℹ️  No .history files found - performing full mirror"
        fi
    else
        echo "ℹ️  No .history directory found - performing full mirror"
    fi
fi

# Show since flag if manually provided
if [[ -n "$SINCE_FLAG" && "$AUTO_SINCE" != "true" ]]; then
    echo "📅 Since: $(echo $SINCE_FLAG | cut -d' ' -f2)"
fi

# Network connectivity checks
echo "🌐 Checking network connectivity..."
if command -v curl &> /dev/null; then
    echo "   Checking Red Hat registry access..."
    if curl -s --connect-timeout 5 https://registry.redhat.io/v2/ &> /dev/null; then
        echo "   ✅ registry.redhat.io accessible"
    else
        echo "   ⚠️  registry.redhat.io not accessible (may affect mirroring)"
    fi
    
    echo "   Checking target registry access..."
    if curl -s --connect-timeout 5 -k https://$TARGET_REGISTRY/v2/ &> /dev/null; then
        echo "   ✅ $TARGET_REGISTRY accessible"
    else
        echo "   ⚠️  $TARGET_REGISTRY not accessible - verify registry is running"
    fi
else
    echo "   ⚠️  curl not available for connectivity checks"
fi
echo ""

# Create directories if they don't exist
mkdir -p "$WORKSPACE_DIR" "$CACHE_DIR"

# Backup imageset configuration if it exists
if [[ -f "$CONFIG_FILE" ]]; then
    BACKUP_DIR="$WORKSPACE_DIR/imageset-configs"
    mkdir -p "$BACKUP_DIR"
    TIMESTAMP=$(date +%Y%m%d-%H%M%S)
    BACKUP_FILE="$BACKUP_DIR/$(basename "$CONFIG_FILE" .yaml)-$TIMESTAMP.yaml"
    cp "$CONFIG_FILE" "$BACKUP_FILE"
    echo "📋 Backup created: $BACKUP_FILE"
fi

# Direct mirror from source registry to target registry
oc-mirror -c "$CONFIG_FILE" \
    --workspace file://"$WORKSPACE_DIR" \
    docker://"$TARGET_REGISTRY" \
    --v2 \
    --cache-dir "$CACHE_DIR" \
    $DRY_RUN

echo ""
if [[ -n "$DRY_RUN" ]]; then
    echo "✅ Dry run completed successfully!"
    echo "🔍 Review the output above to see what would be mirrored"
else
    echo "✅ Direct mirror to registry complete!"
    echo "🗃️ Content mirrored to: $TARGET_REGISTRY"
    echo "💾 Cache created at: $CACHE_DIR"
    echo "🌐 Registry accessible at: https://$TARGET_REGISTRY"
    echo "📋 ImageSet configs backed up in: $WORKSPACE_DIR/imageset-configs/"
fi

echo ""
echo "💡 Next steps:"
if [[ -n "$DRY_RUN" ]]; then
    echo "   • Run without --dry-run to perform actual mirroring"
    echo "   • Verify network access to source and target registries"
    echo "   • Check authentication for both registries"
else
    echo "   • Verify content: Browse https://$TARGET_REGISTRY"
    echo "   • Use for OpenShift installations or upgrades"
    echo "   • Apply IDMS/ITMS configurations for cluster access"
    echo "   • Test image pulls: podman pull $TARGET_REGISTRY/openshift/release:latest"
fi
echo "   • Monitor cache size: du -sh $CACHE_DIR"
echo "   • For incremental updates, run this script again with same configuration"
echo ""

# Show performance information
if [[ -z "$DRY_RUN" ]]; then
    echo "📈 Performance Information:"
    echo "   • Direct mirroring is fastest for semi-connected environments"
    echo "   • No intermediate disk storage required"
    echo "   • Network bandwidth is the primary bottleneck"
    echo "   • Cache helps with subsequent mirror operations"
    echo "   • Consider mirror-to-disk.sh for air-gapped environments"
fi