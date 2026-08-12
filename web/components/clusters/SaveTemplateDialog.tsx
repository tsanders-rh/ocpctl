"use client";

import { useEffect, useState } from "react";
import {
  useClusterTemplates,
  useCreateClusterTemplate,
  useUpdateClusterTemplate,
} from "@/lib/hooks/useClusterTemplates";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Save, Loader2 } from "lucide-react";
import { ApiError } from "@/lib/api/client";

// Mirrors MaxTemplatesPerUser in internal/store/cluster_templates.go.
export const MAX_CLUSTER_TEMPLATES = 5;

type Mode = "new" | "overwrite";

interface SaveTemplateDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  // Snapshot of the form/config values to persist (cluster name excluded).
  getConfig: () => Record<string, unknown>;
  onSaved?: () => void;
}

export function SaveTemplateDialog({
  open,
  onOpenChange,
  getConfig,
  onSaved,
}: SaveTemplateDialogProps) {
  const { data } = useClusterTemplates();
  const createMutation = useCreateClusterTemplate();
  const updateMutation = useUpdateClusterTemplate();

  const templates = data?.templates || [];
  const atLimit = templates.length >= MAX_CLUSTER_TEMPLATES;

  const [mode, setMode] = useState<Mode>("new");
  const [name, setName] = useState("");
  const [targetId, setTargetId] = useState("");
  const [error, setError] = useState("");

  // Reset the dialog each time it opens; force overwrite when the user is at the limit.
  useEffect(() => {
    if (open) {
      setMode(atLimit && templates.length > 0 ? "overwrite" : "new");
      setName("");
      setTargetId("");
      setError("");
    }
  }, [open, atLimit, templates.length]);

  const pending = createMutation.isPending || updateMutation.isPending;

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setError("");
    const config = getConfig();

    try {
      if (mode === "overwrite") {
        const target = templates.find((t) => t.id === targetId);
        if (!target) {
          setError("Select a template to overwrite.");
          return;
        }
        await updateMutation.mutateAsync({
          id: target.id,
          data: { name: target.name, config },
        });
      } else {
        const trimmed = name.trim();
        if (trimmed.length < 3) {
          setError("Name must be at least 3 characters.");
          return;
        }
        await createMutation.mutateAsync({ name: trimmed, config });
      }
      onSaved?.();
      onOpenChange(false);
    } catch (err) {
      if (err instanceof ApiError && err.response?.message) {
        setError(err.response.message);
      } else {
        setError("Failed to save template. A template with this name may already exist.");
      }
    }
  };

  const canOverwrite = templates.length > 0;
  const submitDisabled =
    pending ||
    (mode === "new" ? name.trim().length < 3 : !targetId);

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <form onSubmit={handleSubmit}>
          <DialogHeader>
            <DialogTitle>Save as template</DialogTitle>
            <DialogDescription>
              Save the current settings as a reusable template. The cluster name is not
              stored.
            </DialogDescription>
          </DialogHeader>

          <div className="space-y-4 py-4">
            {canOverwrite && (
              <div className="flex gap-2">
                <Button
                  type="button"
                  size="sm"
                  variant={mode === "new" ? "default" : "outline"}
                  onClick={() => setMode("new")}
                  disabled={atLimit}
                  title={atLimit ? `You already have ${MAX_CLUSTER_TEMPLATES} templates` : undefined}
                >
                  New template
                </Button>
                <Button
                  type="button"
                  size="sm"
                  variant={mode === "overwrite" ? "default" : "outline"}
                  onClick={() => setMode("overwrite")}
                >
                  Overwrite existing
                </Button>
              </div>
            )}

            {atLimit && mode === "overwrite" && (
              <p className="text-xs text-muted-foreground">
                You have reached the limit of {MAX_CLUSTER_TEMPLATES} templates. Overwrite
                one or delete a template to save a new one.
              </p>
            )}

            {mode === "new" ? (
              <div className="space-y-2">
                <Label htmlFor="save-template-name">
                  Name <span className="text-red-500">*</span>
                </Label>
                <Input
                  id="save-template-name"
                  placeholder="My standard AWS SNO"
                  value={name}
                  onChange={(e) => setName(e.target.value)}
                  minLength={3}
                  maxLength={100}
                  autoFocus
                  disabled={pending}
                />
              </div>
            ) : (
              <div className="space-y-2">
                <Label htmlFor="save-template-target">
                  Template to overwrite <span className="text-red-500">*</span>
                </Label>
                <Select value={targetId} onValueChange={setTargetId} disabled={pending}>
                  <SelectTrigger id="save-template-target">
                    <SelectValue placeholder="Choose a template..." />
                  </SelectTrigger>
                  <SelectContent>
                    {templates.map((t) => (
                      <SelectItem key={t.id} value={t.id}>
                        {t.name}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>
            )}

            {error && <p className="text-sm text-red-600 dark:text-red-400">{error}</p>}
          </div>

          <DialogFooter>
            <Button
              type="button"
              variant="outline"
              onClick={() => onOpenChange(false)}
              disabled={pending}
            >
              Cancel
            </Button>
            <Button type="submit" disabled={submitDisabled}>
              {pending ? (
                <>
                  <Loader2 className="h-4 w-4 mr-2 animate-spin" />
                  Saving...
                </>
              ) : (
                <>
                  <Save className="h-4 w-4 mr-2" />
                  Save
                </>
              )}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
