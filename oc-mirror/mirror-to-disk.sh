#!/bin/bash
# Mirror content from registry to disk (m2d) - AutoShift Enhanced
#
# This will download all content based on the imageset configuration to the local cache directory
# It will also create tar file(s) in the content directory for air-gapped transport
#   Note: The tar files are designed to be disposable
#          To generate new tars run again and add the --since flag based on what has already been loaded into the disconnected registry
#          Use: ls content/working-dir/.history to see previous mirror operations

set -e

# Default configuration
CONFIG_FILE="${CONFIG_FILE:-imageset-config.yaml}"
CONTENT_DIR="${CONTENT_DIR:-content}"
CACHE_DIR="${CACHE_DIR:-${XDG_CACHE_HOME:-$HOME/.cache}}"
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
        --content-dir)
            CONTENT_DIR="$2"
            shift 2
            ;;
        --cache-dir)
            CACHE_DIR="$2"
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
            echo "Mirror registry content to disk for air-gapped transport"
            echo ""
            echo "Options:"
            echo "  -c, --config FILE       ImageSet configuration file (default: imageset-config.yaml)"
            echo "  --content-dir DIR       Content output directory (default: content)"
            echo "  --cache-dir DIR         Cache directory (default: \$XDG_CACHE_HOME or ~/.cache)"
            echo "  --since DATE            Mirror only changes since date (YYYY-MM-DD or ISO format)"
            echo "  --incremental           Auto-detect last mirror date from .history files"
            echo "  --dry-run              Show what would be mirrored without downloading"
            echo "  -h, --help             Show this help message"
            echo ""
            echo "Environment Variables:"
            echo "  CONFIG_FILE            Default configuration file"
            echo "  CONTENT_DIR            Default content directory"
            echo "  CACHE_DIR              Default cache directory"
            echo ""
            echo "Examples:"
            echo "  $0                                    # Use defaults"
            echo "  $0 -c imageset-autoshift.yaml        # Use AutoShift generated config"
            echo "  $0 --since 2025-09-01               # Mirror changes since September 1st"
            echo "  $0 --incremental                    # Auto-detect since date from last mirror"
            echo "  $0 --dry-run                         # Preview what would be mirrored"
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

echo "🔄 Mirroring content from registry to disk..."
echo "📋 Config: $CONFIG_FILE"
echo "📁 Content: $CONTENT_DIR"
echo "💾 Cache: $CACHE_DIR"
if [[ -n "$DRY_RUN" ]]; then
    echo "🔍 Mode: DRY RUN (no actual mirroring)"
fi
echo ""

# Create directories if they don't exist
mkdir -p "$CONTENT_DIR" "$CACHE_DIR"

# Auto-detect --since flag from .history files if requested
if [[ "$AUTO_SINCE" == "true" || "$INCREMENTAL_MODE" == "true" && -z "$SINCE_FLAG" ]]; then
    # Use absolute path if CONTENT_DIR starts with /, otherwise make it relative to current directory
    if [[ "$CONTENT_DIR" = /* ]]; then
        HISTORY_DIR="$CONTENT_DIR/working-dir/.history"
    else
        HISTORY_DIR="$(pwd)/$CONTENT_DIR/working-dir/.history"
    fi
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

# Backup imageset configuration if it exists
if [[ -f "$CONFIG_FILE" ]]; then
    BACKUP_DIR="$CONTENT_DIR/imageset-configs"
    mkdir -p "$BACKUP_DIR"
    TIMESTAMP=$(date +%Y%m%d-%H%M%S)
    BACKUP_FILE="$BACKUP_DIR/$(basename "$CONFIG_FILE" .yaml)-$TIMESTAMP.yaml"
    cp "$CONFIG_FILE" "$BACKUP_FILE"
    echo "📋 Backup created: $BACKUP_FILE"
fi

# Run oc-mirror with specified parameters
oc-mirror -c "$CONFIG_FILE" \
    file://"$CONTENT_DIR" \
    --v2 \
    --cache-dir "$CACHE_DIR" \
    $SINCE_FLAG \
    $DRY_RUN

echo ""
if [[ -n "$DRY_RUN" ]]; then
    echo "✅ Dry run completed successfully!"
    echo "🔍 Review the output above to see what would be mirrored"
else
    echo "✅ Mirror to disk completed successfully!"
    echo "📦 Content saved to: $CONTENT_DIR"
    echo "💾 Cache updated at: $CACHE_DIR"
    echo "📊 Content size: $(du -sh "$CONTENT_DIR" 2>/dev/null | cut -f1 || echo 'Unknown')"
    echo "📋 ImageSet configs backed up in: $CONTENT_DIR/imageset-configs/"
    
    # Show history if available
    if [[ -d "$CONTENT_DIR/working-dir/.history" ]]; then
        echo "📜 Mirror history: $(ls -1 "$CONTENT_DIR/working-dir/.history" | wc -l) operations"
        echo "   Latest: $(ls -1t "$CONTENT_DIR/working-dir/.history" | head -1 2>/dev/null || echo 'None')"
    fi
fi

echo ""
echo "💡 Next steps:"
if [[ -n "$DRY_RUN" ]]; then
    echo "   • Run without --dry-run to perform actual mirroring"
    echo "   • Review ImageSet configuration if needed: $CONFIG_FILE"
else
    echo "   • Transport content to air-gapped environment: $CONTENT_DIR"
    echo "   • Use disk-to-mirror.sh to upload to disconnected registry"
    echo "   • For incremental updates, use: --since $(date +%Y-%m-%d)"
fi
echo "   • Monitor cache size: du -sh $CACHE_DIR"