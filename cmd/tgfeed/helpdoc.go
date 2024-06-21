// Code generated by gendocs.go; DO NOT EDIT.

package main

import "go.astrophena.name/tools/internal/cli"

const helpDoc = `
Tgfeed fetches RSS feeds and sends new articles via Telegram.

# How it works?

tgfeed runs as a GitHub Actions workflow.

It fetches RSS feeds from URLs provided in the feeds.json file on GitHub Gist.

New articles are sent to a Telegram chat specified by the CHAT_ID environment
variable.

# Where it keeps state?

tgfeed stores it's state on GitHub Gist.

It maintains a state for each feed, including last modified time, last updated
time, ETag, error count, and last error message. It keeps track of failing
feeds and disables them after a certain threshold of consecutive failures.
State information is stored and updated in the state.json file on GitHub Gist.

# Environment variables

The tgfeed program relies on the following environment variables:

  - CHAT_ID: Telegram chat ID where the program sends new articles.
  - GIST_ID: GitHub Gist ID where the program stores its state.
  - GITHUB_TOKEN: GitHub personal access token for accessing the GitHub API.
  - TELEGRAM_TOKEN: Telegram bot token for accessing the Telegram Bot API.

# Summarization with Gemini API

tgfeed can summarize the text content of articles using the Gemini API. This
feature requires setting the GEMINI_API_KEY environment variable. When provided,
tgfeed will attempt to summarize the description field of fetched RSS items and
include the summary in the Telegram notification along with the article title
and link.

# Administration

To subscribe to a feed, you can use the -subscribe flag followed by the URL of
the feed. For example:

    $ tgfeed -subscribe https://example.com/feed

To unsubscribe from a feed, you can use the -unsubscribe flag followed by the
URL of the feed. For example:

    $ tgfeed -unsubscribe https://example.com/feed

To reenable a failing feed that has been disabled due to consecutive failures,
you can use the -reenable flag followed by the URL of the feed. For example:

    $ tgfeed -reenable https://example.com/feed

To view the list of feeds, you can use the -feeds flag. This will also print the
URLs of feeds that have encountered errors during fetching. For example:

    $ tgfeed -feeds
`

func init() { cli.SetDescription(helpDoc) }
