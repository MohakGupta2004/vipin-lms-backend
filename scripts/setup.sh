#!/usr/bin/env bash
# Install everything needed to run vipin-lms-backend locally (except .env).
#   macOS  : Homebrew
#   Linux  : apt (Debian/Ubuntu), dnf (Fedora/RHEL), pacman (Arch)
#
# Installs: Go, PostgreSQL 17 (server + psql), golang-migrate, swag, air, git, make.
# Then: starts Postgres, creates the DB, runs migrations, downloads Go modules.
#
# Usage: ./scripts/setup.sh [--no-db] [--no-migrate]
#   --no-db       skip creating the database (and migrations)
#   --no-migrate  skip running migrations
set -euo pipefail

PG_VERSION=17
GO_MIN="1.26"
POSTGRES_USER="${POSTGRES_USER:-postgres}"
POSTGRES_PASSWORD="${POSTGRES_PASSWORD:-mohak123}"
POSTGRES_DB="${POSTGRES_DB:-vipin}"
DB_PORT="${DB_PORT:-5432}"

DO_DB=1
DO_MIGRATE=1
for arg in "$@"; do
  case "$arg" in
    --no-db) DO_DB=0; DO_MIGRATE=0 ;;
    --no-migrate) DO_MIGRATE=0 ;;
    -h|--help) sed -n '2,11p' "$0"; exit 0 ;;
    *) echo "unknown flag: $arg" >&2; exit 1 ;;
  esac
done

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

log()  { printf '\033[1;34m==>\033[0m %s\n' "$*"; }
warn() { printf '\033[1;33m!!\033[0m  %s\n' "$*" >&2; }
die()  { printf '\033[1;31mxx\033[0m  %s\n' "$*" >&2; exit 1; }
have() { command -v "$1" >/dev/null 2>&1; }

SUDO=""
if [ "$(id -u)" -ne 0 ]; then
  have sudo && SUDO="sudo" || true
fi

# version_ge A B -> true if A >= B (dotted versions)
version_ge() { [ "$(printf '%s\n%s\n' "$2" "$1" | sort -V | head -n1)" = "$2" ]; }

OS="$(uname -s)"
PM=""
case "$OS" in
  Darwin) PM="brew" ;;
  Linux)
    if   have apt-get; then PM="apt"
    elif have dnf;     then PM="dnf"
    elif have pacman;  then PM="pacman"
    else die "no supported package manager (apt/dnf/pacman)"; fi ;;
  *) die "unsupported OS: $OS (use docker compose instead)" ;;
esac
log "OS: $OS, package manager: $PM"

# ---------- package manager bootstrap ----------
if [ "$PM" = "brew" ]; then
  if ! have brew; then
    log "Installing Homebrew"
    NONINTERACTIVE=1 /bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh)"
  fi
  # make brew available in this shell (Apple Silicon / Intel)
  for p in /opt/homebrew/bin/brew /usr/local/bin/brew; do
    [ -x "$p" ] && eval "$("$p" shellenv)" && break
  done
  have brew || die "brew not on PATH after install"
elif [ "$PM" = "apt" ]; then
  $SUDO apt-get update -y
fi

# ---------- Go ----------
go_ok() { have go && version_ge "$(go env GOVERSION | sed 's/^go//')" "$GO_MIN"; }

install_go_tarball() {
  local arch goos ver
  case "$(uname -m)" in
    x86_64|amd64) arch=amd64 ;;
    arm64|aarch64) arch=arm64 ;;
    *) die "unsupported arch $(uname -m)" ;;
  esac
  goos="$(echo "$OS" | tr '[:upper:]' '[:lower:]')"
  ver="$(grep -E '^go ' go.mod | awk '{print $2}')"
  log "Installing Go $ver from go.dev to /usr/local/go"
  curl -fsSL "https://go.dev/dl/go${ver}.${goos}-${arch}.tar.gz" -o /tmp/go.tgz
  $SUDO rm -rf /usr/local/go
  $SUDO tar -C /usr/local -xzf /tmp/go.tgz
  rm -f /tmp/go.tgz
  export PATH="/usr/local/go/bin:$PATH"
  warn "Add to your shell profile: export PATH=\"/usr/local/go/bin:\$HOME/go/bin:\$PATH\""
}

if go_ok; then
  log "Go ok: $(go version)"
else
  case "$PM" in
    brew)   log "Installing Go"; brew install go ;;
    pacman) log "Installing Go"; $SUDO pacman -S --noconfirm go ;;
    *)      install_go_tarball ;;  # apt/dnf ship old Go; use official tarball
  esac
  go_ok || die "Go >= $GO_MIN required (go.mod wants $(grep -E '^go ' go.mod | awk '{print $2}'))"
fi
export PATH="$(go env GOPATH)/bin:$PATH"

# ---------- git, make, curl ----------
case "$PM" in
  brew)   brew list git >/dev/null 2>&1 || brew install git; have make || xcode-select --install || true ;;
  apt)    $SUDO apt-get install -y git make curl ca-certificates gnupg lsb-release ;;
  dnf)    $SUDO dnf install -y git make curl ;;
  pacman) $SUDO pacman -S --noconfirm --needed git make curl ;;
esac

# ---------- PostgreSQL ----------
PG_BIN_HINT=""
case "$PM" in
  brew)
    if ! brew list "postgresql@${PG_VERSION}" >/dev/null 2>&1; then
      log "Installing postgresql@${PG_VERSION}"
      brew install "postgresql@${PG_VERSION}"
    fi
    PG_BIN_HINT="$(brew --prefix "postgresql@${PG_VERSION}")/bin"
    ;;
  apt)
    if ! apt-cache policy "postgresql-${PG_VERSION}" 2>/dev/null | grep -q Candidate:.*[0-9]; then
      log "Adding PGDG apt repo"
      $SUDO install -d /usr/share/postgresql-common/pgdg
      curl -fsSL https://www.postgresql.org/media/keys/ACCC4CF8.asc | $SUDO tee /usr/share/postgresql-common/pgdg/apt.postgresql.org.asc >/dev/null
      echo "deb [signed-by=/usr/share/postgresql-common/pgdg/apt.postgresql.org.asc] https://apt.postgresql.org/pub/repos/apt $(lsb_release -cs)-pgdg main" \
        | $SUDO tee /etc/apt/sources.list.d/pgdg.list >/dev/null
      $SUDO apt-get update -y
    fi
    log "Installing postgresql-${PG_VERSION}"
    $SUDO apt-get install -y "postgresql-${PG_VERSION}" "postgresql-client-${PG_VERSION}"
    PG_BIN_HINT="/usr/lib/postgresql/${PG_VERSION}/bin"
    ;;
  dnf)
    log "Installing PostgreSQL ${PG_VERSION}"
    $SUDO dnf install -y "https://download.postgresql.org/pub/repos/yum/reporpms/EL-$(rpm -E %{rhel})-$(uname -m)/pgdg-redhat-repo-latest.noarch.rpm" || true
    $SUDO dnf install -y "postgresql${PG_VERSION}-server" "postgresql${PG_VERSION}-contrib"
    $SUDO "/usr/pgsql-${PG_VERSION}/bin/postgresql-${PG_VERSION}-setup" initdb || true
    PG_BIN_HINT="/usr/pgsql-${PG_VERSION}/bin"
    ;;
  pacman)
    log "Installing PostgreSQL"
    $SUDO pacman -S --noconfirm --needed postgresql
    if [ ! -d /var/lib/postgres/data/base ]; then
      $SUDO -u postgres initdb -D /var/lib/postgres/data
    fi
    ;;
esac
[ -n "$PG_BIN_HINT" ] && [ -d "$PG_BIN_HINT" ] && export PATH="$PG_BIN_HINT:$PATH"
have psql || die "psql not on PATH"
log "Postgres client: $(psql --version)"

# ---------- Go tools: migrate, swag, air ----------
if ! have migrate; then
  if [ "$PM" = "brew" ]; then
    log "Installing golang-migrate"; brew install golang-migrate
  else
    log "Installing golang-migrate (go install)"
    go install -tags 'postgres' github.com/golang-migrate/migrate/v4/cmd/migrate@v4.18.3
  fi
fi
have swag || { log "Installing swag";  go install github.com/swaggo/swag/cmd/swag@v1.16.6; }
have air  || { log "Installing air";   go install github.com/air-verse/air@latest; }

# ---------- start Postgres ----------
start_postgres() {
  case "$PM" in
    brew)   brew services start "postgresql@${PG_VERSION}" ;;
    apt)    $SUDO systemctl enable --now postgresql || $SUDO service postgresql start ;;
    dnf)    $SUDO systemctl enable --now "postgresql-${PG_VERSION}" ;;
    pacman) $SUDO systemctl enable --now postgresql ;;
  esac
}
pg_ready() { pg_isready -q -h 127.0.0.1 -p "$DB_PORT" 2>/dev/null; }

if [ "$DO_DB" -eq 1 ]; then
  if ! pg_ready; then
    log "Starting PostgreSQL"
    start_postgres
    for _ in $(seq 1 30); do pg_ready && break; sleep 1; done
  fi
  pg_ready || die "Postgres not accepting connections on 127.0.0.1:$DB_PORT (another instance on that port? set DB_PORT)"

  # Run an admin SQL statement as a superuser. Homebrew: role = $USER. Linux: sudo -u postgres.
  admin_psql() {
    if [ "$PM" = "brew" ]; then
      psql -h 127.0.0.1 -p "$DB_PORT" -d postgres -v ON_ERROR_STOP=1 "$@"
    else
      (cd /tmp && $SUDO -u postgres psql -p "$DB_PORT" -d postgres -v ON_ERROR_STOP=1 "$@")
    fi
  }

  log "Ensuring role '$POSTGRES_USER' and database '$POSTGRES_DB'"
  if ! admin_psql -tAc "SELECT 1 FROM pg_roles WHERE rolname='${POSTGRES_USER}'" | grep -q 1; then
    admin_psql -c "CREATE ROLE \"${POSTGRES_USER}\" WITH LOGIN SUPERUSER PASSWORD '${POSTGRES_PASSWORD}'"
  else
    admin_psql -c "ALTER ROLE \"${POSTGRES_USER}\" WITH LOGIN SUPERUSER PASSWORD '${POSTGRES_PASSWORD}'"
  fi
  if ! admin_psql -tAc "SELECT 1 FROM pg_database WHERE datname='${POSTGRES_DB}'" | grep -q 1; then
    admin_psql -c "CREATE DATABASE \"${POSTGRES_DB}\" OWNER \"${POSTGRES_USER}\""
  fi

  DATABASE_URL="postgres://${POSTGRES_USER}:${POSTGRES_PASSWORD}@127.0.0.1:${DB_PORT}/${POSTGRES_DB}?sslmode=disable"

  if [ "$DO_MIGRATE" -eq 1 ]; then
    log "Running migrations"
    migrate -path ./internal/database/migrations -database "$DATABASE_URL" up
  fi
fi

# ---------- Go modules ----------
log "Downloading Go modules"
go mod download

cat <<EOF

Done.

Next:
  1. Create .env (needs JWT_SECRET_KEY, REFRESH_SECRET_KEY, RESEND_API_KEY, EMAIL_FROM).
     Local DB URL:
       DATABASE_URL=postgres://${POSTGRES_USER}:${POSTGRES_PASSWORD}@127.0.0.1:${DB_PORT}/${POSTGRES_DB}?sslmode=disable
  2. make run        # or: make air (hot reload)
  3. Ensure PATH has: $(go env GOPATH)/bin${PG_BIN_HINT:+ and $PG_BIN_HINT}
  Optional (GCS_ENABLE=true): install gcloud, run 'gcloud auth application-default login'.
EOF
