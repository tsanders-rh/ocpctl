"use client";

import { useState } from "react";
import { useRouter } from "next/navigation";
import { useForm } from "react-hook-form";
import { useCreateUser } from "@/lib/hooks/useUsers";
import { useTeams } from "@/lib/hooks/useTeams";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
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
import { UserRole, type CreateUserRequest } from "@/types/api";

export default function NewUserPage() {
  const router = useRouter();
  const createUser = useCreateUser();
  const { data: teamsData } = useTeams();
  const [selectedTeams, setSelectedTeams] = useState<string[]>([]);

  const {
    register,
    handleSubmit,
    setValue,
    watch,
    formState: { errors },
  } = useForm<CreateUserRequest>({
    defaultValues: {
      role: UserRole.USER,
      teams: [],
    },
  });

  const watchedRole = watch("role");

  const [errorMessage, setErrorMessage] = useState<string>("");

  const onSubmit = async (data: CreateUserRequest) => {
    try {
      setErrorMessage(""); // Clear previous errors

      // Validate that at least one team is selected
      if (selectedTeams.length === 0) {
        setErrorMessage("Users must belong to at least one team");
        return;
      }

      // Add teams to the request
      const requestData = { ...data, teams: selectedTeams };

      await createUser.mutateAsync(requestData);
      router.push("/admin/users");
    } catch (error) {
      console.error("Failed to create user:", error);
      if (error instanceof Error) {
        setErrorMessage(error.message);
      } else {
        setErrorMessage("Failed to create user. Please check all fields and try again.");
      }
    }
  };

  return (
    <div className="space-y-6 max-w-2xl">
      <div className="flex items-center gap-4">
        <Button variant="ghost" size="sm" onClick={() => router.back()}>
          <ArrowLeft className="mr-2 h-4 w-4" />
          Back
        </Button>
        <div>
          <h1 className="text-3xl font-bold">Create User</h1>
          <p className="text-muted-foreground">
            Add a new user to the system
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
              <Label htmlFor="email">Email *</Label>
              <Input
                id="email"
                type="email"
                placeholder="user@example.com"
                {...register("email", {
                  required: "Email is required",
                  pattern: {
                    value: /^[^\s@]+@[^\s@]+\.[^\s@]+$/,
                    message: "Invalid email address",
                  },
                })}
              />
              {errors.email && (
                <p className="text-sm text-red-600">{errors.email.message}</p>
              )}
            </div>

            <div className="space-y-2">
              <Label htmlFor="username">Display Name *</Label>
              <Input
                id="username"
                placeholder="John Doe"
                {...register("username", {
                  required: "Display name is required",
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
              <Label htmlFor="password">Password *</Label>
              <Input
                id="password"
                type="password"
                placeholder="Minimum 8 characters"
                {...register("password", {
                  required: "Password is required",
                  minLength: {
                    value: 8,
                    message: "Password must be at least 8 characters",
                  },
                })}
              />
              {errors.password && (
                <p className="text-sm text-red-600">{errors.password.message}</p>
              )}
            </div>

            <div className="space-y-2">
              <Label htmlFor="role">Role *</Label>
              <Select
                value={watchedRole}
                onValueChange={(value) => setValue("role", value as UserRole)}
              >
                <SelectTrigger>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value={UserRole.ADMIN}>Admin (Full access)</SelectItem>
                  <SelectItem value={UserRole.USER}>User (Standard access)</SelectItem>
                  <SelectItem value={UserRole.VIEWER}>Viewer (Read-only)</SelectItem>
                </SelectContent>
              </Select>
              <p className="text-sm text-muted-foreground">
                {watchedRole === UserRole.ADMIN && "Full system access including user management"}
                {watchedRole === UserRole.USER && "Can create and manage own clusters"}
                {watchedRole === UserRole.VIEWER && "Read-only access to own clusters"}
              </p>
            </div>

            <div className="space-y-2">
              <Label>Team Memberships *</Label>
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
                          onClick={() => setSelectedTeams(selectedTeams.filter((t) => t !== team))}
                          className="ml-1 hover:bg-muted rounded-full"
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
            disabled={createUser.isPending}
          >
            {createUser.isPending ? "Creating..." : "Create User"}
          </Button>
        </div>

        {errorMessage && (
          <div className="mt-4 text-sm text-red-600 bg-red-50 p-3 rounded-md">
            {errorMessage}
          </div>
        )}
      </form>
    </div>
  );
}
