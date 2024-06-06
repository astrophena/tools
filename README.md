This repository holds personal tools:

- cmdtop displays the top of most used commands in bash history.
- dupfind finds duplicate files in a directory.
- goupdate checks the Go version specified in the go.mod file of a Go project, updates it to the latest Go version if it is outdated, and creates a GitHub pull request with the updated go.mod file.
- renamer renames files sequentially.
- starlet implements a Telegram bot written in [Starlark] language.
- tgfeed fetches RSS feeds and sends new articles via Telegram.

Install them:

```sh
go install go.astrophena.name/tools/cmd/...@master
```
