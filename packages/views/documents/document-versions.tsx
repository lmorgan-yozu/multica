"use client";

// DocumentVersions — inline version history shown below the viewer header.
// Loads lazily when the caller renders this component; does not block the
// viewer itself.

import * as React from "react";
import { useEffect, useState } from "react";
import { api } from "@multica/core/api";
import type { DocumentVersion } from "@multica/core/types";

interface DocumentVersionsProps {
  attachmentId: string;
  onSelectVersion?: (version: DocumentVersion) => void;
  className?: string;
}

export function DocumentVersions({
  attachmentId,
  onSelectVersion,
  className,
}: DocumentVersionsProps): React.JSX.Element {
  const [versions, setVersions] = useState<DocumentVersion[] | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    let cancelled = false;
    api
      .listDocumentVersions(attachmentId)
      .then((res) => {
        if (!cancelled) setVersions(res.versions);
      })
      .catch((e: unknown) => {
        if (!cancelled) setError(e instanceof Error ? e.message : String(e));
      });
    return () => {
      cancelled = true;
    };
  }, [attachmentId]);

  if (error) {
    return (
      <div className={className}>
        <span className="text-xs text-destructive">Versions unavailable: {error}</span>
      </div>
    );
  }
  if (versions === null) {
    return <div className={className}><span className="text-xs text-muted-foreground">Loading versions…</span></div>;
  }
  if (versions.length === 0) return <></>;

  return (
    <div className={className}>
      <h3 className="mb-1 text-xs font-semibold uppercase tracking-wide text-muted-foreground">
        Versions
      </h3>
      <ul className="flex flex-col gap-1">
        {versions.map((v) => (
          <li key={v.id} className="flex items-center justify-between rounded-md border border-border px-2 py-1 text-xs">
            <div className="flex flex-col min-w-0">
              <span className="font-medium">
                v{v.version_number}
                {v.notes ? `: ${v.notes}` : ""}
              </span>
              <span className="truncate text-muted-foreground">
                {v.filename} · {new Date(v.created_at).toLocaleString()}
                {v.author_type === "agent" ? " · agent" : ""}
              </span>
            </div>
            {onSelectVersion && (
              <button
                type="button"
                className="shrink-0 rounded-md px-2 py-0.5 text-[11px] hover:bg-muted"
                onClick={() => onSelectVersion(v)}
              >
                View
              </button>
            )}
          </li>
        ))}
      </ul>
    </div>
  );
}
