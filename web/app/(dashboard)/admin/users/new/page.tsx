"use client";

import { useRouter } from "next/navigation";
import { useForm } from "react-hook-form";
import { useCreateUser } from "@/lib/hooks/useUsers";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { ArrowLeft } from "lucide-react";
import { UserRole, type CreateUserRequest } from "@/types/api";

export default function NewUserPage() {
  const router = useRouter();
  const createUser = useCreateUser();
  const {
    register,
    handleSubmit,
    setValue,
    watch,
    formState: { errors },
  } = useForm<CreateUserRequest>({
    defaultValues: {
      role: UserRole.USER,
    },
  });

  const watchedRole = watch("role");

  const onSubmit = async (data: CreateUserRequest) => {
    try {
      await createUser.mutateAsync(data);
      router.push("/admin/users");
    } catch (error) {
      console.error("Failed to create user:", error);
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
              <Label htmlFor="username">Username *</Label>
              <Input
                id="username"
                placeholder="John Doe"
                {...register("username", {
                  required: "Username is required",
                  minLength: {
                    value: 2,
                    message: "Username must be at least 2 characters",
                  },
                })}
              />
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

        {createUser.isError && (
          <div className="mt-4 text-sm text-red-600 bg-red-50 p-3 rounded-md">
            Failed to create user. Please check all fields and try again.
          </div>
        )}
      </form>
    </div>
  );
}
