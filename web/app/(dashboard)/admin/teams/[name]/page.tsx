"use client";

import { useState, useEffect } from "react";
import { useParams, useRouter } from "next/navigation";
import { useForm } from "react-hook-form";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { adminApi } from "@/lib/api/endpoints/admin";
import { useUsers } from "@/lib/hooks/useUsers";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";
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
import { ArrowLeft, Save, UserPlus, Trash2, AlertCircle, CheckCircle } from "lucide-react";
import type { UpdateTeamRequest, GrantTeamAdminRequest, User, UserRole } from "@/types/api";

export default function TeamDetailsPage() {
  const params = useParams();
  const router = useRouter();
  const queryClient = useQueryClient();
  const teamName = decodeURIComponent(params.name as string);

  const [updateSuccess, setUpdateSuccess] = useState("");
  const [updateError, setUpdateError] = useState("");
  const [grantSuccess, setGrantSuccess] = useState("");
  const [grantError, setGrantError] = useState("");
  const [selectedUserId, setSelectedUserId] = useState("");
  const [notes, setNotes] = useState("");
  const [memberSuccess, setMemberSuccess] = useState("");
  const [memberError, setMemberError] = useState("");
  const [selectedMemberUserId, setSelectedMemberUserId] = useState("");

  const { data: team, isLoading: teamLoading } = useQuery({
    queryKey: ["team", teamName],
    queryFn: () => adminApi.getTeam(teamName),
  });

  const { data: adminsData, isLoading: adminsLoading } = useQuery({
    queryKey: ["team-admins", teamName],
    queryFn: () => adminApi.listTeamAdmins(teamName),
  });

  const { data: membersData, isLoading: membersLoading } = useQuery({
    queryKey: ["team-members", teamName],
    queryFn: () => adminApi.listTeamMembers(teamName),
  });

  const { data: usersData } = useUsers();

  const {
    register,
    handleSubmit,
    reset,
    formState: { errors, isDirty },
  } = useForm<UpdateTeamRequest>();

  useEffect(() => {
    if (team) {
      reset({
        description: team.description || "",
      });
    }
  }, [team, reset]);

  const updateMutation = useMutation({
    mutationFn: (data: UpdateTeamRequest) => adminApi.updateTeam(teamName, data),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["team", teamName] });
      queryClient.invalidateQueries({ queryKey: ["teams"] });
      setUpdateSuccess("Team updated successfully!");
      setUpdateError("");
      setTimeout(() => setUpdateSuccess(""), 3000);
    },
    onError: (error: any) => {
      setUpdateError(error.message || "Failed to update team");
      setUpdateSuccess("");
    },
  });

  const grantMutation = useMutation({
    mutationFn: (data: GrantTeamAdminRequest) => adminApi.grantTeamAdmin(teamName, data),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["team-admins", teamName] });
      setGrantSuccess("Team admin privilege granted successfully!");
      setGrantError("");
      setSelectedUserId("");
      setNotes("");
      setTimeout(() => setGrantSuccess(""), 3000);
    },
    onError: (error: any) => {
      setGrantError(error.message || "Failed to grant privilege");
      setGrantSuccess("");
    },
  });

  const revokeMutation = useMutation({
    mutationFn: (userId: string) => adminApi.revokeTeamAdmin(teamName, userId),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["team-admins", teamName] });
    },
    onError: (error: any) => {
      alert(`Failed to revoke privilege: ${error.message || 'Unknown error'}`);
    },
  });

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

  const onUpdateSubmit = async (data: UpdateTeamRequest) => {
    setUpdateSuccess("");
    setUpdateError("");
    updateMutation.mutate(data);
  };

  const handleGrantAccess = () => {
    if (!selectedUserId) {
      setGrantError("Please select a user");
      return;
    }
    setGrantSuccess("");
    setGrantError("");
    grantMutation.mutate({ user_id: selectedUserId, notes: notes || undefined });
  };

  const handleRevoke = (userId: string, userEmail: string) => {
    if (!confirm(`Are you sure you want to revoke team admin privileges for ${userEmail}?`)) {
      return;
    }
    revokeMutation.mutate(userId);
  };

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

  const admins = adminsData?.admins || [];
  const adminUserIds = new Set(admins.map(a => a.user_id));

  const members = membersData?.members || [];
  const memberUserIds = new Set(members.map(m => m.user_id));

  // Filter users to only show TEAM_ADMIN role users who are not already admins of this team
  const eligibleUsers = (usersData?.users || []).filter(
    (user: User) => user.role === "TEAM_ADMIN" && !adminUserIds.has(user.id)
  );

  // Filter users who are not already members of this team
  const eligibleMembers = (usersData?.users || []).filter(
    (user: User) => !memberUserIds.has(user.id)
  );

  return (
    <div className="space-y-6">
      <div className="flex items-center gap-4">
        <Button variant="ghost" size="sm" onClick={() => router.push("/admin/teams")}>
          <ArrowLeft className="mr-2 h-4 w-4" />
          Back to Teams
        </Button>
        <div>
          <h1 className="text-3xl font-bold">{team.name}</h1>
          <p className="text-muted-foreground">
            Manage team details and administrators
          </p>
        </div>
      </div>

      {/* Team Details */}
      <form onSubmit={handleSubmit(onUpdateSubmit)}>
        <Card>
          <CardHeader>
            <CardTitle>Team Details</CardTitle>
          </CardHeader>
          <CardContent className="space-y-4">
            <div className="space-y-2">
              <Label>Team Name</Label>
              <Input
                value={team.name}
                disabled
                className="bg-muted"
              />
              <p className="text-sm text-muted-foreground">
                Team name cannot be changed
              </p>
            </div>

            <div className="space-y-2">
              <Label htmlFor="description">Description</Label>
              <Textarea
                id="description"
                placeholder="Enter a description for this team..."
                rows={3}
                {...register("description")}
              />
            </div>

            <div className="space-y-2">
              <Label>Created</Label>
              <p className="text-sm">{formatDate(team.created_at)}</p>
            </div>

            {updateSuccess && (
              <div className="bg-green-50 border border-green-200 rounded-md p-3 flex items-center gap-2">
                <CheckCircle className="h-5 w-5 text-green-600" />
                <p className="text-sm text-green-800">{updateSuccess}</p>
              </div>
            )}

            {updateError && (
              <div className="bg-red-50 border border-red-200 rounded-md p-3 flex items-center gap-2">
                <AlertCircle className="h-5 w-5 text-red-600" />
                <p className="text-sm text-red-800">{updateError}</p>
              </div>
            )}

            <Button
              type="submit"
              disabled={!isDirty || updateMutation.isPending}
            >
              <Save className="mr-2 h-4 w-4" />
              {updateMutation.isPending ? "Saving..." : "Save Changes"}
            </Button>
          </CardContent>
        </Card>
      </form>

      {/* Grant Team Admin Access */}
      <Card>
        <CardHeader>
          <CardTitle>Grant Team Admin Access</CardTitle>
          <CardDescription>
            Add users who can manage all clusters in this team
          </CardDescription>
        </CardHeader>
        <CardContent className="space-y-4">
          <div className="space-y-2">
            <Label htmlFor="user">Select User</Label>
            <Select value={selectedUserId} onValueChange={setSelectedUserId}>
              <SelectTrigger>
                <SelectValue placeholder="Choose a user with TEAM_ADMIN role..." />
              </SelectTrigger>
              <SelectContent>
                {eligibleUsers.length === 0 ? (
                  <div className="p-2 text-sm text-muted-foreground">
                    No eligible users available. Users must have TEAM_ADMIN role.
                  </div>
                ) : (
                  eligibleUsers.map((user: User) => (
                    <SelectItem key={user.id} value={user.id}>
                      {user.email} ({user.username})
                    </SelectItem>
                  ))
                )}
              </SelectContent>
            </Select>
            <p className="text-sm text-muted-foreground">
              Only users with TEAM_ADMIN role can be granted team admin privileges
            </p>
          </div>

          <div className="space-y-2">
            <Label htmlFor="notes">Notes (Optional)</Label>
            <Textarea
              id="notes"
              placeholder="e.g., Engineering team lead, temporary assignment..."
              rows={2}
              value={notes}
              onChange={(e) => setNotes(e.target.value)}
            />
          </div>

          {grantSuccess && (
            <div className="bg-green-50 border border-green-200 rounded-md p-3 flex items-center gap-2">
              <CheckCircle className="h-5 w-5 text-green-600" />
              <p className="text-sm text-green-800">{grantSuccess}</p>
            </div>
          )}

          {grantError && (
            <div className="bg-red-50 border border-red-200 rounded-md p-3 flex items-center gap-2">
              <AlertCircle className="h-5 w-5 text-red-600" />
              <p className="text-sm text-red-800">{grantError}</p>
            </div>
          )}

          <Button
            onClick={handleGrantAccess}
            disabled={!selectedUserId || grantMutation.isPending}
          >
            <UserPlus className="mr-2 h-4 w-4" />
            {grantMutation.isPending ? "Granting..." : "Grant Access"}
          </Button>
        </CardContent>
      </Card>

      {/* Team Admins List */}
      <Card>
        <CardHeader>
          <CardTitle>Team Administrators</CardTitle>
          <CardDescription>
            Users who can manage all clusters in the {team.name} team
          </CardDescription>
        </CardHeader>
        <CardContent>
          {adminsLoading ? (
            <p className="text-muted-foreground">Loading administrators...</p>
          ) : admins.length === 0 ? (
            <div className="text-center py-8">
              <p className="text-muted-foreground">No administrators assigned yet</p>
              <p className="text-sm text-muted-foreground mt-1">
                Grant team admin access to users above
              </p>
            </div>
          ) : (
            <div className="rounded-lg border">
              <table className="w-full">
                <thead>
                  <tr className="border-b bg-muted/50">
                    <th className="p-3 text-left text-sm font-medium">User</th>
                    <th className="p-3 text-left text-sm font-medium">Email</th>
                    <th className="p-3 text-left text-sm font-medium">Granted</th>
                    <th className="p-3 text-left text-sm font-medium">Notes</th>
                    <th className="p-3 text-left text-sm font-medium">Actions</th>
                  </tr>
                </thead>
                <tbody>
                  {admins.map((admin) => (
                    <tr key={admin.user_id} className="border-b last:border-0">
                      <td className="p-3">{admin.username}</td>
                      <td className="p-3 text-sm text-muted-foreground">{admin.email}</td>
                      <td className="p-3 text-sm">{formatDate(admin.granted_at)}</td>
                      <td className="p-3 text-sm text-muted-foreground max-w-xs truncate">
                        {admin.notes || "-"}
                      </td>
                      <td className="p-3">
                        <Button
                          variant="outline"
                          size="sm"
                          onClick={() => handleRevoke(admin.user_id, admin.email)}
                          disabled={revokeMutation.isPending}
                        >
                          <Trash2 className="h-4 w-4 text-red-600" />
                        </Button>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </CardContent>
      </Card>

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
                    <th className="p-3 text-left text-sm font-medium">Added</th>
                    <th className="p-3 text-left text-sm font-medium">Actions</th>
                  </tr>
                </thead>
                <tbody>
                  {members.map((member) => (
                    <tr key={member.user_id} className="border-b last:border-0">
                      <td className="p-3">
                        {usersData?.users?.find((u: User) => u.id === member.user_id)?.username || "Unknown"}
                      </td>
                      <td className="p-3 text-sm text-muted-foreground">
                        {usersData?.users?.find((u: User) => u.id === member.user_id)?.email || "Unknown"}
                      </td>
                      <td className="p-3 text-sm">{formatDate(member.added_at)}</td>
                      <td className="p-3">
                        <Button
                          variant="outline"
                          size="sm"
                          onClick={() => {
                            const user = usersData?.users?.find((u: User) => u.id === member.user_id);
                            handleRemoveMember(member.user_id, user?.email || "Unknown");
                          }}
                          disabled={removeMemberMutation.isPending}
                        >
                          <Trash2 className="h-4 w-4 text-red-600" />
                        </Button>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </CardContent>
      </Card>
    </div>
  );
}
