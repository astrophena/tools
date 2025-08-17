// © 2024 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

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

const (
	tgURL     = "https://api.telegram.org/bot"
	tgFileURL = "https://api.telegram.org/file/bot"
)

// Module returns a Starlark module that exposes the Telegram Bot API.
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
		URL:    tgURL + m.token + "/" + string(method),
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

	ctx := interpreter.Context(thread)

	type fileInfo struct {
		Result struct {
			FilePath string `json:"file_path"`
		} `json:"result"`
	}

	fi, err := request.Make[fileInfo](ctx, request.Params{
		Method: http.MethodPost,
		URL:    tgURL + m.token + "/getFile",
		Headers: map[string]string{
			"User-Agent": version.UserAgent(),
		},
		Body: map[string]string{
			"file_id": fileID,
		},
		HTTPClient: m.httpc,
		Scrubber:   m.scrubber,
	})
	if err != nil {
		return nil, fmt.Errorf("%s: failed to call getFile on file %q: %v", b.Name(), fileID, err)
	}

	buf, err := request.Make[request.Bytes](ctx, request.Params{
		Method: http.MethodGet,
		URL:    tgFileURL + m.token + "/" + fi.Result.FilePath,
		Headers: map[string]string{
			"User-Agent": version.UserAgent(),
		},
		HTTPClient: m.httpc,
		Scrubber:   m.scrubber,
	})
	if err != nil {
		return nil, fmt.Errorf("%s: failed to download file %q: %v", b.Name(), fileID, err)
	}

	return starlark.Bytes(buf), nil
}
