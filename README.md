# Prix-Essence ⛽

Application web légère pour afficher **les prix des carburants en France** (diesel, essence, éthanol, GPLc) avec :
- une **carte interactive** (Leaflet + OpenStreetMap) avec marqueurs **colorés selon le prix** ;
- la **localisation** : géolocalisation, recherche par ville/code postal, favoris ;
- l'**historique 14 jours** du prix par station (mini-courbes dans les popups) ;
- un **sélecteur de carburant** (Gazole, SP95, E10, SP98, E85, GPLc).

Données issues du jeu de données officiel de l'État
[*Prix des carburants en France (temps réel)*](https://www.data.gouv.fr/fr/datasets/prix-des-carburants-en-francetemps-reel/)
(Ministère de l'Économie, **data.gouv.fr**, ~13 000 stations, mise à jour plusieurs fois par jour).
Il s'agit d'informations indicatives fournies par les distributeurs.

## Architecture

- **Backend** : Go (stdlib, aucun framework) — **un seul binaire statique** (~15 Mo).
- **Base de données** : SQLite embarqué (`modernc.org/sqlite`, **pas de CGO**).
- **Frontend** : HTML/CSS/JS vanilla + [Leaflet](https://leafletjs.com/) (CDN) embarqué via `go:embed`.
- **Planificateur** : rafraîchissement auto toutes les 30 min + au démarrage + bouton « Actualiser ».
- **Déploiement** : LXC Alpine sur Proxmox + service OpenRC/systemd + **Caddy** (HTTPS auto).

Empreinte disque cible sur le LXC : **≈ 200–250 Mo** (rootfs Alpine + binaire + base + Caddy).

## Pré-requis (dev local)

- [Go](https://go.dev/dl/) ≥ 1.22
- GitHub CLI `gh` (facultatif, pour créer le dépôt privé)

## Démarrage en mode dev

```bash
make dev
# ouvre http://localhost:8080
```

Le serveur télécharge les données au démarrage (first refresh), puis toutes les 30 min.
Le fichier de base est créé dans `./data/dev.db`.

Tests :

```bash
make test
```

## Dépôt GitHub privé

```bash
gh repo create Prix-Essence --private --source=. --push
```

La CI (`.github/workflows/ci.yml`) exécute `go vet`, les tests et un build Linux à
chaque push pour valider avant déploiement.

## Déploiement sur un LXC Proxmox (Alpine)

1. **Crée le LXC** (concours Proxmox) : template **Alpine** (ou Debian minimal), unprivileged,
   256 Mo RAM, rootfs ~1–2 Go, réseau DHCP sur `vmbr0`. Note l'IP.
2. **Configure la cible** : édite `LXC_HOST` / `LXC_PORT` dans le `Makefile` (ou via `.env`).
3. **Build + déploiement** (depuis le Mac) :

   ```bash
   make deploy
   ```

   Cela compile le binaire Linux (arm64→amd64), l'envoie au LXC, installe le service
   OpenRC et le démarre.

   Requiert `scp`/`ssh` par clé vers le LXC, et ces paquets sur le LXC :
   ```
   apk add bash curl ca-certificates tzdata
   ```

4. **Reverse proxy HTTPS (Caddy)** — sur le LXC :

   ```
   apk add caddy
   cp /tmp/Caddyfile /etc/caddy/Caddyfile   # édite ton domaine
   rc-service caddy start
   ```

   Enregistre en DNS un enregistrement `A` de ton domaine vers l'IP du LXC.

## Scripts

| Cible            | Description                                            |
|------------------|--------------------------------------------------------|
| `make dev`       | Build + lance le serveur en local                       |
| `make test`      | `go vet` + tests unitaires                              |
| `make build-linux`| Cross-compile le binaire Linux pour le LXC (amd64)      |
| `make deploy`    | Build Linux + copie + installe le service sur le LXC    |
| `make clean`     | Supprime `bin/` et `data/`                              |

## Structure

```
cmd/server/            entrypoint (config, HTTP, scheduler)
internal/config/       configuration (flags + env)
internal/db/           SQLite : schéma + store (stations, prix, historique)
internal/fetcher/      résolution data.gouv.fr + téléchargement + parse JSON/CSV
internal/scheduler/    rafraîchissement périodique
internal/geo/          distance haversine, bounding box
internal/api/          endpoints JSON
web/                   frontend embarqué (go:embed)
deploy/                install.sh, service OpenRC, Caddyfile
scripts/               build.sh, deploy.sh
```
