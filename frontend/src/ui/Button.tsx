import type { ButtonHTMLAttributes } from "react";

/**
 * Button is the single button primitive. It renders a native <button> so it
 * stays keyboard-operable with a visible focus ring; the default type is
 * "button" so it never submits a form by accident.
 */
export default function Button({
  type = "button",
  className,
  ...rest
}: ButtonHTMLAttributes<HTMLButtonElement>) {
  const classes =
    className === undefined || className === "" ? "btn" : `btn ${className}`;
  return <button type={type} className={classes} {...rest} />;
}
