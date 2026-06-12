"use client";

// Issue-scoped document library. Fetches via api.getIssueLibrary and lets
// the shared shell handle layout, navigation and rendering.

import * as React from "react";
import { useEffect, useState } from "react";
import { api } from "@multica/core/api";
import type { IssueLibrary as IssueLibraryData } from "@multica/core/types";
import { useT } from "../i18n";
import { LibraryShell } from "./library-shell";

interface IssueLibraryProps {
  issueId: string;
  /** Title override; defaults to "Documents". */
  title?: string;
  subtitle?: string;
}

export function IssueLibrary({ issueId, title, subtitle }: IssueLibraryProps): React.JSX.Element {
  const { t } = useT("documents");
  const [data, setData] = useState<IssueLibraryData | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    let cancelled = false;
    setLoading(true);
    api
      .getIssueLibrary(issueId)
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
  }, [issueId]);

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
    <LibraryShell
      title={title ?? t(($) => $.library.issue_title)}
      subtitle={subtitle}
      documents={data.documents}
      sections={data.sections}
      exportUrl={api.exportIssueLibraryUrl(issueId)}
      exportFilename={`issue-${issueId}-library.zip`}
    />
  );
}
