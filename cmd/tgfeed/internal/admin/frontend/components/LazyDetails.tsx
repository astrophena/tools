import React, { useState } from "react";

/**
 * LazyDetails renders a details/summary block but only mounts its children when expanded.
 */
export function LazyDetails(props: {
  summary: React.ReactNode;
  children: React.ReactNode;
}) {
  const [isOpen, setIsOpen] = useState(false);

  return (
    <details
      className="detail-toggle"
      onToggle={(event) => {
        setIsOpen((event.target as HTMLDetailsElement).open);
      }}
    >
      <summary>{props.summary}</summary>
      {isOpen && props.children}
    </details>
  );
}
