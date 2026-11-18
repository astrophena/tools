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
	botURL  = "https://api.telegram.org/bot"
	fileURL = "https://api.telegram.org/file/bot"
)

// Module returns the telegram Starlark module.
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
		method string
		params *starlark.Dict
	)
	if err := starlark.UnpackArgs(b.Name(), args, kwargs, "method", &method, "args", &params); err != nil {
		return nil, err
	}

	encoded, err := starlark.Call(thread, starlarkjson.Module.Members["encode"], starlark.Tuple{params}, nil)
	if err != nil {
		return nil, fmt.Errorf("%s: failed to encode received args to JSON: %v", b.Name(), err)
	}
	body := json.RawMessage(encoded.(starlark.String))

	resp, err := request.Make[json.RawMessage](interpreter.Context(thread),
		m.requestParams(http.MethodPost, botURL+m.token+"/"+method, body))
	if err != nil {
		return nil, fmt.Errorf("%s: failed to make request: %s", b.Name(), err)
	}

	return starlark.Call(thread, starlarkjson.Module.Members["decode"], starlark.Tuple{starlark.String(resp)}, nil)
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
	fi, err := request.Make[fileInfo](ctx, m.requestParams(http.MethodPost,
		botURL+m.token+"/getFile", map[string]string{"file_id": fileID}))
	if err != nil {
		return nil, fmt.Errorf("%s: failed to call getFile on file %q: %v", b.Name(), fileID, err)
	}

	buf, err := request.Make[request.Bytes](ctx, m.requestParams(http.MethodGet,
		fileURL+m.token+"/"+fi.Result.FilePath, nil))
	if err != nil {
		return nil, fmt.Errorf("%s: failed to download file %q: %v", b.Name(), fileID, err)
	}

	return starlark.Bytes(buf), nil
}

func (m *module) requestParams(method, url string, body any) request.Params {
	return request.Params{
		Method:     method,
		URL:        url,
		Headers:    map[string]string{"User-Agent": version.UserAgent()},
		Body:       body,
		HTTPClient: m.httpc,
		Scrubber:   m.scrubber,
	}
}
