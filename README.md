# [`go.astrophena.name/tools`](https://go.astrophena.name/tools)

This is a collection of my personal tools.

See documentation at https://go.astrophena.name/tools.

> [!WARNING]
> These tools are for personal use, subject to change without notice and may gain or lose functionality at any time. Don't depend on them (or do, I don't care).
> 
> In addition, I use AI agents to help me work on these tools. This is not vibe coding (I read what these soulless machines write!), but there may be (and will be!) bugs in this code.

## Development

You need the latest version of [Go] installed.

First, clone the repository:

```sh
$ git clone https://github.com/astrophena/tools
$ cd tools
```

To run the tests, run:

```sh
$ go test ./...
```

To set up the Git pre-commit hook for development:

```sh
$ go tool pre-commit
```

## License

This code is licensed under the [ISC](LICENSE.md) license.

[go]: https://go.dev
