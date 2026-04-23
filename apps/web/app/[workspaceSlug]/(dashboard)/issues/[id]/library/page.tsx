"use client";

import { use } from "react";
import { IssueLibrary } from "@multica/views/documents";

export default function IssueLibraryPage({
  params,
}: {
  params: Promise<{ id: string }>;
}) {
  const { id } = use(params);
  return (
    <div className="h-full p-4 md:p-6">
      <IssueLibrary issueId={id} />
    </div>
  );
}
