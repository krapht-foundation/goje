GOLANGCI_VERSION := v2.14.0
GOVULNCHECK_VERSION := v1.10.0

GOLANGCI := $(TOOLBIN)/golangci-lint-$(GOLANGCI_VERSION)
GOVULNCHECK := $(TOOLBIN)/govulncheck-$(GOVULNCHECK_VERSION)

$(GOLANGCI):
	mkdir -p "$(TOOLBIN)"
	GOBIN="$(TOOLBIN)" $(GO) install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_VERSION)
	mv "$(TOOLBIN)/golangci-lint" "$@"

$(GOVULNCHECK):
	mkdir -p "$(TOOLBIN)"
	GOBIN="$(TOOLBIN)" $(GO) install golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION)
	mv "$(TOOLBIN)/govulncheck" "$@"
