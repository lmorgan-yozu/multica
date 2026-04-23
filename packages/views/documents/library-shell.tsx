"use client";

// LibraryShell — two-pane layout (sidebar doc tree + main viewer) shared by
// both the issue library and the project library. Keeps the navigation and
// viewer wiring in one place so the per-scope components only have to fetch
// their data and supply a source-label formatter.

import * as React from "react";
import { useMemo, useState } from "react";
import type { Document, LibrarySection } from "@multica/core/types";
import { DocumentViewer } from "./document-viewer";

export interface LibraryShellProps<D extends Document> {
  title: string;
  subtitle?: string;
  documents: D[];
  sections: LibrarySection[];
  /** Optional label rendered under each doc in the tree (e.g. issue identifier). */
  renderDocMeta?: (doc: D) => React.ReactNode;
  /** Render a doc when the user selects it. Defaults to DocumentViewer. */
  renderViewer?: (doc: D) => React.ReactNode;
  emptyState?: React.ReactNode;
  /** URL for the "Download all" button. Omit to hide the button. */
  exportUrl?: string;
  /** Filename for the downloaded zip. */
  exportFilename?: string;
}

export function LibraryShell<D extends Document>({
  title,
  subtitle,
  documents,
  sections,
  renderDocMeta,
  renderViewer,
  emptyState,
  exportUrl,
  exportFilename,
}: LibraryShellProps<D>): React.JSX.Element {
  const [selectedId, setSelectedId] = useState<string | null>(
    documents[0]?.attachment_id ?? null,
  );
  const [query, setQuery] = useState("");

  const filteredDocuments = useMemo(() => {
    if (!query.trim()) return documents;
    const q = query.trim().toLowerCase();
    return documents.filter((d) => {
      const hay = `${d.filename} ${d.title ?? ""} ${d.summary ?? ""} ${d.category ?? ""} ${d.tags.join(" ")}`.toLowerCase();
      return hay.includes(q);
    });
  }, [documents, query]);

  // Group docs by category for the default tree. Sections override this when
  // an explicit section contains the doc — the first section the doc appears
  // in wins.
  const groups = useMemo(() => groupDocuments(filteredDocuments, sections), [filteredDocuments, sections]);

  const selectedDoc = useMemo(
    () => documents.find((d) => d.attachment_id === selectedId) ?? null,
    [documents, selectedId],
  );

  // Keep the selected doc in sync with the filtered set — if the current
  // selection is filtered out, fall back to the first visible doc.
  React.useEffect(() => {
    if (selectedDoc && filteredDocuments.some((d) => d.attachment_id === selectedDoc.attachment_id)) {
      return;
    }
    setSelectedId(filteredDocuments[0]?.attachment_id ?? null);
  }, [filteredDocuments, selectedDoc]);

  if (documents.length === 0) {
    return (
      <div className="flex min-h-[60vh] items-center justify-center p-8 text-center">
        {emptyState ?? (
          <div className="max-w-md text-sm text-muted-foreground">
            <h2 className="mb-2 text-base font-semibold text-foreground">No documents yet</h2>
            <p>
              Drop markdown, PDFs, images or text files onto a comment or attach them when you create
              an issue. They&rsquo;ll appear here, grouped by category, ready to browse.
            </p>
          </div>
        )}
      </div>
    );
  }

  return (
    <div className="flex h-full min-h-[60vh] flex-col gap-4 lg:flex-row">
      <aside className="flex shrink-0 flex-col gap-3 lg:w-80 lg:border-r lg:border-border lg:pr-4">
        <header className="flex items-start justify-between gap-2">
          <div className="min-w-0">
            <h1 className="text-lg font-semibold">{title}</h1>
            {subtitle && <p className="text-xs text-muted-foreground">{subtitle}</p>}
          </div>
          {exportUrl && (
            <a
              href={exportUrl}
              download={exportFilename ?? true}
              className="shrink-0 rounded-md border border-border px-2 py-1 text-xs text-muted-foreground transition-colors hover:bg-muted hover:text-foreground"
              title="Download all documents as a zip"
            >
              Download all
            </a>
          )}
        </header>
        <input
          type="search"
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          placeholder="Search documents"
          className="w-full rounded-md border border-border bg-background px-2.5 py-1.5 text-sm outline-none focus:border-brand"
        />
        <nav className="flex flex-col gap-4 overflow-y-auto pr-1">
          {groups.map((group) => (
            <div key={group.id}>
              <h2 className="mb-1 text-xs font-semibold uppercase tracking-wide text-muted-foreground">
                {group.name}
              </h2>
              <ul className="flex flex-col">
                {group.documents.map((doc) => {
                  const active = doc.attachment_id === selectedId;
                  const label = doc.title ?? doc.filename;
                  return (
                    <li key={doc.attachment_id}>
                      <button
                        type="button"
                        onClick={() => setSelectedId(doc.attachment_id)}
                        className={
                          "flex w-full flex-col items-start rounded-md px-2 py-1.5 text-left text-sm transition-colors " +
                          (active
                            ? "bg-secondary text-secondary-foreground"
                            : "hover:bg-muted")
                        }
                      >
                        <span className="flex w-full items-center gap-2">
                          {doc.pinned && <span aria-label="pinned">📌</span>}
                          <span className="truncate">{label}</span>
                        </span>
                        {renderDocMeta && (
                          <span className="mt-0.5 text-[11px] text-muted-foreground">
                            {renderDocMeta(doc)}
                          </span>
                        )}
                      </button>
                    </li>
                  );
                })}
              </ul>
            </div>
          ))}
        </nav>
      </aside>
      <section className="min-w-0 flex-1 overflow-y-auto pr-1">
        {selectedDoc ? (
          renderViewer ? renderViewer(selectedDoc) : <DocumentViewer document={selectedDoc} />
        ) : (
          <div className="flex h-full items-center justify-center text-sm text-muted-foreground">
            No document selected.
          </div>
        )}
      </section>
    </div>
  );
}

interface DocGroup<D> {
  id: string;
  name: string;
  documents: D[];
}

// Group by section membership first, then by category, then everything else
// goes under "Uncategorised". A doc that belongs to multiple sections is
// listed in the first matching section only so the tree stays tidy.
function groupDocuments<D extends Document>(
  documents: D[],
  sections: LibrarySection[],
): DocGroup<D>[] {
  const byId = new Map(documents.map((d) => [d.attachment_id, d]));
  const claimed = new Set<string>();
  const groups: DocGroup<D>[] = [];

  for (const section of sections) {
    const docs: D[] = [];
    for (const docId of section.document_ids) {
      const doc = byId.get(docId);
      if (doc && !claimed.has(docId)) {
        docs.push(doc);
        claimed.add(docId);
      }
    }
    if (docs.length > 0) {
      groups.push({ id: `section-${section.id}`, name: section.name, documents: docs });
    }
  }

  const byCategory = new Map<string, D[]>();
  const uncategorised: D[] = [];
  for (const doc of documents) {
    if (claimed.has(doc.attachment_id)) continue;
    if (doc.category) {
      const list = byCategory.get(doc.category) ?? [];
      list.push(doc);
      byCategory.set(doc.category, list);
    } else {
      uncategorised.push(doc);
    }
  }
  const categoryKeys = Array.from(byCategory.keys()).sort((a, b) => a.localeCompare(b));
  for (const cat of categoryKeys) {
    groups.push({ id: `cat-${cat}`, name: cat, documents: byCategory.get(cat) ?? [] });
  }
  if (uncategorised.length > 0) {
    groups.push({ id: "cat-__uncategorised", name: "Uncategorised", documents: uncategorised });
  }
  return groups;
}
