"use client";

import { useState } from "react";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";
import { Checkbox } from "@/components/ui/checkbox";
import { Badge } from "@/components/ui/badge";
import { Save, X, Loader2 } from "lucide-react";
import { useCreatePostConfigTemplate } from "@/lib/hooks/usePostConfig";
import type { CustomPostConfig } from "@/types/api";

interface SaveTemplateDialogProps {
  config: CustomPostConfig;
}

export function SaveTemplateDialog({ config }: SaveTemplateDialogProps) {
  const [isOpen, setIsOpen] = useState(false);
  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [isPublic, setIsPublic] = useState(false);
  const [tagInput, setTagInput] = useState("");
  const [tags, setTags] = useState<string[]>([]);

  const createMutation = useCreatePostConfigTemplate();

  const handleAddTag = () => {
    const trimmedTag = tagInput.trim();
    if (trimmedTag && !tags.includes(trimmedTag)) {
      setTags([...tags, trimmedTag]);
      setTagInput("");
    }
  };

  const handleRemoveTag = (tagToRemove: string) => {
    setTags(tags.filter((tag) => tag !== tagToRemove));
  };

  const handleKeyDown = (e: React.KeyboardEvent<HTMLInputElement>) => {
    if (e.key === "Enter") {
      e.preventDefault();
      handleAddTag();
    }
  };

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();

    if (!name.trim()) {
      return;
    }

    try {
      await createMutation.mutateAsync({
        name: name.trim(),
        description: description.trim(),
        config,
        isPublic,
        tags,
      });

      // Reset form and close dialog
      setName("");
      setDescription("");
      setIsPublic(false);
      setTags([]);
      setTagInput("");
      setIsOpen(false);
    } catch (error) {
      console.error("Failed to create template:", error);
    }
  };

  const handleClose = () => {
    if (!createMutation.isPending) {
      // Reset form when closing
      setName("");
      setDescription("");
      setIsPublic(false);
      setTags([]);
      setTagInput("");
      setIsOpen(false);
    }
  };

  if (!isOpen) {
    return (
      <Button variant="outline" size="sm" onClick={() => setIsOpen(true)}>
        <Save className="h-4 w-4 mr-2" />
        Save as Template
      </Button>
    );
  }

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center">
      {/* Backdrop */}
      <div className="absolute inset-0 bg-black/50" onClick={handleClose} />

      {/* Dialog Card */}
      <Card className="relative z-10 w-full max-w-[525px] mx-4 max-h-[90vh] overflow-y-auto">
        <CardHeader>
          <div className="flex items-center justify-between">
            <CardTitle className="flex items-center gap-2">
              <Save className="h-5 w-5" />
              Save as Template
            </CardTitle>
            <Button
              variant="ghost"
              size="sm"
              onClick={handleClose}
              className="h-8 w-8 p-0"
              disabled={createMutation.isPending}
            >
              <X className="h-4 w-4" />
            </Button>
          </div>
          <p className="text-sm text-muted-foreground mt-2">
            Create a reusable template from your current post-configuration.
            You can apply this template to future clusters.
          </p>
        </CardHeader>

        <CardContent>
          <form onSubmit={handleSubmit} className="space-y-4">
            {/* Name */}
            <div className="space-y-2">
              <Label htmlFor="template-name">
                Name <span className="text-red-500">*</span>
              </Label>
              <Input
                id="template-name"
                placeholder="Production Monitoring Stack"
                value={name}
                onChange={(e) => setName(e.target.value)}
                required
                minLength={3}
                maxLength={100}
                disabled={createMutation.isPending}
              />
            </div>

            {/* Description */}
            <div className="space-y-2">
              <Label htmlFor="template-description">Description</Label>
              <Textarea
                id="template-description"
                placeholder="Prometheus + Grafana + AlertManager with custom dashboards"
                value={description}
                onChange={(e) => setDescription(e.target.value)}
                maxLength={500}
                rows={3}
                disabled={createMutation.isPending}
              />
              <p className="text-xs text-muted-foreground">
                {description.length}/500 characters
              </p>
            </div>

            {/* Tags */}
            <div className="space-y-2">
              <Label htmlFor="template-tags">Tags</Label>
              <div className="flex gap-2">
                <Input
                  id="template-tags"
                  placeholder="monitoring, production, observability"
                  value={tagInput}
                  onChange={(e) => setTagInput(e.target.value)}
                  onKeyDown={handleKeyDown}
                  disabled={createMutation.isPending}
                />
                <Button
                  type="button"
                  variant="secondary"
                  onClick={handleAddTag}
                  disabled={!tagInput.trim() || createMutation.isPending}
                >
                  Add
                </Button>
              </div>
              {tags.length > 0 && (
                <div className="flex flex-wrap gap-2 mt-2">
                  {tags.map((tag) => (
                    <Badge
                      key={tag}
                      variant="secondary"
                      className="gap-1 pr-1"
                    >
                      {tag}
                      <button
                        type="button"
                        onClick={() => handleRemoveTag(tag)}
                        className="ml-1 hover:bg-muted rounded-sm p-0.5"
                        disabled={createMutation.isPending}
                      >
                        <X className="h-3 w-3" />
                      </button>
                    </Badge>
                  ))}
                </div>
              )}
            </div>

            {/* Public checkbox */}
            <div className="flex items-center space-x-2">
              <Checkbox
                id="template-public"
                checked={isPublic}
                onCheckedChange={(checked) => setIsPublic(checked as boolean)}
                disabled={createMutation.isPending}
              />
              <Label
                htmlFor="template-public"
                className="text-sm font-normal cursor-pointer"
              >
                Make this template public (visible to all users)
              </Label>
            </div>

            {/* Error message */}
            {createMutation.isError && (
              <div className="text-sm text-red-600 dark:text-red-400">
                Failed to create template. Please try again.
              </div>
            )}

            {/* Success message */}
            {createMutation.isSuccess && (
              <div className="text-sm text-green-600 dark:text-green-400">
                Template created successfully!
              </div>
            )}

            {/* Action Buttons */}
            <div className="flex gap-2 justify-end pt-4">
              <Button
                type="button"
                variant="outline"
                onClick={handleClose}
                disabled={createMutation.isPending}
              >
                Cancel
              </Button>
              <Button
                type="submit"
                disabled={createMutation.isPending || !name.trim()}
              >
                {createMutation.isPending ? (
                  <>
                    <Loader2 className="h-4 w-4 mr-2 animate-spin" />
                    Saving...
                  </>
                ) : (
                  <>
                    <Save className="h-4 w-4 mr-2" />
                    Save Template
                  </>
                )}
              </Button>
            </div>
          </form>
        </CardContent>
      </Card>
    </div>
  );
}
