"use client";

import { useState } from "react";
import {
  useClusterTemplates,
  useDeleteClusterTemplate,
} from "@/lib/hooks/useClusterTemplates";
import { Button } from "@/components/ui/button";
import { Label } from "@/components/ui/label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Download, Save, Trash2 } from "lucide-react";
import type { ClusterTemplate } from "@/types/api";
import {
  SaveTemplateDialog,
  MAX_CLUSTER_TEMPLATES,
} from "@/components/clusters/SaveTemplateDialog";

interface ClusterTemplateBarProps {
  // Apply a template's stored fields to the create-cluster form.
  onApply: (config: Record<string, unknown>) => void;
  // Snapshot of the current form values to persist as a new template.
  getCurrentConfig: () => Record<string, unknown>;
}

export function ClusterTemplateBar({ onApply, getCurrentConfig }: ClusterTemplateBarProps) {
  const { data, isLoading } = useClusterTemplates();
  const deleteMutation = useDeleteClusterTemplate();

  const [selectedId, setSelectedId] = useState<string>("");
  const [saveOpen, setSaveOpen] = useState(false);

  const templates = data?.templates || [];
  const selected = templates.find((t) => t.id === selectedId);

  const handleApply = () => {
    if (selected) {
      onApply(selected.config);
    }
  };

  const handleDelete = async () => {
    if (!selected) return;
    await deleteMutation.mutateAsync(selected.id);
    setSelectedId("");
  };

  return (
    <div className="rounded-lg border bg-muted/30 p-4">
      <div className="flex items-end gap-3 flex-wrap">
        <div className="flex-1 min-w-[220px] space-y-2">
          <Label htmlFor="cluster-template-select">Load from template</Label>
          <Select value={selectedId} onValueChange={setSelectedId} disabled={isLoading}>
            <SelectTrigger id="cluster-template-select">
              <SelectValue placeholder={isLoading ? "Loading..." : "Choose a template..."} />
            </SelectTrigger>
            <SelectContent>
              {templates.length === 0 ? (
                <div className="p-2 text-sm text-muted-foreground">No templates saved yet</div>
              ) : (
                templates.map((t: ClusterTemplate) => (
                  <SelectItem key={t.id} value={t.id}>
                    {t.name}
                  </SelectItem>
                ))
              )}
            </SelectContent>
          </Select>
        </div>

        <Button type="button" onClick={handleApply} disabled={!selected}>
          <Download className="h-4 w-4 mr-2" />
          Apply
        </Button>

        <Button
          type="button"
          variant="outline"
          onClick={handleDelete}
          disabled={!selected || deleteMutation.isPending}
          title="Delete selected template"
        >
          <Trash2 className="h-4 w-4" />
        </Button>

        <div className="ml-auto">
          <Button type="button" variant="secondary" onClick={() => setSaveOpen(true)}>
            <Save className="h-4 w-4 mr-2" />
            Save as template
          </Button>
        </div>
      </div>

      <p className="mt-2 text-xs text-muted-foreground">
        Applying a template pre-fills the fields it contains. The cluster name is never
        included and any fields not in the template are left for you to complete. You can
        save up to {MAX_CLUSTER_TEMPLATES} templates.
      </p>

      <SaveTemplateDialog
        open={saveOpen}
        onOpenChange={setSaveOpen}
        getConfig={getCurrentConfig}
      />
    </div>
  );
}
