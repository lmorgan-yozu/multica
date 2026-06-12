"use client";

// DocumentViewer — renders a single library Document inline in the UI.
// Markdown and text go through the Markdown component for syntax highlighting
// and mention rendering; PDFs get an iframe; images get an <img>; anything
// else falls back to a download link.

import * as React from "react";
import { useEffect, useState } from "react";
import { api } from "@multica/core/api";
import type { Document } from "@multica/core/types";
import { Markdown } from "../common/markdown";
import { useT } from "../i18n";
import { DocumentComments } from "./document-comments";
import { DocumentVersions } from "./document-versions";

interface DocumentViewerProps {
  document: Document;
  /** Optional class applied to the outer wrapper. */
  className?: string;
  /** Current user id, for own-comment affordances. */
  currentUserId?: string;
  /** Show the version history block. Defaults to true. */
  showVersions?: boolean;
  /** Show the comments block. Defaults to true. */
  showComments?: boolean;
  /** Show a Summarise action when summary is empty. Defaults to true. */
  showSummariseAction?: boolean;
}

const TEXT_TYPES = [
  "text/markdown",
  "text/plain",
  "text/x-markdown",
  "text/csv",
  "application/json",
  "application/x-yaml",
  "text/yaml",
  "text/x-yaml",
];

function isTextish(contentType: string, filename: string): boolean {
  const ct = contentType.toLowerCase();
  if (ct.startsWith("text/")) return true;
  const base = (ct.split(";")[0] ?? "").trim();
  if (TEXT_TYPES.includes(base)) return true;
  if (/\.(md|markdown|txt|csv|json|ya?ml|log)$/i.test(filename)) return true;
  return false;
}

function isMarkdown(contentType: string, filename: string): boolean {
  const ct = contentType.toLowerCase();
  if (ct.includes("markdown")) return true;
  if (/\.(md|markdown)$/i.test(filename)) return true;
  return false;
}

export function DocumentViewer({
  document,
  className,
  currentUserId,
  showVersions = true,
  showComments = true,
  showSummariseAction = true,
}: DocumentViewerProps): React.JSX.Element {
  const { t } = useT("documents");
  const [textContent, setTextContent] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(false);
  const [summarising, setSummarising] = useState(false);
  const [summariseError, setSummariseError] = useState<string | null>(null);
  const [localSummary, setLocalSummary] = useState<string | null>(null);

  const isText = isTextish(document.content_type, document.filename);
  const displaySummary = localSummary ?? document.summary ?? null;

  const onGenerateSummary = async () => {
    if (!textContent) return;
    setSummarising(true);
    setSummariseError(null);
    try {
      // Heuristic summary: first non-empty heading or the first 280 chars of
      // the first paragraph. Deliberately mechanical — real LLM-backed
      // summaries come from the auto-summarise autopilot, see docs_v2.go
      // `ListUnsummarisedDocuments`. This button exists so humans can
      // seed a summary on the spot without waiting for the autopilot.
      const lines = textContent.split(/\r?\n/).map((l) => l.trim()).filter(Boolean);
      const firstHeading = lines.find((l) => /^#{1,3}\s+/.test(l));
      const firstPara = lines.find((l) => !/^#{1,3}\s+/.test(l)) ?? "";
      const heuristic = firstHeading
        ? firstHeading.replace(/^#{1,3}\s+/, "")
        : firstPara.slice(0, 280);
      if (!heuristic) throw new Error(t(($) => $.viewer.summarise_empty_error));
      await api.updateDocumentCuration(document.attachment_id, { summary: heuristic });
      setLocalSummary(heuristic);
    } catch (e) {
      setSummariseError(e instanceof Error ? e.message : String(e));
    } finally {
      setSummarising(false);
    }
  };

  useEffect(() => {
    if (!isText) {
      setTextContent(null);
      return;
    }
    let cancelled = false;
    setLoading(true);
    setError(null);

    fetch(document.inline_url, { credentials: "include" })
      .then(async (res) => {
        if (!res.ok) throw new Error(`${res.status} ${res.statusText}`);
        return res.text();
      })
      .then((text) => {
        if (!cancelled) setTextContent(text);
      })
      .catch((e) => {
        if (!cancelled) setError(e instanceof Error ? e.message : String(e));
      })
      .finally(() => {
        if (!cancelled) setLoading(false);
      });

    return () => {
      cancelled = true;
    };
  }, [document.inline_url, isText]);

  const title = document.title ?? document.filename;

  return (
    <article className={className}>
      <header className="mb-4 border-b border-border pb-3">
        <h1 className="text-xl font-semibold">
          {title}
          {document.version_number && document.version_number > 1 && (
            <span className="ml-2 text-xs font-normal text-muted-foreground">
              {t(($) => $.versions.version_label, {
                version: document.version_number,
              })}
            </span>
          )}
        </h1>
        {displaySummary ? (
          <p className="mt-1 text-sm text-muted-foreground">{displaySummary}</p>
        ) : showSummariseAction && isText ? (
          <div className="mt-1 flex items-center gap-2 text-xs">
            <span className="text-muted-foreground">{t(($) => $.viewer.no_summary)}</span>
            <button
              type="button"
              className="rounded-md border border-border px-2 py-0.5 text-xs hover:bg-muted disabled:opacity-50"
              onClick={onGenerateSummary}
              disabled={summarising || textContent === null}
            >
              {summarising
                ? t(($) => $.viewer.summarising)
                : t(($) => $.viewer.generate_summary)}
            </button>
            {summariseError && <span className="text-destructive">{summariseError}</span>}
          </div>
        ) : null}
        <DocumentMetaRow document={document} curatedByAgentLabel={t(($) => $.viewer.curated_by_agent)} />
      </header>

      <div className="prose prose-sm max-w-none dark:prose-invert">
        {renderContent({
          document,
          textContent,
          loading,
          error,
          isText,
          labels: {
            loadFailed: (message) => t(($) => $.viewer.load_failed, { error: message }),
            downloadInstead: t(($) => $.viewer.download_instead),
            loading: t(($) => $.viewer.loading),
            unsupportedType: (type) => t(($) => $.viewer.unsupported_type, { type }),
            unknownType: t(($) => $.viewer.unknown_type),
            downloadFile: (filename) => t(($) => $.viewer.download_file, { filename }),
          },
        })}
      </div>

      {showVersions && (
        <DocumentVersions attachmentId={document.attachment_id} className="mt-6" />
      )}

      {showComments && (
        <DocumentComments
          attachmentId={document.attachment_id}
          currentUserId={currentUserId}
          className="mt-6 border-t border-border pt-4"
        />
      )}
    </article>
  );
}

function renderContent(args: {
  document: Document;
  textContent: string | null;
  loading: boolean;
  error: string | null;
  isText: boolean;
  labels: {
    loadFailed: (message: string) => string;
    downloadInstead: string;
    loading: string;
    unsupportedType: (type: string) => string;
    unknownType: string;
    downloadFile: (filename: string) => string;
  };
}): React.ReactNode {
  const { document, textContent, loading, error, isText, labels } = args;
  const ct = document.content_type.toLowerCase();

  if (error) {
    return (
      <div className="rounded-md border border-destructive/30 bg-destructive/5 p-3 text-sm text-destructive">
        {labels.loadFailed(error)}
        <div className="mt-2">
          <a className="underline" href={document.download_url} target="_blank" rel="noreferrer">
            {labels.downloadInstead}
          </a>
        </div>
      </div>
    );
  }

  if (ct.startsWith("image/")) {
    return (
      <img
        src={document.inline_url}
        alt={document.filename}
        className="max-h-[80vh] max-w-full rounded-md"
      />
    );
  }

  if (ct === "application/pdf" || /\.pdf$/i.test(document.filename)) {
    return (
      <iframe
        src={document.inline_url}
        title={document.filename}
        className="h-[80vh] w-full rounded-md border border-border"
      />
    );
  }

  if (isText) {
    if (loading) return <div className="text-sm text-muted-foreground">{labels.loading}</div>;
    if (textContent === null) return null;
    if (isMarkdown(document.content_type, document.filename)) {
      // 'full' mode renders proper headings, tables, blockquotes, and
      // code blocks; the 'minimal' default is tuned for chat messages.
      return <Markdown mode="full">{textContent}</Markdown>;
    }
    return (
      <pre className="overflow-x-auto rounded-md border border-border bg-muted p-3 text-xs">
        <code>{textContent}</code>
      </pre>
    );
  }

  // Fallback: offer a download.
  return (
    <div className="rounded-md border border-border bg-muted/40 p-4 text-sm">
      <p>{labels.unsupportedType(document.content_type || labels.unknownType)}</p>
      <a
        className="mt-2 inline-block text-brand underline"
        href={document.download_url}
        target="_blank"
        rel="noreferrer"
      >
        {labels.downloadFile(document.filename)}
      </a>
    </div>
  );
}

function DocumentMetaRow({
  document,
  curatedByAgentLabel,
}: {
  document: Document;
  curatedByAgentLabel: string;
}): React.JSX.Element {
  const parts: string[] = [];
  if (document.category) parts.push(document.category);
  parts.push(humanSize(document.size_bytes));
  parts.push(new Date(document.created_at).toLocaleDateString());
  if (document.curator_type === "agent") parts.push(curatedByAgentLabel);

  return (
    <div className="mt-2 flex flex-wrap items-center gap-2 text-xs text-muted-foreground">
      {parts.map((p, i) => (
        <React.Fragment key={`${p}-${i}`}>
          {i > 0 && <span aria-hidden>·</span>}
          <span>{p}</span>
        </React.Fragment>
      ))}
      {document.tags.length > 0 && (
        <>
          <span aria-hidden>·</span>
          <span className="flex flex-wrap gap-1">
            {document.tags.map((t) => (
              <span
                key={t}
                className="rounded-full bg-secondary px-2 py-0.5 text-[10px] text-secondary-foreground"
              >
                {t}
              </span>
            ))}
          </span>
        </>
      )}
    </div>
  );
}

function humanSize(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
}
