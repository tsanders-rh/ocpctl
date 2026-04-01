"use client";

import { useState } from "react";
import { usePostConfigTemplates } from "@/lib/hooks/usePostConfig";
import { Button } from "@/components/ui/button";
import { Label } from "@/components/ui/label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { FileText, Eye, Download } from "lucide-react";
import type { PostConfigTemplate, CustomPostConfig } from "@/types/api";

interface TemplateSelectorProps {
  onTemplateSelect: (config: CustomPostConfig) => void;
}

export function TemplateSelector({ onTemplateSelect }: TemplateSelectorProps) {
  const [selectedTemplateId, setSelectedTemplateId] = useState<string>("");
  const [showPublicOnly, setShowPublicOnly] = useState(false);

  const { data, isLoading } = usePostConfigTemplates({
    public: showPublicOnly,
  });

  const templates = data?.templates || [];
  const selectedTemplate = templates.find((t) => t.id === selectedTemplateId);

  const handleApplyTemplate = () => {
    if (selectedTemplate) {
      onTemplateSelect(selectedTemplate.config);
    }
  };

  const getTaskCount = (config: CustomPostConfig) => {
    return (
      (config.operators?.length || 0) +
      (config.scripts?.length || 0) +
      (config.manifests?.length || 0) +
      (config.helmCharts?.length || 0)
    );
  };

  if (isLoading) {
    return (
      <div className="flex items-center justify-center py-8">
        <div className="animate-spin rounded-full h-8 w-8 border-b-2 border-primary"></div>
      </div>
    );
  }

  return (
    <div className="space-y-4">
      <div className="space-y-2">
        <Label htmlFor="template-select">Select Template</Label>
        <Select value={selectedTemplateId} onValueChange={setSelectedTemplateId}>
          <SelectTrigger id="template-select">
            <SelectValue placeholder="Choose a template..." />
          </SelectTrigger>
          <SelectContent>
            {templates.length === 0 ? (
              <div className="p-2 text-sm text-muted-foreground">
                No templates available
              </div>
            ) : (
              templates.map((template: PostConfigTemplate) => (
                <SelectItem key={template.id} value={template.id}>
                  {template.name}
                  {template.isPublic && (
                    <span className="ml-2 text-xs text-muted-foreground">
                      (Public)
                    </span>
                  )}
                </SelectItem>
              ))
            )}
          </SelectContent>
        </Select>
      </div>

      {selectedTemplate && (
        <div className="border rounded-lg p-4 space-y-4">
          <div>
            <h4 className="font-semibold flex items-center gap-2">
              <FileText className="h-4 w-4" />
              {selectedTemplate.name}
            </h4>
            <p className="text-sm text-muted-foreground mt-1">
              {selectedTemplate.description}
            </p>
          </div>

          <div className="grid grid-cols-2 gap-3 text-sm">
            {selectedTemplate.config.operators && selectedTemplate.config.operators.length > 0 && (
              <div className="bg-muted/50 rounded-md p-3">
                <p className="font-medium text-muted-foreground">Operators</p>
                <p className="text-xl font-semibold">
                  {selectedTemplate.config.operators.length}
                </p>
              </div>
            )}
            {selectedTemplate.config.scripts && selectedTemplate.config.scripts.length > 0 && (
              <div className="bg-muted/50 rounded-md p-3">
                <p className="font-medium text-muted-foreground">Scripts</p>
                <p className="text-xl font-semibold">
                  {selectedTemplate.config.scripts.length}
                </p>
              </div>
            )}
            {selectedTemplate.config.manifests && selectedTemplate.config.manifests.length > 0 && (
              <div className="bg-muted/50 rounded-md p-3">
                <p className="font-medium text-muted-foreground">Manifests</p>
                <p className="text-xl font-semibold">
                  {selectedTemplate.config.manifests.length}
                </p>
              </div>
            )}
            {selectedTemplate.config.helmCharts && selectedTemplate.config.helmCharts.length > 0 && (
              <div className="bg-muted/50 rounded-md p-3">
                <p className="font-medium text-muted-foreground">Helm Charts</p>
                <p className="text-xl font-semibold">
                  {selectedTemplate.config.helmCharts.length}
                </p>
              </div>
            )}
          </div>

          {selectedTemplate.tags && selectedTemplate.tags.length > 0 && (
            <div className="flex flex-wrap gap-2">
              {selectedTemplate.tags.map((tag) => (
                <span
                  key={tag}
                  className="inline-flex items-center rounded-md bg-blue-50 dark:bg-blue-950 px-2 py-1 text-xs font-medium text-blue-700 dark:text-blue-300 ring-1 ring-inset ring-blue-700/10"
                >
                  {tag}
                </span>
              ))}
            </div>
          )}

          <div className="flex gap-2 pt-2">
            <Button onClick={handleApplyTemplate} className="flex-1">
              <Download className="h-4 w-4 mr-2" />
              Apply Template
            </Button>
          </div>
        </div>
      )}

      {templates.length === 0 && (
        <div className="text-center py-8 text-muted-foreground border rounded-lg border-dashed">
          <FileText className="h-12 w-12 mx-auto mb-3 opacity-50" />
          <p>No templates available yet</p>
          <p className="text-sm mt-1">
            Create your first template to reuse configurations
          </p>
        </div>
      )}
    </div>
  );
}
