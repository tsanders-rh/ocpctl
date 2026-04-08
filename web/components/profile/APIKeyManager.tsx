"use client";

import { useState } from "react";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { apiKeysApi } from "@/lib/api/endpoints/api-keys";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "@/components/ui/dialog";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { Badge } from "@/components/ui/badge";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog";
import {
  Key,
  Plus,
  Trash2,
  Copy,
  CheckCircle2,
  XCircle,
  Clock,
  Shield,
  Eye,
  EyeOff,
  AlertTriangle,
  Edit,
} from "lucide-react";
import { APIKeyScope, type CreateAPIKeyRequest } from "@/types/api";
import { formatDate } from "@/lib/utils/formatters";

export function APIKeyManager() {
  const queryClient = useQueryClient();
  const [createDialogOpen, setCreateDialogOpen] = useState(false);
  const [deleteKeyId, setDeleteKeyId] = useState<string | null>(null);
  const [revokeKeyId, setRevokeKeyId] = useState<string | null>(null);
  const [editKeyId, setEditKeyId] = useState<string | null>(null);
  const [editKeyName, setEditKeyName] = useState("");
  const [newKeyName, setNewKeyName] = useState("");
  const [newKeyScope, setNewKeyScope] = useState<APIKeyScope>(APIKeyScope.FULL_ACCESS);
  const [newlyCreatedKey, setNewlyCreatedKey] = useState<string | null>(null);
  const [copiedKeyId, setCopiedKeyId] = useState<string | null>(null);
  const [showPlainKey, setShowPlainKey] = useState(false);

  // Fetch API keys
  const { data: apiKeys = [], isLoading, error } = useQuery({
    queryKey: ["api-keys"],
    queryFn: apiKeysApi.list,
  });

  // Create API key mutation
  const createMutation = useMutation({
    mutationFn: (data: CreateAPIKeyRequest) => apiKeysApi.create(data),
    onSuccess: (response) => {
      queryClient.invalidateQueries({ queryKey: ["api-keys"] });
      setNewlyCreatedKey(response.plain_key);
      setNewKeyName("");
      setNewKeyScope(APIKeyScope.FULL_ACCESS);
    },
  });

  // Update API key mutation
  const updateMutation = useMutation({
    mutationFn: ({ id, name }: { id: string; name: string }) =>
      apiKeysApi.update(id, { name }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["api-keys"] });
      setEditKeyId(null);
      setEditKeyName("");
    },
  });

  // Revoke API key mutation
  const revokeMutation = useMutation({
    mutationFn: (id: string) => apiKeysApi.revoke(id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["api-keys"] });
      setRevokeKeyId(null);
    },
  });

  // Delete API key mutation
  const deleteMutation = useMutation({
    mutationFn: (id: string) => apiKeysApi.delete(id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["api-keys"] });
      setDeleteKeyId(null);
    },
  });

  const handleCreate = () => {
    if (!newKeyName.trim()) return;

    createMutation.mutate({
      name: newKeyName.trim(),
      scope: newKeyScope,
    });
  };

  const handleUpdate = (id: string) => {
    if (!editKeyName.trim()) return;
    updateMutation.mutate({ id, name: editKeyName.trim() });
  };

  const copyToClipboard = (text: string, keyId?: string) => {
    navigator.clipboard.writeText(text);
    if (keyId) {
      setCopiedKeyId(keyId);
      setTimeout(() => setCopiedKeyId(null), 2000);
    }
  };

  const closeCreateDialog = () => {
    if (newlyCreatedKey) {
      setNewlyCreatedKey(null);
      setShowPlainKey(false);
    }
    setCreateDialogOpen(false);
  };

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <div>
          <h3 className="text-lg font-semibold flex items-center gap-2">
            <Key className="h-5 w-5" />
            API Keys
          </h3>
          <p className="text-sm text-muted-foreground">
            Generate API keys for programmatic access to your clusters
          </p>
        </div>
        <Dialog open={createDialogOpen} onOpenChange={setCreateDialogOpen}>
          <DialogTrigger asChild>
            <Button>
              <Plus className="mr-2 h-4 w-4" />
              Create API Key
            </Button>
          </DialogTrigger>
          <DialogContent className="max-w-lg">
            {newlyCreatedKey ? (
              <>
                <DialogHeader>
                  <DialogTitle className="flex items-center gap-2 text-green-600">
                    <CheckCircle2 className="h-5 w-5" />
                    API Key Created Successfully
                  </DialogTitle>
                  <DialogDescription>
                    Copy your API key now. You won't be able to see it again!
                  </DialogDescription>
                </DialogHeader>
                <div className="space-y-4">
                  <div className="bg-yellow-50 border border-yellow-200 rounded-md p-4">
                    <div className="flex items-start gap-2">
                      <AlertTriangle className="h-5 w-5 text-yellow-600 flex-shrink-0 mt-0.5" />
                      <div className="text-sm text-yellow-800">
                        <p className="font-semibold mb-1">Save this key securely</p>
                        <p>This is the only time you'll see the full API key. Store it in a safe place.</p>
                      </div>
                    </div>
                  </div>
                  <div className="space-y-2">
                    <Label>Your API Key</Label>
                    <div className="flex gap-2">
                      <Input
                        value={newlyCreatedKey}
                        readOnly
                        type={showPlainKey ? "text" : "password"}
                        className="font-mono text-sm"
                      />
                      <Button
                        variant="outline"
                        size="icon"
                        onClick={() => setShowPlainKey(!showPlainKey)}
                      >
                        {showPlainKey ? (
                          <EyeOff className="h-4 w-4" />
                        ) : (
                          <Eye className="h-4 w-4" />
                        )}
                      </Button>
                      <Button
                        variant="outline"
                        size="icon"
                        onClick={() => copyToClipboard(newlyCreatedKey)}
                      >
                        <Copy className="h-4 w-4" />
                      </Button>
                    </div>
                  </div>
                  <div className="bg-muted rounded-md p-3 text-sm">
                    <p className="font-semibold mb-1">Usage Example:</p>
                    <code className="text-xs">
                      curl -H "Authorization: Bearer {newlyCreatedKey}" https://api.example.com/v1/clusters
                    </code>
                  </div>
                </div>
                <DialogFooter>
                  <Button onClick={closeCreateDialog}>
                    Done
                  </Button>
                </DialogFooter>
              </>
            ) : (
              <>
                <DialogHeader>
                  <DialogTitle>Create New API Key</DialogTitle>
                  <DialogDescription>
                    Generate a new API key for programmatic access
                  </DialogDescription>
                </DialogHeader>
                <div className="space-y-4">
                  <div className="space-y-2">
                    <Label htmlFor="key-name">Name</Label>
                    <Input
                      id="key-name"
                      placeholder="e.g., CI/CD Pipeline, Dev Environment"
                      value={newKeyName}
                      onChange={(e) => setNewKeyName(e.target.value)}
                    />
                  </div>
                  <div className="space-y-2">
                    <Label htmlFor="key-scope">Scope</Label>
                    <Select
                      value={newKeyScope}
                      onValueChange={(value) => setNewKeyScope(value as APIKeyScope)}
                    >
                      <SelectTrigger>
                        <SelectValue />
                      </SelectTrigger>
                      <SelectContent>
                        <SelectItem value={APIKeyScope.FULL_ACCESS}>
                          <div className="flex items-center gap-2">
                            <Shield className="h-4 w-4" />
                            <div>
                              <p className="font-medium">Full Access</p>
                              <p className="text-xs text-muted-foreground">
                                Create, read, update, and delete clusters
                              </p>
                            </div>
                          </div>
                        </SelectItem>
                        <SelectItem value={APIKeyScope.READ_ONLY}>
                          <div className="flex items-center gap-2">
                            <Eye className="h-4 w-4" />
                            <div>
                              <p className="font-medium">Read Only</p>
                              <p className="text-xs text-muted-foreground">
                                View clusters and their details only
                              </p>
                            </div>
                          </div>
                        </SelectItem>
                      </SelectContent>
                    </Select>
                  </div>
                </div>
                <DialogFooter>
                  <Button
                    variant="outline"
                    onClick={() => setCreateDialogOpen(false)}
                  >
                    Cancel
                  </Button>
                  <Button
                    onClick={handleCreate}
                    disabled={!newKeyName.trim() || createMutation.isPending}
                  >
                    {createMutation.isPending ? "Creating..." : "Create API Key"}
                  </Button>
                </DialogFooter>
              </>
            )}
          </DialogContent>
        </Dialog>
      </div>

      {isLoading ? (
        <div className="text-center py-8 text-muted-foreground">Loading API keys...</div>
      ) : error ? (
        <div className="text-center py-8 text-red-600">
          Failed to load API keys: {error instanceof Error ? error.message : "Unknown error"}
        </div>
      ) : apiKeys.length === 0 ? (
        <div className="text-center py-12 border-2 border-dashed rounded-lg">
          <Key className="h-12 w-12 text-muted-foreground mx-auto mb-4" />
          <h3 className="text-lg font-semibold mb-2">No API Keys</h3>
          <p className="text-muted-foreground mb-4">
            Create an API key to access your clusters programmatically
          </p>
          <Button onClick={() => setCreateDialogOpen(true)}>
            <Plus className="mr-2 h-4 w-4" />
            Create Your First API Key
          </Button>
        </div>
      ) : (
        <div className="border rounded-lg">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Name</TableHead>
                <TableHead>Key Prefix</TableHead>
                <TableHead>Scope</TableHead>
                <TableHead>Status</TableHead>
                <TableHead>Last Used</TableHead>
                <TableHead>Created</TableHead>
                <TableHead className="text-right">Actions</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {apiKeys.map((key) => (
                <TableRow key={key.id}>
                  <TableCell className="font-medium">{key.name}</TableCell>
                  <TableCell>
                    <div className="flex items-center gap-2">
                      <code className="text-xs bg-muted px-2 py-1 rounded">
                        {key.key_prefix}
                      </code>
                      <Button
                        variant="ghost"
                        size="sm"
                        className="h-6 w-6 p-0"
                        onClick={() => copyToClipboard(key.key_prefix, key.id)}
                      >
                        {copiedKeyId === key.id ? (
                          <CheckCircle2 className="h-3 w-3 text-green-600" />
                        ) : (
                          <Copy className="h-3 w-3" />
                        )}
                      </Button>
                    </div>
                  </TableCell>
                  <TableCell>
                    <Badge variant={key.scope === APIKeyScope.FULL_ACCESS ? "default" : "secondary"}>
                      {key.scope === APIKeyScope.FULL_ACCESS ? "Full Access" : "Read Only"}
                    </Badge>
                  </TableCell>
                  <TableCell>
                    {key.revoked_at ? (
                      <Badge variant="destructive" className="flex items-center gap-1 w-fit">
                        <XCircle className="h-3 w-3" />
                        Revoked
                      </Badge>
                    ) : key.is_expired ? (
                      <Badge variant="destructive" className="flex items-center gap-1 w-fit">
                        <Clock className="h-3 w-3" />
                        Expired
                      </Badge>
                    ) : (
                      <Badge variant="success" className="flex items-center gap-1 w-fit">
                        <CheckCircle2 className="h-3 w-3" />
                        Active
                      </Badge>
                    )}
                  </TableCell>
                  <TableCell className="text-sm text-muted-foreground">
                    {key.last_used_at ? formatDate(key.last_used_at) : "Never"}
                  </TableCell>
                  <TableCell className="text-sm text-muted-foreground">
                    {formatDate(key.created_at)}
                  </TableCell>
                  <TableCell className="text-right">
                    <div className="flex items-center justify-end gap-2">
                      <Dialog
                        open={editKeyId === key.id}
                        onOpenChange={(open) => {
                          if (open) {
                            setEditKeyId(key.id);
                            setEditKeyName(key.name);
                          } else {
                            setEditKeyId(null);
                            setEditKeyName("");
                          }
                        }}
                      >
                        <DialogTrigger asChild>
                          <Button variant="ghost" size="sm">
                            <Edit className="h-4 w-4" />
                          </Button>
                        </DialogTrigger>
                        <DialogContent>
                          <DialogHeader>
                            <DialogTitle>Edit API Key Name</DialogTitle>
                            <DialogDescription>
                              Update the name for this API key
                            </DialogDescription>
                          </DialogHeader>
                          <div className="space-y-4">
                            <div className="space-y-2">
                              <Label htmlFor="edit-name">Name</Label>
                              <Input
                                id="edit-name"
                                value={editKeyName}
                                onChange={(e) => setEditKeyName(e.target.value)}
                              />
                            </div>
                          </div>
                          <DialogFooter>
                            <Button
                              variant="outline"
                              onClick={() => setEditKeyId(null)}
                            >
                              Cancel
                            </Button>
                            <Button
                              onClick={() => handleUpdate(key.id)}
                              disabled={!editKeyName.trim() || updateMutation.isPending}
                            >
                              {updateMutation.isPending ? "Saving..." : "Save"}
                            </Button>
                          </DialogFooter>
                        </DialogContent>
                      </Dialog>
                      {key.is_active && !key.revoked_at && (
                        <Button
                          variant="ghost"
                          size="sm"
                          onClick={() => setRevokeKeyId(key.id)}
                        >
                          <XCircle className="h-4 w-4 text-orange-600" />
                        </Button>
                      )}
                      <Button
                        variant="ghost"
                        size="sm"
                        onClick={() => setDeleteKeyId(key.id)}
                      >
                        <Trash2 className="h-4 w-4 text-red-600" />
                      </Button>
                    </div>
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
      )}

      {/* Revoke Confirmation Dialog */}
      <AlertDialog open={!!revokeKeyId} onOpenChange={() => setRevokeKeyId(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Revoke API Key</AlertDialogTitle>
            <AlertDialogDescription>
              Are you sure you want to revoke this API key? This will prevent it from being used for authentication. You can delete it permanently later if needed.
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>Cancel</AlertDialogCancel>
            <AlertDialogAction
              onClick={() => revokeKeyId && revokeMutation.mutate(revokeKeyId)}
              className="bg-orange-600 hover:bg-orange-700"
            >
              Revoke Key
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>

      {/* Delete Confirmation Dialog */}
      <AlertDialog open={!!deleteKeyId} onOpenChange={() => setDeleteKeyId(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Delete API Key</AlertDialogTitle>
            <AlertDialogDescription>
              Are you sure you want to permanently delete this API key? This action cannot be undone.
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>Cancel</AlertDialogCancel>
            <AlertDialogAction
              onClick={() => deleteKeyId && deleteMutation.mutate(deleteKeyId)}
              className="bg-red-600 hover:bg-red-700"
            >
              Delete Key
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}
