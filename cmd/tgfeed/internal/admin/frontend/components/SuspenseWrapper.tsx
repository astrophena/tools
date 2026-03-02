import React, { ReactNode } from "react";

/** Throws a Promise if loading is true, to trigger Suspense boundaries higher up. */
export function Suspender(
  { loading, promise, children }: {
    loading: boolean;
    promise: Promise<any> | null;
    children: ReactNode;
  },
) {
  if (loading && promise) {
    throw promise;
  }
  return <>{children}</>;
}
