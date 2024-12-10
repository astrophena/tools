// © 2024 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

/*
Tgfeed fetches RSS feeds and sends new articles via Telegram.

# Usage

	$ tgfeed [flags...]

# Environment Variables

The tgfeed program relies on the following environment variables:

  - CHAT_ID: Telegram chat ID where the program sends new articles.
  - GIST_ID: GitHub Gist ID where the program stores its state.
  - GITHUB_TOKEN: GitHub personal access token for accessing the GitHub API.
  - TELEGRAM_TOKEN: Telegram bot token for accessing the Telegram Bot API.
  - STATS_SPREADSHEET_ID: ID of the Google Spreadsheet to which the program uploads
    statistics for every run. This is required if the SERVICE_ACCOUNT_KEY is
    provided.
  - STATS_SPREADSHEET_RANGE: Range of the Google Spreadsheet to which the
    program uploads statistics for every run. Defaults to "Stats".
  - SERVICE_ACCOUNT_KEY: JSON string representing the service account key for
    accessing the Google API. It's not required, and stats won't be uploaded to a
    spreadsheet if this variable is not set.

# Configuration

tgfeed loads it's configuration from config.star file on GitHub Gist. This file
is written in Starlark language and defines a list of feeds, for example:

	feeds = [
	    feed(
	        url = "https://hnrss.org/newest",
	        title = "Hacker News: Newest",
	        block_rule = lambda item: "pdf" in item.title.lower(), # Block PDF files.
	    )
	]

Each feed can have a title, URL, and optional block and keep rules.

Block and keep rules are Starlark functions that take a feed item as an argument
and return a boolean value. If a block rule returns true, the item is not sent
to Telegram. If a keep rule returns true, the item is sent to Telegram;
otherwise, it is not.

The feed item passed to block_rule and keep_rule is a struct with the following
keys:

  - title: The title of the item.
  - url: The URL of the item.
  - description: The description of the item.
  - content: The content of the item.
  - categories: A list of categories the item belongs to.

# Special Feeds

tgfeed supports special feed URLs for integrating with services other than
traditional RSS/Atom feeds.

You can use the special URL tgfeed://github-notifications as a feed URL to
receive your GitHub notifications via Telegram and mark them as read on GitHub.
This requires a GitHub personal access token with the notifications scope, which
should be provided via the GITHUB_TOKEN environment variable.

# State

tgfeed stores it's state on GitHub Gist.

It maintains a state for each feed, including last modified time, last updated
time, ETag, error count, and last error message. It keeps track of failing feeds
and disables them after a certain threshold of consecutive failures. State
information is stored and updated in the state.json file on GitHub Gist. You
won't need to touch this file at all, except from very rare cases.

# Stats Collection

tgfeed collects and reports stats about every run to Google Sheets.
You can specify the ID of the spreadsheet via the STATS_SPREADSHEET_ID
environment variable. To collect stats, you must provide the SERVICE_ACCOUNT_KEY
environment variable with JSON string representing the service account key for
accessing the Google API. Stats include:

  - Total number of feeds fetched
  - Number of successfully fetched feeds
  - Number of feeds that failed to fetch
  - Number of feeds that were not modified
  - Start time of a run
  - Duration of a run
  - Number of parsed RSS items
  - Total fetch time
  - Average fetch time
  - Memory usage

You can use these stats to monitor performance of tgfeed and understand which
feeds are causing problems.

# Administration

To edit the config.star file, you can use the -edit flag. This will open the
file in your default editor (specified by the $EDITOR environment variable).
After you save the changes and close the editor, the updated config.star will
be saved back to the Gist. For example:

	$ tgfeed -edit

To reenable a failing feed that has been disabled due to consecutive failures,
you can use the -reenable flag followed by the URL of the feed. For example:

	$ tgfeed -reenable https://example.com/feed

To view the list of feeds, you can use the -feeds flag. This will also print the
URLs of feeds that have encountered errors during fetching. For example:

	$ tgfeed -feeds
*/
package main

import (
	_ "embed"

	"go.astrophena.name/tools/internal/cli"
)

//go:embed doc.go
var doc []byte

func init() { cli.SetDocComment(doc) }
