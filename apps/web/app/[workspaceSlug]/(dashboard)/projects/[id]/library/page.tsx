"use client";

import { use } from "react";
import { ProjectLibrary } from "@multica/views/documents";

export default function ProjectLibraryPage({
  params,
}: {
  params: Promise<{ id: string }>;
}) {
  const { id } = use(params);
  return (
    <div className="h-full p-4 md:p-6">
      <ProjectLibrary projectId={id} />
    </div>
  );
}
