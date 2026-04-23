"use client";

// DocumentViewer — renders a single library Document inline in the UI.
// Markdown and text go through the Markdown component for syntax highlighting
// and mention rendering; PDFs get an iframe; images get an <img>; anything
// else falls back to a download link.

import * as React from "react";
import { useEffect, useState } from "react";
import type { Document } from "@multica/core/types";
import { Markdown } from "../common/markdown";

interface DocumentViewerProps {
  document: Document;
  /** Optional class applied to the outer wrapper. */
  className?: string;
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

export function DocumentViewer({ document, className }: DocumentViewerProps): React.JSX.Element {
  const [textContent, setTextContent] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(false);

  const isText = isTextish(document.content_type, document.filename);

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
        <h1 className="text-xl font-semibold">{title}</h1>
        {document.summary && (
          <p className="mt-1 text-sm text-muted-foreground">{document.summary}</p>
        )}
        <DocumentMetaRow document={document} />
      </header>

      <div className="prose prose-sm max-w-none dark:prose-invert">
        {renderContent({ document, textContent, loading, error, isText })}
      </div>
    </article>
  );
}

function renderContent(args: {
  document: Document;
  textContent: string | null;
  loading: boolean;
  error: string | null;
  isText: boolean;
}): React.ReactNode {
  const { document, textContent, loading, error, isText } = args;
  const ct = document.content_type.toLowerCase();

  if (error) {
    return (
      <div className="rounded-md border border-destructive/30 bg-destructive/5 p-3 text-sm text-destructive">
        Failed to load document: {error}
        <div className="mt-2">
          <a className="underline" href={document.download_url} target="_blank" rel="noreferrer">
            Download instead
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
    if (loading) return <div className="text-sm text-muted-foreground">Loading…</div>;
    if (textContent === null) return null;
    if (isMarkdown(document.content_type, document.filename)) {
      return <Markdown>{textContent}</Markdown>;
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
      <p>This document type ({document.content_type || "unknown"}) does not have an inline preview.</p>
      <a
        className="mt-2 inline-block text-brand underline"
        href={document.download_url}
        target="_blank"
        rel="noreferrer"
      >
        Download {document.filename}
      </a>
    </div>
  );
}

function DocumentMetaRow({ document }: { document: Document }): React.JSX.Element {
  const parts: string[] = [];
  if (document.category) parts.push(document.category);
  parts.push(humanSize(document.size_bytes));
  parts.push(new Date(document.created_at).toLocaleDateString());
  if (document.curator_type === "agent") parts.push("curated by agent");

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
