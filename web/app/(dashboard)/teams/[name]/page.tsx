"use client";

import { useState, useEffect, useMemo } from "react";
import { useParams, useRouter } from "next/navigation";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { adminApi } from "@/lib/api/endpoints/admin";
import { profilesApi } from "@/lib/api";
import { useAuthStore } from "@/lib/stores/authStore";
import { useTeamCosts } from "@/lib/hooks/useTeamCosts";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
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
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Card, CardContent, CardHeader, CardTitle, CardDescription } from "@/components/ui/card";
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from "@/components/ui/collapsible";
import { formatDate, formatCurrency } from "@/lib/utils/formatters";
import { ArrowLeft, UserPlus, Trash2, AlertCircle, CheckCircle, Save, Search, ChevronDown, ChevronRight, DollarSign, TrendingUp, Calendar } from "lucide-react";
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

  const [activeTab, setActiveTab] = useState("members");
  const [memberSuccess, setMemberSuccess] = useState("");
  const [memberError, setMemberError] = useState("");
  const [selectedMemberUserId, setSelectedMemberUserId] = useState("");
  const [profileSuccess, setProfileSuccess] = useState("");
  const [profileError, setProfileError] = useState("");
  const [selectedProfiles, setSelectedProfiles] = useState<string[]>([]);
  const [searchQuery, setSearchQuery] = useState("");
  const [openPlatforms, setOpenPlatforms] = useState<Record<string, boolean>>({
    aws: true,
    gcp: true,
    ibmcloud: true,
  });

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

  // Fetch team costs for the Costs tab
  const { data: costsData, isLoading: costsLoading } = useTeamCosts(teamName);

  // Initialize selected profiles when allowed profiles data loads
  useEffect(() => {
    if (allowedProfilesData) {
      // null or undefined = all profiles allowed (empty array in UI)
      // [] or ["profile1", ...] = specific restrictions
      setSelectedProfiles(allowedProfilesData.allowed_profiles || []);
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

  const handleSelectAll = () => {
    if (!profilesData) return;
    setSelectedProfiles(profilesData.map((p: Profile) => p.name));
  };

  const handleDeselectAll = () => {
    setSelectedProfiles([]);
  };

  const togglePlatform = (platform: string) => {
    setOpenPlatforms(prev => ({ ...prev, [platform]: !prev[platform] }));
  };

  // Group and filter profiles
  const { groupedProfiles, totalCount, selectedCount } = useMemo(() => {
    if (!profilesData) return { groupedProfiles: {}, totalCount: 0, selectedCount: 0 };

    // Filter by search query
    const filtered = profilesData.filter((p: Profile) => {
      const query = searchQuery.toLowerCase();
      return (
        p.name.toLowerCase().includes(query) ||
        p.display_name.toLowerCase().includes(query) ||
        p.description?.toLowerCase().includes(query) ||
        p.platform.toLowerCase().includes(query)
      );
    });

    // Group by platform
    const grouped: Record<string, Profile[]> = {};
    filtered.forEach((profile: Profile) => {
      const platform = profile.platform.toLowerCase();
      if (!grouped[platform]) {
        grouped[platform] = [];
      }
      grouped[platform].push(profile);
    });

    // Sort profiles within each platform
    Object.keys(grouped).forEach(platform => {
      grouped[platform].sort((a, b) => a.display_name.localeCompare(b.display_name));
    });

    return {
      groupedProfiles: grouped,
      totalCount: profilesData.length,
      selectedCount: selectedProfiles.length,
    };
  }, [profilesData, searchQuery, selectedProfiles]);

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

      <Tabs value={activeTab} onValueChange={setActiveTab}>
        <TabsList className="grid w-full grid-cols-3">
          <TabsTrigger value="members">Members</TabsTrigger>
          <TabsTrigger value="profiles">Allowed Profiles</TabsTrigger>
          <TabsTrigger value="costs">Costs</TabsTrigger>
        </TabsList>

        <TabsContent value="members" className="space-y-4">
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
        </TabsContent>

        <TabsContent value="profiles" className="space-y-4">
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
              {/* Search and Quick Actions */}
              <div className="space-y-3">
                <div className="flex gap-2">
                  <div className="relative flex-1">
                    <Search className="absolute left-3 top-1/2 -translate-y-1/2 h-4 w-4 text-muted-foreground" />
                    <Input
                      placeholder="Search profiles by name, platform, or description..."
                      value={searchQuery}
                      onChange={(e) => setSearchQuery(e.target.value)}
                      className="pl-9"
                    />
                  </div>
                </div>
                <div className="flex gap-2">
                  <Button
                    variant="outline"
                    size="sm"
                    onClick={handleSelectAll}
                  >
                    Select All
                  </Button>
                  <Button
                    variant="outline"
                    size="sm"
                    onClick={handleDeselectAll}
                  >
                    Deselect All
                  </Button>
                  <div className="flex-1" />
                  <p className="text-sm text-muted-foreground flex items-center">
                    {selectedCount === 0
                      ? "No restrictions (all profiles allowed)"
                      : `${selectedCount} of ${totalCount} selected`}
                  </p>
                </div>
              </div>

              {/* Grouped Profiles by Platform */}
              <div className="space-y-3">
                {Object.keys(groupedProfiles).length === 0 ? (
                  <div className="text-center py-8 border rounded-lg">
                    <p className="text-muted-foreground">No profiles found matching "{searchQuery}"</p>
                  </div>
                ) : (
                  Object.entries(groupedProfiles).map(([platform, profiles]) => (
                    <Collapsible
                      key={platform}
                      open={openPlatforms[platform]}
                      onOpenChange={() => togglePlatform(platform)}
                    >
                      <div className="border rounded-lg">
                        <CollapsibleTrigger className="w-full">
                          <div className="flex items-center justify-between p-4 hover:bg-muted/50 transition-colors">
                            <div className="flex items-center gap-3">
                              {openPlatforms[platform] ? (
                                <ChevronDown className="h-4 w-4" />
                              ) : (
                                <ChevronRight className="h-4 w-4" />
                              )}
                              <h3 className="text-sm font-semibold uppercase">
                                {platform}
                              </h3>
                              <Badge variant="secondary">
                                {profiles.length} {profiles.length === 1 ? 'profile' : 'profiles'}
                              </Badge>
                            </div>
                            <div className="text-xs text-muted-foreground">
                              {profiles.filter(p => selectedProfiles.includes(p.name)).length} selected
                            </div>
                          </div>
                        </CollapsibleTrigger>
                        <CollapsibleContent>
                          <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-3 p-4 pt-0">
                            {profiles.map((profile: Profile) => (
                              <div key={profile.name} className="flex items-start space-x-2 border rounded-lg p-3 hover:bg-muted/50 transition-colors">
                                <Checkbox
                                  id={`profile-${profile.name}`}
                                  checked={selectedProfiles.includes(profile.name)}
                                  onCheckedChange={() => handleToggleProfile(profile.name)}
                                />
                                <div className="flex-1">
                                  <div className="flex items-start gap-2">
                                    <label
                                      htmlFor={`profile-${profile.name}`}
                                      className="text-sm font-medium leading-none cursor-pointer flex-1"
                                    >
                                      {profile.display_name}
                                    </label>
                                    <Button
                                      variant="ghost"
                                      size="sm"
                                      className="h-6 px-2 text-xs"
                                      onClick={(e) => {
                                        e.preventDefault();
                                        router.push(`/profiles/${profile.name}`);
                                      }}
                                    >
                                      View
                                    </Button>
                                  </div>
                                  <p className="text-xs text-muted-foreground mt-1 line-clamp-2">
                                    {profile.description}
                                  </p>
                                  {profile.track && (
                                    <div className="mt-2">
                                      <Badge variant="secondary" className="text-xs">
                                        {profile.track}
                                      </Badge>
                                    </div>
                                  )}
                                </div>
                              </div>
                            ))}
                          </div>
                        </CollapsibleContent>
                      </div>
                    </Collapsible>
                  ))
                )}
              </div>

              <div className="flex items-center justify-between pt-4 border-t">
                <p className="text-sm text-muted-foreground">
                  {selectedProfiles.length === 0
                    ? "All profiles allowed (no restrictions)"
                    : `${selectedProfiles.length} profile(s) will be allowed for this team`}
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
        </TabsContent>

        <TabsContent value="costs" className="space-y-6">
          {/* Summary Cards Row */}
          <div className="grid grid-cols-1 md:grid-cols-3 gap-6">
            {/* Current Month Card */}
            <Card>
              <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
                <CardTitle className="text-sm font-medium">Current Month</CardTitle>
                <Calendar className="h-4 w-4 text-muted-foreground" />
              </CardHeader>
              <CardContent>
                <div className="text-2xl font-bold">
                  {costsData ? formatCurrency(costsData.current_month.total_cost) : "-"}
                </div>
                <p className="text-xs text-muted-foreground">
                  {costsData?.current_month.start_date} to {costsData?.current_month.end_date}
                </p>
                <p className="text-xs text-muted-foreground mt-2">
                  Projected: {costsData ? formatCurrency(costsData.current_month.estimated_full_month || 0) : "-"}
                </p>
              </CardContent>
            </Card>

            {/* Last 30 Days Card */}
            <Card>
              <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
                <CardTitle className="text-sm font-medium">Last 30 Days</CardTitle>
                <TrendingUp className="h-4 w-4 text-muted-foreground" />
              </CardHeader>
              <CardContent>
                <div className="text-2xl font-bold">
                  {costsData ? formatCurrency(costsData.last_30_days.total_cost) : "-"}
                </div>
                <p className="text-xs text-muted-foreground">
                  {costsData?.last_30_days.start_date} to {costsData?.last_30_days.end_date}
                </p>
              </CardContent>
            </Card>

            {/* Average Daily Cost Card */}
            <Card>
              <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
                <CardTitle className="text-sm font-medium">Daily Average</CardTitle>
                <DollarSign className="h-4 w-4 text-muted-foreground" />
              </CardHeader>
              <CardContent>
                <div className="text-2xl font-bold">
                  {costsData ? formatCurrency(costsData.last_30_days.total_cost / 30) : "-"}
                </div>
                <p className="text-xs text-muted-foreground">
                  Based on last 30 days
                </p>
              </CardContent>
            </Card>
          </div>

          {/* Per-Cluster Breakdown Table */}
          <Card>
            <CardHeader>
              <CardTitle>Cluster Cost Breakdown</CardTitle>
              <CardDescription>
                Detailed costs for each cluster in this team
              </CardDescription>
            </CardHeader>
            <CardContent>
              {costsLoading ? (
                <p className="text-muted-foreground">Loading cost data...</p>
              ) : !costsData?.clusters || costsData.clusters.length === 0 ? (
                <div className="text-center py-8 border rounded-lg">
                  <p className="text-muted-foreground">No active clusters</p>
                </div>
              ) : (
                <div className="rounded-lg border overflow-x-auto">
                  <table className="w-full">
                    <thead>
                      <tr className="border-b bg-muted/50">
                        <th className="p-3 text-left text-sm font-medium">Cluster</th>
                        <th className="p-3 text-left text-sm font-medium">Profile</th>
                        <th className="p-3 text-left text-sm font-medium">Owner</th>
                        <th className="p-3 text-left text-sm font-medium">Status</th>
                        <th className="p-3 text-right text-sm font-medium">Current Month</th>
                        <th className="p-3 text-right text-sm font-medium">Last 30 Days</th>
                        <th className="p-3 text-right text-sm font-medium">Hourly Rate</th>
                      </tr>
                    </thead>
                    <tbody>
                      {costsData.clusters.map((cluster) => (
                        <tr key={cluster.id} className="border-b last:border-0 hover:bg-muted/50">
                          <td className="p-3 font-medium">{cluster.name}</td>
                          <td className="p-3 text-sm text-muted-foreground">{cluster.profile}</td>
                          <td className="p-3 text-sm text-muted-foreground">{cluster.owner}</td>
                          <td className="p-3">
                            <Badge variant={
                              cluster.status === "READY" ? "default" :
                              cluster.status === "HIBERNATED" ? "secondary" :
                              cluster.status === "FAILED" ? "destructive" : "outline"
                            }>
                              {cluster.status}
                            </Badge>
                          </td>
                          <td className="p-3 text-right font-medium">
                            {formatCurrency(cluster.current_month_cost)}
                            <p className="text-xs text-muted-foreground">
                              {Math.round(cluster.runtime_hours_current_month)}h runtime
                            </p>
                          </td>
                          <td className="p-3 text-right font-medium">
                            {formatCurrency(cluster.last_30_days_cost)}
                            <p className="text-xs text-muted-foreground">
                              {Math.round(cluster.runtime_hours_last_30_days)}h runtime
                            </p>
                          </td>
                          <td className="p-3 text-right text-sm text-muted-foreground">
                            {formatCurrency(cluster.estimated_hourly_cost)}/hr
                          </td>
                        </tr>
                      ))}
                    </tbody>
                    <tfoot>
                      <tr className="border-t bg-muted/30 font-semibold">
                        <td colSpan={4} className="p-3">Total</td>
                        <td className="p-3 text-right">
                          {formatCurrency(costsData.current_month.total_cost)}
                        </td>
                        <td className="p-3 text-right">
                          {formatCurrency(costsData.last_30_days.total_cost)}
                        </td>
                        <td className="p-3"></td>
                      </tr>
                    </tfoot>
                  </table>
                </div>
              )}
            </CardContent>
          </Card>
        </TabsContent>
      </Tabs>
    </div>
  );
}
