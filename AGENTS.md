# Repository Workflow

## Before Pushing

- Run `go test ./...`.
- Run `go vet ./...`.
- Run `golangci-lint run ./...`.
- Keep changes on a feature branch; do not push directly to `main`.
- Open a pull request into `main` and wait for the required `test` and `lint` checks.

## Merge Policy

- Use squash merge for pull requests.
- Keep `main` linear.
- Delete feature branches after merge.
- Same-repository pull requests can have auto-merge enabled automatically. Fork pull requests must be handled manually.
