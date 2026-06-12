"use client";

// DocumentComments — thread of comments attached to a logical document.
// Comments persist across version replacement. Intentionally minimal for
// Phase 1b: plain text, flat rendering (parent_id is stored but not yet
// nested in the UI), no reactions.

import * as React from "react";
import { useCallback, useEffect, useState } from "react";
import { api } from "@multica/core/api";
import type { DocumentComment } from "@multica/core/types";
import { useT } from "../i18n";

interface DocumentCommentsProps {
  attachmentId: string;
  currentUserId?: string;
  className?: string;
}

export function DocumentComments({
  attachmentId,
  currentUserId,
  className,
}: DocumentCommentsProps): React.JSX.Element {
  const { t } = useT("documents");
  const [comments, setComments] = useState<DocumentComment[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [draft, setDraft] = useState("");
  const [submitting, setSubmitting] = useState(false);

  const load = useCallback(async () => {
    try {
      const res = await api.listDocumentComments(attachmentId);
      setComments(res.comments);
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    }
  }, [attachmentId]);

  useEffect(() => {
    let cancelled = false;
    setComments(null);
    load().finally(() => {
      if (cancelled) return;
    });
    return () => {
      cancelled = true;
    };
  }, [load]);

  const onSubmit = async () => {
    const content = draft.trim();
    if (!content) return;
    setSubmitting(true);
    try {
      await api.createDocumentComment(attachmentId, content);
      setDraft("");
      await load();
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    } finally {
      setSubmitting(false);
    }
  };

  const onDelete = async (id: string) => {
    try {
      await api.deleteDocumentComment(id);
      await load();
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    }
  };

  return (
    <section className={className}>
      <h3 className="mb-2 text-sm font-semibold">{t(($) => $.comments.title)}</h3>
      {error && (
        <div className="mb-2 rounded-md border border-destructive/30 bg-destructive/5 p-2 text-xs text-destructive">
          {error}
        </div>
      )}

      {comments === null ? (
        <div className="text-xs text-muted-foreground">{t(($) => $.comments.loading)}</div>
      ) : comments.length === 0 ? (
        <div className="text-xs text-muted-foreground">{t(($) => $.comments.empty)}</div>
      ) : (
        <ul className="flex flex-col gap-2">
          {comments.map((c) => (
            <li key={c.id} className="rounded-md border border-border bg-muted/30 p-2">
              <div className="mb-1 flex items-center justify-between gap-2 text-[11px] text-muted-foreground">
                <span>
                  {c.author_type === "agent"
                    ? `🤖 ${t(($) => $.comments.author_agent)}`
                    : `👤 ${t(($) => $.comments.author_member)}`}
                  {" · "}
                  {new Date(c.created_at).toLocaleString()}
                </span>
                {currentUserId === c.author_id && (
                  <button
                    type="button"
                    className="rounded-md px-1.5 py-0.5 text-[10px] text-destructive hover:bg-destructive/10"
                    onClick={() => onDelete(c.id)}
                    aria-label={t(($) => $.comments.delete_aria)}
                  >
                    {t(($) => $.comments.delete)}
                  </button>
                )}
              </div>
              <p className="whitespace-pre-wrap text-sm">{c.content}</p>
            </li>
          ))}
        </ul>
      )}

      <div className="mt-3 flex flex-col gap-2">
        <textarea
          className="min-h-[60px] w-full rounded-md border border-border bg-background p-2 text-sm outline-none focus:border-brand"
          placeholder={t(($) => $.comments.placeholder)}
          value={draft}
          onChange={(e) => setDraft(e.target.value)}
          disabled={submitting}
        />
        <div className="flex justify-end">
          <button
            type="button"
            className="rounded-md bg-brand px-3 py-1 text-sm text-white disabled:opacity-50"
            onClick={onSubmit}
            disabled={submitting || !draft.trim()}
          >
            {submitting ? t(($) => $.comments.posting) : t(($) => $.comments.post)}
          </button>
        </div>
      </div>
    </section>
  );
}
