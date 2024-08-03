package telegram

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"go.astrophena.name/tools/internal/request"

	"go.starlark.net/starlark"
	"go.starlark.net/starlarkjson"
	"go.starlark.net/starlarkstruct"
)

func Module(token string, client *http.Client) *starlarkstruct.Module {
	m := &module{
		httpc:    client,
		token:    token,
		scrubber: strings.NewReplacer(token, "[EXPUNGED]"),
	}
	return &starlarkstruct.Module{
		Name: "telegram",
		Members: starlark.StringDict{
			"call": starlark.NewBuiltin("telegram.call", m.call),
		},
	}
}

type module struct {
	httpc    *http.Client
	token    string
	scrubber *strings.Replacer
}

func (m *module) call(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	// Unpack arguments passed from Starlark code.
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

	// Encode received args to JSON.
	rawReqVal, err := starlark.Call(thread, starlarkjson.Module.Members["encode"], starlark.Tuple{argsDict}, []starlark.Tuple{})
	if err != nil {
		return nil, fmt.Errorf("%s: failed to encode received args to JSON: %v", b.Name(), err)
	}
	rawReq, ok := rawReqVal.(starlark.String)
	if !ok {
		return nil, fmt.Errorf("%s: unexpected return type of json.encode Starlark function", b.Name())
	}

	// Make Telegram Bot API request.
	// TODO: plumb the context from caller.
	rawResp, err := request.Make[json.RawMessage](context.Background(), request.Params{
		Method:     http.MethodPost,
		URL:        "https://api.telegram.org/bot" + m.token + "/" + string(method),
		Body:       json.RawMessage(rawReq),
		HTTPClient: m.httpc,
		Scrubber:   m.scrubber,
	})
	if err != nil {
		return nil, fmt.Errorf("%s: failed to make request: %s", b.Name(), err)
	}

	// Decode received JSON returned from Telegram and pass it back to Starlark code.
	return starlark.Call(thread, starlarkjson.Module.Members["decode"], starlark.Tuple{starlark.String(rawResp)}, []starlark.Tuple{})
}
