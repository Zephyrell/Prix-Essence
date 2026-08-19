# Prix-Essence — Makefile
BINARY   := bin/prix-essence
DB_PATH  := ./data/dev.db
LISTEN   := localhost:8080
# Cible de déploiement (à adapter pour ton LXC Proxmox)
LXC_HOST := root@192.168.1.100
LXC_PORT := 22

.PHONY: all dev build test vet build-linux build-mac run clean deploy

all: build

## Compiler le binaire pour la machine locale
build:
	go build -o $(BINARY) ./cmd/server

## Lancer en mode dev local (build + run + refresh à froid)
dev: build
	mkdir -p data
	DB_PATH=$(DB_PATH) LISTEN=$(LISTEN) REFRESH_ON_START=true ./$(BINARY)

## Tester (unit + intégration)
test:
	go vet ./...
	go test ./... -count=1

## Compiler le binaire Linux pour le LXC (AMD64)
build-linux:
	mkdir -p bin
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o bin/prix-essence-linux-amd64 ./cmd/server

## Compiler le binaire macOS (arm64)
build-mac:
	CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -trimpath -ldflags="-s -w" -o bin/prix-essence-darwin-arm64 ./cmd/server

run:
	mkdir -p data
	DB_PATH=$(DB_PATH) LISTEN=$(LISTEN) ./$(BINARY)

clean:
	rm -rf bin data

## Déployer le binaire + les scripts sur le LXC (SSH) puis installer le service
deploy: build-linux
	chmod +x scripts/deploy.sh
	LXC_HOST=$(LXC_HOST) LXC_PORT=$(LXC_PORT) ./scripts/deploy.sh
