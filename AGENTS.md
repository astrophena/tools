# Agent Guidelines

This document provides instructions for AI agents working in this repository.

## Commit Messages

Follow the Go style for commit messages.

- **Format:** `path/to/package: lowercase verb describing change`
- **Example:** `cmd/starlet: handle foo when bar`
- The subject line should be short and concise.
- Do not use a trailing period.
- Do not use Markdown in commit messages.
- Add `Assisted-by: [AI Model/Agent Name]` trailer to all commit messages.

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

Before submitting your changes, run the pre-commit checks from the repository
root:

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

## Go Style Guide

In addition to standard Go idioms, follow these specific style guidelines.

### Error Formatting

When using sentinel errors from `go.astrophena.name/base/web`, wrap them at the
beginning of the error message. For internal server errors, include the original
error message.

- **Bad:** `fmt.Errorf("a detailed error message: %w", web.ErrUnauthorized)`
- **Good:** `fmt.Errorf("%w: a detailed error message", web.ErrUnauthorized)`
- **Good:**
  `fmt.Errorf("%w: failed to perform action: %v", web.ErrInternalServerError, err)`

### Logging

Use explicit `slog` attribute constructors instead of alternating key-value
pairs.

- **Bad (and won't work):**
  `logger.Info(ctx, "authenticated request", "repo", claims.Repository)`
- **Good:**
  `logger.Info(ctx, "authenticated request", slog.String("repo", claims.Repository))`

### Modern Go Features

Prefer modern Go features and standard library packages where they improve
clarity and conciseness.

- **Use the `slices` package for slice operations.**
  - **Bad:**
    ```go
    isAllowed := false
    for _, s := range sites {
        if s == host {
            isAllowed = true
            break
        }
    }
    ```
  - **Good:** `isAllowed := slices.Contains(sites, host)`
- **Use modern octal literals.**
  - **Bad:** `os.MkdirAll(path, 0755)`
  - **Good:** `os.MkdirAll(path, 0o755)`

### API Responses

Keep successful JSON API responses minimal and consistent.

- **Bad:** `{"ok": true, "message": "Action was successful."}`
- **Good:** `{"status": "success"}`
