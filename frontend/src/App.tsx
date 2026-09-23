import { useEffect, useState } from "react";

import {
  ApplicationService,
  type Info,
} from "../bindings/github.com/Zendevve/astradew/internal/app";

/**
 * App renders the application's identity as the backend reports it. It invents
 * nothing: while the backend has not answered it says so, and if the backend
 * refuses it reports the refusal instead of a plausible-looking substitute.
 */
export default function App() {
  const [identity, setIdentity] = useState<Info | null>(null);
  const [failure, setFailure] = useState<string | null>(null);

  useEffect(() => {
    let cancelled = false;

    ApplicationService.Info().then(
      (info) => {
        if (!cancelled) {
          setIdentity(info);
        }
      },
      (error: unknown) => {
        if (!cancelled) {
          setFailure(error instanceof Error ? error.message : String(error));
        }
      },
    );

    return () => {
      cancelled = true;
    };
  }, []);

  if (failure !== null) {
    return (
      <main>
        <p role="alert">The backend did not answer: {failure}</p>
      </main>
    );
  }

  if (identity === null) {
    return (
      <main>
        <p>Reading application identity…</p>
      </main>
    );
  }

  return (
    <main>
      <h1>{identity.name}</h1>
      <p>Version {identity.version}</p>
    </main>
  );
}
