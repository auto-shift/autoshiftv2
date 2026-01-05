#!/bin/bash
# Development quality checks for AutoShift
# Runs shellcheck, kubeconform, and other dev-only validations
#
# Usage: ./scripts/dev-checks.sh [--fix]
#
# These checks require dev tools that may not be installed everywhere:
#   - shellcheck: brew install shellcheck
#   - kubeconform: brew install kubeconform
#
# For CI pipelines, install tools first or use the Makefile's `lint` target
# which only requires helm.

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

# Colors (enabled if stdout or stderr is a terminal)
if [[ -t 1 ]] || [[ -t 2 ]]; then
    RED='\033[0;31m'
    GREEN='\033[0;32m'
    YELLOW='\033[1;33m'
    BLUE='\033[0;34m'
    NC='\033[0m'
else
    RED=''
    GREEN=''
    YELLOW=''
    BLUE=''
    NC=''
fi

log() { echo -e "${BLUE}[INFO]${NC} $1"; }
success() { echo -e "${GREEN}[OK]${NC} $1"; }
warn() { echo -e "${YELLOW}[WARN]${NC} $1"; }
error() { echo -e "${RED}[ERROR]${NC} $1"; }

cd "$PROJECT_ROOT"

FAILED=0

# =============================================================================
# ShellCheck - Shell script linting
# =============================================================================
echo ""
log "Running shell script checks..."

if command -v shellcheck &> /dev/null; then
    if shellcheck scripts/*.sh; then
        success "All scripts passed shellcheck"
    else
        error "Some scripts failed shellcheck"
        FAILED=1
    fi
else
    warn "shellcheck not installed - skipping (install: brew install shellcheck)"
fi

# =============================================================================
# Kubeconform - Kubernetes manifest validation
# =============================================================================
echo ""
log "Running Kubernetes manifest validation..."

if command -v kubeconform &> /dev/null; then
    # Skip CRDs that kubeconform doesn't have schemas for (ArgoCD, ACM)
    SKIP_KINDS="ApplicationSet,Application,Policy,PlacementBinding,Placement,PlacementRule,ConfigurationPolicy"

    if helm template autoshift/ 2>/dev/null | kubeconform -skip "$SKIP_KINDS" -summary; then
        success "autoshift/ manifests valid"
    else
        error "autoshift/ manifests failed validation"
        FAILED=1
    fi
else
    warn "kubeconform not installed - skipping (install: brew install kubeconform)"
fi

# =============================================================================
# Helm lint - Chart validation (also in Makefile, but included for completeness)
# =============================================================================
echo ""
log "Running Helm chart validation..."

if helm lint autoshift/ --quiet; then
    success "autoshift/ chart valid"
else
    error "autoshift/ chart failed lint"
    FAILED=1
fi

# Check a sample of policy charts
POLICY_COUNT=$(find policies -maxdepth 2 -name Chart.yaml | wc -l | tr -d ' ')
POLICY_FAILED=0
for chart in policies/*/; do
    if [[ -f "$chart/Chart.yaml" ]]; then
        if ! helm lint "$chart" --quiet 2>/dev/null; then
            error "$chart failed lint"
            POLICY_FAILED=1
        fi
    fi
done

if [[ $POLICY_FAILED -eq 0 ]]; then
    success "All $POLICY_COUNT policy charts valid"
else
    FAILED=1
fi

# =============================================================================
# Summary
# =============================================================================
echo ""
if [[ $FAILED -eq 0 ]]; then
    echo -e "${GREEN}=========================================${NC}"
    success "All dev checks passed!"
    echo -e "${GREEN}=========================================${NC}"
    exit 0
else
    echo -e "${RED}=========================================${NC}"
    error "Some checks failed - see above for details"
    echo -e "${RED}=========================================${NC}"
    exit 1
fi
