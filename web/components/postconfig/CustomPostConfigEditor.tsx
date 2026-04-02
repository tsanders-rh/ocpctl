"use client";

import { useState } from "react";
import { Button } from "@/components/ui/button";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { AddonBrowser } from "./AddonBrowser";
import { TemplateSelector } from "./TemplateSelector";
import { ValidationPanel } from "./ValidationPanel";
import {
  Package,
  FileText,
  Lightbulb,
  PlusCircle,
  CheckCircle,
} from "lucide-react";
import type { CustomPostConfig, AddonSelection } from "@/types/api";

interface CustomPostConfigEditorProps {
  platform?: string;
  value?: CustomPostConfig;
  selectedAddons: AddonSelection[];
  onAddonsChange: (selections: AddonSelection[]) => void;
  onConfigChange: (config: CustomPostConfig | undefined) => void;
}

export function CustomPostConfigEditor({
  platform,
  value,
  selectedAddons,
  onAddonsChange,
  onConfigChange,
}: CustomPostConfigEditorProps) {
  const [activeTab, setActiveTab] = useState<string>("addons");

  const handleTemplateSelect = (templateConfig: CustomPostConfig) => {
    // Merge template config with existing custom config
    onConfigChange({
      operators: [
        ...(value?.operators || []),
        ...(templateConfig.operators || []),
      ],
      scripts: [
        ...(value?.scripts || []),
        ...(templateConfig.scripts || []),
      ],
      manifests: [
        ...(value?.manifests || []),
        ...(templateConfig.manifests || []),
      ],
      helmCharts: [
        ...(value?.helmCharts || []),
        ...(templateConfig.helmCharts || []),
      ],
    });
  };

  const getTaskCount = (config: CustomPostConfig | undefined) => {
    if (!config) return 0;
    return (
      (config.operators?.length || 0) +
      (config.scripts?.length || 0) +
      (config.manifests?.length || 0) +
      (config.helmCharts?.length || 0)
    );
  };

  const customTaskCount = getTaskCount(value);
  const totalItems = selectedAddons.length + customTaskCount;

  return (
    <div className="space-y-4">
      <Tabs value={activeTab} onValueChange={setActiveTab}>
        <TabsList className="grid w-full grid-cols-3">
          <TabsTrigger value="addons" className="gap-2">
            <Package className="h-4 w-4" />
            Add-ons
            {selectedAddons.length > 0 && (
              <span className="ml-1 rounded-full bg-primary px-2 py-0.5 text-xs font-medium text-primary-foreground">
                {selectedAddons.length}
              </span>
            )}
          </TabsTrigger>
          <TabsTrigger value="templates" className="gap-2">
            <FileText className="h-4 w-4" />
            Templates
          </TabsTrigger>
          <TabsTrigger value="validation" className="gap-2">
            <CheckCircle className="h-4 w-4" />
            Validation
          </TabsTrigger>
        </TabsList>

        <TabsContent value="addons" className="space-y-4 mt-4">
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
              selectedAddons={selectedAddons}
              onSelectionChange={onAddonsChange}
            />
          </div>
        </TabsContent>

        <TabsContent value="templates" className="space-y-4 mt-4">
          <div className="rounded-lg border bg-card p-4">
            <div className="flex items-start gap-3 mb-4">
              <Lightbulb className="h-5 w-5 text-blue-600 dark:text-blue-400 mt-0.5 flex-shrink-0" />
              <div className="text-sm">
                <p className="font-medium text-blue-900 dark:text-blue-100">
                  Reusable Templates
                </p>
                <p className="text-blue-800 dark:text-blue-200 mt-1">
                  Select from your saved templates or public templates shared by
                  other users. Templates allow you to quickly reuse complex
                  post-deployment configurations across multiple clusters.
                </p>
              </div>
            </div>
            <TemplateSelector onTemplateSelect={handleTemplateSelect} />
          </div>
        </TabsContent>

        <TabsContent value="validation" className="space-y-4 mt-4">
          <div className="rounded-lg border bg-card p-4">
            <ValidationPanel config={value} autoValidate={true} />
          </div>
        </TabsContent>
      </Tabs>

      {/* Summary Bar */}
      {totalItems > 0 && (
        <div className="rounded-lg border bg-muted/50 p-4">
          <div className="flex items-center justify-between">
            <div>
              <p className="font-semibold">Post-Configuration Summary</p>
              <p className="text-sm text-muted-foreground mt-1">
                {selectedAddons.length} add-on{selectedAddons.length !== 1 ? "s" : ""}
                {customTaskCount > 0 && (
                  <span>
                    {" "}
                    + {customTaskCount} custom task{customTaskCount !== 1 ? "s" : ""}
                  </span>
                )}
              </p>
            </div>
            <div className="flex gap-4 text-sm">
              {value?.operators && value.operators.length > 0 && (
                <div className="text-center">
                  <p className="text-2xl font-bold">{value.operators.length}</p>
                  <p className="text-muted-foreground">Operators</p>
                </div>
              )}
              {value?.scripts && value.scripts.length > 0 && (
                <div className="text-center">
                  <p className="text-2xl font-bold">{value.scripts.length}</p>
                  <p className="text-muted-foreground">Scripts</p>
                </div>
              )}
              {value?.manifests && value.manifests.length > 0 && (
                <div className="text-center">
                  <p className="text-2xl font-bold">{value.manifests.length}</p>
                  <p className="text-muted-foreground">Manifests</p>
                </div>
              )}
              {value?.helmCharts && value.helmCharts.length > 0 && (
                <div className="text-center">
                  <p className="text-2xl font-bold">{value.helmCharts.length}</p>
                  <p className="text-muted-foreground">Helm Charts</p>
                </div>
              )}
            </div>
          </div>
        </div>
      )}
    </div>
  );
}
