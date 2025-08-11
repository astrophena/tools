// © 2025 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

/*
Package telegram contains a Starlark module that exposes the Telegram Bot API.

This module provides two functions: call and get_file.

# call

The call function takes two arguments:

  - method (string): The Telegram Bot API method to call.
  - args (dict): The arguments to pass to the method.

For example, to send a message to a chat:

	response = telegram.call(
	    method="sendMessage",
	    args={
	        "chat_id": 123456789,
	        "text": "Hello, world!",
	    }
	)

The response variable will contain the response from the Telegram Bot API.

# get_file

The get_file function takes one argument:

  - file_id (string): The ID of the file to download.

It returns the content of the file as bytes. For example:

	file_content = telegram.get_file(file_id="...")
*/
package telegram

import (
	_ "embed"
	"sync"

	"go.astrophena.name/tools/internal/starlark/lib/internal"
)

//go:embed doc.go
var doc []byte

var Documentation = sync.OnceValue(func() string {
	return internal.ParseDocComment(doc)
})
