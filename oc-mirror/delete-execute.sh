#!/bin/bash
# Execute deletion of old OpenShift images using generated deletion plan - AutoShift Enhanced
# WARNING: This will permanently delete images from your registry!

set -e

# Default configuration
WORKSPACE_DIR="${WORKSPACE_DIR:-workspace}"
CACHE_DIR="${CACHE_DIR:-${XDG_CACHE_HOME:-$HOME/.cache}}"
TARGET_REGISTRY="${TARGET_REGISTRY:-localhost:8443}"
DELETE_PLAN_FILE=""
FORCE_DELETE=false

# Parse command line arguments
while [[ $# -gt 0 ]]; do
    case $1 in
        --delete-plan)
            DELETE_PLAN_FILE="$2"
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
        --force)
            FORCE_DELETE=true
            shift
            ;;
        -h|--help)
            echo "Usage: $0 [OPTIONS]"
            echo ""
            echo "Execute deletion of old OpenShift images using generated deletion plan"
            echo "WARNING: This will permanently delete images from your registry!"
            echo ""
            echo "Options:"
            echo "  --delete-plan FILE      Deletion plan file (auto-detected if not specified)"
            echo "  --workspace-dir DIR     Workspace directory (default: workspace)"
            echo "  --cache-dir DIR         Cache directory (default: \$XDG_CACHE_HOME or ~/.cache)"
            echo "  -r, --registry HOST     Target registry (default: localhost:8443)"
            echo "  --force                 Skip interactive confirmation (DANGEROUS!)"
            echo "  -h, --help             Show this help message"
            echo ""
            echo "Environment Variables:"
            echo "  WORKSPACE_DIR          Default workspace directory"
            echo "  CACHE_DIR              Default cache directory"
            echo "  TARGET_REGISTRY        Default target registry"
            echo ""
            echo "Examples:"
            echo "  $0                                         # Use auto-detected deletion plan"
            echo "  $0 --delete-plan custom-delete.yaml      # Use specific deletion plan"
            echo "  $0 -r registry.example.com:443           # Custom registry"
            echo "  $0 --force                                # Skip confirmation (not recommended)"
            echo ""
            echo "Prerequisites:"
            echo "  • Run delete-generate.sh first to create deletion plan"
            echo "  • Review deletion plan manually before executing"
            echo "  • Ensure registry backups are available"
            echo "  • Test on non-production registry first"
            exit 0
            ;;
        *)
            echo "Unknown option: $1"
            echo "Use --help for usage information"
            exit 1
            ;;
    esac
done

# Auto-detect deletion plan file if not specified
if [[ -z "$DELETE_PLAN_FILE" ]]; then
    DELETE_PLAN_FILE="$WORKSPACE_DIR/working-dir/delete/delete-images.yaml"
fi

echo "🚨 DANGER: About to execute image deletion!"
echo "🎯 Target registry: $TARGET_REGISTRY"
echo "📄 Deletion plan: $DELETE_PLAN_FILE"
echo "💾 Cache: $CACHE_DIR"
echo "⚠️  WARNING: This will PERMANENTLY DELETE images from registry!"
echo ""

# Verify deletion plan exists
if [[ ! -f "$DELETE_PLAN_FILE" ]]; then
    echo "❌ ERROR: Deletion plan not found: $DELETE_PLAN_FILE"
    echo ""
    echo "💡 Run delete-generate.sh first to create deletion plan"
    echo "   Available deletion plans:"
    find . -name "*delete*.yaml" -type f 2>/dev/null || echo "   No deletion plans found"
    exit 1
fi

echo "🔍 Deletion plan found - showing summary:"
echo "📊 Plan details:"
echo "   File: $DELETE_PLAN_FILE"
echo "   Size: $(du -sh "$DELETE_PLAN_FILE" 2>/dev/null | cut -f1 || echo 'Unknown')"
echo "   Created: $(ls -l "$DELETE_PLAN_FILE" 2>/dev/null | awk '{print $6, $7, $8}' || echo 'Unknown')"

# Count images to be deleted
if command -v grep &> /dev/null; then
    IMAGE_COUNT=$(grep -c 'imageName:' "$DELETE_PLAN_FILE" 2>/dev/null || echo 'Unable to count')
    echo "   Images to delete: $IMAGE_COUNT"
    
    # Show some example images that will be deleted
    echo ""
    echo "📋 Sample images to be deleted:"
    grep 'imageName:' "$DELETE_PLAN_FILE" 2>/dev/null | head -5 | sed 's/^/   /' || echo "   Unable to show examples"
    if [[ "$IMAGE_COUNT" -gt 5 ]]; then
        echo "   ... and $((IMAGE_COUNT - 5)) more images"
    fi
fi
echo ""

# Registry garbage collection information
echo "🧹 Post-deletion cleanup:"
echo "   After deletion, run registry garbage collection to reclaim storage:"
echo "   • For Quay: Log into registry admin panel and run GC"
echo "   • For mirror-registry: sudo podman exec -it quay-app /bin/bash -c 'registry-garbage-collect'"
echo "   • For other registries: Consult registry documentation"
echo ""

# Interactive confirmation unless --force is used
if [[ "$FORCE_DELETE" != "true" ]]; then
    echo "⏰ FINAL CONFIRMATION REQUIRED"
    echo "This operation will:"
    echo "  • Permanently delete images from $TARGET_REGISTRY"
    echo "  • Remove image manifests and layers as specified in deletion plan"
    echo "  • Free up registry storage space (after garbage collection)"
    echo "  • Cannot be undone without restoring from backups"
    echo ""
    echo "🛑 Press Ctrl+C now to abort, or Enter to proceed with deletion..."
    read -r
fi

echo ""
echo "🗑️ Executing deletion plan..."
echo "📊 This may take several minutes depending on registry size and number of images"
echo ""

# Execute deletion using the generated plan
oc mirror delete \
    --delete-yaml-file "$DELETE_PLAN_FILE" \
    docker://"$TARGET_REGISTRY" \
    --v2 \
    --cache-dir "$CACHE_DIR"

echo ""
echo "✅ Deletion execution completed!"
echo ""

# Post-deletion information
echo "💡 Next steps:"
echo "   • Run registry garbage collection to reclaim storage space"
echo "   • Verify deleted versions are gone (test image pulls)"
echo "   • Check that current versions still work"
echo "   • Monitor registry storage usage"
echo "   • Update any documentation about available image versions"
echo ""

# Cache management information
echo "🗂️  Cache Management:"
CACHE_SIZE=$(du -sh "$CACHE_DIR" 2>/dev/null | cut -f1 || echo 'Unknown')
echo "   • Current cache size: $CACHE_SIZE"
echo "   • Cache contains metadata for mirroring operations"
echo "   • Keep cache for future operations (recommended for frequent mirroring)"
echo "   • Manual cleanup if space needed: rm -rf $CACHE_DIR"
echo ""

# Registry verification commands
echo "🔍 Verification Commands:"
echo "   # Test that deleted versions are gone (should fail):"
echo "   oc adm release info $TARGET_REGISTRY/openshift/release-images:4.19.2-x86_64"
echo ""
echo "   # Test that current versions still work (should succeed):"
echo "   oc adm release info $TARGET_REGISTRY/openshift/release-images:latest"
echo ""
echo "   # Check registry storage usage:"
echo "   df -h /opt/quay/  # Adjust path for your registry"