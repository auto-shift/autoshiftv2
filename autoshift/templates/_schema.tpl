{{/*
AutoShift Label Schema — the source of truth for all valid labels.

HOW VALIDATION WORKS:
  1. Operators are AUTO-DETECTED by convention: if a label "{name}-subscription-name"
     exists, then {name} is an operator. The standard labels are allowed automatically:
       {name}, {name}-subscription-name, {name}-channel, {name}-source,
       {name}-source-namespace, {name}-version
     NO SCHEMA ENTRY NEEDED for standard-only operators.

  2. Anything beyond those 6 standard labels MUST be listed here or
     helm template will fail with "unknown label".

HOW TO ADD NEW LABELS:
  - New standard operator (e.g., cert-manager):
      Just add labels to your values file. Nothing to do here.

  - New operator with extra labels beyond the standard 6:
      Add an entry under "Operator extras" with the operator name as key.
      Do NOT include an "enable" field — that signals a feature, not an operator.
      List only the extras: optional, boolLabels, allowedValues, dynamicPrefixes.

  - New feature (non-operator, no subscription-name):
      Add an entry under "Features" WITH an "enable" field.
      Include required, optional, boolLabels, allowedValues, dynamicPrefixes as needed.

  - New standalone label (not tied to any operator or feature):
      Add it to _global.optional.

Schema fields:
  enable:          (features only) Label name that activates this feature
  enableCheck:     "nonempty" for count-based features (default: equals "true")
  required:        Labels that MUST exist and be non-empty when enabled
  optional:        Labels that CAN exist (free-form values)
  boolLabels:      Labels that must be 'true' or 'false'
  allowedValues:   Enum constraints — label must be one of the listed values
  dynamicPrefixes: Wildcard prefixes — any label matching {name}-{prefix}* is allowed
*/}}
{{- define "autoshift.schemas" -}}
# ===========================================================================
# Global — standalone labels not tied to any operator or feature
# ===========================================================================
_global:
  optional:
    - openshift-version
    - self-managed
    - mirror-catalog-suffix
    - master-max-pods
    - dns-node-placement
    - allowed-registries

# ===========================================================================
# Operator extras — additional labels beyond the standard 6
# (No "enable" field — operators are auto-detected by convention)
# ===========================================================================

gitops:
  boolLabels:
    - disable-default-argocd
    - cluster-ca-bundle
  optional:
    - namespace

acm:
  boolLabels:
    - observability
    - search-storage
    - enable-provisioning
    - addon-tuning
  optional:
    - availability-config
  dynamicPrefixes:
    - addon-cpc-
    - addon-gpf-
    - provisioning-
    - search-storage-

nmstate:
  dynamicPrefixes:
    - ethernet-
    - bond-
    - vlan-
    - route-
    - dns-
    - nodeselector-
    - ovs-bridge-
    - ovn-mapping-
    - host-
    - nncp-

acs:
  boolLabels:
    - monitoring
    - vm-scanning
    - admission-control
    - default-policies
  optional:
    - auth-provider
    - auth-min-role
    - auth-admin-group
  allowedValues:
    egress-connectivity:
      - Online
      - Offline
    scanner-v4:
      - Enabled
      - Disabled
    network-policies:
      - Enabled
      - Disabled

odf:
  boolLabels:
    - ocs-flexible-scaling
    - csi-all-nodes
  optional:
    - noobaa-pvpool
    - noobaa-store-size
    - noobaa-store-num-volumes
    - ocs-storage-class-name
    - ocs-storage-size
    - ocs-storage-count
    - ocs-storage-replicas
    - default-storageclass
  allowedValues:
    multi-cloud-gateway:
      - standalone
      - standard
    resource-profile:
      - lean
      - balanced
      - performance

metallb:
  boolLabels:
    - quota
  optional:
    - quota-cpu
    - quota-memory
  dynamicPrefixes:
    - ippool-
    - l2-
    - bgp-
    - peer-

aap:
  boolLabels:
    - hub-disabled
    - file-storage
    - noobaa-s3-storage
    - s3-storage
    - eda-disabled
    - lightspeed-disabled
    - custom-cabundle
  optional:
    - file_storage_storage_class
    - file_storage_size
    - external-s3-secret-name
    - cabundle-name

lvm:
  boolLabels:
    - default
  optional:
    - size-percent
    - overprovision-ratio
  allowedValues:
    fstype:
      - xfs
      - ext4

compliance:
  boolLabels:
    - auto-remediate
  optional:
    - storage-class

loki:
  optional:
    - size
    - storageclass
    - lokistack-name

# ===========================================================================
# Features — non-operator components (MUST have "enable" field)
# ===========================================================================

imageregistry:
  enable: imageregistry
  optional:
    - replicas
    - s3-region
    - pvc-access-mode
    - pvc-size
    - pvc-storage-class
  allowedValues:
    management-state:
      - Managed
      - Unmanaged
    storage-type:
      - s3
      - pvc
    pvc-volume-mode:
      - Block
      - Filesystem
    rollout-strategy:
      - RollingUpdate
      - Recreate

gitops-dev:
  enable: gitops-dev
  dynamicPrefixes:
    - team-

machine-health-checks:
  enable: machine-health-checks
  optional:
    - zones
  boolLabels:
    - worker
    - infra
    - storage

infra-nodes:
  enable: infra-nodes
  enableCheck: nonempty
  optional:
    - instance-type
    - numcpu
    - memory-mib
    - numcores-per-socket
    - zones
  allowedValues:
    provider:
      - aws
      - vmware
      - baremetal
  dynamicPrefixes:
    - zone-

worker-nodes:
  enable: worker-nodes
  enableCheck: nonempty
  optional:
    - numcpu
    - memory-mib
    - numcores-per-socket
    - zones
  dynamicPrefixes:
    - zone-

storage-nodes:
  enable: storage-nodes
  enableCheck: nonempty
  optional:
    - instance-type
    - numcpu
    - memory-mib
    - numcores-per-socket
    - region
    - zones
  allowedValues:
    provider:
      - aws
      - vmware
      - baremetal
  dynamicPrefixes:
    - zone-
    - node-

master-nodes:
  enable: master-nodes

disconnected-mirror:
  enable: disconnected-mirror

dns-tolerations:
  enable: dns-tolerations

manual-remediations:
  enable: manual-remediations
{{- end -}}
