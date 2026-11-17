// © 2024 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

/*
Tgfeed fetches RSS and Atom feeds and sends updates to Telegram. Run it on a schedule.

# Usage

	$ tgfeed [flags] run
	$ tgfeed feeds
	$ tgfeed edit
	$ tgfeed reenable https://example.com/feed
	$ tgfeed admin

Use -dry with run to log actions without sending updates or saving feed state.
Use -remote to read and write state through an admin API over HTTP or a Unix
socket.

# Environment

  - CHAT_ID and TELEGRAM_TOKEN configure Telegram delivery.
  - STATE_DIRECTORY defaults to $XDG_STATE_HOME/tgfeed or ~/.local/state/tgfeed.
  - ERROR_THREAD_ID selects a Telegram topic for error notifications.
  - EDITOR selects the editor for the edit command.
  - ADMIN_ADDR defaults to /run/tgfeed/admin-socket and also accepts TCP addresses.
  - GITHUB_TOKEN enables tgfeed://github-notifications. Use a token with access
    to notifications. Tgfeed marks notifications as read after delivery.

# Configuration

Add feed declarations to config.star in the state directory.

	feed(
	    url="https://hnrss.org/newest",
	    title="Hacker News",
	    block_rule=lambda item: "pdf" in item.title.lower(),
	    message_thread_id=123,
	    digest=True,
	    format=lambda items: "Digest: " + str(len(items)) + " items",
	    always_send_new_items=True,
	)

A block_rule excludes an item when it returns True. A keep_rule keeps only
items for which it returns True. Both take an item with title, url,
description, content, categories, enclosures, extensions, guid, and
published fields. Enclosures have url, type, and length fields.

The format function takes an item, or a list of items in digest mode. It can
return a string, a (text, keyboard) tuple, or a (text, keyboard, media) tuple.
The keyboard is a list of rows of dictionaries with text and url fields.
Media is a list of dictionaries with type and url fields, for example
{"type": "photo", "url": "https://example.com/image.jpg"}. Tgfeed batches
multiple attachments into Telegram albums.

Set message_thread_id to send updates to a Telegram topic. Set
always_send_new_items to consider unseen items with older dates, within a
14-day lookback. Tgfeed uses the updated date when publication time is absent.

# State and administration

  - config.star holds the feed configuration.
  - state.json tracks fetch status, seen items, and pending deliveries.
  - error.tmpl supplies an optional error notification template.
  - stats.sqlite3 holds run statistics, also available from /api/stats.

Items remain pending until delivery and any source acknowledgment succeed.
Tgfeed disables feeds after repeated fetch failures. Use reenable with the
feed URL to resume fetching. The admin API rejects state changes during a run.

Use tgfeed admin to serve the dashboard and API. The remote flag accepts an
HTTP address or a Unix socket path.

	$ tgfeed -remote=http://localhost:8080 edit
	$ tgfeed -remote=/run/tgfeed/admin-socket feeds
	$ ssh -L 8080:/run/tgfeed/admin-socket server
*/
package main

import (
	_ "embed"

	"go.astrophena.name/base/cli"
)

//go:embed doc.go
var doc []byte

func init() { cli.SetDocComment(doc) }
