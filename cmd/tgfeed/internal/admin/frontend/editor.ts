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
import { EditorState } from "@codemirror/state";
import {
  drawSelection,
  dropCursor,
  EditorView,
  highlightActiveLine,
  highlightActiveLineGutter,
  highlightSpecialChars,
  keymap,
  lineNumbers,
} from "@codemirror/view";
import { oneDark } from "@codemirror/theme-one-dark";
import htmx, { HtmxExtension } from "htmx.org";

const hx = htmx as unknown as {
  defineExtension: (name: string, extension: Partial<HtmxExtension>) => void;
};

const editors = new WeakMap<HTMLTextAreaElement, EditorView>();
let registered = false;

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

function initialize(textarea: HTMLTextAreaElement): void {
  if (editors.has(textarea)) return;
  const panel = textarea.closest<HTMLElement>("[data-editor-panel]");
  const status = panel?.querySelector<HTMLElement>("[data-editor-status]");
  const save = panel?.querySelector<HTMLButtonElement>("[data-editor-save]");
  // The textarea remains the form control and source of truth. CodeMirror is
  // only a visual enhancement, so native submission still works without it.
  const baseline = textarea.dataset.baseline ?? textarea.value;
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
          const dirty = textarea.value !== baseline;
          if (status) {
            status.textContent = dirty ? "Unsaved" : "Synced";
            status.classList.toggle("pill-warning", dirty);
          }
          if (save) save.disabled = !dirty;
        }),
      ],
    }),
  });
  editors.set(textarea, view);
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
