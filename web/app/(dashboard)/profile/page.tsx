"use client";

import { useState } from "react";
import { useRouter } from "next/navigation";
import { useForm } from "react-hook-form";
import { useAuthStore } from "@/lib/stores/authStore";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { authApi } from "@/lib/api/endpoints/auth";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Switch } from "@/components/ui/switch";
import { Checkbox } from "@/components/ui/checkbox";
import { User, Save, AlertCircle, CheckCircle, Clock, Lock } from "lucide-react";
import type { UpdateMeRequest, ChangePasswordRequest } from "@/types/api";
import { APIKeyManager } from "@/components/profile/APIKeyManager";

// Common timezones list
const TIMEZONES = [
  { value: "UTC", label: "UTC" },
  { value: "America/New_York", label: "Eastern Time (ET)" },
  { value: "America/Chicago", label: "Central Time (CT)" },
  { value: "America/Denver", label: "Mountain Time (MT)" },
  { value: "America/Los_Angeles", label: "Pacific Time (PT)" },
  { value: "America/Anchorage", label: "Alaska Time (AKT)" },
  { value: "Pacific/Honolulu", label: "Hawaii Time (HT)" },
  { value: "Europe/London", label: "London (GMT/BST)" },
  { value: "Europe/Paris", label: "Paris (CET/CEST)" },
  { value: "Europe/Berlin", label: "Berlin (CET/CEST)" },
  { value: "Europe/Moscow", label: "Moscow (MSK)" },
  { value: "Asia/Dubai", label: "Dubai (GST)" },
  { value: "Asia/Kolkata", label: "India (IST)" },
  { value: "Asia/Singapore", label: "Singapore (SGT)" },
  { value: "Asia/Tokyo", label: "Tokyo (JST)" },
  { value: "Asia/Shanghai", label: "Beijing/Shanghai (CST)" },
  { value: "Australia/Sydney", label: "Sydney (AEDT/AEST)" },
  { value: "Pacific/Auckland", label: "Auckland (NZDT/NZST)" },
];

const DAYS_OF_WEEK = [
  "Sunday",
  "Monday",
  "Tuesday",
  "Wednesday",
  "Thursday",
  "Friday",
  "Saturday",
];

export default function ProfilePage() {
  const router = useRouter();
  const { user, setUser } = useAuthStore();
  const queryClient = useQueryClient();
  const [successMessage, setSuccessMessage] = useState("");
  const [errorMessage, setErrorMessage] = useState("");

  const {
    register,
    handleSubmit,
    watch,
    setValue,
    formState: { errors, isDirty },
  } = useForm({
    defaultValues: {
      username: user?.username || "",
      timezone: user?.timezone || "UTC",
      work_hours_enabled: user?.work_hours_enabled || false,
      work_hours_start: user?.work_hours?.start_time || "09:00",
      work_hours_end: user?.work_hours?.end_time || "17:00",
      work_days: user?.work_hours?.work_days || ["Monday", "Tuesday", "Wednesday", "Thursday", "Friday"],
    },
  });

  const watchedValues = watch();

  const updateProfile = useMutation({
    mutationFn: (data: UpdateMeRequest) => authApi.updateMe(data),
    onSuccess: (updatedUser) => {
      setUser(updatedUser);
      queryClient.invalidateQueries({ queryKey: ["user"] });
      setSuccessMessage("Profile updated successfully!");
      setErrorMessage("");
      setTimeout(() => setSuccessMessage(""), 3000);
    },
    onError: (error: any) => {
      setErrorMessage(error.message || "Failed to update profile");
      setSuccessMessage("");
    },
  });

  const onSubmit = async (data: any) => {
    setSuccessMessage("");
    setErrorMessage("");

    const updates: UpdateMeRequest = {};
    if (data.username !== user?.username) {
      updates.username = data.username;
    }
    if (data.timezone !== user?.timezone) {
      updates.timezone = data.timezone;
    }
    if (data.work_hours_enabled !== user?.work_hours_enabled) {
      updates.work_hours_enabled = data.work_hours_enabled;
    }

    // Include work hours if enabled and changed
    if (data.work_hours_enabled) {
      const currentWorkHours = user?.work_hours;
      const hasWorkHoursChanged =
        data.work_hours_start !== currentWorkHours?.start_time ||
        data.work_hours_end !== currentWorkHours?.end_time ||
        JSON.stringify(data.work_days.sort()) !== JSON.stringify(currentWorkHours?.work_days?.sort() || []);

      if (hasWorkHoursChanged || data.work_hours_enabled !== user?.work_hours_enabled) {
        updates.work_hours = {
          start_time: data.work_hours_start,
          end_time: data.work_hours_end,
          work_days: data.work_days,
        };
      }
    }

    if (Object.keys(updates).length === 0) {
      setErrorMessage("No changes to save");
      return;
    }

    updateProfile.mutate(updates);
  };

  if (!user) {
    return (
      <div className="flex h-full items-center justify-center">
        <div className="text-lg">Loading...</div>
      </div>
    );
  }

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-3xl font-bold">Profile Settings</h1>
        <p className="text-muted-foreground">
          Manage your personal settings and preferences
        </p>
      </div>

      <div className="grid grid-cols-1 lg:grid-cols-3 gap-6">
        {/* Left column - Profile Form */}
        <div className="lg:col-span-2">
          <Card>
            <CardHeader>
              <CardTitle className="flex items-center gap-2">
                <User className="h-5 w-5" />
                Profile Information
              </CardTitle>
            </CardHeader>
            <CardContent>
              <form onSubmit={handleSubmit(onSubmit)} className="space-y-6">
                {/* Email (read-only) */}
                <div className="space-y-2">
                  <Label htmlFor="email">Email</Label>
                  <Input
                    id="email"
                    type="email"
                    value={user.email}
                    disabled
                    className="bg-muted"
                  />
                  <p className="text-sm text-muted-foreground">
                    Email cannot be changed
                  </p>
                </div>

                {/* Username */}
                <div className="space-y-2">
                  <Label htmlFor="username">Display Name</Label>
                  <Input
                    id="username"
                    {...register("username", {
                      required: "Display name is required",
                      minLength: {
                        value: 2,
                        message: "Display name must be at least 2 characters",
                      },
                    })}
                  />
                  {errors.username && (
                    <p className="text-sm text-red-600">
                      {errors.username.message}
                    </p>
                  )}
                </div>

                {/* Timezone */}
                <div className="space-y-2">
                  <Label htmlFor="timezone">Timezone</Label>
                  <Select
                    value={watchedValues.timezone}
                    onValueChange={(value) => setValue("timezone", value, { shouldDirty: true })}
                  >
                    <SelectTrigger>
                      <SelectValue placeholder="Select timezone" />
                    </SelectTrigger>
                    <SelectContent>
                      {TIMEZONES.map((tz) => (
                        <SelectItem key={tz.value} value={tz.value}>
                          {tz.label}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                  <p className="text-sm text-muted-foreground">
                    Used for displaying times and work hours enforcement
                  </p>
                </div>

                {/* Work Hours */}
                <div className="space-y-4 pt-4 border-t">
                  <div className="flex items-center justify-between">
                    <div className="space-y-0.5">
                      <Label htmlFor="work_hours_enabled" className="flex items-center gap-2">
                        <Clock className="h-4 w-4" />
                        Automatic Cluster Hibernation
                      </Label>
                      <p className="text-sm text-muted-foreground">
                        Automatically hibernate clusters outside of work hours to save costs
                      </p>
                    </div>
                    <Switch
                      id="work_hours_enabled"
                      checked={watchedValues.work_hours_enabled}
                      onCheckedChange={(checked) => setValue("work_hours_enabled", checked, { shouldDirty: true })}
                    />
                  </div>

                  {watchedValues.work_hours_enabled && (
                    <div className="space-y-4 pl-6 border-l-2 border-muted">
                      {/* Time Range */}
                      <div className="grid grid-cols-2 gap-4">
                        <div className="space-y-2">
                          <Label htmlFor="work_hours_start">Start Time</Label>
                          <Input
                            id="work_hours_start"
                            type="time"
                            {...register("work_hours_start")}
                            onChange={(e) => setValue("work_hours_start", e.target.value, { shouldDirty: true })}
                          />
                        </div>
                        <div className="space-y-2">
                          <Label htmlFor="work_hours_end">End Time</Label>
                          <Input
                            id="work_hours_end"
                            type="time"
                            {...register("work_hours_end")}
                            onChange={(e) => setValue("work_hours_end", e.target.value, { shouldDirty: true })}
                          />
                        </div>
                      </div>

                      {/* Days Selector */}
                      <div className="space-y-2">
                        <Label>Work Days</Label>
                        <div className="grid grid-cols-4 sm:grid-cols-7 gap-2">
                          {DAYS_OF_WEEK.map((day) => (
                            <div key={day} className="flex items-center space-x-2">
                              <Checkbox
                                id={`day-${day}`}
                                checked={watchedValues.work_days?.includes(day)}
                                onCheckedChange={(checked) => {
                                  const currentDays = watchedValues.work_days || [];
                                  const newDays = checked
                                    ? [...currentDays, day]
                                    : currentDays.filter((d) => d !== day);
                                  setValue("work_days", newDays, { shouldDirty: true });
                                }}
                              />
                              <Label
                                htmlFor={`day-${day}`}
                                className="text-sm font-normal cursor-pointer"
                              >
                                {day.substring(0, 3)}
                              </Label>
                            </div>
                          ))}
                        </div>
                        <p className="text-sm text-muted-foreground">
                          Clusters will be hibernated outside these hours and days
                        </p>
                      </div>

                      {/* Schedule Preview */}
                      <div className="bg-muted/50 rounded-md p-3 text-sm">
                        <p className="font-medium mb-1">Schedule Preview:</p>
                        <p className="text-muted-foreground">
                          Clusters will be active from{" "}
                          <span className="font-medium text-foreground">
                            {watchedValues.work_hours_start}
                          </span>{" "}
                          to{" "}
                          <span className="font-medium text-foreground">
                            {watchedValues.work_hours_end}
                          </span>{" "}
                          on{" "}
                          <span className="font-medium text-foreground">
                            {watchedValues.work_days?.length === 7
                              ? "all days"
                              : watchedValues.work_days?.join(", ")}
                          </span>
                        </p>
                      </div>
                    </div>
                  )}
                </div>

                {/* Role (read-only) */}
                <div className="space-y-2">
                  <Label htmlFor="role">Role</Label>
                  <Input
                    id="role"
                    value={user.role}
                    disabled
                    className="bg-muted"
                  />
                  <p className="text-sm text-muted-foreground">
                    Contact an administrator to change your role
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

                {/* Action Buttons */}
                <div className="flex gap-4 pt-4">
                  <Button
                    type="button"
                    variant="outline"
                    onClick={() => router.back()}
                  >
                    Cancel
                  </Button>
                  <Button
                    type="submit"
                    disabled={!isDirty || updateProfile.isPending}
                  >
                    <Save className="mr-2 h-4 w-4" />
                    {updateProfile.isPending ? "Saving..." : "Save Changes"}
                  </Button>
                </div>
              </form>
            </CardContent>
          </Card>
        </div>

        {/* Right column - Account Info */}
        <div className="space-y-6">
          <Card>
            <CardHeader>
              <CardTitle>Account Information</CardTitle>
            </CardHeader>
            <CardContent className="space-y-4 text-sm">
              <div>
                <p className="text-muted-foreground">Account ID</p>
                <p className="font-mono text-xs mt-1">{user.id}</p>
              </div>
              <div>
                <p className="text-muted-foreground">Member Since</p>
                <p className="mt-1">
                  {new Date(user.created_at).toLocaleDateString()}
                </p>
              </div>
              <div>
                <p className="text-muted-foreground">Last Updated</p>
                <p className="mt-1">
                  {new Date(user.updated_at).toLocaleDateString()}
                </p>
              </div>
              <div>
                <p className="text-muted-foreground">Status</p>
                <p className="mt-1">
                  {user.active ? (
                    <span className="text-green-600 font-medium">Active</span>
                  ) : (
                    <span className="text-red-600 font-medium">Inactive</span>
                  )}
                </p>
              </div>
            </CardContent>
          </Card>
        </div>
      </div>

      {/* Password Change Section */}
      <Card>
        <CardHeader>
          <CardTitle className="flex items-center gap-2">
            <Lock className="h-5 w-5" />
            Change Password
          </CardTitle>
        </CardHeader>
        <CardContent>
          <PasswordChangeForm />
        </CardContent>
      </Card>

      {/* API Keys Section */}
      <Card>
        <CardContent className="pt-6">
          <APIKeyManager />
        </CardContent>
      </Card>
    </div>
  );
}

// Password Change Component
function PasswordChangeForm() {
  const router = useRouter();
  const [successMessage, setSuccessMessage] = useState("");
  const [errorMessage, setErrorMessage] = useState("");

  const {
    register,
    handleSubmit,
    reset,
    formState: { errors },
  } = useForm<ChangePasswordRequest>({
    defaultValues: {
      current_password: "",
      new_password: "",
    },
  });

  const changePassword = useMutation({
    mutationFn: (data: ChangePasswordRequest) => authApi.changePassword(data),
    onSuccess: () => {
      setSuccessMessage("Password changed successfully! Redirecting to login...");
      setErrorMessage("");
      reset();
      // Redirect to login after 2 seconds
      setTimeout(() => {
        router.push("/login");
      }, 2000);
    },
    onError: (error: any) => {
      setErrorMessage(error.message || "Failed to change password");
      setSuccessMessage("");
    },
  });

  const onSubmit = async (data: ChangePasswordRequest) => {
    setSuccessMessage("");
    setErrorMessage("");
    changePassword.mutate(data);
  };

  return (
    <form onSubmit={handleSubmit(onSubmit)} className="space-y-6 max-w-md">
      {/* Current Password */}
      <div className="space-y-2">
        <Label htmlFor="current_password">Current Password</Label>
        <Input
          id="current_password"
          type="password"
          autoComplete="current-password"
          {...register("current_password", {
            required: "Current password is required",
          })}
        />
        {errors.current_password && (
          <p className="text-sm text-red-600">
            {errors.current_password.message}
          </p>
        )}
      </div>

      {/* New Password */}
      <div className="space-y-2">
        <Label htmlFor="new_password">New Password</Label>
        <Input
          id="new_password"
          type="password"
          autoComplete="new-password"
          {...register("new_password", {
            required: "New password is required",
            minLength: {
              value: 8,
              message: "Password must be at least 8 characters",
            },
          })}
        />
        {errors.new_password && (
          <p className="text-sm text-red-600">
            {errors.new_password.message}
          </p>
        )}
        <p className="text-sm text-muted-foreground">
          Password must be at least 8 characters long
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

      {/* Submit Button */}
      <Button
        type="submit"
        disabled={changePassword.isPending}
      >
        <Lock className="mr-2 h-4 w-4" />
        {changePassword.isPending ? "Changing Password..." : "Change Password"}
      </Button>

      <div className="bg-yellow-50 border border-yellow-200 rounded-md p-3">
        <p className="text-sm text-yellow-800">
          <strong>Note:</strong> Changing your password will log you out of all sessions. You'll need to log in again.
        </p>
      </div>
    </form>
  );
}
