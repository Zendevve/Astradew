import { useEffect, useId, useRef, type ReactNode } from "react";

import Button from "./Button";

/**
 * Everything the focus trap cycles through. Disabled controls are excluded,
 * which is also what keeps a disabled Cancel out of the tab order while a run
 * is being cancelled.
 */
const focusableSelector = [
  "a[href]",
  "button:not([disabled])",
  "input:not([disabled])",
  "select:not([disabled])",
  "textarea:not([disabled])",
  "[tabindex]:not([tabindex='-1'])",
].join(", ");

interface SheetProps {
  /** Accessible dialog name; rendered as the sheet's heading. */
  title: string;
  /** Called for Esc and the Close button. The caller decides what closing means. */
  onClose: () => void;
  /** True while closing is already under way: both close routes go quiet. */
  closeDisabled?: boolean;
  children: ReactNode;
}

/**
 * Sheet is the modal dialog primitive: a labelled role="dialog" with
 * aria-modal, a visible heading, focus moved into it on open and returned to
 * the opener on close, Esc to close, and a Tab trap so keyboard focus cannot
 * wander behind the overlay. Backdrop clicks deliberately do not close: in
 * this product closing a sheet can have an effect (cancelling a running
 * inspection), so only deliberate keys and buttons close it.
 *
 * Esc and the trap are bound to the document while the sheet is open, not to
 * the panel: a backdrop click moves focus to the body in a real browser, and
 * both keys must keep working from wherever focus went. The listener goes
 * away with the sheet, so a later sheet is never handled twice.
 */
export default function Sheet({ title, onClose, closeDisabled = false, children }: SheetProps) {
  const titleId = useId();
  const dialogRef = useRef<HTMLDivElement>(null);
  const openerRef = useRef<Element | null>(null);

  useEffect(() => {
    openerRef.current = document.activeElement;
    dialogRef.current?.focus();
    return () => {
      if (openerRef.current instanceof HTMLElement) {
        openerRef.current.focus();
      }
    };
  }, []);

  /**
   * Esc and the Tab trap, bound to the document for as long as the sheet is
   * open. Binding them to the panel would lose both the moment focus left it
   * — the state a backdrop click leaves behind.
   */
  useEffect(() => {
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === "Escape") {
        event.stopPropagation();
        if (!closeDisabled) {
          onClose();
        }
        return;
      }
      if (event.key !== "Tab") {
        return;
      }
      const dialog = dialogRef.current;
      if (dialog === null) {
        return;
      }
      const items = Array.from(dialog.querySelectorAll<HTMLElement>(focusableSelector));
      if (items.length === 0) {
        event.preventDefault();
        dialog.focus();
        return;
      }
      const first = items[0];
      const last = items[items.length - 1];
      const active = document.activeElement;
      const inside = active instanceof HTMLElement && dialog.contains(active);
      if (!inside) {
        // Focus sits on the body: send the next Tab to the sheet's own
        // controls rather than to whatever is behind the overlay.
        event.preventDefault();
        (event.shiftKey ? last : first).focus();
        return;
      }
      if (event.shiftKey) {
        if (active === first || active === dialog) {
          event.preventDefault();
          last.focus();
        }
        return;
      }
      if (active === last) {
        event.preventDefault();
        first.focus();
      }
    };
    document.addEventListener("keydown", onKeyDown);
    return () => {
      document.removeEventListener("keydown", onKeyDown);
    };
  }, [closeDisabled, onClose]);

  return (
    <div className="sheet-backdrop">
      <div
        className="sheet"
        role="dialog"
        aria-modal="true"
        aria-labelledby={titleId}
        tabIndex={-1}
        ref={dialogRef}
      >
        <div className="sheet-head">
          <h2 id={titleId}>{title}</h2>
          <Button className="sheet-close" onClick={onClose} disabled={closeDisabled}>
            Close
          </Button>
        </div>
        <div className="sheet-body">{children}</div>
      </div>
    </div>
  );
}
