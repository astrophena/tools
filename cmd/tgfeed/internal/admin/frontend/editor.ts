import {
  autocompletion,
  closeBrackets,
  closeBracketsKeymap,
  CompletionContext,
  completionKeymap,
} from "@codemirror/autocomplete";
import { defaultKeymap, history, historyKeymap } from "@codemirror/commands";
import { python } from "@codemirror/lang-python";
import {
  bracketMatching,
  defaultHighlightStyle,
  indentOnInput,
  syntaxHighlighting,
} from "@codemirror/language";
import {
  EditorState,
  RangeSetBuilder,
  StateEffect,
  StateField,
} from "@codemirror/state";
import {
  Decoration,
  DecorationSet,
  drawSelection,
  dropCursor,
  EditorView,
  highlightActiveLine,
  highlightActiveLineGutter,
  highlightSpecialChars,
  keymap,
  lineNumbers,
  WidgetType,
} from "@codemirror/view";
import { oneDark } from "@codemirror/theme-one-dark";
import htmx, { HtmxExtension } from "htmx.org";

const hx = htmx as unknown as {
  defineExtension: (name: string, extension: Partial<HtmxExtension>) => void;
};

const editors = new WeakMap<HTMLTextAreaElement, EditorView>();
let registered = false;

class ErrorWidget extends WidgetType {
  constructor(readonly message: string) {
    super();
  }

  override eq(other: ErrorWidget): boolean {
    return other.message === this.message;
  }

  override toDOM(): HTMLElement {
    const element = document.createElement("div");
    element.className = "cm-error-widget";
    element.textContent = this.message;
    return element;
  }
}

type InlineError = { line: number; message: string } | null;

const setInlineError = StateEffect.define<InlineError>();

const inlineErrorField = StateField.define<DecorationSet>({
  create: () => Decoration.none,
  update(decorations, transaction) {
    decorations = transaction.docChanged
      ? Decoration.none
      : decorations.map(transaction.changes);
    for (const effect of transaction.effects) {
      if (!effect.is(setInlineError)) continue;
      const error = effect.value;
      if (
        !error || error.line <= 0 || error.line > transaction.state.doc.lines
      ) {
        decorations = Decoration.none;
        continue;
      }
      const line = transaction.state.doc.line(error.line);
      const builder = new RangeSetBuilder<Decoration>();
      builder.add(
        line.from,
        line.from,
        Decoration.widget({
          widget: new ErrorWidget(error.message),
          side: -1,
          block: true,
        }),
      );
      if (line.to > line.from) {
        builder.add(
          line.from,
          line.to,
          Decoration.mark({
            class: "cm-error-line",
          }),
        );
      }
      decorations = builder.finish();
    }
    return decorations;
  },
  provide: (field) => EditorView.decorations.from(field),
});

function completions(context: CompletionContext) {
  const word = context.matchBefore(/\w*/);
  if (!word || (word.from === word.to && !context.explicit)) return null;
  return {
    from: word.from,
    options: [
      "feed",
      "url=",
      "title=",
      "message_thread_id=",
      "block_rule=",
      "keep_rule=",
      "digest=",
      "format=",
      "always_send_new_items=",
      "True",
      "False",
    ].map((label) => ({
      label,
      type: label === "feed" ? "function" : "keyword",
    })),
  };
}

function updateDirtyState(
  textarea: HTMLTextAreaElement,
  panel: HTMLElement | null,
  status: HTMLElement | null,
  save: HTMLButtonElement | null,
  baseline: string,
): void {
  const dirty = textarea.value !== baseline;
  textarea.dataset.dirty = String(dirty);
  if (panel) panel.dataset.dirty = String(dirty);
  if (status) {
    status.textContent = dirty ? "Unsaved" : "Synced";
    status.classList.toggle("pill-warning", dirty);
  }
  if (save) save.disabled = !dirty;
}

function showInlineError(view: EditorView, value: string | undefined): void {
  const match = value?.match(/:(\d+):(\d+): (.*)/);
  if (!match) return;
  const lineNumber = Number.parseInt(match[1], 10);
  const column = Number.parseInt(match[2], 10);
  if (lineNumber <= 0 || lineNumber > view.state.doc.lines) return;
  const line = view.state.doc.line(lineNumber);
  const position = column > 0 && column <= line.length + 1
    ? line.from + column - 1
    : line.from;
  view.dispatch({
    selection: { anchor: position },
    effects: [
      setInlineError.of({ line: lineNumber, message: match[3] }),
      EditorView.scrollIntoView(position, { y: "center" }),
    ],
  });
  view.focus();
}

function initialize(textarea: HTMLTextAreaElement): void {
  if (editors.has(textarea)) return;
  const panel = textarea.closest<HTMLElement>("[data-editor-panel]");
  const status = panel?.querySelector<HTMLElement>("[data-editor-status]") ??
    null;
  const save = panel?.querySelector<HTMLButtonElement>("[data-editor-save]") ??
    null;
  // The textarea remains the form control and source of truth. CodeMirror is
  // only a visual enhancement, so native submission still works without it.
  const baseline = textarea.dataset.baseline ?? textarea.value;
  updateDirtyState(textarea, panel, status, save, baseline);
  const parent = document.createElement("div");
  textarea.insertAdjacentElement("afterend", parent);
  textarea.hidden = true;
  const view = new EditorView({
    parent,
    state: EditorState.create({
      doc: textarea.value,
      extensions: [
        lineNumbers(),
        highlightActiveLineGutter(),
        highlightSpecialChars(),
        history(),
        drawSelection(),
        dropCursor(),
        indentOnInput(),
        syntaxHighlighting(defaultHighlightStyle, { fallback: true }),
        bracketMatching(),
        closeBrackets(),
        python(),
        oneDark,
        EditorView.lineWrapping,
        EditorState.tabSize.of(4),
        autocompletion({ override: [completions] }),
        highlightActiveLine(),
        inlineErrorField,
        keymap.of([
          ...closeBracketsKeymap,
          ...defaultKeymap,
          ...historyKeymap,
          ...completionKeymap,
        ]),
        EditorView.editorAttributes.of({
          class: "editor",
          style: "min-height:320px;max-height:65vh",
        }),
        EditorView.updateListener.of((update) => {
          if (!update.docChanged) return;
          textarea.value = update.state.doc.toString();
          updateDirtyState(textarea, panel, status, save, baseline);
        }),
      ],
    }),
  });
  editors.set(textarea, view);
  showInlineError(view, textarea.dataset.error);
}

function process(root: HTMLElement): void {
  if (
    root instanceof HTMLTextAreaElement && root.matches("[data-code-editor]")
  ) initialize(root);
  root.querySelectorAll<HTMLTextAreaElement>("textarea[data-code-editor]")
    .forEach(initialize);
}

function cleanup(root: HTMLElement): void {
  const textareas = root instanceof HTMLTextAreaElement &&
      root.matches("[data-code-editor]")
    ? [root]
    : Array.from(
      root.querySelectorAll<HTMLTextAreaElement>("textarea[data-code-editor]"),
    );
  for (const textarea of textareas) {
    editors.get(textarea)?.destroy();
    editors.delete(textarea);
  }
}

export function registerEditors(root: ParentNode = document): void {
  if (!registered) {
    // Register once for cleanup and future swaps, then process the subtree that
    // caused this lazily loaded module to be imported.
    registered = true;
    hx.defineExtension("code-editor", {
      onEvent(name, event) {
        const element = (event as CustomEvent).detail?.elt as
          | HTMLElement
          | undefined;
        if (name === "htmx:afterProcessNode" && element) process(element);
        if (name === "htmx:beforeCleanupElement" && element) cleanup(element);
        return true;
      },
    });
  }
  if (root instanceof HTMLElement) process(root);
  else {root.querySelectorAll<HTMLTextAreaElement>("textarea[data-code-editor]")
      .forEach(initialize);}
}
