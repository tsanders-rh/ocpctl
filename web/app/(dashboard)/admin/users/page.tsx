"use client";

import { useState } from "react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { useUsers, useDeleteUser } from "@/lib/hooks/useUsers";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { formatDate } from "@/lib/utils/formatters";
import { Plus, Edit, Trash2 } from "lucide-react";
import { UserRole, type User } from "@/types/api";

export default function UsersPage() {
  const router = useRouter();
  const { data, isLoading, error } = useUsers();
  const deleteUser = useDeleteUser();
  const [deletingId, setDeletingId] = useState<string | null>(null);

  const handleDelete = async (user: User) => {
    if (!confirm(`Are you sure you want to delete user "${user.email}"? This action cannot be undone.`)) {
      return;
    }

    setDeletingId(user.id);
    try {
      await deleteUser.mutateAsync(user.id);
    } catch (error) {
      alert(`Failed to delete user: ${error instanceof Error ? error.message : 'Unknown error'}`);
    } finally {
      setDeletingId(null);
    }
  };

  const getRoleBadgeVariant = (role: UserRole): "default" | "destructive" | "outline" | "secondary" => {
    switch (role) {
      case UserRole.ADMIN:
        return "destructive";
      case UserRole.USER:
        return "default";
      case UserRole.VIEWER:
        return "secondary";
      default:
        return "outline";
    }
  };

  if (isLoading) {
    return (
      <div className="flex h-full items-center justify-center">
        <div className="text-lg">Loading users...</div>
      </div>
    );
  }

  if (error) {
    return (
      <div className="flex h-full items-center justify-center">
        <div className="text-lg text-red-600">
          Error loading users: {error instanceof Error ? error.message : 'Unknown error'}
        </div>
      </div>
    );
  }

  const users = data?.users || [];

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-3xl font-bold">User Management</h1>
          <p className="text-muted-foreground">
            Manage system users and their permissions
          </p>
        </div>
        <Link href="/admin/users/new">
          <Button>
            <Plus className="mr-2 h-4 w-4" />
            Create User
          </Button>
        </Link>
      </div>

      <div className="rounded-lg border bg-card">
        <div className="overflow-x-auto">
          <table className="w-full">
            <thead>
              <tr className="border-b bg-muted/50">
                <th className="p-4 text-left text-sm font-medium">Email</th>
                <th className="p-4 text-left text-sm font-medium">Username</th>
                <th className="p-4 text-left text-sm font-medium">Role</th>
                <th className="p-4 text-left text-sm font-medium">Status</th>
                <th className="p-4 text-left text-sm font-medium">Created</th>
                <th className="p-4 text-left text-sm font-medium">Actions</th>
              </tr>
            </thead>
            <tbody>
              {users.length === 0 ? (
                <tr>
                  <td colSpan={6} className="p-8 text-center text-muted-foreground">
                    No users found.
                  </td>
                </tr>
              ) : (
                users.map((user) => (
                  <tr
                    key={user.id}
                    className="border-b last:border-0 hover:bg-muted/50"
                  >
                    <td className="p-4 font-medium">{user.email}</td>
                    <td className="p-4">{user.username}</td>
                    <td className="p-4">
                      <Badge variant={getRoleBadgeVariant(user.role)}>
                        {user.role}
                      </Badge>
                    </td>
                    <td className="p-4">
                      <Badge variant={user.active ? "default" : "secondary"}>
                        {user.active ? "Active" : "Inactive"}
                      </Badge>
                    </td>
                    <td className="p-4 text-sm text-muted-foreground">
                      {formatDate(user.created_at)}
                    </td>
                    <td className="p-4">
                      <div className="flex gap-2">
                        <Button
                          variant="outline"
                          size="sm"
                          onClick={() => router.push(`/admin/users/${user.id}`)}
                        >
                          <Edit className="h-4 w-4" />
                        </Button>
                        <Button
                          variant="outline"
                          size="sm"
                          onClick={() => handleDelete(user)}
                          disabled={deletingId === user.id}
                        >
                          <Trash2 className="h-4 w-4 text-red-600" />
                        </Button>
                      </div>
                    </td>
                  </tr>
                ))
              )}
            </tbody>
          </table>
        </div>
      </div>

      {data && (
        <div className="text-sm text-muted-foreground">
          Total users: {data.total}
        </div>
      )}
    </div>
  );
}
