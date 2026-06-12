"use client";

// LibraryShell — two-pane layout (sidebar doc tree + main viewer) shared by
// both the issue library and the project library. Keeps the navigation and
// viewer wiring in one place so the per-scope components only have to fetch
// their data and supply a source-label formatter.

import * as React from "react";
import { useMemo, useState } from "react";
import { api } from "@multica/core/api";
import type { Document, LibrarySection } from "@multica/core/types";
import { useT } from "../i18n";
import { DocumentViewer } from "./document-viewer";

// DownloadAllButton does an authed GET of the export URL via the API
// client (which sends X-Workspace-Slug and credentials), then blobs the
// response and triggers a client-side download. A raw `<a href>` would
// skip those headers and hit the workspace-required middleware with a 400.
function DownloadAllButton({ url, filename }: { url: string; filename?: string }): React.JSX.Element {
  const { t } = useT("documents");
  const [pending, setPending] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const onClick = async () => {
    setPending(true);
    setError(null);
    try {
      const blob = await api.downloadBlob(url);
      const href = URL.createObjectURL(blob);
      const a = document.createElement("a");
      a.href = href;
      a.download = filename ?? "library.zip";
      document.body.appendChild(a);
      a.click();
      a.remove();
      URL.revokeObjectURL(href);
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    } finally {
      setPending(false);
    }
  };

  return (
    <div className="flex shrink-0 flex-col items-end gap-1">
      <button
        type="button"
        onClick={onClick}
        disabled={pending}
        className="rounded-md border border-border px-2 py-1 text-xs text-muted-foreground transition-colors hover:bg-muted hover:text-foreground disabled:opacity-50"
        title={t(($) => $.library.download_all_title)}
      >
        {pending ? t(($) => $.library.preparing) : t(($) => $.library.download_all)}
      </button>
      {error && <span className="text-[11px] text-destructive">{error}</span>}
    </div>
  );
}

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
  const { t } = useT("documents");
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

  // Build the nav tree. Sections are top-level folders; categories are
  // split on "/" so an agent can write `Research/Raw` and get a
  // nested folder in the UI. Uncategorised docs live in a trailing folder.
  const tree = useMemo(
    () => buildTree(filteredDocuments, sections, t(($) => $.library.uncategorised)),
    [filteredDocuments, sections, t],
  );

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
            <h2 className="mb-2 text-base font-semibold text-foreground">
              {t(($) => $.library.empty_title)}
            </h2>
            <p>{t(($) => $.library.empty_description)}</p>
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
          {exportUrl && <DownloadAllButton url={exportUrl} filename={exportFilename} />}
        </header>
        <input
          type="search"
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          placeholder={t(($) => $.library.search_placeholder)}
          className="w-full rounded-md border border-border bg-background px-2.5 py-1.5 text-sm outline-none focus:border-brand"
        />
        <nav className="flex flex-col gap-0.5 overflow-y-auto pr-1 text-sm">
          {tree.map((node) => (
            <TreeFolder
              key={node.path}
              node={node}
              depth={0}
              selectedId={selectedId}
              onSelect={setSelectedId}
              renderDocMeta={renderDocMeta}
              pinnedLabel={t(($) => $.library.pinned)}
              // Expand the first two levels by default so the landing
              // view feels browseable; deeper nesting stays collapsed.
              defaultExpanded={true}
            />
          ))}
        </nav>
      </aside>
      <section className="min-w-0 flex-1 overflow-y-auto pr-1">
        {selectedDoc ? (
          renderViewer ? renderViewer(selectedDoc) : <DocumentViewer document={selectedDoc} />
        ) : (
          <div className="flex h-full items-center justify-center text-sm text-muted-foreground">
            {t(($) => $.library.no_selected)}
          </div>
        )}
      </section>
    </div>
  );
}

// ---------------------------------------------------------------------------
// Tree model
// ---------------------------------------------------------------------------
// Libraries can be arbitrarily nested. Two sources of hierarchy:
//
//   1. Explicit sections. Each section is a top-level folder.
//   2. Category strings. Categories split on "/" so an agent that writes
//      `--category Research/Raw` gets a two-level tree. Unset category
//      rolls up into a trailing "Uncategorised" folder.
//
// Future: hierarchical sections (parent_section_id) slot into the same tree
// naturally; for now sections are flat top-level entries.

interface TreeNode<D extends Document> {
  /** Unique stable key for React, also drives persisted expand state. */
  path: string;
  name: string;
  /** Child folders. Sorted alphabetically. */
  children: TreeNode<D>[];
  /** Documents that live at this node (no deeper category segments). */
  docs: D[];
  /** True for section roots; used to give them a subtle visual marker. */
  isSection?: boolean;
}

function buildTree<D extends Document>(
  documents: D[],
  sections: LibrarySection[],
  uncategorisedLabel: string,
): TreeNode<D>[] {
  const byId = new Map(documents.map((d) => [d.attachment_id, d]));
  const claimed = new Set<string>();
  const roots: TreeNode<D>[] = [];

  // Sections first. Each becomes a top-level node; docs inside use their
  // category path within that section (uncategorised → directly under section).
  for (const section of sections) {
    const sectionDocs: D[] = [];
    for (const docId of section.document_ids) {
      const doc = byId.get(docId);
      if (doc && !claimed.has(docId)) {
        sectionDocs.push(doc);
        claimed.add(docId);
      }
    }
    if (sectionDocs.length === 0) continue;
    const node: TreeNode<D> = {
      path: `section:${section.id}`,
      name: section.name,
      children: [],
      docs: [],
      isSection: true,
    };
    for (const doc of sectionDocs) {
      insertDoc(node, splitCategoryPath(doc.category), doc);
    }
    sortNode(node);
    roots.push(node);
  }

  // Category tree for everything else.
  const categoryRoot: TreeNode<D> = { path: "__root__", name: "", children: [], docs: [] };
  const uncategorised: D[] = [];
  for (const doc of documents) {
    if (claimed.has(doc.attachment_id)) continue;
    const segments = splitCategoryPath(doc.category);
    if (segments.length === 0) {
      uncategorised.push(doc);
    } else {
      insertDoc(categoryRoot, segments, doc);
    }
  }
  sortNode(categoryRoot);
  roots.push(...categoryRoot.children);

  if (uncategorised.length > 0) {
    roots.push({
      path: "cat:__uncategorised",
      name: uncategorisedLabel,
      children: [],
      docs: uncategorised,
    });
  }

  return roots;
}

function splitCategoryPath(category: string | undefined): string[] {
  if (!category) return [];
  return category
    .split("/")
    .map((s) => s.trim())
    .filter((s) => s.length > 0);
}

function insertDoc<D extends Document>(node: TreeNode<D>, segments: string[], doc: D): void {
  if (segments.length === 0) {
    node.docs.push(doc);
    return;
  }
  const [head, ...rest] = segments;
  if (head === undefined) {
    node.docs.push(doc);
    return;
  }
  const childPath = node.path === "__root__" ? `cat:${head}` : `${node.path}/${head}`;
  let child = node.children.find((c) => c.name === head);
  if (!child) {
    child = { path: childPath, name: head, children: [], docs: [] };
    node.children.push(child);
  }
  insertDoc(child, rest, doc);
}

function sortNode<D extends Document>(node: TreeNode<D>): void {
  node.children.sort((a, b) => a.name.localeCompare(b.name));
  node.docs.sort(docSort);
  for (const child of node.children) sortNode(child);
}

function docSort<D extends Document>(a: D, b: D): number {
  // Pinned first, then explicit sort_order, then alpha on title/filename.
  if (a.pinned !== b.pinned) return a.pinned ? -1 : 1;
  if (a.sort_order !== b.sort_order) return a.sort_order - b.sort_order;
  const ax = (a.title ?? a.filename).toLowerCase();
  const bx = (b.title ?? b.filename).toLowerCase();
  return ax.localeCompare(bx);
}

// ---------------------------------------------------------------------------
// TreeFolder — recursive folder node with expand/collapse
// ---------------------------------------------------------------------------

function TreeFolder<D extends Document>({
  node,
  depth,
  selectedId,
  onSelect,
  renderDocMeta,
  pinnedLabel,
  defaultExpanded,
}: {
  node: TreeNode<D>;
  depth: number;
  selectedId: string | null;
  onSelect: (id: string) => void;
  renderDocMeta?: (doc: D) => React.ReactNode;
  pinnedLabel: string;
  defaultExpanded: boolean;
}): React.JSX.Element {
  // Default expanded for depth 0 (top-level) and depth 1; deeper levels
  // start collapsed so the tree doesn't explode on load.
  const [expanded, setExpanded] = useState(defaultExpanded && depth < 2);
  const totalCount =
    node.docs.length + node.children.reduce((n, c) => n + countDocs(c), 0);

  const isSection = node.isSection;

  return (
    <div className="flex flex-col">
      <button
        type="button"
        onClick={() => setExpanded((v) => !v)}
        className={
          "flex w-full items-center gap-1.5 rounded-md px-1.5 py-1 text-left transition-colors hover:bg-muted " +
          (isSection
            ? "font-semibold text-foreground"
            : depth === 0
              ? "font-medium text-foreground"
              : "text-muted-foreground")
        }
        style={{ paddingLeft: `${depth * 12 + 6}px` }}
      >
        <span className="w-3 shrink-0 text-[10px] text-muted-foreground">
          {expanded ? "▾" : "▸"}
        </span>
        <span className="truncate">{node.name}</span>
        <span className="ml-auto shrink-0 text-[10px] text-muted-foreground">
          {totalCount}
        </span>
      </button>
      {expanded && (
        <div className="flex flex-col">
          {node.children.map((child) => (
            <TreeFolder
              key={child.path}
              node={child}
              depth={depth + 1}
              selectedId={selectedId}
              onSelect={onSelect}
              renderDocMeta={renderDocMeta}
              pinnedLabel={pinnedLabel}
              defaultExpanded={defaultExpanded}
            />
          ))}
          {node.docs.map((doc) => (
            <TreeLeaf
              key={doc.attachment_id}
              doc={doc}
              depth={depth + 1}
              active={doc.attachment_id === selectedId}
              onSelect={onSelect}
              renderDocMeta={renderDocMeta}
              pinnedLabel={pinnedLabel}
            />
          ))}
        </div>
      )}
    </div>
  );
}

function TreeLeaf<D extends Document>({
  doc,
  depth,
  active,
  onSelect,
  renderDocMeta,
  pinnedLabel,
}: {
  doc: D;
  depth: number;
  active: boolean;
  onSelect: (id: string) => void;
  renderDocMeta?: (doc: D) => React.ReactNode;
  pinnedLabel: string;
}): React.JSX.Element {
  const label = doc.title ?? doc.filename;
  return (
    <button
      type="button"
      onClick={() => onSelect(doc.attachment_id)}
      className={
        "flex w-full flex-col items-start rounded-md py-1 pr-2 text-left text-sm transition-colors " +
        (active ? "bg-secondary text-secondary-foreground" : "hover:bg-muted")
      }
      style={{ paddingLeft: `${depth * 12 + 20}px` }}
    >
      <span className="flex w-full items-center gap-2">
        {doc.pinned && <span aria-label={pinnedLabel}>📌</span>}
        <span className="truncate">{label}</span>
      </span>
      {renderDocMeta && (
        <span className="mt-0.5 text-[11px] text-muted-foreground">
          {renderDocMeta(doc)}
        </span>
      )}
    </button>
  );
}

function countDocs<D extends Document>(node: TreeNode<D>): number {
  return node.docs.length + node.children.reduce((n, c) => n + countDocs(c), 0);
}
