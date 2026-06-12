"use client";

import type { ReactNode } from "react";
import { ArrowUp, Loader2, Square } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@multica/ui/components/ui/tooltip";

interface SubmitButtonProps {
  onClick: () => void;
  disabled?: boolean;
  loading?: boolean;
  running?: boolean;
  onStop?: () => void;
  /**
   * Tooltip shown over the send button when idle. Pass a string or a node
   * (e.g. `Send · ⌘↵`). Omit to render no tooltip.
   * Callers compose the shortcut hint themselves to keep this component
   * free of `@multica/core` (platform-detection) and i18n imports.
   */
  tooltip?: ReactNode;
  /** Tooltip shown over the stop button while a run is in progress. */
  stopTooltip?: ReactNode;
  /**
   * Accessible name for the icon-only send button. Callers pass their
   * translated "Send" string — without it the button has no name for
   * assistive tech (the tooltip content is not exposed as a label).
   */
  ariaLabel?: string;
  /** Accessible name for the icon-only stop button. */
  stopAriaLabel?: string;
}

function SubmitButton({
  onClick,
  disabled,
  loading,
  running,
  onStop,
  tooltip,
  stopTooltip,
  ariaLabel,
  stopAriaLabel,
}: SubmitButtonProps) {
  if (running) {
    const stopButton = (
      <Button size="icon-sm" onClick={onStop} aria-label={stopAriaLabel}>
        <Square className="fill-current" />
      </Button>
    );
    if (!stopTooltip) return stopButton;
    return (
      <Tooltip>
        <TooltipTrigger render={stopButton} />
        <TooltipContent side="top">{stopTooltip}</TooltipContent>
      </Tooltip>
    );
  }

  const submitButton = (
    <Button
      size="icon-sm"
      disabled={disabled || loading}
      onClick={onClick}
      aria-label={ariaLabel}
    >
      {loading ? <Loader2 className="animate-spin" /> : <ArrowUp />}
    </Button>
  );
  if (!tooltip) return submitButton;
  return (
    <Tooltip>
      <TooltipTrigger render={submitButton} />
      <TooltipContent side="top">{tooltip}</TooltipContent>
    </Tooltip>
  );
}

export { SubmitButton, type SubmitButtonProps };
