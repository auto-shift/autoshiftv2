#!/usr/bin/env bash
# Installs the toolchain `make release` needs inside the ubi9 container.
#
# Both the release workflow and the CI release dry run source their environment from here, so the
# two cannot drift: a tool a new release step depends on is added once, and the PR gate exercises
# the same container the tag push will. Go is NOT installed here — it comes from actions/setup-go,
# which tracks tools/go.mod.
#
# Assumes git is already present, because the caller has to check the repository out to reach this
# script.
set -euo pipefail

# renovate: datasource=github-releases depName=helm/helm
HELM_VERSION=v3.19.4

dnf -y install \
  make \
  jq \
  git \
  findutils \
  skopeo \
  https://dl.fedoraproject.org/pub/epel/epel-release-latest-9.noarch.rpm
dnf -y install yq

# The OpenShift CLI and oc-mirror come from the channel of the OpenShift minor the profiles target,
# read from versions.yaml. The bare `stable` channel tracks the newest major, which can ship no
# oc-mirror at all.
OCP_MINOR=$(yq '.inputs.openshift' "$(dirname "$0")/../versions.yaml")
OCP_CLIENTS="https://mirror.openshift.com/pub/openshift-v4/x86_64/clients/ocp/stable-${OCP_MINOR}"

curl -fsSL "https://get.helm.sh/helm-${HELM_VERSION}-linux-amd64.tar.gz" | tar xz -C /tmp
install -m 0755 /tmp/linux-amd64/helm /usr/local/bin/helm
helm version

# oc backs the mirror artifact generation.
curl -fsSL "${OCP_CLIENTS}/openshift-client-linux.tar.gz" | tar xz -C /tmp
install -m 0755 /tmp/oc /tmp/kubectl /usr/local/bin/
oc version --client

# oc-mirror backs `make verify-imageset`, a dry run that proves the published imageset resolves.
curl -fsSL "${OCP_CLIENTS}/oc-mirror.rhel9.tar.gz" | tar xz -C /tmp
install -m 0755 /tmp/oc-mirror /usr/local/bin/
oc-mirror version --output=yaml | head -3
