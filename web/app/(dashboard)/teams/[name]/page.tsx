"use client";

import { useState, useEffect } from "react";
import { useParams, useRouter } from "next/navigation";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { adminApi } from "@/lib/api/endpoints/admin";
import { profilesApi } from "@/lib/api";
import { useAuthStore } from "@/lib/stores/authStore";
import { Button } from "@/components/ui/button";
import { Label } from "@/components/ui/label";
import { Badge } from "@/components/ui/badge";
import { Checkbox } from "@/components/ui/checkbox";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Card, CardContent, CardHeader, CardTitle, CardDescription } from "@/components/ui/card";
import { formatDate } from "@/lib/utils/formatters";
import { ArrowLeft, UserPlus, Trash2, AlertCircle, CheckCircle, Save } from "lucide-react";
import type { User, Profile } from "@/types/api";

export default function TeamDetailPage() {
  const params = useParams();
  const router = useRouter();
  const queryClient = useQueryClient();
  const { user: currentUser } = useAuthStore();
  const teamName = decodeURIComponent(params.name as string);

  // Refetch user data when page loads to ensure we have fresh managed_teams data
  useEffect(() => {
    queryClient.refetchQueries({ queryKey: ["auth", "me"] });
  }, [queryClient]);

  const [memberSuccess, setMemberSuccess] = useState("");
  const [memberError, setMemberError] = useState("");
  const [selectedMemberUserId, setSelectedMemberUserId] = useState("");
  const [profileSuccess, setProfileSuccess] = useState("");
  const [profileError, setProfileError] = useState("");
  const [selectedProfiles, setSelectedProfiles] = useState<string[]>([]);

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

  const { data: eligibleUsersData } = useQuery({
    queryKey: ["eligible-users", teamName],
    queryFn: () => adminApi.getEligibleUsers(teamName),
  });

  const { data: profilesData } = useQuery({
    queryKey: ["profiles"],
    queryFn: () => profilesApi.list(),
  });

  const { data: allowedProfilesData } = useQuery({
    queryKey: ["allowed-profiles", teamName],
    queryFn: () => adminApi.getAllowedProfiles(teamName),
  });

  // Initialize selected profiles when allowed profiles data loads
  useEffect(() => {
    if (allowedProfilesData?.allowed_profiles) {
      setSelectedProfiles(allowedProfilesData.allowed_profiles);
    }
  }, [allowedProfilesData]);

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

  const updateAllowedProfilesMutation = useMutation({
    mutationFn: (profiles: string[]) => adminApi.updateAllowedProfiles(teamName, { allowed_profiles: profiles }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["allowed-profiles", teamName] });
      queryClient.invalidateQueries({ queryKey: ["team", teamName] });
      setProfileSuccess("Allowed profiles updated successfully!");
      setProfileError("");
      setTimeout(() => setProfileSuccess(""), 3000);
    },
    onError: (error: any) => {
      setProfileError(error.message || "Failed to update allowed profiles");
      setProfileSuccess("");
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
    if (!confirm(`Are you sure you want to remove ${userEmail} from this team?`)) {
      return;
    }
    removeMemberMutation.mutate(userId);
  };

  const handleToggleProfile = (profileName: string) => {
    setSelectedProfiles((prev) => {
      if (prev.includes(profileName)) {
        return prev.filter((p) => p !== profileName);
      } else {
        return [...prev, profileName];
      }
    });
  };

  const handleSaveAllowedProfiles = () => {
    setProfileSuccess("");
    setProfileError("");
    updateAllowedProfilesMutation.mutate(selectedProfiles);
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
  const eligibleMembers = eligibleUsersData?.users || [];

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
                    return (
                      <tr key={member.user_id} className="border-b last:border-0">
                        <td className="p-3">{member.user?.username || "Unknown"}</td>
                        <td className="p-3 text-sm text-muted-foreground">
                          {member.user?.email || "Unknown"}
                        </td>
                        <td className="p-3">
                          <Badge variant={member.user?.role === "ADMIN" ? "destructive" : "default"}>
                            {member.user?.role || "Unknown"}
                          </Badge>
                        </td>
                        <td className="p-3 text-sm">{formatDate(member.added_at)}</td>
                        <td className="p-3">
                          <Button
                            variant="outline"
                            size="sm"
                            onClick={() => handleRemoveMember(member.user_id, member.user?.email || "Unknown")}
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

      {/* Allowed Profiles Section */}
      <Card>
        <CardHeader>
          <CardTitle>Allowed Profiles</CardTitle>
          <CardDescription>
            Control which cluster profiles team members can use. Uncheck all to allow all profiles.
          </CardDescription>
        </CardHeader>
        <CardContent className="space-y-4">
          {profileSuccess && (
            <div className="bg-green-50 border border-green-200 rounded-md p-3 flex items-center gap-2">
              <CheckCircle className="h-5 w-5 text-green-600" />
              <p className="text-sm text-green-800">{profileSuccess}</p>
            </div>
          )}

          {profileError && (
            <div className="bg-red-50 border border-red-200 rounded-md p-3 flex items-center gap-2">
              <AlertCircle className="h-5 w-5 text-red-600" />
              <p className="text-sm text-red-800">{profileError}</p>
            </div>
          )}

          {!profilesData ? (
            <p className="text-muted-foreground">Loading profiles...</p>
          ) : (
            <>
              <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
                {profilesData.map((profile: Profile) => (
                  <div key={profile.name} className="flex items-start space-x-2 border rounded-lg p-3">
                    <Checkbox
                      id={`profile-${profile.name}`}
                      checked={selectedProfiles.includes(profile.name)}
                      onCheckedChange={() => handleToggleProfile(profile.name)}
                    />
                    <div className="flex-1">
                      <label
                        htmlFor={`profile-${profile.name}`}
                        className="text-sm font-medium leading-none cursor-pointer"
                      >
                        {profile.display_name}
                      </label>
                      <p className="text-xs text-muted-foreground mt-1">
                        {profile.description}
                      </p>
                      <div className="flex gap-1 mt-2">
                        <Badge variant="outline" className="text-xs">
                          {profile.platform}
                        </Badge>
                        {profile.track && (
                          <Badge variant="secondary" className="text-xs">
                            {profile.track}
                          </Badge>
                        )}
                      </div>
                    </div>
                  </div>
                ))}
              </div>

              <div className="flex items-center justify-between pt-4 border-t">
                <p className="text-sm text-muted-foreground">
                  {selectedProfiles.length === 0
                    ? "All profiles allowed (no restrictions)"
                    : `${selectedProfiles.length} profile(s) selected`}
                </p>
                <Button
                  onClick={handleSaveAllowedProfiles}
                  disabled={updateAllowedProfilesMutation.isPending}
                >
                  <Save className="mr-2 h-4 w-4" />
                  {updateAllowedProfilesMutation.isPending ? "Saving..." : "Save Changes"}
                </Button>
              </div>
            </>
          )}
        </CardContent>
      </Card>
    </div>
  );
}
