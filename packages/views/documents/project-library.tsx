"use client";

// Project-scoped document library. Aggregates every attachment across every
// issue in the project and renders them through the same shell the
// issue-scoped library uses, with the source issue surfaced as doc meta.

import * as React from "react";
import { useEffect, useState } from "react";
import { api } from "@multica/core/api";
import type { ProjectLibrary as ProjectLibraryData, ProjectLibraryDocument } from "@multica/core/types";
import { LibraryShell } from "./library-shell";

interface ProjectLibraryProps {
  projectId: string;
  title?: string;
  subtitle?: string;
}

export function ProjectLibrary({ projectId, title, subtitle }: ProjectLibraryProps): React.JSX.Element {
  const [data, setData] = useState<ProjectLibraryData | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    let cancelled = false;
    setLoading(true);
    api
      .getProjectLibrary(projectId)
      .then((res) => {
        if (!cancelled) setData(res);
      })
      .catch((e: unknown) => {
        if (!cancelled) setError(e instanceof Error ? e.message : String(e));
      })
      .finally(() => {
        if (!cancelled) setLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [projectId]);

  if (loading) {
    return <div className="p-6 text-sm text-muted-foreground">Loading library…</div>;
  }
  if (error) {
    return (
      <div className="p-6 text-sm text-destructive">
        Failed to load library: {error}
      </div>
    );
  }
  if (!data) return <></>;

  return (
    <LibraryShell<ProjectLibraryDocument>
      title={title ?? "Project library"}
      subtitle={subtitle}
      documents={data.documents}
      sections={data.sections}
      renderDocMeta={(doc) => (
        <span>
          {doc.source_issue_identifier} · {doc.source_issue_title}
        </span>
      )}
    />
  );
}
