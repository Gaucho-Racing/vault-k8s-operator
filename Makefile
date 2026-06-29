IMAGE ?= ghcr.io/gaucho-racing/vault-k8s-operator:latest

.PHONY: build
build:
	go build -o bin/manager ./cmd

.PHONY: run
run:
	go run ./cmd --vault-url=https://vault.gauchoracing.com

.PHONY: tidy
tidy:
	go mod tidy

.PHONY: docker-build
docker-build:
	docker build -t $(IMAGE) .

.PHONY: docker-push
docker-push:
	docker push $(IMAGE)

.PHONY: install
install:
	kubectl apply -f config/crd/bases

.PHONY: uninstall
uninstall:
	kubectl delete -f config/crd/bases

.PHONY: deploy
deploy:
	kubectl apply -k config/default

.PHONY: undeploy
undeploy:
	kubectl delete -k config/default
