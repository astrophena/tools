// © 2024 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

/*
Tgfeed fetches RSS feeds and sends updates to Telegram. It's designed to be run
as a periodic job.

# Usage

	$ tgfeed [flags...] <command>

Where <command> is one of the following commands:

  - run: Fetch feeds and send updates to Telegram.
  - edit: Open the config.star configuration file in your $EDITOR for editing.
  - feeds: List all configured feeds and their status.
  - reenable: Re-enable a previously disabled feed by its URL.
  - admin: Start the admin API server for remote management and statistics download.

# Flags

  - -remote: Remote admin API URL for state management (e.g., 'http://localhost:8080'
    or '/run/tgfeed/admin-socket'). When specified, commands will interact with
    the remote admin API instead of local files.
  - -dry: Enable dry-run mode for the run command. Actions are logged but no
    updates are sent and state is not saved.

# Environment Variables

The tgfeed program relies on the following environment variables:

Required:

  - CHAT_ID: Telegram chat ID where the program sends new articles.
  - STATE_DIRECTORY: Directory where tgfeed stores its state files (config.star,
    state.json, error.tmpl). Defaults to $XDG_STATE_HOME/tgfeed directory if not
    set (~/.local/state/tgfeed).
  - TELEGRAM_TOKEN: Telegram bot token for accessing the Telegram Bot API.

Required for GitHub notifications special feed:

  - GITHUB_TOKEN: GitHub personal access token for accessing the GitHub API.

Optional:

  - ADMIN_ADDR: Address for the admin API server. Can be a TCP address like
    "localhost:8080" or a Unix socket path like "/run/tgfeed/admin-socket".
    Defaults to "/run/tgfeed/admin-socket".
  - ERROR_THREAD_ID: Telegram message thread ID where the program sends error
    notifications. This is applicable only for supergroups with topics enabled.

# Configuration

tgfeed loads its configuration from the config.star file in STATE_DIRECTORY.
This file is written in Starlark language and defines a list of feeds, for example:

	feed(
	    url="https://hnrss.org/newest",
	    title="Hacker News: Newest",
	    block_rule=lambda item: "pdf" in item.title.lower(), # Block PDF files.
	    message_thread_id=123, # Send updates to a specific topic.
	    digest=True, # Bundle updates into a single message.
	    format=lambda items: "Digest: " + str(len(items)) + " items", # Custom format.
	    always_send_new_items=True, # Send items even if they have an old publication date.
	)

Each feed can have a title, URL, and optional block and keep rules.
Optionally, message_thread_id can be specified to send updates from this
feed to a specific message thread (topic) within the chat. This is applicable
only for supergroups with topics enabled.

If always_send_new_items is set to true, tgfeed will send items even if they
have a publication date in the past. This is useful for feeds that add items
retrospectively, like CourtListener docket feeds. To avoid duplicates, tgfeed
tracks seen items in its state. Only items published within the last 14 days
are considered.

Digest mode can be enabled by setting digest to true. In this mode, updates
are bundled into a single message instead of sending one message per item.

A custom format function can be specified using the format argument. It takes
a single item (or a list of items in digest mode) and returns a string message.
In digest mode, the returned string is sent as the message body. In normal mode,
it is used as the message text. The format function can also return a tuple of
(text, keyboard) to attach an inline keyboard to the message.

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

tgfeed stores its state in local files within STATE_DIRECTORY.

It maintains a state for each feed, including last modified time, last updated
time, ETag, error count, and last error message. It keeps track of failing feeds
and disables them after a certain threshold of consecutive failures. State
information is stored and updated in the state.json file. You won't need to
touch this file at all, except in very rare cases.

The following files and directories are used:

  - config.star: Feed configuration written in Starlark.
  - state.json: Feed state information (last fetch times, errors, etc.).
  - error.tmpl: Optional custom error notification template.
  - stats/: A directory containing JSON files with statistics for each run.

# Stats Collection

tgfeed collects statistics for every execution and saves them as individual
JSON files in the $STATE_DIRECTORY/stats directory. This allows for historical
performance monitoring.

Stats include:

  - Total number of feeds
  - Number of successfully fetched feeds
  - Number of feeds that failed to fetch
  - Number of feeds that were not modified
  - Start time of a run
  - Duration of a run
  - Number of parsed RSS items
  - Total fetch time for all successful feeds
  - Average fetch time per successful feed
  - Memory usage at the end of the run

These stats can be downloaded in bulk as a single CSV file from the admin
server's /debug/stats.csv endpoint. This is useful for analyzing feed
performance and identifying problematic feeds over time.

# Administration

To edit the config.star file, you can use the edit command. This will open the
file in your default editor (specified by the $EDITOR environment variable).
After you save the changes and close the editor, the updated config.star will
be saved. For example:

	$ tgfeed edit

To reenable a failing feed that has been disabled due to consecutive failures,
you can use the reenable command followed by the URL of the feed. For example:

	$ tgfeed reenable https://example.com/feed

To view the list of feeds, you can use the feeds command. This will also print the
URLs of feeds that have encountered errors during fetching. For example:

	$ tgfeed feeds

# Remote Management

tgfeed supports remote state management via an HTTP API server. This is useful
when tgfeed runs on a server with tight sandboxing, but you want to manage its
configuration from your local machine.

To start the admin API server:

	$ tgfeed admin

The server listens on the address specified by ADMIN_ADDR (defaults to
/run/tgfeed/admin-socket). It provides a simple web interface for viewing and
editing the configuration and state, and for downloading run statistics via
/debug/stats.csv.

To manage tgfeed remotely, use the -remote flag with any command:

	$ tgfeed -remote=http://localhost:8080 feeds
	$ tgfeed -remote=http://localhost:8080 edit
	$ tgfeed -remote=http://localhost:8080 reenable https://example.com/feed

For Unix sockets, you can use SSH port forwarding:

	$ ssh -L 8080:/run/tgfeed/admin-socket server

Or connect directly with the socket path:

	$ tgfeed -remote=/run/tgfeed/admin-socket feeds

All state modification operations are blocked while the run command is active
(protected by a lock file). This prevents concurrent state corruption.

# Scheduling

tgfeed is intended to be run periodically. You can use a task scheduler like:

  - cron (on Linux/macOS)
  - systemd timer (on systemd-based Linux distributions)
  - GitHub Actions (using a scheduled workflow)
  - Task Scheduler (on Windows)
*/
package main

import (
	_ "embed"

	"go.astrophena.name/base/cli"
)

//go:embed doc.go
var doc []byte

func init() { cli.SetDocComment(doc) }
