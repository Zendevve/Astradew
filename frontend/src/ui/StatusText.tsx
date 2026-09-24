import type { ReactNode } from "react";

interface StatusTextProps {
  /** Decorative glyph; hidden from assistive tech, never the only signal. */
  icon?: string;
  children: ReactNode;
}

/**
 * StatusText is a polite live region: an icon plus words, so state is never
 * colour-alone and screen readers announce changes.
 */
export default function StatusText({ icon = "●", children }: StatusTextProps) {
  return (
    <p className="status-text" role="status">
      <span className="status-icon" aria-hidden="true">
        {icon}
      </span>
      <span>{children}</span>
    </p>
  );
}
