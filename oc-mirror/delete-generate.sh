#!/bin/bash
# Generate deletion plan for old OpenShift images - AutoShift Enhanced
# Creates reviewable deletion plan without executing any deletions (SAFE!)

set -e

# Default configuration
DELETE_CONFIG_FILE="${DELETE_CONFIG_FILE:-imageset-delete.yaml}"
WORKSPACE_DIR="${WORKSPACE_DIR:-workspace}"
CACHE_DIR="${CACHE_DIR:-${XDG_CACHE_HOME:-$HOME/.cache}}"
TARGET_REGISTRY="${TARGET_REGISTRY:-localhost:8443}"

# Parse command line arguments
while [[ $# -gt 0 ]]; do
    case $1 in
        -c|--config)
            DELETE_CONFIG_FILE="$2"
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
        -h|--help)
            echo "Usage: $0 [OPTIONS]"
            echo ""
            echo "Generate deletion plan for old OpenShift images (SAFE - no actual deletions)"
            echo ""
            echo "Options:"
            echo "  -c, --config FILE       Delete configuration file (default: imageset-delete.yaml)"
            echo "  --workspace-dir DIR     Workspace directory (default: workspace)"
            echo "  --cache-dir DIR         Cache directory (default: \$XDG_CACHE_HOME or ~/.cache)"
            echo "  -r, --registry HOST     Target registry (default: localhost:8443)"
            echo "  -h, --help             Show this help message"
            echo ""
            echo "Environment Variables:"
            echo "  DELETE_CONFIG_FILE     Default delete configuration file"
            echo "  WORKSPACE_DIR          Default workspace directory"
            echo "  CACHE_DIR              Default cache directory"
            echo "  TARGET_REGISTRY        Default target registry"
            echo ""
            echo "Examples:"
            echo "  $0                                         # Use defaults"
            echo "  $0 -c my-delete-config.yaml              # Custom delete config"
            echo "  $0 -r registry.example.com:443           # Custom registry"
            echo ""
            echo "Safety Features:"
            echo "  • GENERATES deletion plan only - no actual deletions"
            echo "  • Creates reviewable YAML file for manual inspection"
            echo "  • Must run delete-execute.sh separately to perform deletions"
            exit 0
            ;;
        *)
            echo "Unknown option: $1"
            echo "Use --help for usage information"
            exit 1
            ;;
    esac
done

# Validate delete configuration file exists
if [[ ! -f "$DELETE_CONFIG_FILE" ]]; then
    echo "❌ ERROR: Delete configuration file not found: $DELETE_CONFIG_FILE"
    echo ""
    echo "Available delete configurations:"
    find . -maxdepth 2 -name "*delete*.yaml" -type f 2>/dev/null || echo "  No delete configurations found"
    echo ""
    echo "💡 Create a delete configuration based on imageset-delete.yaml template"
    exit 1
fi

echo "🗑️ Generating deletion plan for old images..."
echo "🎯 Target registry: $TARGET_REGISTRY"
echo "📋 Config: $DELETE_CONFIG_FILE"
echo "📁 Workspace: file://$WORKSPACE_DIR"
echo "💾 Cache: $CACHE_DIR"
echo "⚠️  SAFE MODE: No deletions will be executed"
echo ""

# Show delete configuration summary
echo "📋 Delete Configuration Summary:"
if command -v yq &> /dev/null; then
    echo "   Channels: $(yq eval '.delete.platform.channels[].name' "$DELETE_CONFIG_FILE" 2>/dev/null | tr '\n' ' ' || echo 'Unable to parse')"
    echo "   Version range: $(yq eval '.delete.platform.channels[].minVersion' "$DELETE_CONFIG_FILE" 2>/dev/null || echo 'N/A') - $(yq eval '.delete.platform.channels[].maxVersion' "$DELETE_CONFIG_FILE" 2>/dev/null || echo 'N/A')"
elif command -v grep &> /dev/null; then
    echo "   Channels: $(grep -A 5 'channels:' "$DELETE_CONFIG_FILE" | grep 'name:' | sed 's/.*name: *//' | tr '\n' ' ' || echo 'Unable to parse')"
    echo "   Min version: $(grep 'minVersion:' "$DELETE_CONFIG_FILE" | sed 's/.*minVersion: *//' || echo 'N/A')"
    echo "   Max version: $(grep 'maxVersion:' "$DELETE_CONFIG_FILE" | sed 's/.*maxVersion: *//' || echo 'N/A')"
else
    echo "   Use 'cat $DELETE_CONFIG_FILE' to review configuration"
fi
echo ""

# Create directories if they don't exist
mkdir -p "$WORKSPACE_DIR" "$CACHE_DIR"

# Backup delete configuration if it exists
if [[ -f "$DELETE_CONFIG_FILE" ]]; then
    BACKUP_DIR="$WORKSPACE_DIR/imageset-configs"
    mkdir -p "$BACKUP_DIR"
    TIMESTAMP=$(date +%Y%m%d-%H%M%S)
    BACKUP_FILE="$BACKUP_DIR/$(basename "$DELETE_CONFIG_FILE" .yaml)-$TIMESTAMP.yaml"
    cp "$DELETE_CONFIG_FILE" "$BACKUP_FILE"
    echo "📋 Delete config backup created: $BACKUP_FILE"
fi

# Generate deletion plan (safe preview - no actual deletion occurs)
oc mirror delete \
    -c "$DELETE_CONFIG_FILE" \
    --generate \
    --workspace file://"$WORKSPACE_DIR" \
    docker://"$TARGET_REGISTRY" \
    --v2 \
    --cache-dir "$CACHE_DIR"

echo ""
echo "✅ Deletion plan generated successfully!"
echo "📋 Delete configs backed up in: $WORKSPACE_DIR/imageset-configs/"

# Check if deletion plan was created
DELETE_PLAN_FILE="$WORKSPACE_DIR/working-dir/delete/delete-images.yaml"
if [[ -f "$DELETE_PLAN_FILE" ]]; then
    echo "📄 Plan saved to: $DELETE_PLAN_FILE"
    echo "📊 Plan details:"
    echo "   File size: $(du -sh "$DELETE_PLAN_FILE" 2>/dev/null | cut -f1 || echo 'Unknown')"
    
    # Count images to be deleted
    if command -v grep &> /dev/null; then
        IMAGE_COUNT=$(grep -c 'imageName:' "$DELETE_PLAN_FILE" 2>/dev/null || echo 'Unable to count')
        echo "   Images to delete: $IMAGE_COUNT"
    fi
    
    echo ""
    echo "🔍 IMPORTANT: Review the deletion plan before executing!"
    echo "💡 Preview commands:"
    echo "   cat $DELETE_PLAN_FILE"
    echo "   head -50 $DELETE_PLAN_FILE  # Show first 50 lines"
    echo "   grep 'imageName:' $DELETE_PLAN_FILE | head -10  # Show first 10 images"
else
    echo "⚠️  Deletion plan file not found at expected location: $DELETE_PLAN_FILE"
    echo "💡 Check the workspace directory: ls -la $WORKSPACE_DIR/"
fi

echo ""
echo "💡 Next steps:"
echo "   • Review plan: cat $DELETE_PLAN_FILE"
echo "   • Execute deletion: ./delete-execute.sh"
echo "   • Or manually: oc mirror delete --delete-yaml-file $DELETE_PLAN_FILE docker://$TARGET_REGISTRY --v2 --cache-dir $CACHE_DIR"
echo ""
echo "⚠️  WARNING: Deletion is permanent!"
echo "   • Ensure you have backups of important images"
echo "   • Test deletion on non-production registry first"
echo "   • Verify current OpenShift versions are preserved"
echo "   • Plan registry garbage collection after deletion"