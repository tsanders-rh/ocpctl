"use client";

import { useParams, useRouter } from "next/navigation";
import { useForm } from "react-hook-form";
import { useUser, useUpdateUser } from "@/lib/hooks/useUsers";
import { useTeams } from "@/lib/hooks/useTeams";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Checkbox } from "@/components/ui/checkbox";
import { Badge } from "@/components/ui/badge";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { ArrowLeft, X } from "lucide-react";
import { UserRole, type UpdateUserRequest } from "@/types/api";
import { useEffect, useState } from "react";

export default function EditUserPage() {
  const params = useParams();
  const router = useRouter();
  const userId = params.id as string;
  const { data: user, isLoading } = useUser(userId);
  const { data: teamsData } = useTeams();
  const updateUser = useUpdateUser();
  const [selectedTeams, setSelectedTeams] = useState<string[]>([]);

  const {
    register,
    handleSubmit,
    setValue,
    watch,
    reset,
    formState: { errors },
  } = useForm<UpdateUserRequest & { confirm_password?: string }>();

  const watchedRole = watch("role");
  const watchedActive = watch("active");
  const watchedPassword = watch("new_password");
  const watchedConfirmPassword = watch("confirm_password");

  // Populate form when user data loads
  useEffect(() => {
    if (user) {
      reset({
        username: user.username,
        role: user.role,
        active: user.active,
      });
      setSelectedTeams(user.teams || []);
    }
  }, [user, reset]);

  const onSubmit = async (data: UpdateUserRequest & { confirm_password?: string }) => {
    try {
      // Validate that user belongs to at least one team
      if (selectedTeams.length === 0) {
        alert("Users must belong to at least one team");
        return;
      }

      // Validate password confirmation if password is being changed
      if (data.new_password) {
        if (data.new_password !== data.confirm_password) {
          alert("Passwords do not match");
          return;
        }
        if (data.new_password.length < 8) {
          alert("Password must be at least 8 characters");
          return;
        }
      }

      // Remove confirm_password before sending to API
      const { confirm_password, ...updateData } = data;

      // Only send fields that have values
      const cleanData: UpdateUserRequest = {};
      if (updateData.username) cleanData.username = updateData.username;
      if (updateData.role) cleanData.role = updateData.role;
      if (updateData.active !== undefined) cleanData.active = updateData.active;
      if (updateData.new_password) cleanData.new_password = updateData.new_password;
      cleanData.teams = selectedTeams; // Always include teams

      await updateUser.mutateAsync({ id: userId, data: cleanData });
      router.push("/admin/users");
    } catch (error) {
      console.error("Failed to update user:", error);
    }
  };

  if (isLoading) {
    return <div>Loading user...</div>;
  }

  if (!user) {
    return <div>User not found</div>;
  }

  return (
    <div className="space-y-6 max-w-2xl">
      <div className="flex items-center gap-4">
        <Button variant="ghost" size="sm" onClick={() => router.back()}>
          <ArrowLeft className="mr-2 h-4 w-4" />
          Back
        </Button>
        <div>
          <h1 className="text-3xl font-bold">Edit User</h1>
          <p className="text-muted-foreground">
            Update user details and permissions
          </p>
        </div>
      </div>

      <form onSubmit={handleSubmit(onSubmit)}>
        <Card>
          <CardHeader>
            <CardTitle>User Details</CardTitle>
          </CardHeader>
          <CardContent className="space-y-4">
            <div className="space-y-2">
              <Label>Email</Label>
              <Input
                value={user.email}
                disabled
                className="bg-muted"
              />
              <p className="text-sm text-muted-foreground">
                Email cannot be changed
              </p>
            </div>

            <div className="space-y-2">
              <Label htmlFor="username">Display Name</Label>
              <Input
                id="username"
                placeholder="John Doe"
                {...register("username", {
                  minLength: {
                    value: 2,
                    message: "Display name must be at least 2 characters",
                  },
                })}
              />
              <p className="text-sm text-muted-foreground">
                Friendly name shown in the UI (e.g., &quot;John Smith&quot;)
              </p>
              {errors.username && (
                <p className="text-sm text-red-600">{errors.username.message}</p>
              )}
            </div>

            <div className="space-y-2">
              <Label htmlFor="role">Role</Label>
              <Select
                value={watchedRole || user.role}
                onValueChange={(value) => setValue("role", value as UserRole)}
              >
                <SelectTrigger>
                  <SelectValue placeholder="Select a role" />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="ADMIN">Admin (Full access)</SelectItem>
                  <SelectItem value="TEAM_ADMIN">Team Admin (Team management)</SelectItem>
                  <SelectItem value="USER">User (Standard access)</SelectItem>
                  <SelectItem value="VIEWER">Viewer (Read-only)</SelectItem>
                </SelectContent>
              </Select>
              <p className="text-sm text-muted-foreground">
                {(watchedRole === UserRole.ADMIN || user.role === UserRole.ADMIN) && "Full system access including user management"}
                {(watchedRole === UserRole.TEAM_ADMIN || user.role === UserRole.TEAM_ADMIN) && "Can manage own clusters and assigned team clusters"}
                {(watchedRole === UserRole.USER || user.role === UserRole.USER) && "Can create and manage own clusters"}
                {(watchedRole === UserRole.VIEWER || user.role === UserRole.VIEWER) && "Read-only access to own clusters"}
              </p>
            </div>

            <div className="space-y-2">
              <Label>Team Memberships</Label>
              <div className="space-y-2">
                <Select
                  value=""
                  onValueChange={(value) => {
                    if (value && !selectedTeams.includes(value)) {
                      setSelectedTeams([...selectedTeams, value]);
                    }
                  }}
                >
                  <SelectTrigger>
                    <SelectValue placeholder="Add team..." />
                  </SelectTrigger>
                  <SelectContent>
                    {teamsData?.teams && teamsData.teams.length > 0 ? (
                      teamsData.teams
                        .filter((team) => !selectedTeams.includes(team.name))
                        .map((team) => (
                          <SelectItem key={team.id} value={team.name}>
                            {team.name}
                          </SelectItem>
                        ))
                    ) : (
                      <div className="p-2 text-sm text-muted-foreground">
                        No teams available
                      </div>
                    )}
                  </SelectContent>
                </Select>
                {selectedTeams.length > 0 && (
                  <div className="flex flex-wrap gap-2 mt-2">
                    {selectedTeams.map((team) => (
                      <Badge key={team} variant="secondary" className="gap-1">
                        {team}
                        <button
                          type="button"
                          onClick={() => {
                            if (selectedTeams.length === 1) {
                              alert("Cannot remove the last team. Users must belong to at least one team.");
                              return;
                            }
                            setSelectedTeams(selectedTeams.filter((t) => t !== team));
                          }}
                          className="ml-1 hover:bg-muted rounded-full"
                          title={selectedTeams.length === 1 ? "Cannot remove the last team" : "Remove team"}
                        >
                          <X className="h-3 w-3" />
                        </button>
                      </Badge>
                    ))}
                  </div>
                )}
                <p className="text-sm text-muted-foreground">
                  Teams this user belongs to (for team-based access control)
                  {selectedTeams.length === 0 && (
                    <span className="text-amber-600 font-medium"> - At least one team is required</span>
                  )}
                </p>
              </div>
            </div>

            <div className="flex items-center space-x-2">
              <Checkbox
                id="active"
                checked={watchedActive}
                onCheckedChange={(checked) =>
                  setValue("active", checked as boolean)
                }
              />
              <Label htmlFor="active" className="cursor-pointer">
                Active (User can log in)
              </Label>
            </div>
          </CardContent>
        </Card>

        <Card className="mt-6">
          <CardHeader>
            <CardTitle>Change Password</CardTitle>
          </CardHeader>
          <CardContent className="space-y-4">
            <p className="text-sm text-muted-foreground">
              Leave blank to keep current password
            </p>

            <div className="space-y-2">
              <Label htmlFor="new_password">New Password</Label>
              <Input
                id="new_password"
                type="password"
                placeholder="••••••••"
                {...register("new_password", {
                  minLength: {
                    value: 8,
                    message: "Password must be at least 8 characters",
                  },
                })}
              />
              {errors.new_password && (
                <p className="text-sm text-red-600">{errors.new_password.message}</p>
              )}
            </div>

            <div className="space-y-2">
              <Label htmlFor="confirm_password">Confirm New Password</Label>
              <Input
                id="confirm_password"
                type="password"
                placeholder="••••••••"
                {...register("confirm_password")}
              />
              {watchedPassword && watchedConfirmPassword && watchedPassword !== watchedConfirmPassword && (
                <p className="text-sm text-red-600">Passwords do not match</p>
              )}
            </div>
          </CardContent>
        </Card>

        <Card className="mt-6">
          <CardHeader>
            <CardTitle>Account Information</CardTitle>
          </CardHeader>
          <CardContent className="space-y-2 text-sm">
            <div>
              <span className="font-medium">User ID:</span> {user.id}
            </div>
            <div>
              <span className="font-medium">Created:</span> {new Date(user.created_at).toLocaleString()}
            </div>
            <div>
              <span className="font-medium">Last Updated:</span> {new Date(user.updated_at).toLocaleString()}
            </div>
          </CardContent>
        </Card>

        <div className="flex gap-4 mt-6">
          <Button
            type="button"
            variant="outline"
            onClick={() => router.back()}
          >
            Cancel
          </Button>
          <Button
            type="submit"
            disabled={updateUser.isPending}
          >
            {updateUser.isPending ? "Saving..." : "Save Changes"}
          </Button>
        </div>

        {updateUser.isError && (
          <div className="mt-4 text-sm text-red-600 bg-red-50 p-3 rounded-md">
            Failed to update user. Please try again.
          </div>
        )}
      </form>
    </div>
  );
}
