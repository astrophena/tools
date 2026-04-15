import React, { useEffect, useRef } from "react";
import CodeMirror, { ReactCodeMirrorRef } from "@uiw/react-codemirror";
import { python } from "@codemirror/lang-python";
import {
  Decoration,
  DecorationSet,
  EditorView,
  WidgetType,
} from "@codemirror/view";
import {
  EditorState,
  RangeSetBuilder,
  StateEffect,
  StateField,
} from "@codemirror/state";
import { autocompletion, CompletionContext } from "@codemirror/autocomplete";

import { EditableResource } from "../types.ts";

function starlarkCompletions(context: CompletionContext) {
  const word = context.matchBefore(/\w*/);
  if (!word || (word.from === word.to && !context.explicit)) return null;
  return {
    from: word.from,
    options: [
      { label: "feed", type: "function", info: "feed(url, ...)" },
      { label: "url=", type: "keyword" },
      { label: "title=", type: "keyword" },
      { label: "message_thread_id=", type: "keyword" },
      { label: "block_rule=", type: "keyword" },
      { label: "keep_rule=", type: "keyword" },
      { label: "digest=", type: "keyword" },
      { label: "format=", type: "keyword" },
      { label: "always_send_new_items=", type: "keyword" },
      { label: "True", type: "keyword" },
      { label: "False", type: "keyword" },
    ],
  };
}

class ErrorWidget extends WidgetType {
  constructor(readonly message: string) {
    super();
  }
  override eq(other: ErrorWidget) {
    return other.message === this.message;
  }
  override toDOM() {
    const wrap = document.createElement("div");
    wrap.className = "cm-error-widget";
    wrap.textContent = this.message;
    return wrap;
  }
}

const setInlineError = StateEffect.define<
  { line: number; message: string } | null
>();

const inlineErrorField = StateField.define<DecorationSet>({
  create() {
    return Decoration.none;
  },
  update(decos, tr) {
    decos = decos.map(tr.changes);
    for (const e of tr.effects) {
      if (e.is(setInlineError)) {
        if (!e.value) {
          decos = Decoration.none;
        } else {
          const { line, message } = e.value;
          if (line > 0 && line <= tr.state.doc.lines) {
            const lineInfo = tr.state.doc.line(line);
            const builder = new RangeSetBuilder<Decoration>();
            builder.add(
              lineInfo.from,
              lineInfo.from,
              Decoration.widget({
                widget: new ErrorWidget(message),
                side: -1,
                block: true,
              }),
            );
            builder.add(
              lineInfo.from,
              lineInfo.to > lineInfo.from ? lineInfo.to : lineInfo.from,
              Decoration.mark({
                class: "cm-error-line",
              }),
            );
            decos = builder.finish();
          } else {
            decos = Decoration.none;
          }
        }
      }
    }
    return decos;
  },
  provide: (f) => EditorView.decorations.from(f),
});

const inlineErrorTheme = EditorView.theme({
  ".cm-error-widget": {
    backgroundColor: "rgba(239, 68, 68, 0.2)",
    color: "#fca5a5",
    padding: "4px 8px",
    borderRadius: "4px",
    margin: "0 0 4px 0",
    fontFamily: "var(--font-mono, monospace)",
    fontSize: "0.85em",
    whiteSpace: "pre-wrap",
    borderLeft: "2px solid #ef4444",
  },
  ".cm-error-line": {
    textDecoration: "underline wavy #ef4444",
    backgroundColor: "rgba(239, 68, 68, 0.1)",
  },
});

/** Props for a single editable resource panel. */
type EditorPanelProps = {
  title: string;
  description: string;
  placeholder: string;
  languageHint: string;
  resource: EditableResource;
};

/**
 * Renders a save/reload editor panel for a mutable backend resource.
 */
export const EditorPanel = React.memo(
  function EditorPanel(props: EditorPanelProps) {
    const { title, description, placeholder, languageHint, resource } = props;
    const editorRef = useRef<ReactCodeMirrorRef>(null);

    // Parse Starlark errors and jump to the corresponding line and column.
    useEffect(() => {
      const view = editorRef.current?.view;
      if (!view) return;

      if (!resource.error) {
        view.dispatch({ effects: setInlineError.of(null) });
        return;
      }

      const match = resource.error.match(/:(\d+):(\d+): (.*)/);
      if (!match) {
        view.dispatch({ effects: setInlineError.of(null) });
        return;
      }

      const line = parseInt(match[1], 10);
      const col = parseInt(match[2], 10);
      const message = match[3];

      view.dispatch({ effects: setInlineError.of({ line, message }) });

      const state = view.state;
      // Ensure the line exists within the document.
      if (line > 0 && line <= state.doc.lines) {
        const lineInfo = state.doc.line(line);
        // Calculate the exact position or fallback to the start of the line.
        const pos = col > 0 && col <= lineInfo.length + 1
          ? lineInfo.from + col - 1
          : lineInfo.from;

        view.dispatch({
          selection: { anchor: pos, head: pos },
          effects: EditorView.scrollIntoView(pos, {
            y: "center",
          }),
        });
        view.focus();
      }
    }, [resource.error]);

    return (
      <section className="panel editor-panel">
        <header className="panel-header">
          <div>
            <h2>{title}</h2>
            <p>{description}</p>
          </div>
          <div className="status-cluster">
            {resource.dirty
              ? <span className="pill pill-warning">Unsaved</span>
              : <span className="pill">Synced</span>}
            <span className="pill pill-subtle">{languageHint}</span>
          </div>
        </header>
        <div className="panel-actions">
          <button
            className="button button-ghost"
            type="button"
            onClick={() => {
              void resource.load();
            }}
            disabled={resource.loading || resource.saving}
          >
            {resource.loading ? "Loading..." : "Reload"}
          </button>
          <button
            className="button button-solid"
            type="button"
            onClick={() => {
              void resource.save();
            }}
            disabled={!resource.dirty || resource.loading || resource.saving}
          >
            {resource.saving ? "Saving..." : "Save"}
          </button>
        </div>
        <CodeMirror
          className="editor"
          theme="dark"
          ref={editorRef}
          extensions={[
            python(),
            EditorView.lineWrapping,
            EditorState.tabSize.of(4),
            autocompletion({ override: [starlarkCompletions] }),
            inlineErrorField,
            inlineErrorTheme,
          ]}
          minHeight="320px"
          maxHeight="65vh"
          value={resource.value}
          placeholder={placeholder}
          onChange={(value) => {
            resource.setValue(value);
            // Clear inline error immediately upon editing the code to unblock the view.
            if (editorRef.current?.view) {
              editorRef.current.view.dispatch({
                effects: setInlineError.of(null),
              });
            }
          }}
        />
        {resource.error && (
          <p className="message message-error">{resource.error}</p>
        )}
      </section>
    );
  },
);
