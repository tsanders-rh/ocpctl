"use client";

import { useState } from "react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { adminApi } from "@/lib/api/endpoints/admin";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { formatDate } from "@/lib/utils/formatters";
import { Plus, Users, Trash2, AlertCircle } from "lucide-react";
import type { TeamWithCount } from "@/types/api";

export default function TeamsPage() {
  const router = useRouter();
  const queryClient = useQueryClient();
  const [deletingTeam, setDeletingTeam] = useState<string | null>(null);

  const { data, isLoading, error } = useQuery({
    queryKey: ["teams"],
    queryFn: () => adminApi.listTeams(),
  });

  const deleteMutation = useMutation({
    mutationFn: (teamName: string) => adminApi.deleteTeam(teamName),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["teams"] });
      setDeletingTeam(null);
    },
    onError: (error: any) => {
      alert(`Failed to delete team: ${error.message || 'Unknown error'}`);
      setDeletingTeam(null);
    },
  });

  const handleDelete = async (team: TeamWithCount) => {
    if (team.cluster_count > 0) {
      alert(`Cannot delete team "${team.name}" because it has ${team.cluster_count} active cluster(s). Please reassign or delete the clusters first.`);
      return;
    }

    if (!confirm(`Are you sure you want to delete team "${team.name}"? This action cannot be undone.`)) {
      return;
    }

    setDeletingTeam(team.name);
    deleteMutation.mutate(team.name);
  };

  if (isLoading) {
    return (
      <div className="flex h-full items-center justify-center">
        <div className="text-lg">Loading teams...</div>
      </div>
    );
  }

  if (error) {
    return (
      <div className="flex h-full items-center justify-center">
        <div className="text-lg text-red-600">
          Error loading teams: {error instanceof Error ? error.message : 'Unknown error'}
        </div>
      </div>
    );
  }

  const teams = data?.teams || [];

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-3xl font-bold flex items-center gap-3">
            Team Management
            <Badge variant="secondary" className="text-base font-normal">
              {teams.length}
            </Badge>
          </h1>
          <p className="text-muted-foreground">
            Manage teams and assign team administrators
          </p>
        </div>
        <Link href="/admin/teams/new">
          <Button>
            <Plus className="mr-2 h-4 w-4" />
            Create Team
          </Button>
        </Link>
      </div>

      <div className="rounded-lg border bg-card">
        <div className="overflow-x-auto">
          <table className="w-full">
            <thead>
              <tr className="border-b bg-muted/50">
                <th className="p-4 text-left text-sm font-medium">Team Name</th>
                <th className="p-4 text-left text-sm font-medium">Description</th>
                <th className="p-4 text-left text-sm font-medium">Clusters</th>
                <th className="p-4 text-left text-sm font-medium">Created</th>
                <th className="p-4 text-left text-sm font-medium">Actions</th>
              </tr>
            </thead>
            <tbody>
              {teams.length === 0 ? (
                <tr>
                  <td colSpan={5} className="p-12 text-center">
                    <div className="flex flex-col items-center gap-4">
                      <Users className="h-12 w-12 text-muted-foreground" />
                      <div>
                        <h3 className="font-semibold text-lg">No teams yet</h3>
                        <p className="text-sm text-muted-foreground mt-1">
                          Get started by creating your first team
                        </p>
                      </div>
                      <Link href="/admin/teams/new">
                        <Button>
                          <Plus className="mr-2 h-4 w-4" />
                          Create Team
                        </Button>
                      </Link>
                    </div>
                  </td>
                </tr>
              ) : (
                teams.map((team) => (
                  <tr
                    key={team.id}
                    className="border-b last:border-0 hover:bg-muted/50"
                  >
                    <td className="p-4">
                      <Link
                        href={`/admin/teams/${encodeURIComponent(team.name)}`}
                        className="font-medium hover:underline"
                      >
                        {team.name}
                      </Link>
                    </td>
                    <td className="p-4 text-sm text-muted-foreground max-w-md truncate">
                      {team.description || "-"}
                    </td>
                    <td className="p-4">
                      <Badge variant={team.cluster_count > 0 ? "default" : "secondary"}>
                        {team.cluster_count} {team.cluster_count === 1 ? "cluster" : "clusters"}
                      </Badge>
                    </td>
                    <td className="p-4 text-sm text-muted-foreground">
                      {formatDate(team.created_at)}
                    </td>
                    <td className="p-4">
                      <div className="flex gap-2">
                        <Link href={`/admin/teams/${encodeURIComponent(team.name)}`}>
                          <Button variant="outline" size="sm">
                            <Users className="mr-2 h-4 w-4" />
                            Manage
                          </Button>
                        </Link>
                        <Button
                          variant="outline"
                          size="sm"
                          onClick={() => handleDelete(team)}
                          disabled={deletingTeam === team.name || team.cluster_count > 0}
                          title={team.cluster_count > 0 ? "Cannot delete team with active clusters" : "Delete team"}
                        >
                          {team.cluster_count > 0 ? (
                            <AlertCircle className="h-4 w-4 text-yellow-600" />
                          ) : (
                            <Trash2 className="h-4 w-4 text-red-600" />
                          )}
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
    </div>
  );
}
