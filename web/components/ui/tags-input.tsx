"use client";

import { useState } from "react";
import { Button } from "./button";
import { Input } from "./input";
import { X, Plus } from "lucide-react";

interface TagsInputProps {
  value?: Record<string, string>;
  onChange: (tags: Record<string, string>) => void;
}

export function TagsInput({ value = {}, onChange }: TagsInputProps) {
  const [newKey, setNewKey] = useState("");
  const [newValue, setNewValue] = useState("");
  const [error, setError] = useState("");

  const tags = Object.entries(value);

  const addTag = () => {
    setError("");

    // Validate key
    if (!newKey.trim()) {
      setError("Tag key is required");
      return;
    }

    if (!/^[a-zA-Z0-9_-]+$/.test(newKey)) {
      setError("Tag key must be alphanumeric with hyphens and underscores");
      return;
    }

    // Validate value
    if (!newValue.trim()) {
      setError("Tag value is required");
      return;
    }

    // Check for duplicates
    if (value[newKey]) {
      setError("Tag key already exists");
      return;
    }

    // Add tag
    onChange({ ...value, [newKey]: newValue });
    setNewKey("");
    setNewValue("");
  };

  const removeTag = (key: string) => {
    const newTags = { ...value };
    delete newTags[key];
    onChange(newTags);
  };

  return (
    <div className="space-y-3">
      {/* Existing Tags */}
      {tags.length > 0 && (
        <div className="space-y-2">
          {tags.map(([key, val]) => (
            <div
              key={key}
              className="flex items-center gap-2 bg-muted p-2 rounded-md"
            >
              <span className="font-mono text-sm flex-1">
                <span className="text-blue-600">{key}</span>
                <span className="text-muted-foreground mx-1">=</span>
                <span className="text-green-600">{val}</span>
              </span>
              <Button
                type="button"
                variant="ghost"
                size="sm"
                onClick={() => removeTag(key)}
                className="h-6 w-6 p-0"
              >
                <X className="h-4 w-4" />
              </Button>
            </div>
          ))}
        </div>
      )}

      {/* Add New Tag */}
      <div className="space-y-2">
        <div className="flex gap-2">
          <Input
            placeholder="Key (e.g., environment)"
            value={newKey}
            onChange={(e) => setNewKey(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Enter") {
                e.preventDefault();
                addTag();
              }
            }}
            className="flex-1"
          />
          <Input
            placeholder="Value (e.g., production)"
            value={newValue}
            onChange={(e) => setNewValue(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Enter") {
                e.preventDefault();
                addTag();
              }
            }}
            className="flex-1"
          />
          <Button
            type="button"
            variant="outline"
            size="sm"
            onClick={addTag}
            className="px-3"
          >
            <Plus className="h-4 w-4" />
          </Button>
        </div>
        {error && <p className="text-sm text-red-600">{error}</p>}
      </div>

      {tags.length === 0 && (
        <p className="text-sm text-muted-foreground">
          No tags added yet.
        </p>
      )}
    </div>
  );
}
