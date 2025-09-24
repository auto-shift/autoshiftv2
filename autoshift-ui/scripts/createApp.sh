export APP_NAME=$0
export REPO_URL=$1
export TARGET_REVISION=$2
export VALUES_FILE=$3
export ARGO_PROJECT=$4
export GITOPS_NAMESPACE=$5

cat << EOF | oc apply -f -
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: $APP_NAME
  namespace: $GITOPS_NAMESPACE
spec:
  destination:
    namespace: ''
    server: https://kubernetes.default.svc
  source:
    path: autoshift
    repoURL: $REPO_URL
    targetRevision: $TARGET_REVISION
    helm:
      valueFiles:
        - $VALUES_FILE
      values: |-
        autoshiftGitRepo: $REPO_URL
        autoshiftGitBranchTag: $TARGET_REVISION
  sources: []
  project: $ARGO_PROJECT
  syncPolicy:
    automated:
      prune: false
      selfHeal: true
EOF