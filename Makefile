.PHONY: help
help: ## Show this help message
	@echo 'Usage: make [target]'
	@echo ''
	@echo 'Available targets:'
	@awk 'BEGIN {FS = ":.*?## "} /^[a-zA-Z_-]+:.*?## / {printf "  %-20s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

# Configuration
VERSION ?= $(error VERSION is required. Usage: make release VERSION=1.0.0)
REGISTRY ?= quay.io
REGISTRY_NAMESPACE ?= autoshift
DRY_RUN ?= false
INCLUDE_MIRROR ?= false
CHARTS_DIR := .helm-charts
ARTIFACTS_DIR := release-artifacts

# Colors for output
GREEN := \033[0;32m
YELLOW := \033[1;33m
RED := \033[0;31m
BLUE := \033[0;34m
NC := \033[0m

# Discover all charts
BOOTSTRAP_CHARTS := $(shell find . -maxdepth 2 -name Chart.yaml -not -path "./policies/*" -not -path "./autoshift/*" -exec dirname {} \;)
POLICY_CHARTS := $(shell find policies -maxdepth 2 -name Chart.yaml -exec dirname {} \;)
POLICY_NAMES := $(notdir $(POLICY_CHARTS))

.PHONY: discover
discover: ## Discover all charts in the repository
	@printf "$(BLUE)[INFO]$(NC) Discovering charts...\n"
	@printf "$(GREEN)Bootstrap charts:$(NC)\n"
	@$(foreach chart,$(BOOTSTRAP_CHARTS),echo "  - $(notdir $(chart))";)
	@echo ""
	@printf "$(GREEN)Policy charts ($(words $(POLICY_NAMES))):$(NC)\n"
	@$(foreach policy,$(POLICY_NAMES),echo "  - $(policy)";)
	@echo ""
	@printf "$(GREEN)Total charts: $(shell echo $$(($(words $(BOOTSTRAP_CHARTS)) + $(words $(POLICY_CHARTS)) + 1)))$(NC) ($(words $(BOOTSTRAP_CHARTS)) bootstrap + 1 main + $(words $(POLICY_CHARTS)) policies)\n"

.PHONY: validate
validate: ## Validate that required tools are installed
	@printf "$(BLUE)[INFO]$(NC) Validating required tools...\n"
	@command -v helm >/dev/null 2>&1 || { printf "$(RED)[ERROR]$(NC) helm is required but not installed\n"; exit 1; }
	@command -v yq >/dev/null 2>&1 || { printf "$(RED)[ERROR]$(NC) yq is required but not installed. Install: brew install yq\n"; exit 1; }
	@command -v git >/dev/null 2>&1 || { printf "$(RED)[ERROR]$(NC) git is required but not installed\n"; exit 1; }
	@printf "$(GREEN)✓$(NC) All required tools are installed\n"

.PHONY: lint
lint: ## Lint all Helm charts
	@printf "$(BLUE)[INFO]$(NC) Linting Helm charts...\n"
	@helm lint autoshift/ --quiet && printf "$(GREEN)✓$(NC) autoshift/ passed\n"
	@failed=0; \
	for chart in policies/*/; do \
		if [ -f "$$chart/Chart.yaml" ]; then \
			helm lint "$$chart" --quiet 2>/dev/null || { printf "$(RED)✗$(NC) $$chart failed\n"; failed=1; }; \
		fi; \
	done; \
	if [ $$failed -eq 0 ]; then printf "$(GREEN)✓$(NC) All policy charts passed\n"; else exit 1; fi

.PHONY: validate-version
validate-version: ## Validate version format
	@printf "$(BLUE)[INFO]$(NC) Validating version format: $(VERSION)\n"
	@echo "$(VERSION)" | grep -qE '^[0-9]+\.[0-9]+\.[0-9]+(-[a-zA-Z0-9]+)?$$' || { \
		printf "$(RED)[ERROR]$(NC) Invalid version format: $(VERSION)\n"; \
		echo "Expected: X.Y.Z or X.Y.Z-prerelease (e.g., 1.0.0 or 1.0.0-rc.1)"; \
		exit 1; \
	}
	@printf "$(GREEN)✓$(NC) Version format is valid\n"

.PHONY: clean
clean: ## Clean build artifacts
	@printf "$(BLUE)[INFO]$(NC) Cleaning build artifacts...\n"
	@rm -rf $(CHARTS_DIR)
	@rm -rf $(ARTIFACTS_DIR)
	@rm -rf autoshift/files
	@printf "$(GREEN)✓$(NC) Build artifacts cleaned\n"

.PHONY: update-versions
update-versions: validate-version ## Update all chart versions
	@printf "$(BLUE)[INFO]$(NC) Updating chart versions to $(VERSION)...\n"
	@# Update bootstrap charts
	@$(foreach chart,$(BOOTSTRAP_CHARTS), \
		yq eval -i '.version = "$(VERSION)" | .appVersion = "$(VERSION)"' $(chart)/Chart.yaml;)
	@# Update autoshift chart
	@yq eval -i '.version = "$(VERSION)" | .appVersion = "$(VERSION)"' autoshift/Chart.yaml
	@# Update policy charts
	@$(foreach chart,$(POLICY_CHARTS), \
		yq eval -i '.version = "$(VERSION)" | .appVersion = "$(VERSION)"' $(chart)/Chart.yaml;)
	@printf "$(GREEN)✓$(NC) All chart versions updated to $(VERSION)\n"

.PHONY: sync-values
sync-values: ## Sync bootstrap chart values from policy charts
	@printf "$(BLUE)[INFO]$(NC) Syncing bootstrap values from policy charts...\n"
	@bash scripts/sync-bootstrap-values.sh
	@printf "$(GREEN)✓$(NC) Bootstrap values synced\n"

.PHONY: generate-policy-list
generate-policy-list: ## Generate policy-list.txt for OCI mode
	@printf "$(BLUE)[INFO]$(NC) Generating policy-list.txt with $(words $(POLICY_NAMES)) policies...\n"
	@mkdir -p autoshift/files
	@$(foreach policy,$(POLICY_NAMES),echo "$(policy)" >> autoshift/files/policy-list.txt;)
	@printf "$(GREEN)✓$(NC) Generated policy-list.txt with $(words $(POLICY_NAMES)) policies\n"

.PHONY: package-charts
package-charts: ## Package all Helm charts
	@printf "$(BLUE)[INFO]$(NC) Packaging Helm charts...\n"
	@mkdir -p $(CHARTS_DIR)/bootstrap $(CHARTS_DIR)/policies
	@# Package bootstrap charts to separate directory (avoids name collision with policy charts)
	@printf "$(BLUE)[INFO]$(NC) Packaging bootstrap charts to $(CHARTS_DIR)/bootstrap/...\n"
	@$(foreach chart,$(BOOTSTRAP_CHARTS), \
		echo "  - Packaging $(notdir $(chart))..."; \
		helm package $(chart) -d $(CHARTS_DIR)/bootstrap >/dev/null;)
	@# Package policy charts to separate directory
	@printf "$(BLUE)[INFO]$(NC) Packaging policy charts to $(CHARTS_DIR)/policies/...\n"
	@$(foreach chart,$(POLICY_CHARTS), \
		echo "  - Packaging $(notdir $(chart))..."; \
		helm package $(chart) -d $(CHARTS_DIR)/policies >/dev/null;)
	@# Package autoshift chart (last, after template generation)
	@printf "$(BLUE)[INFO]$(NC) Packaging autoshift chart...\n"
	@helm package autoshift -d $(CHARTS_DIR) >/dev/null
	@echo ""
	@printf "$(BLUE)Bootstrap charts:$(NC)\n"
	@ls -lh $(CHARTS_DIR)/bootstrap/
	@echo ""
	@printf "$(BLUE)Policy charts:$(NC)\n"
	@ls -lh $(CHARTS_DIR)/policies/ | head -10
	@echo "  ... and $(shell ls -1 $(CHARTS_DIR)/policies/ | wc -l | tr -d ' ') total policy charts"
	@echo ""
	@printf "$(BLUE)Main chart:$(NC)\n"
	@ls -lh $(CHARTS_DIR)/autoshift-*.tgz
	@echo ""
	@printf "$(GREEN)✓$(NC) Packaged charts: $(shell ls -1 $(CHARTS_DIR)/bootstrap/ | wc -l | tr -d ' ') bootstrap + $(shell ls -1 $(CHARTS_DIR)/policies/ | wc -l | tr -d ' ') policies + 1 main\n"

.PHONY: push-charts
push-charts: ## Push charts to OCI registry with namespaced paths
	@if [ "$(DRY_RUN)" = "true" ]; then \
		printf "$(YELLOW)[WARN]$(NC) DRY RUN: Skipping push to registry\n"; \
		echo "Bootstrap charts would be pushed to: oci://$(REGISTRY)/$(REGISTRY_NAMESPACE)/bootstrap"; \
		echo "Main chart would be pushed to: oci://$(REGISTRY)/$(REGISTRY_NAMESPACE)"; \
		echo "Policy charts would be pushed to: oci://$(REGISTRY)/$(REGISTRY_NAMESPACE)/policies"; \
	else \
		printf "$(BLUE)[INFO]$(NC) Pushing charts to OCI registry...\n"; \
		echo ""; \
		printf "$(BLUE)[INFO]$(NC) Pushing bootstrap charts to: oci://$(REGISTRY)/$(REGISTRY_NAMESPACE)/bootstrap\n"; \
		for chart in $(CHARTS_DIR)/bootstrap/*.tgz; do \
			echo "  - Pushing $$(basename $$chart)..."; \
			helm push $$chart oci://$(REGISTRY)/$(REGISTRY_NAMESPACE)/bootstrap || exit 1; \
		done; \
		echo ""; \
		printf "$(BLUE)[INFO]$(NC) Pushing main chart to: oci://$(REGISTRY)/$(REGISTRY_NAMESPACE)\n"; \
		if [ -f "$(CHARTS_DIR)/autoshift-$(VERSION).tgz" ]; then \
			echo "  - Pushing autoshift-$(VERSION).tgz..."; \
			helm push $(CHARTS_DIR)/autoshift-$(VERSION).tgz oci://$(REGISTRY)/$(REGISTRY_NAMESPACE) || exit 1; \
		fi; \
		echo ""; \
		printf "$(BLUE)[INFO]$(NC) Pushing policy charts to: oci://$(REGISTRY)/$(REGISTRY_NAMESPACE)/policies\n"; \
		for chart in $(CHARTS_DIR)/policies/*.tgz; do \
			echo "  - Pushing $$(basename $$chart)..."; \
			helm push $$chart oci://$(REGISTRY)/$(REGISTRY_NAMESPACE)/policies || exit 1; \
		done; \
		echo ""; \
		printf "$(GREEN)✓$(NC) All charts pushed\n"; \
		echo "  Bootstrap: oci://$(REGISTRY)/$(REGISTRY_NAMESPACE)/bootstrap"; \
		echo "  Main: oci://$(REGISTRY)/$(REGISTRY_NAMESPACE)"; \
		echo "  Policies: oci://$(REGISTRY)/$(REGISTRY_NAMESPACE)/policies"; \
	fi

.PHONY: generate-artifacts
generate-artifacts: ## Generate bootstrap installation scripts and documentation
	@printf "$(BLUE)[INFO]$(NC) Generating bootstrap installation artifacts...\n"
	@mkdir -p $(ARTIFACTS_DIR)
	@bash scripts/generate-bootstrap-installer.sh $(VERSION) $(REGISTRY) $(REGISTRY_NAMESPACE) $(ARTIFACTS_DIR)
	@printf "$(GREEN)✓$(NC) Bootstrap installation artifacts generated in $(ARTIFACTS_DIR)/\n"

.PHONY: release
release: validate validate-version clean sync-values update-versions generate-policy-list package-charts push-charts generate-artifacts ## Full release process (add INCLUDE_MIRROR=true for mirror artifacts)
	@if [ "$(INCLUDE_MIRROR)" = "true" ]; then \
		printf "$(BLUE)[INFO]$(NC) Generating mirror artifacts...\n"; \
		$(MAKE) generate-imageset-full VERSION=$(VERSION) || { \
			printf "$(YELLOW)[WARN]$(NC) Mirror artifact generation failed - continuing without them\n"; \
		}; \
	fi
	@echo ""
	@printf "$(GREEN)=========================================$(NC)\n"
	@printf "$(GREEN)Release preparation complete!$(NC)\n"
	@printf "$(GREEN)=========================================$(NC)\n"
	@echo ""
	@printf "$(BLUE)Version:$(NC) $(VERSION)\n"
	@printf "$(BLUE)Registry:$(NC) oci://$(REGISTRY)/$(REGISTRY_NAMESPACE)\n"
	@printf "$(BLUE)Charts:$(NC) $(shell ls -1 $(CHARTS_DIR) | wc -l)\n"
	@printf "$(BLUE)Artifacts:$(NC) $(ARTIFACTS_DIR)/\n"
	@if [ "$(INCLUDE_MIRROR)" = "true" ]; then \
		printf "$(BLUE)Mirror artifacts:$(NC) included\n"; \
	else \
		printf "$(BLUE)Mirror artifacts:$(NC) not included (use INCLUDE_MIRROR=true to generate)\n"; \
	fi
	@echo ""
	@if [ "$(DRY_RUN)" = "false" ]; then \
		printf "$(GREEN)Next steps:$(NC)\n"; \
		echo "  1. Create git tag: git tag v$(VERSION)"; \
		echo "  2. Push tag: git push origin v$(VERSION)"; \
		echo "  3. Create GitHub/GitLab release with artifacts from: $(ARTIFACTS_DIR)/"; \
	fi

.PHONY: release-mirror
release-mirror: ## Full release with mirror artifacts (imageset-config.yaml, operator-dependencies.json)
	@$(MAKE) release VERSION=$(VERSION) INCLUDE_MIRROR=true

.PHONY: package-only
package-only: validate clean generate-policy-list package-charts ## Package charts without updating versions or pushing
	@echo ""
	@printf "$(GREEN)Packaging complete!$(NC)\n"
	@echo "Charts are ready in: $(CHARTS_DIR)/"

.PHONY: generate-dependencies
generate-dependencies: ## Generate operator dependencies JSON (requires oc CLI and pull secret)
	@printf "$(BLUE)[INFO]$(NC) Generating operator dependencies...\n"
	@command -v oc >/dev/null 2>&1 || { printf "$(RED)[ERROR]$(NC) oc CLI is required. Install from https://mirror.openshift.com/pub/openshift-v4/clients/ocp/stable/\n"; exit 1; }
	@command -v jq >/dev/null 2>&1 || { printf "$(RED)[ERROR]$(NC) jq is required. Install: brew install jq\n"; exit 1; }
	@mkdir -p $(ARTIFACTS_DIR)
	@bash scripts/get-operator-dependencies.sh --all --json > $(ARTIFACTS_DIR)/operator-dependencies.json
	@printf "$(GREEN)✓$(NC) Dependencies saved to $(ARTIFACTS_DIR)/operator-dependencies.json\n"

# All values files for complete operator coverage
VALUES_FILES := $(shell ls autoshift/values*.yaml | tr '\n' ',' | sed 's/,$$//')

.PHONY: generate-imageset
generate-imageset: ## Generate ImageSetConfiguration for disconnected mirroring (all values files)
	@printf "$(BLUE)[INFO]$(NC) Generating ImageSetConfiguration from all values files...\n"
	@printf "$(BLUE)[INFO]$(NC) Values files: $(VALUES_FILES)\n"
	@mkdir -p $(ARTIFACTS_DIR)
	@if [ -f "$(ARTIFACTS_DIR)/operator-dependencies.json" ]; then \
		bash scripts/generate-imageset-config.sh $(VALUES_FILES) \
			--output $(ARTIFACTS_DIR)/imageset-config.yaml \
			--dependencies-file $(ARTIFACTS_DIR)/operator-dependencies.json \
			--include-autoshift-charts; \
	else \
		bash scripts/generate-imageset-config.sh $(VALUES_FILES) \
			--output $(ARTIFACTS_DIR)/imageset-config.yaml \
			--include-autoshift-charts; \
	fi
	@printf "$(GREEN)✓$(NC) ImageSetConfiguration saved to $(ARTIFACTS_DIR)/imageset-config.yaml\n"

.PHONY: generate-imageset-full
generate-imageset-full: generate-dependencies generate-imageset ## Generate ImageSetConfiguration with dependencies (requires oc CLI)
