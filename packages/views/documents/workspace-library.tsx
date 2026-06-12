"use client";

// Workspace-scoped document library. Aggregates every non-archived attachment
// across every issue in the workspace. Shares the LibraryShell with the
// issue- and project-scoped views so the grouping, search, viewer, and
// export behaviour stay consistent across scopes.

import * as React from "react";
import { useEffect, useState } from "react";
import { api } from "@multica/core/api";
import type { ProjectLibrary as ProjectLibraryData, ProjectLibraryDocument } from "@multica/core/types";
import { useT } from "../i18n";
import { LibraryShell } from "./library-shell";

interface WorkspaceLibraryProps {
  title?: string;
  subtitle?: string;
}

export function WorkspaceLibrary({
  title,
  subtitle,
}: WorkspaceLibraryProps): React.JSX.Element {
  const { t } = useT("documents");
  const [data, setData] = useState<ProjectLibraryData | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    let cancelled = false;
    setLoading(true);
    api
      .getWorkspaceLibrary()
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
  }, []);

  if (loading) {
    return <div className="p-6 text-sm text-muted-foreground">{t(($) => $.library.loading)}</div>;
  }
  if (error) {
    return (
      <div className="p-6 text-sm text-destructive">
        {t(($) => $.library.load_failed, { error })}
      </div>
    );
  }
  if (!data) return <></>;

  return (
    <LibraryShell<ProjectLibraryDocument>
      title={title ?? t(($) => $.library.workspace_title)}
      subtitle={subtitle ?? t(($) => $.library.workspace_subtitle)}
      documents={data.documents}
      sections={data.sections}
      exportUrl={api.exportWorkspaceLibraryUrl()}
      exportFilename="workspace-library.zip"
      renderDocMeta={(doc) =>
        doc.source_issue_id ? (
          <span>
            {doc.source_issue_identifier} · {doc.source_issue_title}
          </span>
        ) : null
      }
    />
  );
}
