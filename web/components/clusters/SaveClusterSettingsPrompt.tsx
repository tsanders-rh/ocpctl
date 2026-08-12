"use client";

import { useEffect, useState } from "react";
import { useClusterTemplates } from "@/lib/hooks/useClusterTemplates";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Save } from "lucide-react";
import { SaveTemplateDialog } from "@/components/clusters/SaveTemplateDialog";

const STORAGE_KEY = "ocpctl:new-cluster-template-config";

// Canonical JSON with sorted keys and undefined values dropped, so two configs
// that differ only in key order or absent fields compare equal.
function canonical(value: unknown): string {
  const sort = (v: unknown): unknown => {
    if (Array.isArray(v)) return v.map(sort);
    if (v && typeof v === "object") {
      return Object.keys(v as Record<string, unknown>)
        .sort()
        .reduce<Record<string, unknown>>((acc, k) => {
          acc[k] = sort((v as Record<string, unknown>)[k]);
          return acc;
        }, {});
    }
    return v;
  };
  return JSON.stringify(sort(JSON.parse(JSON.stringify(value ?? {}))));
}

// Reads the just-created cluster's settings from sessionStorage and, if they don't
// already match one of the user's saved templates, offers to save them as a template.
export function SaveClusterSettingsPrompt() {
  const { data, isLoading } = useClusterTemplates();
  const [pendingConfig, setPendingConfig] = useState<Record<string, unknown> | null>(null);
  const [confirmOpen, setConfirmOpen] = useState(false);
  const [saveOpen, setSaveOpen] = useState(false);

  // Consume the stashed config once on mount.
  useEffect(() => {
    try {
      const raw = sessionStorage.getItem(STORAGE_KEY);
      if (raw) {
        sessionStorage.removeItem(STORAGE_KEY);
        setPendingConfig(JSON.parse(raw));
      }
    } catch {
      // Ignore malformed/unavailable storage.
    }
  }, []);

  // Once templates load, decide whether to prompt.
  useEffect(() => {
    if (!pendingConfig || isLoading) return;
    const templates = data?.templates || [];
    const target = canonical(pendingConfig);
    const alreadySaved = templates.some((t) => canonical(t.config) === target);
    if (!alreadySaved) {
      setConfirmOpen(true);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [pendingConfig, isLoading]);

  if (!pendingConfig) return null;

  return (
    <>
      <Dialog open={confirmOpen} onOpenChange={setConfirmOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Save these settings as a template?</DialogTitle>
            <DialogDescription>
              You just created this cluster with settings you haven&apos;t saved. Save them
              as a template to reuse them next time. The cluster name is not stored.
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="outline" onClick={() => setConfirmOpen(false)}>
              Not now
            </Button>
            <Button
              onClick={() => {
                setConfirmOpen(false);
                setSaveOpen(true);
              }}
            >
              <Save className="h-4 w-4 mr-2" />
              Save as template
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <SaveTemplateDialog
        open={saveOpen}
        onOpenChange={setSaveOpen}
        getConfig={() => pendingConfig}
      />
    </>
  );
}
