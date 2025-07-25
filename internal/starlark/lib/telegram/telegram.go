// © 2024 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

// Package telegram contains a Starlark module that exposes the Telegram Bot API.
package telegram

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"go.astrophena.name/base/request"
	"go.astrophena.name/base/version"
	"go.astrophena.name/tools/internal/starlark/interpreter"

	starlarkjson "go.starlark.net/lib/json"
	"go.starlark.net/starlark"
	"go.starlark.net/starlarkstruct"
)

// Module returns a Starlark module that exposes the Telegram Bot API.
//
// This module provides two functions: call and get_file.
//
// # call
//
// The call function takes two arguments:
//
//   - method (string): The Telegram Bot API method to call.
//   - args (dict): The arguments to pass to the method.
//
// For example, to send a message to a chat:
//
//	response = telegram.call(
//	    method="sendMessage",
//	    args={
//	        "chat_id": 123456789,
//	        "text": "Hello, world!",
//	    }
//	)
//
// The response variable will contain the response from the Telegram Bot API.
//
// # get_file
//
// The get_file function takes one argument:
//
//   - file_id (string): The ID of the file to download.
//
// It returns the content of the file as bytes. For example:
//
//	file_content = telegram.get_file(file_id="...")
func Module(token string, client *http.Client) *starlarkstruct.Module {
	m := &module{
		httpc:    client,
		token:    token,
		scrubber: strings.NewReplacer(token, "[EXPUNGED]"),
	}
	return &starlarkstruct.Module{
		Name: "telegram",
		Members: starlark.StringDict{
			"call":     starlark.NewBuiltin("telegram.call", m.call),
			"get_file": starlark.NewBuiltin("telegram.get_file", m.getFile),
		},
	}
}

type module struct {
	httpc    *http.Client
	token    string
	scrubber *strings.Replacer
}

func (m *module) call(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	if len(args) > 0 {
		return starlark.None, fmt.Errorf("%s: unexpected positional arguments", b.Name())
	}
	var (
		method   starlark.String
		argsDict *starlark.Dict
	)
	if err := starlark.UnpackArgs(b.Name(), args, kwargs, "method", &method, "args", &argsDict); err != nil {
		return nil, err
	}

	rawReqVal, err := starlark.Call(thread, starlarkjson.Module.Members["encode"], starlark.Tuple{argsDict}, []starlark.Tuple{})
	if err != nil {
		return nil, fmt.Errorf("%s: failed to encode received args to JSON: %v", b.Name(), err)
	}
	rawReq, ok := rawReqVal.(starlark.String)
	if !ok {
		return nil, fmt.Errorf("%s: unexpected return type of json.encode Starlark function", b.Name())
	}

	ctx := interpreter.Context(thread)
	rawResp, err := request.Make[json.RawMessage](ctx, request.Params{
		Method: http.MethodPost,
		URL:    "https://api.telegram.org/bot" + m.token + "/" + string(method),
		Body:   json.RawMessage(rawReq),
		Headers: map[string]string{
			"User-Agent": version.UserAgent(),
		},
		HTTPClient: m.httpc,
		Scrubber:   m.scrubber,
	})
	if err != nil {
		return nil, fmt.Errorf("%s: failed to make request: %s", b.Name(), err)
	}

	return starlark.Call(thread, starlarkjson.Module.Members["decode"], starlark.Tuple{starlark.String(rawResp)}, []starlark.Tuple{})
}

func (m *module) getFile(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var fileID string
	if err := starlark.UnpackArgs(b.Name(), args, kwargs, "file_id", &fileID); err != nil {
		return nil, err
	}

	type fileInfo struct {
		FilePath string `json:"file_path"`
	}

	fi, err := request.Make[fileInfo](interpreter.Context(thread), request.Params{
		Method: http.MethodGet,
		URL:    "https://api.telegram.org/bot" + m.token + "/getFile",
		Body: map[string]string{
			"file_id": fileID,
		},
		Scrubber: m.scrubber,
	})
	if err != nil {
		return nil, err
	}

	buf, err := request.Make[request.Bytes](interpreter.Context(thread), request.Params{
		Method:   http.MethodGet,
		URL:      "https://api.telegram.org/bot" + m.token + "/" + fi.FilePath,
		Scrubber: m.scrubber,
	})
	if err != nil {
		return nil, err
	}

	return starlark.Bytes(buf), nil
}
