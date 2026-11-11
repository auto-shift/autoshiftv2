#!/bin/sh
# Blocks the MultiClusterHub post-install hook until the hub can actually be created.
#
# Two things must be true, in order:
#   1. The multiclusterhubs CRD exists.
#   2. The multiclusterhub-operator-webhook Service has a ready endpoint.
#
# The CSV registers the CRD well before the webhook pod is serving. Creating the MultiClusterHub
# in that gap fails with: no endpoints available for service "multiclusterhub-operator-webhook".
# Helm records the failed release, a later `helm upgrade --install` does not re-run post-install
# hooks, and the hub is then never created (auto-shift/autoshiftv2#281). Waiting on the webhook
# closes the gap.
#
# Readiness is read from EndpointSlices (discovery.k8s.io/v1), the replacement for the deprecated
# Endpoints API. The hook gives up after WAIT_TIMEOUT_SECONDS with a message naming what it was
# waiting for, instead of looping until Helm's own timeout kills it without one.
#
# Settings come from the environment, so the script can be run outside the cluster against a
# stubbed `oc`.
NAMESPACE="${NAMESPACE:-open-cluster-management}"
CRD="${CRD:-multiclusterhubs.operator.open-cluster-management.io}"
WEBHOOK_SERVICE="${WEBHOOK_SERVICE:-multiclusterhub-operator-webhook}"
WAIT_TIMEOUT_SECONDS="${WAIT_TIMEOUT_SECONDS:-900}"
POLL_SECONDS="${POLL_SECONDS:-5}"

start=$(date +%s)

# wait_for DESCRIPTION COMMAND...: runs COMMAND until it succeeds or the shared deadline passes.
wait_for() {
  what="$1"
  shift
  echo "Waiting for ${what}"
  until "$@"; do
    if [ $(( $(date +%s) - start )) -ge "$WAIT_TIMEOUT_SECONDS" ]; then
      echo "Timed out after ${WAIT_TIMEOUT_SECONDS}s waiting for ${what}." >&2
      return 1
    fi
    sleep "$POLL_SECONDS"
  done
  echo "Ready: ${what}"
}

crd_exists() {
  oc get crd "$CRD" >/dev/null 2>&1
}

webhook_has_ready_endpoint() {
  oc get endpointslices -n "$NAMESPACE" -l "kubernetes.io/service-name=${WEBHOOK_SERVICE}" \
    -o jsonpath='{range .items[*].endpoints[*]}{.conditions.ready}{"\n"}{end}' 2>/dev/null \
    | grep -qx true
}

wait_for "CRD ${CRD}" crd_exists || exit 1
wait_for "a ready endpoint on Service ${NAMESPACE}/${WEBHOOK_SERVICE}" webhook_has_ready_endpoint || {
  echo "The ACM operator is installed but its validating webhook is not serving. Check the" >&2
  echo "multiclusterhub-operator pods in ${NAMESPACE}." >&2
  exit 1
}
