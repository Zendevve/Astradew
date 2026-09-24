import type { AnchorHTMLAttributes } from "react";

interface NavLinkProps extends AnchorHTMLAttributes<HTMLAnchorElement> {
  /** Route path (e.g. "/library"); rendered as a hash href, no router dep. */
  to: string;
  /** Marks the link for the current route via aria-current. */
  current?: boolean;
}

/**
 * NavLink is a real anchor, so it is keyboard-focusable and -activatable by
 * platform behaviour with the global :focus-visible ring. No click handlers.
 */
export default function NavLink({ to, current = false, ...rest }: NavLinkProps) {
  return (
    <a {...rest} href={`#${to}`} aria-current={current ? "page" : undefined} />
  );
}
