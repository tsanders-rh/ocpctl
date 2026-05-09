"use client";

import { useState } from "react";
import { useParams, useRouter } from "next/navigation";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { adminApi } from "@/lib/api/endpoints/admin";
import { useUsers } from "@/lib/hooks/useUsers";
import { useAuthStore } from "@/lib/stores/authStore";
import { Button } from "@/components/ui/button";
import { Label } from "@/components/ui/label";
import { Badge } from "@/components/ui/badge";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Card, CardContent, CardHeader, CardTitle, CardDescription } from "@/components/ui/card";
import { formatDate } from "@/lib/utils/formatters";
import { ArrowLeft, UserPlus, Trash2, AlertCircle, CheckCircle } from "lucide-react";
import type { User } from "@/types/api";

export default function TeamDetailPage() {
  const params = useParams();
  const router = useRouter();
  const queryClient = useQueryClient();
  const { user: currentUser } = useAuthStore();
  const teamName = decodeURIComponent(params.name as string);

  const [memberSuccess, setMemberSuccess] = useState("");
  const [memberError, setMemberError] = useState("");
  const [selectedMemberUserId, setSelectedMemberUserId] = useState("");

  // Check if current user is a team admin for this team
  const isTeamAdmin = currentUser?.managed_teams?.includes(teamName);

  const { data: team, isLoading: teamLoading } = useQuery({
    queryKey: ["team", teamName],
    queryFn: () => adminApi.getTeam(teamName),
  });

  const { data: membersData, isLoading: membersLoading } = useQuery({
    queryKey: ["team-members", teamName],
    queryFn: () => adminApi.listTeamMembers(teamName),
  });

  const { data: usersData } = useUsers();

  const addMemberMutation = useMutation({
    mutationFn: (userId: string) => adminApi.addUserToTeam(teamName, { user_id: userId }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["team-members", teamName] });
      queryClient.invalidateQueries({ queryKey: ["users"] });
      setMemberSuccess("Team member added successfully!");
      setMemberError("");
      setSelectedMemberUserId("");
      setTimeout(() => setMemberSuccess(""), 3000);
    },
    onError: (error: any) => {
      setMemberError(error.message || "Failed to add member");
      setMemberSuccess("");
    },
  });

  const removeMemberMutation = useMutation({
    mutationFn: (userId: string) => adminApi.removeUserFromTeam(teamName, userId),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["team-members", teamName] });
      queryClient.invalidateQueries({ queryKey: ["users"] });
    },
    onError: (error: any) => {
      alert(`Failed to remove member: ${error.message || 'Unknown error'}`);
    },
  });

  const handleAddMember = () => {
    if (!selectedMemberUserId) {
      setMemberError("Please select a user");
      return;
    }
    setMemberSuccess("");
    setMemberError("");
    addMemberMutation.mutate(selectedMemberUserId);
  };

  const handleRemoveMember = (userId: string, userEmail: string) => {
    // Check if this is the user's last team (admins don't need any teams)
    const user = usersData?.users?.find((u: User) => u.id === userId);
    const userTeams = user?.teams || [];

    // Only prevent removal if user is not admin and this is their last team
    if (user?.role !== "ADMIN" && userTeams.length <= 1) {
      alert(`Cannot remove ${userEmail} from this team. Non-admin users must belong to at least one team. This is their only team.`);
      return;
    }

    if (!confirm(`Are you sure you want to remove ${userEmail} from this team?`)) {
      return;
    }
    removeMemberMutation.mutate(userId);
  };

  if (teamLoading) {
    return <div className="p-8">Loading team...</div>;
  }

  if (!team) {
    return <div className="p-8">Team not found</div>;
  }

  if (!isTeamAdmin) {
    return (
      <div className="p-8">
        <div className="text-center">
          <h1 className="text-2xl font-bold text-red-600">Access Denied</h1>
          <p className="mt-2 text-muted-foreground">
            You don't have admin privileges for this team.
          </p>
          <Button className="mt-4" onClick={() => router.push("/teams")}>
            Back to My Teams
          </Button>
        </div>
      </div>
    );
  }

  const members = membersData?.members || [];
  const memberUserIds = new Set(members.map(m => m.user_id));

  // Filter users who are not already members of this team
  const eligibleMembers = (usersData?.users || []).filter(
    (user: User) => !memberUserIds.has(user.id)
  );

  return (
    <div className="space-y-6">
      <div className="flex items-center gap-4">
        <Button variant="ghost" size="sm" onClick={() => router.push("/teams")}>
          <ArrowLeft className="mr-2 h-4 w-4" />
          Back to My Teams
        </Button>
        <div>
          <h1 className="text-3xl font-bold">{team.name}</h1>
          {team.description && (
            <p className="text-muted-foreground">{team.description}</p>
          )}
        </div>
      </div>

      {/* Team Members Section */}
      <Card>
        <CardHeader>
          <CardTitle>Team Members</CardTitle>
          <CardDescription>
            Add or remove users who belong to this team
          </CardDescription>
        </CardHeader>
        <CardContent className="space-y-4">
          <div className="space-y-2">
            <Label htmlFor="member-user">Add Member</Label>
            <div className="flex gap-2">
              <Select value={selectedMemberUserId} onValueChange={setSelectedMemberUserId}>
                <SelectTrigger className="flex-1">
                  <SelectValue placeholder="Choose a user to add..." />
                </SelectTrigger>
                <SelectContent>
                  {eligibleMembers.length === 0 ? (
                    <div className="p-2 text-sm text-muted-foreground">
                      No users available to add
                    </div>
                  ) : (
                    eligibleMembers.map((user: User) => (
                      <SelectItem key={user.id} value={user.id}>
                        {user.email} ({user.username})
                      </SelectItem>
                    ))
                  )}
                </SelectContent>
              </Select>
              <Button
                onClick={handleAddMember}
                disabled={!selectedMemberUserId || addMemberMutation.isPending}
              >
                <UserPlus className="mr-2 h-4 w-4" />
                {addMemberMutation.isPending ? "Adding..." : "Add"}
              </Button>
            </div>
            <p className="text-sm text-muted-foreground">
              Team members can create clusters for this team
            </p>
          </div>

          {memberSuccess && (
            <div className="bg-green-50 border border-green-200 rounded-md p-3 flex items-center gap-2">
              <CheckCircle className="h-5 w-5 text-green-600" />
              <p className="text-sm text-green-800">{memberSuccess}</p>
            </div>
          )}

          {memberError && (
            <div className="bg-red-50 border border-red-200 rounded-md p-3 flex items-center gap-2">
              <AlertCircle className="h-5 w-5 text-red-600" />
              <p className="text-sm text-red-800">{memberError}</p>
            </div>
          )}

          {membersLoading ? (
            <p className="text-muted-foreground">Loading members...</p>
          ) : members.length === 0 ? (
            <div className="text-center py-8 border rounded-lg">
              <p className="text-muted-foreground">No members yet</p>
              <p className="text-sm text-muted-foreground mt-1">
                Add users to this team above
              </p>
            </div>
          ) : (
            <div className="rounded-lg border">
              <table className="w-full">
                <thead>
                  <tr className="border-b bg-muted/50">
                    <th className="p-3 text-left text-sm font-medium">User</th>
                    <th className="p-3 text-left text-sm font-medium">Email</th>
                    <th className="p-3 text-left text-sm font-medium">Role</th>
                    <th className="p-3 text-left text-sm font-medium">Added</th>
                    <th className="p-3 text-left text-sm font-medium">Actions</th>
                  </tr>
                </thead>
                <tbody>
                  {members.map((member) => {
                    const user = usersData?.users?.find((u: User) => u.id === member.user_id);
                    return (
                      <tr key={member.user_id} className="border-b last:border-0">
                        <td className="p-3">{user?.username || "Unknown"}</td>
                        <td className="p-3 text-sm text-muted-foreground">
                          {user?.email || "Unknown"}
                        </td>
                        <td className="p-3">
                          <Badge variant={user?.role === "ADMIN" ? "destructive" : "default"}>
                            {user?.role || "Unknown"}
                          </Badge>
                        </td>
                        <td className="p-3 text-sm">{formatDate(member.added_at)}</td>
                        <td className="p-3">
                          <Button
                            variant="outline"
                            size="sm"
                            onClick={() => handleRemoveMember(member.user_id, user?.email || "Unknown")}
                            disabled={removeMemberMutation.isPending}
                          >
                            <Trash2 className="h-4 w-4 text-red-600" />
                          </Button>
                        </td>
                      </tr>
                    );
                  })}
                </tbody>
              </table>
            </div>
          )}
        </CardContent>
      </Card>
    </div>
  );
}
