
MAKEFILE_PATH = $(dir $(realpath -s $(firstword $(MAKEFILE_LIST))))

# Image URL to use all building/pushing image targets
IMG ?= amazon/aws-alb-ingress-controller:v2.2.1

CRD_OPTIONS ?= "crd:crdVersions=v1"

# Whether to override AWS SDK models. set to 'y' when we need to build against custom AWS SDK models.
AWS_SDK_MODEL_OVERRIDE ?= "n"

# Get the currently used golang install path (in GOPATH/bin, unless GOBIN is set)
ifeq (,$(shell go env GOBIN))
GOBIN=$(shell go env GOPATH)/bin
else
GOBIN=$(shell go env GOBIN)
endif

all: controller

# Run tests
test: generate fmt vet manifests helm-lint
	go test -race ./pkg/... ./webhooks/... -coverprofile cover.out

# Build controller binary
controller: generate fmt vet
	go build -o bin/controller main.go

# Run against the configured Kubernetes cluster in ~/.kube/config
run: generate fmt vet manifests
	go run ./main.go

# Install CRDs into a cluster
install: manifests
	$(KUSTOMIZE) build config/crd | kubectl apply -f -

# Uninstall CRDs from a cluster
uninstall: manifests
	$(KUSTOMIZE) build config/crd | kubectl delete -f -

# Deploy controller in the configured Kubernetes cluster in ~/.kube/config
deploy: manifests
	cd config/controller && $(KUSTOMIZE) edit set image controller=${IMG}
	$(KUSTOMIZE) build config/default | kubectl apply -f -

# Generate manifests e.g. CRD, RBAC etc.
manifests: controller-gen kustomize
	$(CONTROLLER_GEN) $(CRD_OPTIONS) rbac:roleName=controller-role webhook paths="./..." output:crd:artifacts:config=config/crd/bases
	yq eval '.metadata.name = "webhook"' -i config/webhook/manifests.yaml
	$(KUSTOMIZE) build config/crd > helm/aws-load-balancer-controller/crds/crds.yaml

# Run go fmt against code
fmt:
	go fmt ./...

# Run go vet against code
vet:
	go vet ./...

helm-lint:
	${MAKEFILE_PATH}/test/helm/helm-lint.sh

# Generate code
generate: aws-sdk-model-override controller-gen
	$(CONTROLLER_GEN) object:headerFile="hack/boilerplate.go.txt" paths="./..."

aws-sdk-model-override:
	@if [ "$(AWS_SDK_MODEL_OVERRIDE)" = "y" ] ; then \
		./scripts/aws_sdk_model_override/setup.sh ; \
	else \
		./scripts/aws_sdk_model_override/cleanup.sh ; \
	fi


# Push the docker image
docker-push:
	docker buildx build . --target bin \
        		--tag $(IMG) \
        		--push \
        		--platform linux/amd64,linux/arm64

# find or download controller-gen
# download controller-gen if necessary
controller-gen:
ifeq (, $(shell which controller-gen))
	@{ \
	set -e ;\
	CONTROLLER_GEN_TMP_DIR=$$(mktemp -d) ;\
	cd $$CONTROLLER_GEN_TMP_DIR ;\
	go mod init tmp ;\
	go install sigs.k8s.io/controller-tools/cmd/controller-gen@v0.5.0 ;\
	rm -rf $$CONTROLLER_GEN_TMP_DIR ;\
	}
CONTROLLER_GEN=$(GOBIN)/controller-gen
else
CONTROLLER_GEN=$(shell which controller-gen)
endif

# install kustomize if not found
kustomize:
ifeq (, $(shell which kustomize))
	@{ \
	set -e ;\
        KUSTOMIZE_TMP_DIR=$$(mktemp -d) ;\
        cd $$KUSTOMIZE_TMP_DIR ;\
        go mod init tmp ;\
        go install sigs.k8s.io/kustomize/kustomize/v3 ;\
        rm -rf $$KUSTOMIZE_TMP_DIR ;\
        }
KUSTOMIZE=$(GOBIN)/kustomize
else
KUSTOMIZE=$(shell which kustomize)
endif

# preview docs
docs-preview: docs-dependencies
	pipenv run mkdocs serve

# publish the versioned docs using mkdocs mike util
docs-publish: docs-dependencies
	pipenv run mike deploy v2.2 latest -p --update-aliases

# install dependencies needed to preview and publish docs
docs-dependencies:
	pipenv install --dev

lint:
	echo "TODO"

unit-test:
	./scripts/ci_unit_test.sh

e2e-test:
	./scripts/ci_e2e_test.sh
