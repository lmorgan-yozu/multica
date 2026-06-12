"use client";

// LibraryTabLink — a visible, labelled link to the document library for an
// issue or project, with a count badge. Deliberately bigger than the
// original icon-only button so the library is discoverable from every
// issue and project detail page.

import * as React from "react";
import { useEffect, useState } from "react";
import { Library } from "lucide-react";
import { api } from "@multica/core/api";
import { AppLink } from "../navigation";
import { useT } from "../i18n";

interface LibraryTabLinkProps {
  href: string;
  scope: "issue" | "project";
  scopeId: string;
  className?: string;
}

export function LibraryTabLink({
  href,
  scope,
  scopeId,
  className,
}: LibraryTabLinkProps): React.JSX.Element {
  const { t } = useT("documents");
  const [count, setCount] = useState<number | null>(null);

  useEffect(() => {
    let cancelled = false;
    const loader =
      scope === "issue" ? api.getIssueLibrary(scopeId) : api.getProjectLibrary(scopeId);
    loader
      .then((res) => {
        if (!cancelled) setCount(res.documents.length);
      })
      .catch(() => {
        if (!cancelled) setCount(null);
      });
    return () => {
      cancelled = true;
    };
  }, [scope, scopeId]);

  return (
    <AppLink
      href={href}
      className={
        "inline-flex items-center gap-1.5 rounded-md border border-border px-2 py-1 text-xs font-medium text-muted-foreground transition-colors hover:bg-secondary hover:text-foreground " +
        (className ?? "")
      }
      aria-label={t(($) => $.tab.open_aria, { count: count ?? 0 })}
    >
      <Library className="h-3.5 w-3.5" />
      <span>{t(($) => $.tab.label)}</span>
      {count !== null && (
        <span className="rounded-full bg-muted px-1.5 py-0.5 text-[10px] text-muted-foreground">
          {count}
        </span>
      )}
    </AppLink>
  );
}
