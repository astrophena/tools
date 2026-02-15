import React from "npm:react";

import { EditableResource } from "../types.ts";

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
export function EditorPanel(props: EditorPanelProps) {
  const { title, description, placeholder, languageHint, resource } = props;

  /**
   * Inserts spaces instead of moving focus when Tab is pressed inside the editor.
   */
  function onEditorKeyDown(event: React.KeyboardEvent<HTMLTextAreaElement>): void {
    if (event.key !== "Tab") {
      return;
    }
    event.preventDefault();

    const target = event.currentTarget;
    const indent = "  ";
    const start = target.selectionStart;
    const end = target.selectionEnd;

    const nextValue = `${resource.value.slice(0, start)}${indent}${resource.value.slice(end)}`;
    resource.setValue(nextValue);

    requestAnimationFrame(() => {
      target.selectionStart = start + indent.length;
      target.selectionEnd = start + indent.length;
    });
  }

  return (
    <section className="panel editor-panel">
      <header className="panel-header">
        <div>
          <h2>{title}</h2>
          <p>{description}</p>
        </div>
        <div className="status-cluster">
          {resource.dirty ? <span className="pill pill-warning">Unsaved</span> : <span className="pill">Synced</span>}
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
      <textarea
        className="editor"
        spellCheck={false}
        placeholder={placeholder}
        value={resource.value}
        onKeyDown={onEditorKeyDown}
        onChange={(event) => {
          resource.setValue(event.target.value);
        }}
      />
      {resource.error && <p className="message message-error">{resource.error}</p>}
    </section>
  );
}
