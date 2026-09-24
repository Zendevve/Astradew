import { useId } from "react";

interface EmptyStateProps {
  title: string;
  /** Honest copy: what is missing and what will fill it. Never sample data. */
  description: string;
}

/**
 * EmptyState renders a section with a labelled heading and plain-words
 * description. Status is words, never colour-alone.
 */
export default function EmptyState({ title, description }: EmptyStateProps) {
  const titleId = useId();
  return (
    <section className="empty-state" aria-labelledby={titleId}>
      <h1 id={titleId}>{title}</h1>
      <p>{description}</p>
    </section>
  );
}
