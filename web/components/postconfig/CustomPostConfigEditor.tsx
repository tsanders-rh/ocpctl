"use client";

import { AddonBrowser } from "./AddonBrowser";
import { Lightbulb } from "lucide-react";
import type { CustomPostConfig, AddonSelection } from "@/types/api";

interface CustomPostConfigEditorProps {
  platform?: string;
  profile?: string;
  value?: CustomPostConfig;
  selectedAddons: AddonSelection[];
  onAddonsChange: (selections: AddonSelection[]) => void;
  onConfigChange: (config: CustomPostConfig | undefined) => void;
}

export function CustomPostConfigEditor({
  platform,
  profile,
  selectedAddons,
  onAddonsChange,
}: CustomPostConfigEditorProps) {
  return (
    <div className="space-y-4">
      <div className="rounded-lg border bg-card p-4">
        <div className="flex items-start gap-3 mb-4">
          <Lightbulb className="h-5 w-5 text-blue-600 dark:text-blue-400 mt-0.5 flex-shrink-0" />
          <div className="text-sm">
            <p className="font-medium text-blue-900 dark:text-blue-100">
              Pre-Approved Add-ons
            </p>
            <p className="text-blue-800 dark:text-blue-200 mt-1">
              Select from our curated library of pre-approved add-ons.
              These are production-ready configurations maintained by the
              platform team.
            </p>
          </div>
        </div>
        <AddonBrowser
          platform={platform}
          profile={profile}
          selectedAddons={selectedAddons}
          onSelectionChange={onAddonsChange}
        />
      </div>
    </div>
  );
}
