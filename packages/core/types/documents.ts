// Document library types. The library layer sits on top of the attachment
// record and adds curation metadata (category, tags, pinned/archived, sort
// order, curator attribution) so issues and projects can be browsed as a
// structured document set rather than a flat file list.

export interface Document {
  attachment_id: string;
  issue_id: string | null;
  comment_id: string | null;
  filename: string;
  content_type: string;
  size_bytes: number;
  download_url: string;
  /** Authenticated proxy URL suitable for inline rendering in the UI. */
  inline_url: string;
  created_at: string;
  uploader_type: "member" | "agent";
  uploader_id: string;

  /** Curation fields — present only when a curation record exists. */
  title?: string;
  summary?: string;
  category?: string;
  tags: string[];
  sort_order: number;
  pinned: boolean;
  archived: boolean;

  curator_type?: "member" | "agent";
  curator_id?: string;
  curated_at?: string;

  // Document-version identity. Populated once the attachment has been
  // touched through a versioning-aware endpoint; null for older data.
  document_id?: string;
  version_number?: number;
}

export interface DocumentVersion {
  id: string;
  document_id: string;
  attachment_id: string;
  version_number: number;
  notes?: string;
  author_type: "member" | "agent";
  author_id: string;
  created_at: string;
  filename: string;
  size_bytes: number;
  content_type: string;
}

export interface DocumentVersionsResponse {
  document_id: string;
  versions: DocumentVersion[];
}

export interface DocumentComment {
  id: string;
  document_id: string;
  parent_id?: string;
  content: string;
  author_type: "member" | "agent";
  author_id: string;
  created_at: string;
  updated_at: string;
}

export interface DocumentCommentsResponse {
  document_id: string;
  comments: DocumentComment[];
}

export interface UnsummarisedDocumentsResponse {
  documents: Array<{
    attachment_id: string;
    filename: string;
    content_type: string;
    size_bytes: number;
    created_at: string;
  }>;
}

export interface ProjectLibraryDocument extends Document {
  source_issue_id: string;
  source_issue_identifier: string;
  source_issue_title: string;
}

export interface LibrarySection {
  id: string;
  name: string;
  description?: string;
  sort_order: number;
  created_at: string;
  updated_at: string;
  document_ids: string[];
}

export interface IssueLibrary {
  issue_id: string;
  documents: Document[];
  sections: LibrarySection[];
}

export interface ProjectLibrary {
  project_id: string;
  documents: ProjectLibraryDocument[];
  sections: LibrarySection[];
}

export interface DocumentCurationPayload {
  title?: string;
  summary?: string;
  category?: string;
  tags?: string[];
  sort_order?: number;
  pinned?: boolean;
  archived?: boolean;
}

export interface CreateLibrarySectionPayload {
  name: string;
  description?: string;
  sort_order?: number;
  issue_id?: string;
  project_id?: string;
}
