# Agent Guidelines

This document provides instructions for AI agents working in this repository.

## Commit Messages

Follow the Go style for commit messages.

- **Format:** `path/to/package: lowercase verb describing change`
- **Example:** `cmd/starlet: handle foo when bar`
- The subject line should be short and concise.
- Do not use a trailing period.
- Do not use Markdown in commit messages.

## Repository Structure

- **`cmd`**: Command-line tools.
- **`cmd/.../internal/...`**: Internal packages for a specific tool.
- **`internal/...`**: Shared internal packages.

## Documentation

- Document all Go packages and exported members.
- Write meaningful comments. Avoid stating the obvious.
  - **Bad:** `// WriteFile writes a file.`
  - **Good:** `// WriteFile writes data to a file, creating it if necessary.`

## Dependencies

- Avoid external dependencies unless absolutely necessary.
- A little duplication is better than a small dependency.

## Testing and Verification

Before submitting your changes, run the pre-commit checks from the repository root:

```sh
$ go tool pre-commit
```

This command runs `gofmt`, `ruff`, `staticcheck`, and more.

If you see errors about missing copyright headers, run this command from the
repository root to fix them:

```sh
$ go tool addcopyright
```

If the pre-commit tool fails because `ruff` is not installed, install it with:

```sh
$ pip install ruff
```
