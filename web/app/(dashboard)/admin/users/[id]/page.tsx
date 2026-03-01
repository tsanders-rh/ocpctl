"use client";

import { useParams, useRouter } from "next/navigation";
import { useForm } from "react-hook-form";
import { useUser, useUpdateUser } from "@/lib/hooks/useUsers";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Checkbox } from "@/components/ui/checkbox";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { ArrowLeft } from "lucide-react";
import { UserRole, type UpdateUserRequest } from "@/types/api";
import { useEffect } from "react";

export default function EditUserPage() {
  const params = useParams();
  const router = useRouter();
  const userId = params.id as string;
  const { data: user, isLoading } = useUser(userId);
  const updateUser = useUpdateUser();

  const {
    register,
    handleSubmit,
    setValue,
    watch,
    reset,
    formState: { errors },
  } = useForm<UpdateUserRequest>();

  const watchedRole = watch("role");
  const watchedActive = watch("active");

  // Populate form when user data loads
  useEffect(() => {
    if (user) {
      reset({
        username: user.username,
        role: user.role,
        active: user.active,
      });
    }
  }, [user, reset]);

  const onSubmit = async (data: UpdateUserRequest) => {
    try {
      await updateUser.mutateAsync({ id: userId, data });
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
              <Label htmlFor="username">Username</Label>
              <Input
                id="username"
                placeholder="John Doe"
                {...register("username", {
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
              <Label htmlFor="role">Role</Label>
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
