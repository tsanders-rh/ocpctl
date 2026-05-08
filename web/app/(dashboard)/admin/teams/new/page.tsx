"use client";

import { useState } from "react";
import { useRouter } from "next/navigation";
import { useForm } from "react-hook-form";
import { useMutation } from "@tanstack/react-query";
import { adminApi } from "@/lib/api/endpoints/admin";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";
import { Card, CardContent, CardHeader, CardTitle, CardDescription } from "@/components/ui/card";
import { ArrowLeft, AlertCircle, CheckCircle } from "lucide-react";
import type { CreateTeamRequest } from "@/types/api";

export default function NewTeamPage() {
  const router = useRouter();
  const [successMessage, setSuccessMessage] = useState("");
  const [errorMessage, setErrorMessage] = useState("");

  const {
    register,
    handleSubmit,
    formState: { errors, isSubmitting },
  } = useForm<CreateTeamRequest>();

  const createMutation = useMutation({
    mutationFn: (data: CreateTeamRequest) => adminApi.createTeam(data),
    onSuccess: (team) => {
      setSuccessMessage(`Team "${team.name}" created successfully!`);
      setErrorMessage("");
      setTimeout(() => {
        router.push(`/admin/teams/${encodeURIComponent(team.name)}`);
      }, 1500);
    },
    onError: (error: any) => {
      setErrorMessage(error.message || "Failed to create team");
      setSuccessMessage("");
    },
  });

  const onSubmit = async (data: CreateTeamRequest) => {
    setSuccessMessage("");
    setErrorMessage("");
    createMutation.mutate(data);
  };

  return (
    <div className="space-y-6 max-w-2xl">
      <div className="flex items-center gap-4">
        <Button variant="ghost" size="sm" onClick={() => router.back()}>
          <ArrowLeft className="mr-2 h-4 w-4" />
          Back
        </Button>
        <div>
          <h1 className="text-3xl font-bold">Create Team</h1>
          <p className="text-muted-foreground">
            Create a new team for organizing clusters
          </p>
        </div>
      </div>

      <form onSubmit={handleSubmit(onSubmit)}>
        <Card>
          <CardHeader>
            <CardTitle>Team Details</CardTitle>
            <CardDescription>
              Teams help organize clusters and control access
            </CardDescription>
          </CardHeader>
          <CardContent className="space-y-4">
            <div className="space-y-2">
              <Label htmlFor="name">Team Name *</Label>
              <Input
                id="name"
                placeholder="engineering"
                {...register("name", {
                  required: "Team name is required",
                  pattern: {
                    value: /^[a-z0-9-]+$/,
                    message: "Team name must be lowercase letters, numbers, and hyphens only",
                  },
                  minLength: {
                    value: 2,
                    message: "Team name must be at least 2 characters",
                  },
                  maxLength: {
                    value: 64,
                    message: "Team name must not exceed 64 characters",
                  },
                })}
              />
              {errors.name && (
                <p className="text-sm text-red-600">{errors.name.message}</p>
              )}
              <p className="text-sm text-muted-foreground">
                Use lowercase letters, numbers, and hyphens (e.g., "engineering", "data-science")
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
              <p className="text-sm text-muted-foreground">
                Optional description to help identify the team's purpose
              </p>
            </div>

            {/* Status Messages */}
            {successMessage && (
              <div className="bg-green-50 border border-green-200 rounded-md p-3 flex items-center gap-2">
                <CheckCircle className="h-5 w-5 text-green-600" />
                <p className="text-sm text-green-800">{successMessage}</p>
              </div>
            )}

            {errorMessage && (
              <div className="bg-red-50 border border-red-200 rounded-md p-3 flex items-center gap-2">
                <AlertCircle className="h-5 w-5 text-red-600" />
                <p className="text-sm text-red-800">{errorMessage}</p>
              </div>
            )}
          </CardContent>
        </Card>

        <div className="flex gap-4 mt-6">
          <Button
            type="button"
            variant="outline"
            onClick={() => router.back()}
            disabled={isSubmitting}
          >
            Cancel
          </Button>
          <Button
            type="submit"
            disabled={isSubmitting || createMutation.isPending}
          >
            {isSubmitting || createMutation.isPending ? "Creating..." : "Create Team"}
          </Button>
        </div>
      </form>
    </div>
  );
}
